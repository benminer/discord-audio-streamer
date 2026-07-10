package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"

	"beatbot/audio"
	"beatbot/config"

	log "github.com/sirupsen/logrus"
	"google.golang.org/genai"
	"gopkg.in/hraban/opus.v2"
)

func main() {
	log.SetLevel(log.DebugLevel)

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY env var required")
	}

	model := os.Getenv("GEMINI_TTS_MODEL")
	if model == "" {
		model = "gemini-2.5-flash-preview-tts"
	}

	voice := os.Getenv("TTS_VOICE")
	if voice == "" {
		voice = "Kore"
	}

	script := "Hey what's up everyone, your DJ is live and we're about to drop some serious beats tonight."
	if len(os.Args) > 1 {
		script = os.Args[1]
	}

	config.NewConfig()

	fmt.Printf("=== TTS Debug Tool ===\n")
	fmt.Printf("Model:  %s\n", model)
	fmt.Printf("Voice:  %s\n", voice)
	fmt.Printf("Script: %s\n\n", script)

	// Step 1: Call Gemini TTS API directly
	fmt.Println("Step 1: Calling Gemini TTS API...")
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	content := []*genai.Content{{Parts: []*genai.Part{{Text: script}}}}
	resp, err := client.Models.GenerateContent(context.Background(), model, content, &genai.GenerateContentConfig{
		ResponseModalities: []string{"AUDIO"},
		SpeechConfig: &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voice,
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("TTS generation failed: %v", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil ||
		len(resp.Candidates[0].Content.Parts) == 0 {
		log.Fatal("Empty response from Gemini")
	}

	part := resp.Candidates[0].Content.Parts[0]
	if part.InlineData == nil {
		log.Fatal("No InlineData in response part")
	}

	rawData := part.InlineData.Data
	mimeType := part.InlineData.MIMEType

	fmt.Printf("  MIMEType:  %s\n", mimeType)
	fmt.Printf("  Data size: %d bytes\n", len(rawData))
	fmt.Printf("  First 16 bytes: %x\n", rawData[:min(16, len(rawData))])

	isWAV := len(rawData) >= 4 && string(rawData[:4]) == "RIFF"
	fmt.Printf("  Has WAV header: %v\n\n", isWAV)

	// Step 2: Save raw Gemini output
	rawFile := "tts_1_raw_gemini_output.bin"
	os.WriteFile(rawFile, rawData, 0644)
	fmt.Printf("Step 2: Saved raw Gemini output to %s\n", rawFile)

	if isWAV {
		wavFile := "tts_1_raw_gemini_output.wav"
		os.WriteFile(wavFile, rawData, 0644)
		fmt.Printf("  Also saved as %s (it's already WAV!)\n", wavFile)
		fmt.Printf("  >>> Play with: open %s\n\n", wavFile)
	} else {
		// Save as playable WAV (assuming 24kHz mono s16le)
		wavFile := "tts_2_raw_as_24k_mono.wav"
		writeWAV(wavFile, rawData, 1, 24000)
		fmt.Printf("  Saved as 24kHz mono WAV: %s\n", wavFile)
		fmt.Printf("  >>> Play with: open %s\n\n", wavFile)

		// Also try as 48kHz mono in case that's the actual rate
		wavFile48 := "tts_2b_raw_as_48k_mono.wav"
		writeWAV(wavFile48, rawData, 1, 48000)
		fmt.Printf("  Also saved as 48kHz mono WAV: %s (in case rate is wrong)\n", wavFile48)
		fmt.Printf("  >>> Play with: open %s\n\n", wavFile48)
	}

	// Step 3: Run through FFmpeg conversion (our ConvertTTSToDiscord)
	fmt.Println("Step 3: Running FFmpeg conversion (24kHz mono → 48kHz stereo)...")
	samples, convErr := audio.ConvertTTSToDiscord(rawData)
	if convErr != nil {
		fmt.Printf("  FFmpeg conversion FAILED: %v\n\n", convErr)
		fmt.Println("  Trying raw conversion without FFmpeg...")
		// Manual fallback for diagnosis
		samples = manualConvert(rawData)
	} else {
		fmt.Printf("  Success: %d samples (%.1fs at 48kHz stereo)\n", len(samples), float64(len(samples))/2/48000)
	}

	// Save converted output as WAV
	convertedFile := "tts_3_converted_48k_stereo.wav"
	sampleBytes := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(sampleBytes[i*2:], uint16(s))
	}
	writeWAV(convertedFile, sampleBytes, 2, 48000)
	fmt.Printf("  Saved as 48kHz stereo WAV: %s\n", convertedFile)
	fmt.Printf("  >>> Play with: open %s\n\n", convertedFile)

	// Step 4: Opus encode → decode roundtrip
	fmt.Println("Step 4: Opus encode → decode roundtrip...")
	opusRoundtrip(samples)

	fmt.Println("\n=== Done ===")
	fmt.Println("Listen to each WAV file to find where quality degrades.")
	fmt.Println("If Step 2 sounds bad, the issue is the Gemini API or format assumption.")
	fmt.Println("If Step 2 sounds good but Step 3 sounds bad, the issue is the conversion.")
	fmt.Println("If Step 3 sounds good but Discord sounds bad, the issue is playback/Opus.")
}

func writeWAV(filename string, pcmData []byte, channels, sampleRate int) {
	f, err := os.Create(filename)
	if err != nil {
		log.Errorf("Failed to create %s: %v", filename, err)
		return
	}
	defer f.Close()

	dataSize := len(pcmData)
	bitsPerSample := 16
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	// RIFF header
	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(36+dataSize))
	f.Write([]byte("WAVE"))

	// fmt chunk
	f.Write([]byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(f, binary.LittleEndian, uint16(channels))
	binary.Write(f, binary.LittleEndian, uint32(sampleRate))
	binary.Write(f, binary.LittleEndian, uint32(byteRate))
	binary.Write(f, binary.LittleEndian, uint16(blockAlign))
	binary.Write(f, binary.LittleEndian, uint16(bitsPerSample))

	// data chunk
	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, uint32(dataSize))
	f.Write(pcmData)
}

func manualConvert(rawData []byte) []int16 {
	reader := bytes.NewReader(rawData)
	samples := make([]int16, 0, (len(rawData)/2)*4)
	for {
		var sample int16
		if err := binary.Read(reader, binary.LittleEndian, &sample); err != nil {
			break
		}
		samples = append(samples, sample, sample, sample, sample)
	}
	return samples
}

func opusRoundtrip(samples []int16) {
	// Try to use the same opus encoder configuration as the main player
	encoder, err := opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		fmt.Printf("  Opus encoder creation failed: %v\n", err)
		return
	}
	encoder.SetComplexity(10)
	encoder.SetBitrate(64000)

	pcmBytes := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(s))
	}

	// We need to feed the encoder frame by frame to match the main player's behavior
	opusData := bytes.Buffer{}
	frameBuf := make([]int16, 960*2)
	opusBuf := make([]byte, 960*4)

	for i := 0; i < len(samples); i += 960 * 2 {
		end := i + 960*2
		if end > len(samples) {
			end = len(samples)
		}
		copy(frameBuf, samples[i:end])
		if end-i < 960*2 {
			for j := end - i; j < 960*2; j++ {
				frameBuf[j] = 0
			}
		}

		encoded, err := encoder.Encode(frameBuf, opusBuf)
		if err != nil {
			fmt.Printf("  Opus encode failed: %v\n", err)
			return
		}
		opusData.Write(opusBuf[:encoded])
	}
	fmt.Printf("  Opus encoded: %d bytes\n", opusData.Len())

	// Decode back to PCM
	cmd2 := exec.Command("ffmpeg",
		"-f", "opus",
		"-i", "pipe:0",
		"-f", "s16le", "-ar", "48000", "-ac", "2",
		"pipe:1",
	)
	cmd2.Stdin = bytes.NewReader(opusData.Bytes())
	decodedPCM, err := cmd2.Output()
	if err != nil {
		fmt.Printf("  Opus decode failed: %v\n", err)
		return
	}

	outFile := "tts_4_opus_roundtrip_48k_stereo.wav"
	writeWAV(outFile, decodedPCM, 2, 48000)
	fmt.Printf("  Saved Opus roundtrip as WAV: %s\n", outFile)
	fmt.Printf("  >>> Play with: open %s\n", outFile)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
