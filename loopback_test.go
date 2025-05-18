package voxaudio

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

func saveToWav(path string, samplesChan <-chan []float32, sampleRate, numChannels int) error {
	// 1. Create output file
	outFile, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create WAV file: %w", err)
	}
	defer outFile.Close()

	// 2. Initialize WAV encoder: 16-bit, PCM, multi-channel
	encoder := wav.NewEncoder(outFile, sampleRate, 16, numChannels, 1) // PCM encoding mode
	defer encoder.Close()

	// Calculate total sample count to pre-allocate buffer
	// A reasonable size to avoid frequent memory allocation
	var samplesBuffer [][]float32
	for raw := range samplesChan {
		samplesBuffer = append(samplesBuffer, raw)
	}

	// Ensure all sample blocks are the same size to avoid jitter
	bufferSize := 0
	if len(samplesBuffer) > 0 {
		// Use first sample block size as standard
		bufferSize = len(samplesBuffer[0])
	} else {
		return fmt.Errorf("no audio samples received")
	}

	// Create uniformly sized PCM buffer
	buf := &audio.IntBuffer{
		Data:           make([]int, bufferSize),
		Format:         &audio.Format{SampleRate: sampleRate, NumChannels: numChannels},
		SourceBitDepth: 16,
	}

	// Apply slight smoothing to reduce potential noise and jitter
	// Create a simple moving average filter
	const smoothFactor = 0.2 // Smoothing factor, between 0-1, larger means more smoothing
	var prevSample float32 = 0

	for _, raw := range samplesBuffer {
		// Ensure data doesn't overflow
		processLength := len(raw)
		if processLength > bufferSize {
			processLength = bufferSize
		}

		// Use simple moving average to smooth audio data
		for i := 0; i < processLength; i++ {
			// Apply smoothing
			smoothed := raw[i]*(1-smoothFactor) + prevSample*smoothFactor
			prevSample = smoothed

			// Clipping
			if smoothed > 1.0 {
				smoothed = 1.0
			} else if smoothed < -1.0 {
				smoothed = -1.0
			}

			// Convert to 16-bit integer
			buf.Data[i] = int(smoothed * 32767) // Scale to int16 range
		}

		// Write to WAV file
		if err := encoder.Write(buf); err != nil {
			return fmt.Errorf("failed to write WAV data: %w", err)
		}
	}

	// 4. Complete and close encoder (updates file header)
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close WAV encoder: %w", err)
	}
	return nil
}

func TestLoopbackRecorder_Start(t *testing.T) {
	rec, err := NewLoopbackRecorder()
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Stop()

	// 2. List devices (optional)
	apis, err := rec.ListDevices()
	if err != nil {
		t.Fatal(err)
	}

	// Find specified device and record its sample rate and channel count
	var deviceName string = "UGREEN CM564 USB Audio "
	var sampleRate float64 = 48000
	var channelCount int = 1

	fmt.Println("Available audio devices:")
	for _, api := range apis {
		fmt.Printf("API: %s\n", api.Name)
		for idx, dev := range api.Devices {
			fmt.Printf("  [%d] %d %s (in:%d out:%d, rate:%.0f)\n",
				idx, dev.Index, dev.Name, dev.MaxInputChannels, dev.MaxOutputChannels, dev.DefaultSampleRate)

			// Record sample rate and channel count when specified device is found
			if dev.Name == deviceName {
				sampleRate = dev.DefaultSampleRate
				channelCount = dev.MaxInputChannels
				t.Logf("Selected device: %s, Sample rate: %.0f, Channels: %d", deviceName, sampleRate, channelCount)
			}
		}
	}

	// Create a buffer channel to collect samples
	sampleBuffer := make([][]float32, 0, 600) // Pre-allocate space for 10 seconds of audio
	doneCh := make(chan struct{})
	var soundLevel float32
	var sampleCount int64
	lastLog := time.Now()
	// Set up a goroutine to collect samples
	go func() {
		for samples := range rec.Samples {
			// Create sample copy to avoid referencing the same underlying array
			sampleCopy := make([]float32, len(samples))
			copy(sampleCopy, samples)
			sampleBuffer = append(sampleBuffer, sampleCopy)
			soundLevel = 0
			for _, sample := range samples {
				if absFloat32(sample) > soundLevel {
					soundLevel = absFloat32(sample)
				}
			}
			sampleCount += int64(len(samples))

			// Log once per second to avoid excessive logging
			if time.Since(lastLog) > time.Second {
				durationSeconds := float64(sampleCount) / float64(sampleRate)
				soundStatus := "Silent"
				if soundLevel > 0 {
					soundStatus = fmt.Sprintf("Sound detected (level: %.2f)", soundLevel)
				}
				fmt.Printf("[Audio] Captured: %.1f seconds of audio (%d samples) - %s\n",
					durationSeconds, sampleCount, soundStatus)
				lastLog = time.Now()
			}
		}
		close(doneCh)
	}()

	// Start recording
	t.Logf("Starting recording from device: %s", deviceName)
	if err := rec.Start(deviceName); err != nil {
		t.Fatal(err)
	}

	// Record for 10 seconds
	time.Sleep(10 * time.Second)

	// Stop recording
	t.Logf("Stopping recording...")
	rec.Stop()

	// Wait for sample collection to complete
	<-doneCh

	t.Logf("Collected %d audio sample blocks", len(sampleBuffer))
	if len(sampleBuffer) == 0 {
		t.Fatal("No audio samples collected")
	}

	// Now that we have all samples, create a new channel to pass to saveToWav
	sampleCh := make(chan []float32, len(sampleBuffer))
	go func() {
		for _, samples := range sampleBuffer {
			sampleCh <- samples
		}
		close(sampleCh)
	}()

	// Save as WAV file - using device's actual sample rate and channel count
	outFileName := "test.wav"
	t.Logf("Saving WAV file, Sample rate: %.0f, Channels: %d", sampleRate, channelCount)
	if err := saveToWav(outFileName, sampleCh, int(sampleRate), channelCount); err != nil {
		t.Fatalf("Failed to save WAV file: %v", err)
	}

	t.Logf("Successfully recorded and saved 10 seconds of audio to %s", outFileName)
}
