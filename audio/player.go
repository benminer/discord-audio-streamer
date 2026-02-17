package audio

import (
	"context"
	"encoding/binary"
	"fmt"
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
	Notifications     chan PlaybackNotification
	completed         chan bool
	logger            *log.Entry
	encoder           *opus.Encoder
	paused            atomic.Bool
	stopping          atomic.Bool
	playing           *bool
	volume            int
	fadeOutRemaining  int
	mutex             sync.Mutex
	playbackStartTime time.Time
	playbackPosition  atomic.Int64 // microseconds
	silenceBuffer     []int16      // Pre-allocated for pause loop
	silenceOpus       []byte       // Pre-allocated for pause loop
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
		silenceBuffer:    make([]int16, 960*2),
		silenceOpus:      make([]byte, 960*4),
	}
	player.paused.Store(false)
	player.stopping.Store(false)
	return player, nil
}

func (p *Player) Play(ctx context.Context, data *LoadResult, voiceChannel *discordgo.VoiceConnection) error {
	// Start tracing span for the playback session
	span := sentry.StartSpan(ctx, "audio.playback")
	span.Description = "Audio playback session"
	span.SetTag("video_id", data.VideoID)
	span.SetTag("title", data.Title)

	p.mutex.Lock()

	defer func() {
		// Recover from send on closed channel (voice connection closed during playback)
		if r := recover(); r != nil {
			p.logger.Warnf("Recovered from panic during playback: %v", r)
			sentry.CaptureMessage(fmt.Sprintf("Recovered from playback panic: %v", r))
		}
		*p.playing = false
		p.mutex.Unlock()
		span.Finish()
	}()

	*p.playing = true
	p.stopping.Store(false) // Reset stopping flag for new song
	// Initialize position tracking
	p.playbackStartTime = time.Now()
	p.playbackPosition.Store(0)
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
				span.Status = sentry.SpanStatusCanceled
				return nil
			} else {
				p.logger.Trace("Playback stopped by done signal")
			}
			span.Status = sentry.SpanStatusCanceled
			return nil
		default:
			// Handle fade-out when pausing or stopping
			if p.fadeOutRemaining > 0 {
				err := binary.Read(data.ffmpegOut, binary.LittleEndian, &buffer)
				if err != nil {
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						p.fadeOutRemaining = 0
						if p.stopping.Load() {
							p.logger.Debug("EOF during fade-out, stopping playback")
							span.Status = sentry.SpanStatusCanceled
							return nil
						}
						continue
					}
					p.logger.Warnf("Error reading during fade-out: %v", err)
					sentry.CaptureException(err)
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
				if err != nil {
					p.logger.Warnf("Error encoding during fade-out: %v", err)
					sentry.CaptureException(err)
				} else {
					if !safeSendOpus(voiceChannel, opusBuffer[:encoded], p.completed) {
						return nil
					}
				}

				p.fadeOutRemaining--

				// If we were stopping and fade-out is complete, exit
				if p.stopping.Load() && p.fadeOutRemaining == 0 {
					p.logger.Debug("Fade-out complete, stopping playback")
					span.Status = sentry.SpanStatusCanceled
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
				// If a stop was requested while paused, unblock so the stopping
				// check below can run its fade-out and exit cleanly.
				if p.stopping.Load() {
					p.logger.Debug("Stop requested while paused, exiting pause loop")
					p.paused.Store(false)
					continue
				}

				// Drain buffer to prevent stale data buildup
				err := binary.Read(data.ffmpegOut, binary.LittleEndian, &buffer)
				if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
					p.logger.Warnf("Error draining buffer during pause: %v", err)
					sentry.CaptureException(err)
				}

				// Send silence frame to maintain stream continuity
				encoded, err := p.encoder.Encode(p.silenceBuffer, p.silenceOpus)
				if err != nil {
					p.logger.Warnf("Error encoding silence during pause: %v", err)
					sentry.CaptureException(err)
				} else {
					safeSendOpus(voiceChannel, p.silenceOpus[:encoded], p.completed)
				}

				time.Sleep(20 * time.Millisecond) // ~50 frames per second
				continue
			}

			var attempts int
			for attempts < 3 {
				err := binary.Read(data.ffmpegOut, binary.LittleEndian, &buffer)
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					p.logger.Trace("Reached end of audio stream")
					span.Status = sentry.SpanStatusOK
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
						span.Status = sentry.SpanStatusInternalError
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

			// Update position tracking BEFORE sending (each opus frame is 20ms)
			// This ensures position stays accurate even if send fails
			if !p.paused.Load() && !p.stopping.Load() {
				currentPos := p.playbackPosition.Load() + 20000 // 20ms in microseconds
				p.playbackPosition.Store(currentPos)
			}

			if !safeSendOpus(voiceChannel, opusBuffer[:encoded], p.completed) {
				p.logger.Debug("Playback stopped - voice channel closed or completed")
				span.Status = sentry.SpanStatusCanceled
				p.Notifications <- PlaybackNotification{
					Event:   PlaybackStopped,
					VideoID: &data.VideoID,
				}
				return nil
			}
		}
	}
}

// safeSendOpus sends opus data to the voice connection, returning false if the channel is closed.
func safeSendOpus(vc *discordgo.VoiceConnection, data []byte, completed <-chan bool) (sent bool) {
	defer func() {
		if r := recover(); r != nil {
			sent = false
		}
	}()
	select {
	case vc.OpusSend <- data:
		return true
	case <-completed:
		return false
	}
}

func (p *Player) Pause(ctx context.Context) {
	_ = ctx // ctx available for future Sentry tracing if needed
	p.logger.Info("Pausing playback - starting fade-out")
	if p.fadeOutRemaining == 0 {
		p.fadeOutRemaining = 5 // 5 frames = 100ms fade-out
	}
	// Position tracking automatically freezes when paused (checked in Play loop)
	p.paused.Store(true)
	p.Notifications <- PlaybackNotification{
		Event: PlaybackPaused,
	}
}

func (p *Player) Resume(ctx context.Context) {
	_ = ctx // ctx available for future Sentry tracing if needed
	p.logger.Info("Resuming playback")
	p.fadeOutRemaining = 0 // Cancel any ongoing fade-out
	// Position tracking automatically resumes when unpaused (checked in Play loop)
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

func (p *Player) IsPaused() bool {
	isPaused := p.paused.Load()
	p.logger.Tracef("Player is paused: %t", isPaused)
	return isPaused
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


func (p *Player) GetPosition() time.Duration {
	if p.playing == nil || !*p.playing {
		return 0
	}

	microseconds := p.playbackPosition.Load()
	return time.Duration(microseconds) * time.Microsecond
}
