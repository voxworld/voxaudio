package voxaudio

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
)

var _ = godotenv.Load()

// TestIntegratedRealtime Integration test - End-to-end test using real OpenAI API and real devices
func TestIntegratedRealtime(t *testing.T) {
	// Get API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Get API model
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "whisper"
		t.Logf("OPENAI_MODEL not specified, using default: %s", model)
	}

	// Create session
	fmt.Println("Creating session...")
	session, err := NewSession(apiKey, model, "English", "alloy")
	assert.NoError(t, err, "Failed to create session")
	defer session.Stop()

	// Get test device
	selectedDevice := os.Getenv("TEST_AUDIO_DEVICE")
	if selectedDevice == "" {
		t.Fatal("TEST_AUDIO_DEVICE environment variable is required")
		return
	}
	fmt.Printf("Selected audio device: %s\n", selectedDevice)

	// Set test duration
	testDuration := 5 * time.Minute
	doneSignal := make(chan struct{})

	// Register data channel event listener
	dcOpenedSignal := make(chan struct{})
	var dcOpened bool

	session.dc.OnOpen(func() {
		dcOpened = true
		fmt.Println("[Test] Data channel opened")
		close(dcOpenedSignal)
	})

	// Register message receiver - simple message printing
	var receivedMsgCount int
	session.dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		receivedMsgCount++

		// Only print first few messages and every 20th message
		if receivedMsgCount <= 3 || receivedMsgCount%20 == 0 {
			fmt.Printf("[Test] Received message #%d: %s\n", receivedMsgCount, string(msg.Data))
		}
	})

	// Establish WebRTC connection
	fmt.Println("[Step 1] Establishing WebRTC connection...")
	err = session.Conn()
	assert.NoError(t, err, "Failed to establish WebRTC connection")

	// Register audio track
	fmt.Println("[Step 2] Registering audio track...")
	session.RegisterLocalTrack()

	// Wait for connection to be ready
	fmt.Println("[Step 3] Waiting for data channel to open... (this may take 30 seconds or longer)")

	// Periodically check and report WebRTC connection status
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Set longer timeout - 30 seconds
	timeout := time.After(30 * time.Second)

waitLoop:
	for {
		select {
		case <-dcOpenedSignal:
			fmt.Println("[Step 3] Data channel is ready")
			break waitLoop
		case <-ticker.C:
			// Periodically report status
			fmt.Printf("WebRTC connection state: %s, ICE connection state: %s, Data channel state: %s\n",
				session.pc.ConnectionState().String(),
				session.pc.ICEConnectionState().String(),
				session.dc.ReadyState().String())
		case <-timeout:
			t.Log("Timeout waiting for data channel to open - attempting to continue test")
			break waitLoop
		}
	}

	// Try to start capture even if data channel is not open
	// Some environments might have issues, but we can still try
	fmt.Printf("[Step 4] Starting audio capture from device '%s'...\n", selectedDevice)
	err = session.Start(selectedDevice)
	assert.NoError(t, err, "Failed to start audio capture")

	// Run for a while
	fmt.Printf("[Step 5] Starting real-time communication with AI (duration: %v)...\n", testDuration)
	fmt.Println("Please speak into the microphone, AI will respond in real-time...")

	// Set test end time
	time.AfterFunc(testDuration, func() {
		close(doneSignal)
	})

	// Wait for test to complete
	<-doneSignal
	fmt.Println("[Step 6] Test time reached, stopping session...")

	// Stop session
	session.Stop()

	// Report test results
	fmt.Println("\n============ Test Summary ============")
	fmt.Printf("Data channel opened: %v\n", dcOpened)
	fmt.Printf("Messages received: %d\n", receivedMsgCount)
	fmt.Println("======================================")

	fmt.Println("Test completed")
}

// TestRealtimeConnection Test connection with OpenAI Realtime API (without actual audio capture)
func TestRealtimeConnection(t *testing.T) {
	// Skip this test by default unless explicitly specified to run
	if os.Getenv("RUN_CONN_TEST") != "true" {
		t.Skip("Skipping connection test. Set RUN_CONN_TEST=true environment variable to run this test")
	}

	// Get OpenAI API key
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Get API model
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "whisper"
		t.Logf("OPENAI_MODEL not specified, using default: %s", model)
	}

	// Create session
	t.Log("Creating session...")
	session, err := NewSession(key, model, "English", "alloy")
	assert.NoError(t, err)
	defer session.Stop()

	// Listen for data channel events
	dataChannelReady := make(chan struct{})
	var dcOpened bool

	session.dc.OnOpen(func() {
		dcOpened = true
		t.Log("Data channel opened")
		close(dataChannelReady)
	})

	// Connection state change listener
	session.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		t.Logf("WebRTC connection state changed: %s", state.String())
	})

	// Establish WebRTC connection
	t.Log("Establishing WebRTC connection...")
	err = session.Conn()
	assert.NoError(t, err)

	// Register audio track
	session.RegisterLocalTrack()

	// Periodically report connection status
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Use longer timeout - 30 seconds
	t.Log("Waiting for data channel to open (up to 30 seconds)...")

	// Wait for DataChannel to open
	timeout := time.After(30 * time.Second)

waitLoop:
	for {
		select {
		case <-dataChannelReady:
			t.Log("Data channel opened")
			break waitLoop
		case <-ticker.C:
			// Report status
			t.Logf("WebRTC connection state: %s, ICE connection state: %s, Data channel state: %s",
				session.pc.ConnectionState().String(),
				session.pc.ICEConnectionState().String(),
				session.dc.ReadyState().String())
		case <-timeout:
			if dcOpened {
				t.Log("Data channel opened, but might have missed the signal")
			} else {
				t.Log("Timeout waiting for data channel to open")
			}
			break waitLoop
		}
	}

	// Try to initialize session even if data channel is not open - for test robustness
	if session.dc.ReadyState() == webrtc.DataChannelStateOpen {
		session.initializeSession()
		t.Log("Initialization message sent")

		// Wait a few seconds to observe any response
		time.Sleep(5 * time.Second)
	} else {
		t.Log("Data channel not open, skipping initialization")
	}

	t.Log("WebRTC connection test completed")
}
