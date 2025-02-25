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

	start := time.Now()

	done := make(chan struct {
		output []byte
		err    error
	})

	go func() {
		output, err := ffmpeg.Output()
		done <- struct {
			output []byte
			err    error
		}{output, err}
	}()

	select {
	case <-l.canceled:
		l.logger.Debugf("load for %s canceled", job.VideoID)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadCanceled,
			VideoID: &job.VideoID,
		}
		log.Tracef("sent load canceled event for %s", job.VideoID)
		ffmpeg.Process.Kill()
		return
	case result := <-done:
		if result.err != nil {
			log.Errorf("error loading %s: %v", job.VideoID, result.err)
			sentry.CaptureException(result.err)
			l.Notifications <- PlaybackNotification{
				Event:   PlaybackLoadError,
				VideoID: &job.VideoID,
				Error:   &result.err,
			}
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
		ffmpeg.Process.Kill()
		return
	case <-time.After(30 * time.Second):
		error := errors.New("ffmpeg timed out after 30 seconds")
		log.Errorf("ffmpeg timed out after 30 seconds for %s", job.VideoID)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &error,
		}
		ffmpeg.Process.Kill()
		return
	}
}

func (l *Loader) Cancel() {
	l.canceled <- true
}
