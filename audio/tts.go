package audio

// TTS audio conversion and playback utilities.
// Gemini TTS returns raw 16-bit LE PCM at 24kHz mono; Discord voice expects
// 48kHz stereo int16 frames matching the Opus encoder used by Player.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os/exec"
	"time"

	log "github.com/sirupsen/logrus"
)

const ttsFrameSamples = 960 * 2 // 20ms frame at 48kHz stereo

// ConvertTTSToDiscord resamples TTS audio to 48kHz stereo PCM using FFmpeg.
// Accepts raw 24kHz mono PCM (the default Gemini TTS output) or WAV.
// Falls back to raw PCM input flags if auto-detection fails.
func ConvertTTSToDiscord(ttsAudio []byte) ([]int16, error) {
	if len(ttsAudio) == 0 {
		return nil, fmt.Errorf("empty TTS audio data")
	}

	isWAV := len(ttsAudio) >= 4 && string(ttsAudio[:4]) == "RIFF"

	var cmd *exec.Cmd
	if isWAV {
		log.Debug("TTS audio detected as WAV, using auto-detection")
		cmd = exec.Command("ffmpeg",
			"-i", "pipe:0",
			"-f", "s16le",
			"-ar", "48000",
			"-ac", "2",
			"-loglevel", "error",
			"pipe:1")
	} else {
		log.Debug("TTS audio detected as raw PCM, using explicit format flags")
		cmd = exec.Command("ffmpeg",
			"-f", "s16le",
			"-ar", "24000",
			"-ac", "1",
			"-i", "pipe:0",
			"-f", "s16le",
			"-ar", "48000",
			"-ac", "2",
			"-af", "aresample=48000",
			"-loglevel", "error",
			"pipe:1")
	}

	cmd.Stdin = bytes.NewReader(ttsAudio)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("FFmpeg TTS conversion failed: %w (stderr: %s)", err, stderr.String())
	}

	if len(output) == 0 {
		return nil, fmt.Errorf("FFmpeg produced empty output")
	}

	samples := make([]int16, len(output)/2)
	if err := binary.Read(bytes.NewReader(output), binary.LittleEndian, &samples); err != nil {
		return nil, fmt.Errorf("failed to read FFmpeg output as int16: %w", err)
	}

	log.WithFields(log.Fields{
		"input_bytes":    len(ttsAudio),
		"output_samples": len(samples),
		"duration_ms":    len(samples) / 2 / 48,
		"is_wav":         isWAV,
	}).Debug("TTS audio converted to Discord format")

	return samples, nil
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
