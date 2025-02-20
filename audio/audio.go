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

	log "github.com/sirupsen/logrus"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

type PlaybackState struct {
	ffmpeg        *exec.Cmd
	ffmpegOut     io.ReadCloser
	encoder       *opus.Encoder
	done          chan bool
	paused        bool
	buffer        []int16
	opusBuffer    []byte
	mutex         sync.Mutex
	notifications chan PlaybackNotification
	log           *log.Entry
}

type PlaybackNotificationType string

const (
	PlaybackStarted   PlaybackNotificationType = "started"
	PlaybackPaused    PlaybackNotificationType = "paused"
	PlaybackResumed   PlaybackNotificationType = "resumed"
	PlaybackStopped   PlaybackNotificationType = "stopped"
	PlaybackCompleted PlaybackNotificationType = "completed"
	PlaybackError     PlaybackNotificationType = "error"
)

type PlaybackNotification struct {
	PlaybackState *PlaybackState
	Error         *error
	Event         PlaybackNotificationType
}

func NewPlaybackState(notifications chan PlaybackNotification) *PlaybackState {
	return &PlaybackState{
		done:          make(chan bool),
		buffer:        make([]int16, 960*2), // 20ms at 48kHz, stereo
		opusBuffer:    make([]byte, 960*4),
		notifications: notifications,
		log:           log.WithFields(log.Fields{"module": "audio"}),
	}
}

func (ps *PlaybackState) StartStream(vc *discordgo.VoiceConnection, streamURL string) error {
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
		if result.err != nil {
			ps.notifications <- PlaybackNotification{
				PlaybackState: ps,
				Event:         PlaybackError,
				Error:         &result.err,
			}
			return result.err
		}
		duration := time.Since(start)
		ps.log.Debugf("Buffered %.2f MB in %v", float64(len(result.output))/(1024*1024), duration)
		// loading the whole stream into memory is not ideal, but it's the only way to get the duration of the stream
		// this also is way less buggy when piping to discord
		ps.ffmpegOut = io.NopCloser(bytes.NewReader(result.output))
		encoder, opusErr := opus.NewEncoder(48000, 2, opus.Application(opus.AppAudio))
		if opusErr != nil {
			ps.log.Errorf("error creating opus encoder: %v", opusErr)
			return fmt.Errorf("error creating opus encoder: %v", opusErr)
		}
		encoder.SetComplexity(10)
		encoder.SetBitrateToMax()
		ps.encoder = encoder

		go ps.streamLoop(vc)
		return nil
	case <-time.After(15 * time.Second):
		if err := ps.ffmpeg.Process.Kill(); err != nil {
			ps.log.Warnf("Error killing ffmpeg: %v", err)
		}
		error := errors.New("ffmpeg timed out after 15 seconds")
		ps.notifications <- PlaybackNotification{
			PlaybackState: ps,
			Event:         PlaybackError,
			Error:         &error,
		}
		return error
	}
}

func (ps *PlaybackState) streamLoop(vc *discordgo.VoiceConnection) {
	defer ps.cleanup()

	firstPacket := true
	buffer := make([]int16, 960*2)

	for {
		select {
		case <-ps.done:
			ps.log.Debug("Playback stopped by done signal")
			return
		default:
			if ps.paused {
				time.Sleep(100 * time.Millisecond)
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
					}
					return
				}
				if err != nil {
					readAttempts++
					ps.log.Warnf("Error reading from buffer (attempt %d/3): %v", readAttempts, err)
					if readAttempts == 3 {
						ps.notifications <- PlaybackNotification{
							PlaybackState: ps,
							Event:         PlaybackError,
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
				}
				firstPacket = false
			}

			n, err := ps.encoder.Encode(buffer, ps.opusBuffer)
			if err != nil {
				ps.log.Warnf("Error encoding to opus: %v", err)
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
	if ps.done != nil {
		close(ps.done)
		ps.done = nil
	}
}

func (ps *PlaybackState) Quit() {
	ps.Stop()
}

func (ps *PlaybackState) cleanup() {
	ps.log.Trace("cleaning up")

	if ps.ffmpegOut != nil {
		ps.ffmpegOut.Close()
		ps.ffmpegOut = nil
	}

	if ps.encoder != nil {
		ps.encoder = nil
	}

	ps.notifications <- PlaybackNotification{
		PlaybackState: ps,
		Event:         PlaybackStopped,
	}
}
