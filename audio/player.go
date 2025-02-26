package audio

import (
	"encoding/binary"
	"io"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	sentry "github.com/getsentry/sentry-go"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

type Player struct {
	Notifications chan PlaybackNotification
	completed     chan bool
	logger        *log.Entry
	encoder       *opus.Encoder
	paused        bool
	playing       *bool
	volume        int
	mutex         sync.Mutex
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

	return &Player{
		completed:     make(chan bool),
		Notifications: make(chan PlaybackNotification, 100),
		logger: log.WithFields(log.Fields{
			"module": "player",
		}),
		encoder: encoder,
		paused:  false,
		playing: &playing,
		volume:  100,
		mutex:   sync.Mutex{},
	}, nil
}

func (p *Player) Play(data *LoadResult, voiceChannel *discordgo.VoiceConnection) error {
	p.mutex.Lock()

	defer func() {
		*p.playing = false
		p.mutex.Unlock()
	}()

	*p.playing = true
	firstPacket := true
	buffer := make([]int16, 960*2)
	opusBuffer := make([]byte, 960*4)

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
			if p.paused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			var attempts int
			for attempts < 3 {
				err := binary.Read(data.ffmpegOut, binary.LittleEndian, &buffer)
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
	p.paused = true
	p.Notifications <- PlaybackNotification{
		Event: PlaybackPaused,
	}
}

func (p *Player) Resume() {
	p.paused = false
	p.Notifications <- PlaybackNotification{
		Event: PlaybackResumed,
	}
}

func (p *Player) Stop() {
	p.completed <- true
	p.completed = make(chan bool)
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
