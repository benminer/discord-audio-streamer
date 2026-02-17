package audio

import (
	"bytes"
	"context"
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

func (l *Loader) Load(ctx context.Context, job LoadJob) {
	l.logger.Debugf("starting load for %s", job.VideoID)

	// Start tracing span for the entire load operation
	span := sentry.StartSpan(ctx, "audio.load")
	span.Description = "Load audio via FFmpeg"
	span.SetTag("video_id", job.VideoID)
	span.SetTag("title", job.Title)

	l.mutex.Lock()
	defer func() {
		l.mutex.Unlock()
		select {
		case l.completed <- true:
		default:
		}
		span.Finish()
	}()

	l.Notifications <- PlaybackNotification{
		Event:   PlaybackLoading,
		VideoID: &job.VideoID,
	}

	// Memory-based buffering approach:
	// - Loads entire audio into memory before playback starts
	// - More reliable than streaming (no partial reads, no mid-stream failures)
	// - Uses bytes.Buffer for efficient memory growth
	// - Go 1.24+ GC handles ~55MB allocations well without noticeable pauses
	// - Simpler player.go code with binary.Read()

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

	// Get stdout pipe for reading
	stdout, err := ffmpeg.StdoutPipe()
	if err != nil {
		detailedErr := errors.New("failed to create stdout pipe: " + err.Error())
		log.Errorf("error creating pipe for %s: %v", job.VideoID, detailedErr)
		sentry.CaptureException(detailedErr)
		span.Status = sentry.SpanStatusInternalError
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
		span.Status = sentry.SpanStatusInternalError
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &detailedErr,
		}
		return
	}

	// Buffer the entire audio output into memory
	type result struct {
		buf *bytes.Buffer
		err error
	}
	done := make(chan result, 1)

	go func() {
		var buf bytes.Buffer
		_, err := io.Copy(&buf, stdout)
		done <- result{&buf, err}
	}()

	// Wait for FFmpeg to complete, handle cancellation or timeout
	select {
	case <-l.canceled:
		l.logger.Debugf("load for %s canceled", job.VideoID)
		span.Status = sentry.SpanStatusCanceled
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
			ffmpeg.Wait() // Reap zombie process
		}
		// Drain the done channel to let the goroutine exit cleanly
		go func() { <-done }()
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadCanceled,
			VideoID: &job.VideoID,
		}
		log.Tracef("sent load canceled event for %s", job.VideoID)
		return

	case res := <-done:
		// Check for copy errors
		if res.err != nil {
			errMsg := "failed to read ffmpeg output: " + res.err.Error()
			if stderr.Len() > 0 {
				errMsg += " | ffmpeg stderr: " + stderr.String()
			}
			detailedErr := errors.New(errMsg)
			log.Errorf("error loading %s: %v", job.VideoID, detailedErr)
			sentry.CaptureException(detailedErr)
			span.Status = sentry.SpanStatusInternalError
			if ffmpeg.Process != nil {
				ffmpeg.Process.Kill()
				ffmpeg.Wait() // Reap zombie process
			}
			l.Notifications <- PlaybackNotification{
				Event:   PlaybackLoadError,
				VideoID: &job.VideoID,
				Error:   &detailedErr,
			}
			return
		}

		// Wait for FFmpeg to exit and check for errors
		if err := ffmpeg.Wait(); err != nil {
			errMsg := "ffmpeg exited with error: " + err.Error()
			if stderr.Len() > 0 {
				errMsg += " | ffmpeg stderr: " + stderr.String()
			}
			detailedErr := errors.New(errMsg)
			log.Errorf("error loading %s: %v", job.VideoID, detailedErr)
			sentry.CaptureException(detailedErr)
			span.Status = sentry.SpanStatusInternalError
			l.Notifications <- PlaybackNotification{
				Event:   PlaybackLoadError,
				VideoID: &job.VideoID,
				Error:   &detailedErr,
			}
			return
		}

		// Success - record buffer size and set OK status
		span.Status = sentry.SpanStatusOK
		span.SetData("buffer_bytes", res.buf.Len())
		span.SetData("load_duration_ms", time.Since(start).Milliseconds())

		log.Tracef("loaded %s (%d bytes)", job.VideoID, res.buf.Len())
		bytesLoaded := uint64(res.buf.Len())
		bytesPerSecond := uint64(48000 * 2 * 2) // sampleRate * bytesPerSample * channels
		durationNs := (bytesLoaded * 1000000000) / bytesPerSecond
		duration := time.Duration(durationNs).Round(time.Second)

		output := io.NopCloser(bytes.NewReader(res.buf.Bytes()))
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoaded,
			VideoID: &job.VideoID,
			LoadResult: &LoadResult{
				ffmpegOut: output,
				VideoID:   job.VideoID,
				Title:     job.Title,
				Duration:  duration,
			},
		}
		log.Tracef("sent loaded event for %s", job.VideoID)
		return

	case <-time.After(30 * time.Second):
		errMsg := "ffmpeg timed out after 30 seconds"
		if stderr.Len() > 0 {
			errMsg += " | ffmpeg stderr: " + stderr.String()
		}
		detailedErr := errors.New(errMsg)
		log.Errorf("ffmpeg timed out for %s: %v", job.VideoID, detailedErr)
		sentry.CaptureException(detailedErr)
		span.Status = sentry.SpanStatusDeadlineExceeded
		if ffmpeg.Process != nil {
			ffmpeg.Process.Kill()
			ffmpeg.Wait() // Reap zombie process
		}
		// Drain the done channel to let the goroutine exit cleanly
		go func() { <-done }()
		l.Notifications <- PlaybackNotification{
			Event:   PlaybackLoadError,
			VideoID: &job.VideoID,
			Error:   &detailedErr,
		}
		return
	}
}

func (l *Loader) Cancel() {
	// Non-blocking send: signals an active Load() to abort without blocking
	// if no load is in progress. Buffered(1) was wrong — a stale signal would
	// sit in the buffer and incorrectly cancel the next Load() call.
	select {
	case l.canceled <- true:
	default:
	}
}
