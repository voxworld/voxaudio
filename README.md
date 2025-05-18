[English](README.md) | [简体中文](README_zh.md)

# VoxAudio

![Version](https://img.shields.io/github/v/release/voxworld/voxaudio?style=flat-square)
![License](https://img.shields.io/github/license/voxworld/voxaudio?style=flat-square)

> [!IMPORTANT]
> This project is currently in research phase, primarily focused on exploring the applications of OpenAI Realtime API in real-time speech translation. Code and features may change at any time, and it is not recommended for production use. Issues and Pull Requests are welcome to help improve the project.

VoxAudio is a real-time speech translation tool based on OpenAI Realtime API. It captures audio input in real-time, translates it through OpenAI's API, and outputs the translated speech.

## Research Goals

- Explore applications of OpenAI Realtime API in real-time speech translation
- Research best practices for low-latency speech processing
- Test the impact of different audio devices and sample rates on translation quality
- Capture local audio stream input, transmit to OpenAI servers via WebRTC, and perform real-time translation
- Optimize real-time speech processing performance

## Core Features

- Real-time audio capture and processing
- Real-time speech translation using OpenAI Realtime API
- Support for multiple language pairs
- Low-latency real-time speech processing
- Custom audio device selection
- Audio loopback testing functionality
- WAV file export for verification

## Technical Features

- WebRTC for real-time communication
- Support for multiple audio formats and sample rates
- Audio device management and selection
- Comprehensive test suite
- Audio data smoothing and noise reduction

## Requirements

- Go 1.16 or higher
- OpenAI API key
- Audio input device (microphone)

## Configuration

1. Set environment variables:

```bash
export OPENAI_API_KEY="your-api-key"
export OPENAI_MODEL="gpt-4o-mini-realtime-preview"
export TEST_AUDIO_DEVICE="your-audio-device-name"  # for testing
```

2. Install dependencies:

```bash
go mod download
```

## Usage Example

```go
// Create new session
session, err := NewSession(apiKey, model, "English", "alloy")
if err != nil {
    log.Fatal(err)
}
defer session.Stop()

// Establish WebRTC connection
err = session.Conn()
if err != nil {
    log.Fatal(err)
}

// Register audio track
session.RegisterLocalTrack()

// Start audio capture
err = session.Start(deviceName)
if err != nil {
    log.Fatal(err)
}
```

## Testing

The project includes several test cases:

- `TestIntegratedRealtime`: End-to-end integration test
- `TestRealtimeConnection`: WebRTC connection test
- `TestLoopbackRecorder`: Audio loopback test

Run tests:

```bash
go test -v
```

## Project Status

This project is currently in research phase, focusing on:

- Real-time speech translation accuracy and latency optimization
- Translation quality comparison between different language pairs
- Audio processing algorithm improvements
- WebRTC connection stability enhancement

Issues and Pull Requests are welcome, especially for:
- New language pair support
- Audio processing algorithm optimization
- Performance improvement suggestions
- User experience feedback

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## License

[MIT License](LICENSE)
