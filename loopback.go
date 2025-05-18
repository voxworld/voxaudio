package voxaudio

import (
	"fmt"
	"sync"

	"github.com/gordonklaus/portaudio"
)

// Helper function: Check if string contains substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// LoopbackRecorder captures audio samples from specified loopback device.
type LoopbackRecorder struct {
	mu       sync.Mutex
	stream   *portaudio.Stream
	Samples  chan []float32
	isClosed bool // Add flag to track if channel is closed
}

// NewLoopbackRecorder initializes PortAudio and returns an instance.
func NewLoopbackRecorder() (*LoopbackRecorder, error) {
	if err := SafePortAudioInit(); err != nil {
		return nil, err
	}
	return &LoopbackRecorder{Samples: make(chan []float32, 1024), isClosed: false}, nil
}

// ListDevices lists all PortAudio devices and their indices.
func (r *LoopbackRecorder) ListDevices() ([]*portaudio.HostApiInfo, error) {
	apis, err := portaudio.HostApis() // Enumerate all Host APIs (e.g., CoreAudio)
	if err != nil {
		return nil, err
	}
	return apis, nil
}

// Start opens loopback input stream based on device name and begins capture.
func (r *LoopbackRecorder) Start(deviceName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Enumerate all devices, look for input device with matching name
	var selected *portaudio.DeviceInfo
	apis, err := portaudio.HostApis()
	if err != nil {
		return err
	}

	// Exact device name match
	for _, api := range apis {
		for _, dev := range api.Devices {
			if dev.MaxInputChannels > 0 && dev.Name == deviceName {
				selected = dev
				break
			}
		}
		if selected != nil {
			break
		}
	}

	// If no exact match found, try partial match (input device names usually contain relevant information)
	if selected == nil && deviceName != "" {
		for _, api := range apis {
			for _, dev := range api.Devices {
				if dev.MaxInputChannels > 0 && contains(dev.Name, deviceName) {
					selected = dev
					fmt.Printf("Found partial match device: %s\n", dev.Name)
					break
				}
			}
			if selected != nil {
				break
			}
		}
	}

	// If device still not found, return error
	if selected == nil {
		return fmt.Errorf("specified input device not found: %s", deviceName)
	}

	// Optimized stream parameters to reduce jitter and distortion
	// Use smaller frame buffer size to reduce latency, but not too small to avoid crackling
	const framesPerBuffer = 512 // Small value to reduce latency, but large enough to avoid frame drops

	// Use low latency settings instead of high latency configuration
	params := portaudio.LowLatencyParameters(selected, nil)
	params.Input.Channels = selected.MaxInputChannels
	params.SampleRate = selected.DefaultSampleRate
	params.FramesPerBuffer = framesPerBuffer

	// Open stream and set callback
	stream, err := portaudio.OpenStream(params, func(input []float32) {
		// Use independent copy instead of shared slice
		sampleCopy := make([]float32, len(input))
		copy(sampleCopy, input)

		// Try to send samples, discard if channel is full (avoid blocking)
		select {
		case r.Samples <- sampleCopy:
			// Successfully sent
		default:
			// Channel is full, discard sample to avoid blocking (this should rarely happen)
			fmt.Println("Warning: Sample channel is full, discarding one frame")
		}
	})

	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	r.stream = stream

	// Start stream and check for errors
	if err := r.stream.Start(); err != nil {
		r.stream.Close()
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Printf("Successfully started capturing device: %s (Sample rate: %.0f Hz, Channels: %d)\n",
		selected.Name, selected.DefaultSampleRate, selected.MaxInputChannels)

	return nil
}

// Stop stops capture and releases resources.
func (r *LoopbackRecorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stream != nil {
		r.stream.Stop()
		r.stream.Close()
		r.stream = nil
	}

	// Safely close channel, avoid duplicate closure
	if !r.isClosed {
		close(r.Samples)
		r.isClosed = true
	}

	SafePortAudioTerminate()
	return nil
}
