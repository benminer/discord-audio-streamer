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
	logger            *log.Entry
	encoder           *opus.Encoder
	paused            atomic.Bool
	stopping          atomic.Bool
	playing           atomic.Bool
	volume            atomic.Int32
	fadeOutRemaining  atomic.Int32
	mutex             sync.Mutex
	playbackStartTime time.Time
	playbackPosition  atomic.Int64                // microseconds
	silenceBuffer     []int16                     // Pre-allocated for pause loop
	silenceOpus       []byte                      // Pre-allocated for pause loop
	ttsPlayback       atomic.Pointer[TTSPlayback] // pre-generated TTS to mix during crossfade
	crossfading       atomic.Bool                 // true when crossfade is active
	crossfadePos      int32                       // frames into the crossfade (NOT atomic — only accessed in Play loop)
}

func NewPlayer() (*Player, error) {
	encoder, err := opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}

	encoder.SetComplexity(10)
	encoder.SetBitrateToMax()

	player := &Player{
		Notifications: make(chan PlaybackNotification, 100),
		logger: log.WithFields(log.Fields{
			"module": "player",
		}),
		encoder:       encoder,
		mutex:         sync.Mutex{},
		silenceBuffer: make([]int16, 960*2),
		silenceOpus:   make([]byte, 960*4),
	}
	player.paused.Store(false)
	player.stopping.Store(false)
	player.volume.Store(100) // default to 100% volume
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
		p.playing.Store(false)
		p.mutex.Unlock()
		span.Finish()
	}()

	p.playing.Store(true)
	p.stopping.Store(false)
	p.paused.Store(false)
	p.crossfading.Store(false)
	p.crossfadePos = 0
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
		// Handle fade-out when pausing or stopping
		if p.fadeOutRemaining.Load() > 0 {
			err := binary.Read(data.ffmpegOut, binary.LittleEndian, &buffer)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					p.fadeOutRemaining.Store(0)
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
			remaining := p.fadeOutRemaining.Load()
			t := float64(remaining) / 5.0
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
				if !safeSendOpus(voiceChannel, opusBuffer[:encoded]) {
					return nil
				}
			}

			p.fadeOutRemaining.Add(-1)

			// If we were stopping and fade-out is complete, exit
			if p.stopping.Load() && p.fadeOutRemaining.Load() == 0 {
				p.logger.Debug("Fade-out complete, stopping playback")
				span.Status = sentry.SpanStatusCanceled
				return nil
			}

			time.Sleep(20 * time.Millisecond)
			continue
		}

		// Check if we should start fade-out for stop
		if p.stopping.Load() && p.fadeOutRemaining.Load() == 0 {
			p.logger.Debug("Stop requested, starting fade-out")
			p.fadeOutRemaining.Store(5)
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
				if !safeSendOpus(voiceChannel, p.silenceOpus[:encoded]) {
					p.logger.Debug("Pause loop exiting - voice connection lost")
					p.Notifications <- PlaybackNotification{
						Event:   PlaybackStopped,
						VideoID: &data.VideoID,
					}
					return nil
				}
			}

			time.Sleep(20 * time.Millisecond) // ~50 frames per second
			continue
		}

		var attempts int
		for attempts < 3 {
			err := binary.Read(data.ffmpegOut, binary.LittleEndian, &buffer)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// If crossfading and TTS still has audio, play remaining TTS solo
				if p.crossfading.Load() {
					tts := p.ttsPlayback.Load()
					if tts != nil && tts.Remaining() > 0 {
						p.logger.Debug("Music ended, continuing TTS solo")
						ttsBuf := make([]int16, 960*2)
						for tts.ReadFrame(ttsBuf) {
							encoded, encErr := p.encoder.Encode(ttsBuf, opusBuffer)
							if encErr != nil {
								break
							}
							frame := make([]byte, encoded)
							copy(frame, opusBuffer[:encoded])
							if !safeSendOpus(voiceChannel, frame) {
								break
							}
						}
						p.crossfading.Store(false)
						p.ttsPlayback.Store(nil)
					}
				}
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

		vol := int(p.volume.Load())
		if vol != 100 {
			for i := range buffer {
				sample := float64(buffer[i]) * float64(vol) / 100.0
				// clamp
				if sample > 32767 {
					sample = 32767
				} else if sample < -32768 {
					sample = -32768
				}
				buffer[i] = int16(sample)
			}
		}

		// Crossfade: fade out music and mix in TTS audio
		if p.crossfading.Load() {
			tts := p.ttsPlayback.Load()
			if tts != nil {
				// Slow fade-out of music over ~250 frames (5 seconds)
				const crossfadeDuration int32 = 250
				p.crossfadePos++
				t := 1.0 - float64(p.crossfadePos)/float64(crossfadeDuration)
				if t < 0 {
					t = 0
				}
				musicFade := t * t // quadratic fade

				// Apply music fade
				for i := range buffer {
					buffer[i] = int16(float64(buffer[i]) * musicFade)
				}

				// Mix in TTS audio
				ttsBuf := make([]int16, 960*2)
				if tts.ReadFrame(ttsBuf) || tts.Position > 0 {
					for i := range buffer {
						mixed := int32(buffer[i]) + int32(ttsBuf[i])
						if mixed > 32767 {
							mixed = 32767
						} else if mixed < -32768 {
							mixed = -32768
						}
						buffer[i] = int16(mixed)
					}
				}
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

		// Check if we should start crossfading (TTS ready + approaching end of song)
		if !p.crossfading.Load() && data.Duration > 0 {
			tts := p.ttsPlayback.Load()
			if tts != nil {
				remaining := data.Duration - p.GetPosition()
				if remaining <= 5*time.Second && remaining > 0 {
					p.crossfading.Store(true)
					p.crossfadePos = 0
					p.logger.Debug("Starting TTS crossfade")
				}
			}
		}

		if !safeSendOpus(voiceChannel, opusBuffer[:encoded]) {
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

// safeSendOpus sends opus data to the voice connection.
// Returns false if the OpusSend channel is closed (voice disconnected),
// which is recovered from the panic that a send on a closed channel causes.
func safeSendOpus(vc *discordgo.VoiceConnection, data []byte) (sent bool) {
	defer func() {
		if r := recover(); r != nil {
			sent = false
		}
	}()
	vc.OpusSend <- data
	return true
}

func (p *Player) SetTTSPlayback(tts *TTSPlayback) {
	p.ttsPlayback.Store(tts)
}

func (p *Player) ClearTTSPlayback() {
	p.ttsPlayback.Store(nil)
	p.crossfading.Store(false)
}

func (p *Player) Pause(ctx context.Context) {
	_ = ctx // ctx available for future Sentry tracing if needed
	p.logger.Info("Pausing playback - starting fade-out")
	if p.fadeOutRemaining.Load() == 0 {
		p.fadeOutRemaining.Store(5) // 5 frames = 100ms fade-out
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
	p.fadeOutRemaining.Store(0) // Cancel any ongoing fade-out
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
	return p.playing.Load()
}

func (p *Player) IsPaused() bool {
	return p.paused.Load()
}

func (p *Player) SetPlaying(v bool) {
	p.playing.Store(v)
}

func (p *Player) SetPaused(v bool) {
	p.paused.Store(v)
}

func (p *Player) SetVolume(volume int) {
	if volume < 0 {
		volume = 0
	}
	if volume > 150 {
		volume = 150
	}
	p.volume.Store(int32(volume))
}

func (p *Player) GetVolume() int {
	return int(p.volume.Load())
}

func (p *Player) GetPosition() time.Duration {
	if !p.playing.Load() {
		return 0
	}

	microseconds := p.playbackPosition.Load()
	return time.Duration(microseconds) * time.Microsecond
}
