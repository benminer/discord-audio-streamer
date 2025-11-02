package audio

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

type Loader struct {
	mutex         sync.Mutex
	completed     chan bool
	Notifications chan PlaybackNotification
	canceled      chan bool
	logger        *log.Entry
}

type LoadJob struct {
	URL     string
	VideoID string
	Title   string
}

type LoadResult struct {
	ffmpegOut io.ReadCloser
	VideoID   string
	Title     string
	Error     *error
	Duration  time.Duration
}

func NewLoader() *Loader {
	return &Loader{
		Notifications: make(chan PlaybackNotification, 100),
		canceled:      make(chan bool),
		completed:     make(chan bool),
		logger: log.WithFields(log.Fields{
			"module": "audio-loader",
		}),
	}
}

func (l *Loader) Load(job LoadJob) {
	l.logger.Debugf("starting load for %s", job.VideoID)
	l.mutex.Lock()
	defer func() {
		l.mutex.Unlock()
		l.completed <- true
	}()

	l.Notifications <- PlaybackNotification{
		Event:   PlaybackLoading,
		VideoID: &job.VideoID,
	}

	ffmpeg := exec.Command("ffmpeg",
		"-i", job.URL,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-af", "aresample=48000",
		"-loglevel", "error",
		"pipe:1")

	var stderr bytes.Buffer
	ffmpeg.Stderr = &stderr

	start := time.Now()

	done := make(chan struct {
		output []byte
		stderr string
		err    error
	})

	go func() {
		output, err := ffmpeg.Output()
		done <- struct {
			output []byte
			stderr string
			err    error
		}{output, stderr.String(), err}
	}()

	select {
	case <-l.canceled:
		l.logger.Debugf("load for %s canceled", job.VideoID)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadCanceled,
			VideoID: &job.VideoID,
		}
		log.Tracef("sent load canceled event for %s", job.VideoID)
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
		}
		return
	case result := <-done:
		if result.err != nil {
			// Build detailed error message with stderr output
			errMsg := result.err.Error()
			if result.stderr != "" {
				errMsg += " | ffmpeg stderr: " + result.stderr
			}
			detailedErr := errors.New(errMsg)

			log.Errorf("error loading %s: %v", job.VideoID, detailedErr)
			sentry.CaptureException(detailedErr)
			l.Notifications <- PlaybackNotification{
				Event:   PlaybackLoadError,
				VideoID: &job.VideoID,
				Error:   &detailedErr,
			}
			if ffmpeg.Process != nil {
				ffmpeg.Process.Kill()
			}
			return
		}
		log.Tracef("loaded %s", job.VideoID)
		output := io.NopCloser(bytes.NewReader(result.output))
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoaded,
			VideoID: &job.VideoID,
			LoadResult: &LoadResult{
				ffmpegOut: output,
				VideoID:   job.VideoID,
				Title:     job.Title,
				Duration:  time.Since(start),
			},
		}
		log.Tracef("sent loaded event for %s", job.VideoID)
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
		}
		return
	case <-time.After(30 * time.Second):
		error := errors.New("ffmpeg timed out after 30 seconds")
		log.Errorf("ffmpeg timed out after 30 seconds for %s", job.VideoID)
		sentry.CaptureException(error)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &error,
		}
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
		}
		return
	}
}

func (l *Loader) Cancel() {
	l.canceled <- true
}
