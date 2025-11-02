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

	// IMPORTANT: FFmpeg Streaming vs Memory Buffering Trade-offs
	//
	// CURRENT APPROACH: Streaming from FFmpeg stdout pipe
	// - Low memory footprint (~4KB buffers instead of 55+ MB for 5min song)
	// - No GC pressure/pauses (was causing audio stutters)
	// - Faster startup (no waiting for entire file to download)
	// - Requires io.ReadFull() in player.go to ensure complete frames
	//
	// ALTERNATIVE APPROACH: Buffer entire file to memory (previous implementation)
	// - Use: output, err := ffmpeg.Output() instead of StdoutPipe()
	// - Then: io.NopCloser(bytes.NewReader(result.output))
	// - Pros: More reliable, simpler code, no partial read issues
	// - Cons: Huge memory allocations (55MB+ per song), GC pauses causing stutters
	//
	// If reverting to buffered approach:
	// 1. Replace StdoutPipe() with Output() call
	// 2. Remove Start() call and use Output() blocking behavior
	// 3. Increase timeout from 5s to 30s for full download
	// 4. Can use binary.Read() directly in player.go (no ReadFull needed)
	//
	// Current implementation chosen because GC stutter > network stutter in practice

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

	// Get stdout pipe for streaming
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		detailedErr := errors.New("failed to create stdout pipe: " + err.Error())
		log.Errorf("error creating pipe for %s: %v", job.VideoID, detailedErr)
		sentry.CaptureException(detailedErr)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &detailedErr,
		}
		return
	}

	start := time.Now()

	// Start FFmpeg process
	if err := ffmpeg.Start(); err != nil {
		detailedErr := errors.New("failed to start ffmpeg: " + err.Error())
		log.Errorf("error starting ffmpeg for %s: %v", job.VideoID, detailedErr)
		sentry.CaptureException(detailedErr)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &detailedErr,
		}
		return
	}

	// Give FFmpeg a moment to start and validate the stream
	started := make(chan bool, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		started <- true
	}()

	// Wait for FFmpeg to start or handle cancellation
	select {
	case <-l.canceled:
		l.logger.Debugf("load for %s canceled", job.VideoID)
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
		}
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadCanceled,
			VideoID: &job.VideoID,
		}
		log.Tracef("sent load canceled event for %s", job.VideoID)
		return
	case <-started:
		// FFmpeg started successfully, return streaming pipe
		l.logger.Tracef("ffmpeg started for %s, streaming", job.VideoID)
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoaded,
			VideoID: &job.VideoID,
			LoadResult: &LoadResult{
				ffmpegOut: stdout,
				VideoID:   job.VideoID,
				Title:     job.Title,
				Duration:  time.Since(start),
			},
		}
		log.Tracef("sent loaded event for %s", job.VideoID)
		return
	case <-time.After(5 * time.Second):
		// Timeout waiting for FFmpeg to start
		stderrStr := stderr.String()
		errMsg := "ffmpeg failed to start within 5 seconds"
		if stderrStr != "" {
			errMsg += " | ffmpeg stderr: " + stderrStr
		}
		error := errors.New(errMsg)
		log.Errorf("ffmpeg start timeout for %s: %v", job.VideoID, error)
		sentry.CaptureException(error)
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
		}
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &error,
		}
		return
	}
}

func (l *Loader) Cancel() {
	l.canceled <- true
}
