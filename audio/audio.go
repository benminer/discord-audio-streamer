package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

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
}

type PlaybackNotificationType string

const (
	PlaybackStarted   PlaybackNotificationType = "started"
	PlaybackPaused    PlaybackNotificationType = "paused"
	PlaybackResumed   PlaybackNotificationType = "resumed"
	PlaybackStopped   PlaybackNotificationType = "stopped"
	PlaybackCompleted PlaybackNotificationType = "completed"
)

type PlaybackNotification struct {
	PlaybackState *PlaybackState
	Event         PlaybackNotificationType
}

func NewPlaybackState(notifications chan PlaybackNotification) *PlaybackState {
	return &PlaybackState{
		done:          make(chan bool),
		buffer:        make([]int16, 960*2), // 20ms at 48kHz, stereo
		opusBuffer:    make([]byte, 960*4),
		notifications: notifications,
	}
}

func (ps *PlaybackState) StartStream(vc *discordgo.VoiceConnection, streamURL string) error {
	log.Printf("Starting ffmpeg buffer for %s", streamURL)
	ps.ffmpeg = exec.Command("ffmpeg",
		"-i", streamURL,
		"-f", "s16le",
		"-ar", "48000",
		"-ac", "2",
		"-af", "aresample=48000",
		"-loglevel", "error",
		"pipe:1")

	start := time.Now()
	output, err := ps.ffmpeg.Output()
	if err != nil {
		return fmt.Errorf("error getting stream: %v", err)
	}
	duration := time.Since(start)

	log.Printf("Buffered %.2f MB in %v", float64(len(output))/(1024*1024), duration)

	// loading the whole stream into memory is not ideal, but it's the only way to get the duration of the stream
	// this also is way less buggy when piping to discord
	ps.ffmpegOut = io.NopCloser(bytes.NewReader(output))

	ps.encoder, err = opus.NewEncoder(48000, 2, opus.Application(opus.AppAudio))
	if err != nil {
		return fmt.Errorf("error creating opus encoder: %v", err)
	}
	ps.encoder.SetComplexity(10)
	ps.encoder.SetBitrateToMax()

	go ps.streamLoop(vc)
	return nil
}

func (ps *PlaybackState) streamLoop(vc *discordgo.VoiceConnection) {
	defer ps.cleanup()

	firstPacket := true
	buffer := make([]int16, 960*2)

	for {
		select {
		case <-ps.done:
			return
		default:
			if ps.paused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			err := binary.Read(ps.ffmpegOut, binary.LittleEndian, &buffer)
			if err == io.EOF {
				ps.notifications <- PlaybackNotification{
					PlaybackState: ps,
					Event:         PlaybackCompleted,
				}
				return
			}
			if err != nil {
				log.Printf("Error reading from buffer: %v", err)
				continue
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
				log.Printf("Error encoding to opus: %v", err)
				continue
			}

			vc.OpusSend <- ps.opusBuffer[:n]
		}
	}
}

func (ps *PlaybackState) Pause() {
	ps.paused = true
	ps.notifications <- PlaybackNotification{
		PlaybackState: ps,
		Event:         PlaybackPaused,
	}
}

func (ps *PlaybackState) Resume() {
	ps.paused = false
	ps.notifications <- PlaybackNotification{
		PlaybackState: ps,
		Event:         PlaybackResumed,
	}
}

func (ps *PlaybackState) Stop() {
	log.Printf("Stopping playback")
	if ps.done != nil {
		close(ps.done)
		ps.done = nil
	}
}

func (ps *PlaybackState) Quit() {
	ps.Stop()
}

func (ps *PlaybackState) cleanup() {
	ps.mutex.Lock()
	defer ps.mutex.Unlock()

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
