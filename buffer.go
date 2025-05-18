package voxaudio

import "sync"

type AudioBuffer struct {
	buffer []byte
	mu     sync.Mutex
	cond   *sync.Cond
}

func NewAudioBuffer() *AudioBuffer {
	ab := &AudioBuffer{
		buffer: make([]byte, 0, 48000*2), // 1 second of audio data
	}
	ab.cond = sync.NewCond(&ab.mu)
	return ab
}

func (ab *AudioBuffer) Write(data []byte) {
	ab.mu.Lock()
	ab.buffer = append(ab.buffer, data...)
	ab.cond.Signal()
	ab.mu.Unlock()
}

func (ab *AudioBuffer) Read(p []byte) (n int, err error) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	// Wait for data to be available
	for len(ab.buffer) == 0 {
		ab.cond.Wait()
	}

	// Read available data
	n = copy(p, ab.buffer)
	ab.buffer = ab.buffer[n:]
	return n, nil
}
