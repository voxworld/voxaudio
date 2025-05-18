package voxaudio

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/gordonklaus/portaudio"
	"github.com/hraban/opus"
	"github.com/pion/webrtc/v4"
)

// Session manages Loopback capture and Realtime WebRTC connection
type Session struct {
	pc *webrtc.PeerConnection
	dc *webrtc.DataChannel
	// audioTrack   *webrtc.TrackLocalStaticSample
	stopCh       chan struct{}
	model        string
	ephemeralKey string
	recorder     *LoopbackRecorder
	systemPrompt string
	targetLang   string
	voice        string
	audioDir     string // Directory for saving audio files
}

const (
	realtime_url  = "https://api.openai.com/v1/realtime"
	sampleRate    = 48000
	channels      = 1
	frameSize     = 960
	opusFrameSize = 960     // 20ms @ 48kHz
	maxDataBytes  = 1000    // Large enough buffer for Opus encoded data
	defaultVoice  = "alloy" // Default voice
)

// SessionConfig configures session parameters
type SessionConfig struct {
	TargetLang   string // Target language, e.g., "English", "Chinese", "Japanese", etc.
	SystemPrompt string // Custom system prompt
	Voice        string // Voice synthesis voice, e.g., "alloy", "echo", "fable", "onyx", "alloy", "shimmer"
}

// NewSession creates and initializes a Session
func NewSession(ephemeralKey, model, targetLang, voice string) (*Session, error) {

	if voice == "" {
		voice = defaultVoice
	}

	if targetLang == "" {
		targetLang = "English"
	}

	// Create audio save directory
	audioDir := filepath.Join(os.TempDir(), "voxaudio")
	if err := os.MkdirAll(audioDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audio save directory: %w", err)
	}

	// 1. PeerConnection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{
					"stun:stun.l.google.com:19302",
					"stun:stun.l.google.com:5349",
					"stun:stun1.l.google.com:3478",
					"stun:stun1.l.google.com:5349",
					"stun:stun2.l.google.com:19302",
					"stun:stun2.l.google.com:5349",
					"stun:stun3.l.google.com:3478",
					"stun:stun3.l.google.com:5349",
					"stun:stun4.l.google.com:19302",
					"stun:stun4.l.google.com:5349",
				},
			},
		},
	}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}
	// 2. Audio Track
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio", "pion",
	)
	if err != nil {
		return nil, err
	}
	if _, err := pc.AddTrack(track); err != nil {
		return nil, err
	}
	// 3. DataChannel
	dc, err := pc.CreateDataChannel("oai-events", nil)
	if err != nil {
		return nil, err
	}

	// 4. Initialize LoopbackRecorder
	recorder, err := NewLoopbackRecorder()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audio capturer: %w", err)
	}

	return &Session{
		pc:           pc,
		dc:           dc,
		stopCh:       make(chan struct{}),
		ephemeralKey: ephemeralKey,
		model:        model,
		recorder:     recorder,
		voice:        voice,
		targetLang:   targetLang,
		audioDir:     audioDir,
	}, nil
}

// Build default translation prompt
func (s *Session) buildTranslationPrompt() string {
	// Build default translation prompt, explicitly indicating voice output is needed
	return fmt.Sprintf(
		"You are a real-time simultaneous interpreter. Please translate the user's speech into %s while maintaining the original speech rhythm, tone, emotion, and characteristics."+
			"When translating, accurately convey the original meaning while making the translated language sound natural and fluent, conforming to %s expression habits."+
			"Please only output the translation result, do not add any additional explanations or prefixes like 'translation:'. Please ensure to generate voice output.",
		s.targetLang, s.targetLang)
}

func (s *Session) Conn() error {
	// Add state change listener
	s.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		fmt.Printf("[WebRTC] Connection state changed: %s\n", state.String())
	})

	// Data channel listener
	s.dc.OnOpen(func() {
		fmt.Println("[DataChannel] Opened")
		// Note: initializeSession is not called here, but in Start
		// This ensures audio capture is ready
	})

	s.dc.OnError(func(err error) {
		fmt.Printf("[DataChannel] Error: %v\n", err)
	})

	s.dc.OnClose(func() {
		fmt.Println("[DataChannel] Closed")
	})

	// Create offer
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("failed to create offer: %w", err)
	}

	// Set local description
	if err := s.pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("failed to set local description: %w", err)
	}

	fmt.Println("[WebRTC] Local SDP set, sending to OpenAI...")

	// Request WebRTC answer
	url := realtime_url
	req, err := http.NewRequest("POST", fmt.Sprintf("%s?model=%s", url, s.model), bytes.NewReader([]byte(offer.SDP)))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.ephemeralKey))
	req.Header.Set("Content-Type", "application/sdp")

	// Set longer timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read SDP answer
	ansSDP, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read answer: %w", err)
	}

	fmt.Println("[WebRTC] Received OpenAI SDP answer, setting remote description...")

	// Set remote description
	if err := s.pc.SetRemoteDescription(
		webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(ansSDP)},
	); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	fmt.Println("[WebRTC] Remote SDP set, waiting for connection to establish...")

	return nil
}

func (s *Session) RegisterLocalTrack() {
	s.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			go s.localTrack(track)
		}
	})
}

func (s *Session) localTrack(track *webrtc.TrackRemote) error {
	// Create Opus decoder
	decoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return err
	}

	// Initialize oto context
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE, // Use explicit format
	})
	if err != nil {
		return err
	}
	<-ready

	// Create audio buffer
	audioBuffer := bytes.NewBuffer(make([]byte, 0, 1024*1024))

	// Create player
	player := ctx.NewPlayer(audioBuffer)
	defer player.Close()

	// Start playback
	player.Play()

	buffer := make([]int16, frameSize)
	pcm := make([]int16, frameSize)      // Decoded PCM data
	samples := make([]byte, frameSize*2) // Temporary buffer for conversion

	// Create audio file
	timestamp := time.Now().Format("20060102-150405")
	audioFileName := filepath.Join(s.audioDir, fmt.Sprintf("openai-audio-%s.wav", timestamp))
	audioFile, err := os.Create(audioFileName)
	if err != nil {
		fmt.Printf("[Audio] Failed to create audio file: %v\n", err)
		// Continue execution even if file creation fails
	} else {
		defer audioFile.Close()
		// Write WAV file header
		writeWavHeader(audioFile, sampleRate, 1, 16)
		fmt.Printf("[Audio] Starting to save audio to file: %s\n", audioFileName)
	}

	// Add local stop signal
	done := make(chan struct{})

	// Listen for session stop signal to ensure exit this goroutine when session ends
	go func() {
		<-s.stopCh
		close(done)
	}()

	fmt.Println("[Audio] Starting to receive and play OpenAI returned audio...")

	var packetCount int
	lastLog := time.Now()
	var totalAudioBytes int64
	var hasSoundData bool // Record whether valid audio data is received

	for {
		select {
		case <-done:
			// Update data size in WAV file header before exiting
			if audioFile != nil {
				updateWavHeader(audioFile, totalAudioBytes)
				fmt.Printf("[Audio] Saved %.2f seconds of audio to file (valid audio: %v)\n",
					float64(totalAudioBytes)/float64(sampleRate*2), hasSoundData)
			}
			return nil // Graceful exit
		default:
			// Read RTP packet
			rtp, _, err := track.ReadRTP()
			if err != nil {
				// Check if it's due to connection closure (EOF) or other serious error
				if err == io.EOF || strings.Contains(err.Error(), "closed") {
					if audioFile != nil {
						updateWavHeader(audioFile, totalAudioBytes)
						fmt.Printf("[Audio] Saved %.2f seconds of audio to file (valid audio: %v)\n",
							float64(totalAudioBytes)/float64(sampleRate*2), hasSoundData)
					}
					return nil // Connection closed, exit directly
				}
				// Other temporarily error, continue to try
				continue
			}

			// Check if it's the first time receiving audio packet
			if packetCount == 0 {
				fmt.Printf("[Audio] First time receiving audio packet, answer length: %d bytes\n", len(rtp.Payload))
			}

			// Decode Opus data
			n, err := decoder.Decode(rtp.Payload, pcm)
			if err != nil {
				// If it's a decoding error, record but continue
				fmt.Printf("Decoding audio failed: %v\n", err)
				continue
			}

			// Copy decoded data to playback buffer
			copy(buffer, pcm[:n])

			// Check if it's valid audio (not all 0 or close to 0)
			hasSound := false
			for i := 0; i < n; i++ {
				if abs(buffer[i]) > 0 { // Use threshold detection for non-silent
					hasSound = true
					hasSoundData = true
					break
				}
			}

			// Convert int16 data to byte sequence
			for i := 0; i < n; i++ {
				samples[i*2] = byte(buffer[i])
				samples[i*2+1] = byte(buffer[i] >> 8)
			}

			// Write audio data to buffer
			audioBuffer.Write(samples[:n*2])

			// Save audio data to file
			if audioFile != nil {
				bytesWritten, err := audioFile.Write(samples[:n*2])
				if err != nil {
					fmt.Printf("[Audio] Failed to save audio data: %v\n", err)
				} else {
					totalAudioBytes += int64(bytesWritten)
				}
			}

			// Update packet count
			packetCount++

			// Record log every second to avoid too many logs
			if time.Since(lastLog) > time.Second {
				soundStatus := ""
				if hasSound {
					soundStatus = " (sound)"
				} else {
					soundStatus = " (no sound)"
				}
				fmt.Printf("[Audio] Played OpenAI audio packet: %d packets (about %.1f seconds of audio)%s\n",
					packetCount, float64(packetCount*frameSize)/float64(sampleRate), soundStatus)
				lastLog = time.Now()
			}
		}
	}
}

// Write WAV file header
func writeWavHeader(file *os.File, sampleRate int, numChannels, bitsPerSample int) error {
	// RIFF header
	_, err := file.WriteString("RIFF")
	if err != nil {
		return err
	}

	// File size (will be updated before closing file)
	err = binary.Write(file, binary.LittleEndian, uint32(0))
	if err != nil {
		return err
	}

	// WAVE format
	_, err = file.WriteString("WAVE")
	if err != nil {
		return err
	}

	// fmt subchunk
	_, err = file.WriteString("fmt ")
	if err != nil {
		return err
	}

	// Subchunk1 size
	err = binary.Write(file, binary.LittleEndian, uint32(16))
	if err != nil {
		return err
	}

	// Audio format (PCM=1)
	err = binary.Write(file, binary.LittleEndian, uint16(1))
	if err != nil {
		return err
	}

	// Number of channels
	err = binary.Write(file, binary.LittleEndian, uint16(numChannels))
	if err != nil {
		return err
	}

	// Sample rate
	err = binary.Write(file, binary.LittleEndian, uint32(sampleRate))
	if err != nil {
		return err
	}

	// Byte rate = SampleRate * NumChannels * BitsPerSample/8
	err = binary.Write(file, binary.LittleEndian, uint32(sampleRate*numChannels*bitsPerSample/8))
	if err != nil {
		return err
	}

	// Block alignment = NumChannels * BitsPerSample/8
	err = binary.Write(file, binary.LittleEndian, uint16(numChannels*bitsPerSample/8))
	if err != nil {
		return err
	}

	// Bit depth
	err = binary.Write(file, binary.LittleEndian, uint16(bitsPerSample))
	if err != nil {
		return err
	}

	// Data subchunk
	_, err = file.WriteString("data")
	if err != nil {
		return err
	}

	// Subchunk2 size (will be updated before closing file)
	err = binary.Write(file, binary.LittleEndian, uint32(0))
	if err != nil {
		return err
	}

	return nil
}

// Update WAV file header with size information
func updateWavHeader(file *os.File, dataSize int64) error {
	// Update data subchunk size
	_, err := file.Seek(40, 0)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, uint32(dataSize))
	if err != nil {
		return err
	}

	// Update RIFF chunk size (file total size minus 8-byte RIFF header)
	_, err = file.Seek(4, 0)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, uint32(dataSize+36)) // 36 = Standard WAV header size-8
	if err != nil {
		return err
	}

	return nil
}

// Start starts capturing and pushing audio
func (s *Session) Start(deviceName string) error {
	// First start device audio capture to ensure ready before data channel initialization
	if err := s.recorder.Start(deviceName); err != nil {
		return fmt.Errorf("failed to start audio capture: %w", err)
	}

	fmt.Printf("[Audio] Starting to capture device audio: %s\n", deviceName)

	// Audio capture and push
	go func() {
		defer s.recorder.Stop()

		var sampleCount int64
		var bytesSent int64
		lastLog := time.Now()
		var hasSoundInput bool // Track whether sound input is detected
		var soundLevel float32 // Record sound level

		for {
			select {
			case <-s.stopCh:
				if !hasSoundInput {
					fmt.Println("[Warning] No valid microphone audio input detected throughout the session")
				}
				return
			case samples, ok := <-s.recorder.Samples:
				if !ok {
					return // Channel closed
				}

				// Detect sound level
				soundLevel = 0
				for _, sample := range samples {
					if absFloat32(sample) > soundLevel {
						soundLevel = absFloat32(sample)
					}
				}

				// Check if there's sound (non-silent)
				if soundLevel > 0 { // Threshold adjustable
					hasSoundInput = true
				}

				// If data channel is not ready, skip sending
				if s.dc == nil || s.dc.ReadyState() != webrtc.DataChannelStateOpen {
					continue
				}

				// Convert float32 samples to byte sequence
				pcmBytes := make([]byte, len(samples)*2) // 16-bit PCM = 2 bytes/sample
				for i, sample := range samples {
					// Limit value to [-1.0, 1.0] range
					if sample > 1.0 {
						sample = 1.0
					} else if sample < -1.0 {
						sample = -1.0
					}

					// Convert to int16, then split into two bytes
					sampleInt := int16(sample * 32767.0)   // Convert to int16 range
					pcmBytes[i*2] = byte(sampleInt)        // Low byte
					pcmBytes[i*2+1] = byte(sampleInt >> 8) // High byte
				}

				// Base64 encode
				audioB64 := base64.StdEncoding.EncodeToString(pcmBytes)

				// Use input_audio_buffer.append for actual real-time audio stream
				evt := map[string]interface{}{
					"type":  "input_audio_buffer.append",
					"audio": audioB64,
				}

				msg, _ := json.Marshal(evt)
				if err := s.dc.SendText(string(msg)); err != nil {
					fmt.Printf("[Audio] Failed to send audio data: %v\n", err)
				}

				// Update statistics
				sampleCount += int64(len(samples))
				bytesSent += int64(len(msg))

				// Record log every second to avoid too many logs
				if time.Since(lastLog) > time.Second {
					durationSeconds := float64(sampleCount) / float64(sampleRate)
					soundStatus := "silent"
					if soundLevel > 0 {
						soundStatus = fmt.Sprintf("sound (level: %.2f)", soundLevel)
					}
					fmt.Printf("[Audio] Uploaded: %.1f seconds of audio (%d samples, %.2f KB) - %s\n",
						durationSeconds, sampleCount, float64(bytesSent)/1024, soundStatus)
					lastLog = time.Now()
				}
			}
		}
	}()

	// Register data channel open event handler
	if s.dc.ReadyState() != webrtc.DataChannelStateOpen {
		s.dc.OnOpen(func() {
			fmt.Println("[DataChannel] Opened, immediately initialize session")
			s.initializeSession()
		})
	} else {
		// If channel is already opened, immediately initialize
		fmt.Println("[DataChannel] Already in opened state, immediately initialize session")
		s.initializeSession()
	}

	return nil
}

// Session initialization logic, extracted from Start method
func (s *Session) initializeSession() {
	fmt.Println("[Session] Initializing session...")

	// No need to cancel response first because there may be no active response
	// Just set system prompt directly

	// Set system prompt - Explicitly indicate translation and voice output
	prompt := s.buildTranslationPrompt()
	if prompt == "" {
		prompt = fmt.Sprintf("Translate to %s and read out loud", s.targetLang)
	}

	// Set voice and instructions
	voiceSettings := map[string]interface{}{
		"voice":        s.voice,
		"instructions": prompt,
	}
	evt := map[string]interface{}{"type": "session.update", "session": voiceSettings}
	b, _ := json.Marshal(evt)
	s.dc.SendText(string(b))

	fmt.Printf("[Session] Sent session settings: voice=%s, target language=%s, sample rate=%d\n",
		s.voice, s.targetLang, sampleRate)

	// Wait for a short period to ensure settings take effect before sending audio
	time.Sleep(100 * time.Millisecond)
}

// Stop stops audio capture and WebRTC connection
func (s *Session) Stop() {
	// First close stop signal channel, this will trigger all goroutines to exit
	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil // Prevent multiple closures
	}

	// Close audio capture
	if s.recorder != nil {
		s.recorder.Stop()
	}

	// Close data channel
	if s.dc != nil && s.dc.ReadyState() == webrtc.DataChannelStateOpen {
		// Try to send close message
		closeMsg := map[string]string{"type": "response.cancel"}
		if msgBytes, err := json.Marshal(closeMsg); err == nil {
			// Ignore send error, try to do it
			_ = s.dc.SendText(string(msgBytes))
		}
		// Close data channel
		_ = s.dc.Close()
	}

	// Wait for a moment for the above operations to complete
	time.Sleep(100 * time.Millisecond)

	// Close PeerConnection
	if s.pc != nil {
		_ = s.pc.Close()
	}
}

// SetTargetLanguage sets target translation language
func (s *Session) SetTargetLanguage(lang string) {
	s.targetLang = lang
}

// SetSystemPrompt directly sets system prompt
func (s *Session) SetSystemPrompt(prompt string) {
	s.systemPrompt = prompt
}

// SetVoice sets voice synthesis voice type
func (s *Session) SetVoice(voice string) {
	s.voice = voice
}

// UpdateSessionSettings updates session settings, such as voice type
// Note: This method is only effective when data channel is opened
func (s *Session) UpdateSessionSettings() error {
	if s.dc == nil || s.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not opened")
	}

	voiceSettings := map[string]string{"voice": s.voice}
	evt := map[string]interface{}{"type": "session.update", "session": voiceSettings}
	b, _ := json.Marshal(evt)
	s.dc.SendText(string(b))
	return nil
}

// UpdateSystemPrompt updates system prompt
// Note: This method is only effective when data channel is opened
func (s *Session) UpdateSystemPrompt() error {
	if s.dc == nil || s.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not opened")
	}

	prompt := s.buildTranslationPrompt()
	if prompt == "" {
		return nil // No prompt to send if none
	}

	promptEvt := map[string]interface{}{
		"type": "session.update",
		"session": map[string]string{
			"instructions": prompt,
		},
	}
	promptJson, _ := json.Marshal(promptEvt)
	s.dc.SendText(string(promptJson))
	return nil
}

// RegisterBlackHoleTrack registers a handler to redirect audio to BlackHole virtual mic
func (s *Session) RegisterBlackHoleTrack() {
	s.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			go s.blackHoleTrack(track)
		}
	})
}

func (s *Session) blackHoleTrack(track *webrtc.TrackRemote) error {
	// Create Opus decoder
	decoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return err
	}

	// Create audio buffer for temporarily storing decoded PCM data
	buffer := make([]int16, frameSize)
	pcm := make([]int16, frameSize) // Decoded PCM data

	// Use portaudio to output to BlackHole
	if err := SafePortAudioInit(); err != nil {
		fmt.Printf("[BlackHole] PortAudio initialization failed: %v\n", err)
		return err
	}
	defer SafePortAudioTerminate()

	// Find BlackHole device
	apis, err := portaudio.HostApis()
	if err != nil {
		fmt.Printf("[BlackHole] Failed to get audio device: %v\n", err)
		return err
	}

	// Find BlackHole output device
	var outputDevice *portaudio.DeviceInfo
	for _, api := range apis {
		for _, dev := range api.Devices {
			if dev.MaxOutputChannels > 0 && strings.Contains(dev.Name, "BlackHole") {
				outputDevice = dev
				fmt.Printf("[BlackHole] Found device: %s (output channels: %d, sample rate: %.0f)\n",
					dev.Name, dev.MaxOutputChannels, dev.DefaultSampleRate)
				break
			}
		}
		if outputDevice != nil {
			break
		}
	}

	if outputDevice == nil {
		fmt.Println("[BlackHole] No BlackHole device found, please ensure BlackHole 2ch is installed")
		return fmt.Errorf("no BlackHole device found")
	}

	// Create receive signal channel for coordinating concurrent access
	audioDataChan := make(chan []float32, 8)

	// Set output parameters
	params := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDevice,
			Channels: channels,
			Latency:  10 * time.Millisecond, // 10ms latency
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	// Create and start output stream
	stream, err := portaudio.OpenStream(params, func(out []float32) {
		select {
		case newData := <-audioDataChan:
			// Copy new data to output buffer
			n := copy(out, newData)
			if n < len(out) {
				// If data is insufficient, fill the remaining part with silence
				for i := n; i < len(out); i++ {
					out[i] = 0
				}
			}
		default:
			// If no data in channel, output silence
			for i := range out {
				out[i] = 0
			}
		}
	})

	if err != nil {
		fmt.Printf("[BlackHole] Failed to open audio stream: %v\n", err)
		return err
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		fmt.Printf("[BlackHole] Failed to start audio stream: %v\n", err)
		return err
	}
	defer stream.Stop()

	// Add local stop signal
	done := make(chan struct{})

	// Listen for session stop signal to ensure exit this goroutine when session ends
	go func() {
		<-s.stopCh
		close(done)
	}()

	fmt.Println("[BlackHole] Starting to receive and redirect OpenAI audio to BlackHole 2ch...")

	var packetCount int
	lastLog := time.Now()
	var hasSoundData bool // Record whether valid audio data is received

	for {
		select {
		case <-done:
			fmt.Printf("[BlackHole] Stopped redirecting audio to BlackHole 2ch (valid audio: %v)\n", hasSoundData)
			return nil // Graceful exit
		default:
			// Read RTP packet
			rtp, _, err := track.ReadRTP()
			if err != nil {
				// Check if it's due to connection closure (EOF) or other serious error
				if err == io.EOF || strings.Contains(err.Error(), "closed") {
					return nil // Connection closed, exit directly
				}
				// Other temporarily error, continue to try
				continue
			}

			// Check if it's the first time receiving audio packet
			if packetCount == 0 {
				fmt.Printf("[BlackHole] First time receiving audio packet, answer length: %d bytes\n", len(rtp.Payload))
			}

			// Decode Opus data
			n, err := decoder.Decode(rtp.Payload, pcm)
			if err != nil {
				// If it's a decoding error, record but continue
				fmt.Printf("[BlackHole] Decoding audio failed: %v\n", err)
				continue
			}

			// Copy decoded data to playback buffer
			copy(buffer, pcm[:n])

			// Check if it's valid audio (not all 0 or close to 0)
			hasSound := false
			for i := 0; i < n; i++ {
				if abs(buffer[i]) > 0 { // Use threshold detection for non-silent
					hasSound = true
					hasSoundData = true
					break
				}
			}

			// Create new output buffer for PortAudio callback
			newBuffer := make([]float32, n*channels)

			// Convert int16 data to float32 for portaudio output
			for i := 0; i < n; i++ {
				// Convert int16 to float32 range [-1.0, 1.0]
				val := float32(buffer[i]) / 32767.0

				// For stereo output, copy to two channels
				for c := 0; c < channels; c++ {
					newBuffer[i*channels+c] = val
				}
			}

			// Non-blocking send to audio channel
			select {
			case audioDataChan <- newBuffer:
				// Successfully sent
			default:
				// Channel is full, discard current data to avoid blocking (only happens when processing too slow)
			}

			// Update packet count
			packetCount++

			// Record log every second to avoid too many logs
			if time.Since(lastLog) > time.Second {
				soundStatus := ""
				if hasSound {
					soundStatus = " (sound)"
				} else {
					soundStatus = " (no sound)"
				}
				fmt.Printf("[BlackHole] Redirect OpenAI audio packet: %d packets (about %.1f seconds of audio)%s\n",
					packetCount, float64(packetCount*frameSize)/float64(sampleRate), soundStatus)
				lastLog = time.Now()
			}
		}
	}
}
