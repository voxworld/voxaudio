package voxaudio

import (
	"fmt"
	"sync"

	"github.com/gordonklaus/portaudio"
)

var (
	paInitMutex sync.Mutex
	paInitCount int
)

// Safely initialize PortAudio, only actually initializes on first call
func SafePortAudioInit() error {
	paInitMutex.Lock()
	defer paInitMutex.Unlock()

	if paInitCount == 0 {
		if err := portaudio.Initialize(); err != nil {
			return fmt.Errorf("portaudio initialization failed: %w", err)
		}
	}
	paInitCount++
	return nil
}

// Safely terminate PortAudio, only actually terminates when last user calls
func SafePortAudioTerminate() {
	paInitMutex.Lock()
	defer paInitMutex.Unlock()

	paInitCount--
	if paInitCount <= 0 {
		portaudio.Terminate()
		paInitCount = 0 // Ensure it doesn't become negative
	}
}
