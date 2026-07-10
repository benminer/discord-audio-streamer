package audio

// TTS audio conversion and playback utilities.
// Gemini TTS returns raw 16-bit LE PCM at 24kHz mono; Discord voice expects
// 48kHz stereo int16 frames matching the Opus encoder used by Player.

import (
	"bytes"
	"encoding/binary"
	"time"
)

const ttsFrameSamples = 960 * 2 // 20ms frame at 48kHz stereo

// ConvertTTSToDiscord upsamples 24kHz mono PCM to 48kHz stereo by
// duplicating each sample twice for rate doubling and twice more for
// stereo, producing S, S, S, S per input sample S.
func ConvertTTSToDiscord(pcm24kMono []byte) []int16 {
	reader := bytes.NewReader(pcm24kMono)
	samples := make([]int16, 0, (len(pcm24kMono)/2)*4)

	for {
		var sample int16
		if err := binary.Read(reader, binary.LittleEndian, &sample); err != nil {
			break
		}
		samples = append(samples, sample, sample, sample, sample)
	}

	return samples
}

type TTSPlayback struct {
	Samples  []int16 // 48kHz stereo, pre-converted
	Position int     // current read position (in samples, not frames)
}

// ReadFrame reads the next 20ms frame (960*2 samples) into buf, advancing
// Position. Returns false once the frame could not be fully filled, in
// which case any remaining space in buf is zero-filled.
func (t *TTSPlayback) ReadFrame(buf []int16) bool {
	remaining := len(t.Samples) - t.Position
	if remaining <= 0 {
		for i := range buf {
			buf[i] = 0
		}
		return false
	}

	if remaining < ttsFrameSamples {
		n := copy(buf, t.Samples[t.Position:])
		for i := n; i < len(buf); i++ {
			buf[i] = 0
		}
		t.Position += remaining
		return false
	}

	copy(buf, t.Samples[t.Position:t.Position+ttsFrameSamples])
	t.Position += ttsFrameSamples
	return true
}

// Remaining returns how much TTS audio is left to play.
func (t *TTSPlayback) Remaining() time.Duration {
	remainingSamples := len(t.Samples) - t.Position
	if remainingSamples < 0 {
		remainingSamples = 0
	}
	monoSamples := float64(remainingSamples) / 2
	return time.Duration(monoSamples / 48000 * float64(time.Second))
}
