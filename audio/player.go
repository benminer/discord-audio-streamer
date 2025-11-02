package audio

import (
	"encoding/binary"
	"io"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	sentry "github.com/getsentry/sentry-go"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

type Player struct {
	Notifications    chan PlaybackNotification
	completed        chan bool
	logger           *log.Entry
	encoder          *opus.Encoder
	paused           atomic.Bool
	stopping         atomic.Bool
	playing          *bool
	volume           int
	fadeOutRemaining int
	mutex            sync.Mutex
}

func NewPlayer() (*Player, error) {
	encoder, err := opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}

	encoder.SetComplexity(10)
	encoder.SetBitrateToMax()

	playing := false

	player := &Player{
		completed:     make(chan bool),
		Notifications: make(chan PlaybackNotification, 100),
		logger: log.WithFields(log.Fields{
			"module": "player",
		}),
		encoder:          encoder,
		playing:          &playing,
		volume:           100,
		fadeOutRemaining: 0,
		mutex:            sync.Mutex{},
	}
	player.paused.Store(false)
	player.stopping.Store(false)
	return player, nil
}

func (p *Player) Play(data *LoadResult, voiceChannel *discordgo.VoiceConnection) error {
	p.mutex.Lock()

	defer func() {
		*p.playing = false
		p.mutex.Unlock()
	}()

	*p.playing = true
	p.stopping.Store(false) // Reset stopping flag for new song
	firstPacket := true
	buffer := make([]int16, 960*2)
	opusBuffer := make([]byte, 960*4)

	// Prime the voice connection before streaming
	p.logger.Debug("Setting Speaking(true) to prime voice connection")
	voiceChannel.Speaking(true)

	// Small delay to let Discord prepare its pipeline
	time.Sleep(50 * time.Millisecond)
	p.logger.Debug("Starting audio stream")

	for {
		select {
		case _, ok := <-p.completed:
			if !ok {
				p.logger.Trace("Playback stopped by channel close")
				return nil
			} else {
				p.logger.Trace("Playback stopped by done signal")
			}
			return nil
		default:
			// Handle fade-out when pausing or stopping
			if p.fadeOutRemaining > 0 {
				// IMPORTANT: io.ReadFull() is required for streaming FFmpeg pipes
				// binary.Read() can return partial data from pipes, causing artifacts
				// ReadFull() blocks until we get exactly 3840 bytes (1920 samples * 2 bytes)
				// See loader.go for streaming vs buffering trade-offs
				byteBuffer := make([]byte, len(buffer)*2)
				_, err := io.ReadFull(data.ffmpegOut, byteBuffer)
				if err == nil {
					for i := 0; i < len(buffer); i++ {
						buffer[i] = int16(binary.LittleEndian.Uint16(byteBuffer[i*2:]))
					}
				}
				if err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						p.fadeOutRemaining = 0
						continue
					}
					p.logger.Warnf("Error reading during fade-out: %v", err)
					continue
				}

				// Apply fade-out multiplier (cubic fade for sharp curve)
				t := float64(p.fadeOutRemaining) / 5.0
				fadeMultiplier := t * t * t
				for i := range buffer {
					sample := float64(buffer[i]) * fadeMultiplier
					if sample > 32767 {
						sample = 32767
					} else if sample < -32768 {
						sample = -32768
					}
					buffer[i] = int16(sample)
				}

				// Encode and send
				encoded, err := p.encoder.Encode(buffer, opusBuffer)
				if err == nil {
					select {
					case voiceChannel.OpusSend <- opusBuffer[:encoded]:
					case <-p.completed:
						return nil
					}
				}

				p.fadeOutRemaining--

				// If we were stopping and fade-out is complete, exit
				if p.stopping.Load() && p.fadeOutRemaining == 0 {
					p.logger.Debug("Fade-out complete, stopping playback")
					return nil
				}

				time.Sleep(20 * time.Millisecond)
				continue
			}

			// Check if we should start fade-out for stop
			if p.stopping.Load() && p.fadeOutRemaining == 0 {
				p.logger.Debug("Stop requested, starting fade-out")
				p.fadeOutRemaining = 5
				continue
			}

			if p.paused.Load() {
				// Drain FFmpeg buffer to prevent stale data buildup - use ReadFull for complete frame
				byteBuffer := make([]byte, len(buffer)*2)
				_, err := io.ReadFull(data.ffmpegOut, byteBuffer)
				if err == nil {
					for i := 0; i < len(buffer); i++ {
						buffer[i] = int16(binary.LittleEndian.Uint16(byteBuffer[i*2:]))
					}
				}
				if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
					p.logger.Warnf("Error draining buffer during pause: %v", err)
				}

				// Send silence frame to maintain stream continuity
				silenceBuffer := make([]int16, 960*2)
				silenceOpus := make([]byte, 960*4)
				encoded, err := p.encoder.Encode(silenceBuffer, silenceOpus)
				if err == nil {
					select {
					case voiceChannel.OpusSend <- silenceOpus[:encoded]:
					case <-p.completed:
						return nil
					default:
						// Skip if channel is full
					}
				}

				time.Sleep(20 * time.Millisecond) // ~50 frames per second
				continue
			}

			var attempts int
			for attempts < 3 {
				// Use ReadFull to ensure we get complete frames from streaming pipe
				byteBuffer := make([]byte, len(buffer)*2)
				_, err := io.ReadFull(data.ffmpegOut, byteBuffer)
				if err == nil {
					for i := 0; i < len(buffer); i++ {
						buffer[i] = int16(binary.LittleEndian.Uint16(byteBuffer[i*2:]))
					}
				}
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					p.logger.Trace("Reached end of audio stream")
					p.Notifications <- PlaybackNotification{
						Event:   PlaybackCompleted,
						VideoID: &data.VideoID,
					}
					return nil
				}
				if err != nil {
					attempts++
					p.logger.Warnf("Error reading from buffer (attempt %d/3): %v", attempts, err)
					sentry.CaptureException(err)
					if attempts == 3 {
						p.Notifications <- PlaybackNotification{
							Event:   PlaybackError,
							VideoID: &data.VideoID,
							Error:   &err,
						}
						return err
					}
					continue
				}
				break
			}

			if firstPacket {
				p.Notifications <- PlaybackNotification{
					Event:   PlaybackStarted,
					VideoID: &data.VideoID,
				}
				firstPacket = false
			}

			if p.volume != 100 {
				for i := range buffer {
					sample := float64(buffer[i]) * float64(p.volume) / 100.0
					// clamp
					if sample > 32767 {
						sample = 32767
					} else if sample < -32768 {
						sample = -32768
					}
					buffer[i] = int16(sample)
				}
			}

			encoded, err := p.encoder.Encode(buffer, opusBuffer)
			if err != nil {
				p.logger.Warnf("Error encoding to opus: %v", err)
				sentry.CaptureException(err)
				p.Notifications <- PlaybackNotification{
					Event:   PlaybackError,
					VideoID: &data.VideoID,
					Error:   &err,
				}
				continue
			}

			select {
			case voiceChannel.OpusSend <- opusBuffer[:encoded]:
			case <-p.completed:
				p.logger.Debug("Playback stopped by channel close")
				p.Notifications <- PlaybackNotification{
					Event:   PlaybackStopped,
					VideoID: &data.VideoID,
				}
				return nil
			}
		}
	}
}

func (p *Player) Pause() {
	p.logger.Info("Pausing playback - starting fade-out")
	if p.fadeOutRemaining == 0 {
		p.fadeOutRemaining = 5 // 5 frames = 100ms fade-out
	}
	p.paused.Store(true)
	p.Notifications <- PlaybackNotification{
		Event: PlaybackPaused,
	}
}

func (p *Player) Resume() {
	p.logger.Info("Resuming playback")
	p.fadeOutRemaining = 0 // Cancel any ongoing fade-out
	p.paused.Store(false)
	p.Notifications <- PlaybackNotification{
		Event: PlaybackResumed,
	}
}

func (p *Player) Stop() {
	p.logger.Info("Stopping playback - will fade out")
	p.stopping.Store(true)
}

func (p *Player) IsPlaying() bool {
	isPlaying := *p.playing
	p.logger.Tracef("Player is playing: %t", isPlaying)
	return isPlaying
}

func (p *Player) SetVolume(volume int) {
	if volume < 0 {
		volume = 0
	}
	if volume > 150 {
		volume = 150
	}
	p.volume = volume
}

func (p *Player) GetVolume() int {
	return p.volume
}
