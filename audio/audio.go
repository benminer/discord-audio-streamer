package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

type PlaybackState struct {
	ffmpeg        *exec.Cmd
	ffmpegOut     io.ReadCloser
	encoder       *opus.Encoder
	done          chan bool
	loading       bool
	paused        bool
	buffer        []int16
	opusBuffer    []byte
	mutex         sync.Mutex
	notifications chan PlaybackNotification
	resetChannel  chan bool
	log           *log.Entry
}

type PlaybackNotificationType string

const (
	PlaybackStarted   PlaybackNotificationType = "started"
	PlaybackPaused    PlaybackNotificationType = "paused"
	PlaybackResumed   PlaybackNotificationType = "resumed"
	PlaybackCompleted PlaybackNotificationType = "completed"
	PlaybackStopped   PlaybackNotificationType = "stopped"
	PlaybackError     PlaybackNotificationType = "error"
)

type PlaybackNotification struct {
	PlaybackState *PlaybackState
	Error         *error
	VideoID       *string
	Event         PlaybackNotificationType
}

func NewPlaybackState(notifications chan PlaybackNotification, resetChannel chan bool) *PlaybackState {
	return &PlaybackState{
		done:          make(chan bool),
		buffer:        make([]int16, 960*2), // 20ms at 48kHz, stereo
		opusBuffer:    make([]byte, 960*4),
		notifications: notifications,
		resetChannel:  resetChannel,
		loading:       false,
		paused:        false,
		log:           log.WithFields(log.Fields{"module": "audio"}),
	}
}

func (ps *PlaybackState) IsLoading() bool {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()
	return ps.loading
}

func (ps *PlaybackState) IsPlaying() bool {
	// these are only set while actively streaming
	return ps.ffmpegOut != nil && ps.encoder != nil
}

func (ps *PlaybackState) StartStream(vc *discordgo.VoiceConnection, streamURL string, videoID string) error {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	ps.log.Debug("starting ffmpeg")

	ps.ffmpeg = exec.Command("ffmpeg",
		"-i", streamURL,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-af", "aresample=48000",
		"-loglevel", "error",
		"pipe:1")

	done := make(chan struct {
		output []byte
		err    error
	})

	start := time.Now()

	ps.loading = true

	go func() {
		output, err := ps.ffmpeg.Output()
		done <- struct {
			output []byte
			err    error
		}{output, err}
	}()

	// It's possible for a user to queue something HUGE
	// In the case of this, ffmpeg will take forever to load the stream
	// So, we kill it and emit an error after 15 seconds
	select {
	case result := <-done:
		ps.loading = false
		if result.err != nil {
			ps.notifications <- PlaybackNotification{
				PlaybackState: ps,
				Event:         PlaybackError,
				VideoID:       &videoID,
				Error:         &result.err,
			}
			sentry.CaptureException(result.err)
			return result.err
		}

		duration := time.Since(start)
		ps.log.Debugf("Buffered %.2f MB in %v", float64(len(result.output))/(1024*1024), duration)
		// loading the whole stream into memory is not ideal, but it's the only way to get the duration of the stream
		// this also is way less buggy when piping to discord
		ps.ffmpegOut = io.NopCloser(bytes.NewReader(result.output))
		encoder, opusErr := opus.NewEncoder(48000, 2, opus.Application(opus.AppAudio))
		if opusErr != nil {
			sentry.CaptureException(opusErr)
			ps.log.Errorf("error creating opus encoder: %v", opusErr)
			return fmt.Errorf("error creating opus encoder: %v", opusErr)
		}
		encoder.SetComplexity(10)
		encoder.SetBitrateToMax()
		ps.encoder = encoder

		go ps.streamLoop(vc, videoID)
		return nil
	case <-ps.done:
		ps.loading = false
		if ps.ffmpeg.Process != nil {
			ps.ffmpeg.Process.Kill()
		}
		ps.notifications <- PlaybackNotification{
			PlaybackState: ps,
			Event:         PlaybackStopped,
			VideoID:       &videoID,
		}
		ps.log.Debug("Stream initialization cancelled")
		return fmt.Errorf("stream initialization cancelled")
	case <-time.After(15 * time.Second):
		ps.loading = false
		if err := ps.ffmpeg.Process.Kill(); err != nil {
			ps.log.Warnf("Error killing ffmpeg: %v", err)
		}
		error := errors.New("ffmpeg timed out after 15 seconds")
		sentry.CaptureException(error)
		ps.notifications <- PlaybackNotification{
			PlaybackState: ps,
			Event:         PlaybackError,
			VideoID:       &videoID,
			Error:         &error,
		}
		return error
	}
}

func (ps *PlaybackState) streamLoop(vc *discordgo.VoiceConnection, videoID string) {
	defer ps.cleanup(videoID)

	firstPacket := true
	buffer := make([]int16, 960*2)

	for {
		select {
		case _, ok := <-ps.done:
			if !ok {
				// stream was pre-emptively stopped, probably from a /skip command
				ps.log.Trace("Playback stopped by channel close")
			} else {
				// the stream ended naturally
				ps.log.Trace("Playback stopped by done signal")
			}
			return
		default:
			if ps.paused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if ps.ffmpegOut == nil {
				ps.log.Debug("ffmpegOut is nil, skipping")
				continue
			}

			var readAttempts int
			for readAttempts < 3 {
				err := binary.Read(ps.ffmpegOut, binary.LittleEndian, &buffer)
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					ps.log.Debug("Reached end of audio stream")
					ps.notifications <- PlaybackNotification{
						PlaybackState: ps,
						Event:         PlaybackCompleted,
						VideoID:       &videoID,
					}
					return
				}
				if err != nil {
					readAttempts++
					ps.log.Warnf("Error reading from buffer (attempt %d/3): %v", readAttempts, err)
					sentry.CaptureException(err)
					if readAttempts == 3 {
						ps.notifications <- PlaybackNotification{
							PlaybackState: ps,
							Event:         PlaybackError,
							VideoID:       &videoID,
							Error:         &err,
						}
						return
					}
					continue
				}
				break
			}

			if firstPacket {
				ps.notifications <- PlaybackNotification{
					PlaybackState: ps,
					Event:         PlaybackStarted,
					VideoID:       &videoID,
				}
				firstPacket = false
			}

			if ps.encoder == nil {
				ps.log.Warn("encoder is nil, skipping")
				error := errors.New("encoder is nil")
				ps.notifications <- PlaybackNotification{
					PlaybackState: ps,
					Event:         PlaybackError,
					VideoID:       &videoID,
					Error:         &error,
				}
				continue
			}
			n, err := ps.encoder.Encode(buffer, ps.opusBuffer)

			if err != nil {
				ps.log.Warnf("Error encoding to opus: %v", err)
				sentry.CaptureException(err)
				ps.notifications <- PlaybackNotification{
					PlaybackState: ps,
					Event:         PlaybackError,
					VideoID:       &videoID,
					Error:         &err,
				}
				continue
			}

			select {
			case vc.OpusSend <- ps.opusBuffer[:n]:
			case <-ps.done:
				ps.log.Debug("Playback stopped during opus send")
				return
			}
		}
	}
}

func (ps *PlaybackState) Pause() {
	ps.log.Trace("pausing playback")
	ps.paused = true
	ps.notifications <- PlaybackNotification{
		PlaybackState: ps,
		Event:         PlaybackPaused,
	}
}

func (ps *PlaybackState) Resume() {
	ps.log.Trace("resuming playback")
	ps.paused = false
	ps.notifications <- PlaybackNotification{
		PlaybackState: ps,
		Event:         PlaybackResumed,
	}
}

func (ps *PlaybackState) Stop() {
	ps.log.Trace("stopping playback")
	ps.done <- true
	ps.done = make(chan bool)
}

func (ps *PlaybackState) Clear() {
	if ps.ffmpegOut != nil {
		log.Trace("closing ffmpeg output")
		ps.ffmpegOut.Close()
		ps.ffmpegOut = nil
	}

	if ps.ffmpeg.Process != nil {
		log.Trace("killing ffmpeg process")
		ps.ffmpeg.Process.Kill()
	}

	if ps.encoder != nil {
		log.Trace("closing encoder")
		ps.encoder = nil
	}
}

// on reset, we just clear the playback state
// the controller will handle starting a new stream
func (ps *PlaybackState) Reset() {
	ps.Clear()
}

func (ps *PlaybackState) cleanup(videoID string) {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	ps.log.Trace("cleaning up")

	ps.Clear()
	ps.resetChannel <- true

	// we send a stopped event to indicate that the stream has ended
	// this could either be because the stream ended, or because it was stopped by the user i.e. skip or stop
	ps.notifications <- PlaybackNotification{
		PlaybackState: ps,
		Event:         PlaybackStopped,
		VideoID:       &videoID,
	}
}
