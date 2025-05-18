package voxaudio

import (
	"fmt"
	"sync"

	"github.com/gordonklaus/portaudio"
)

// OutputCaptureRecorder 捕获系统音频输出（扬声器）。
type OutputCaptureRecorder struct {
	mu       sync.Mutex
	stream   *portaudio.Stream
	Samples  chan []float32
	isClosed bool // 添加标志来跟踪通道是否已关闭
}

// NewOutputCaptureRecorder 初始化 PortAudio 并返回实例。
func NewOutputCaptureRecorder() (*OutputCaptureRecorder, error) {
	if err := SafePortAudioInit(); err != nil {
		return nil, err
	}
	return &OutputCaptureRecorder{Samples: make(chan []float32, 1024), isClosed: false}, nil
}

// ListDevices 列举所有 PortAudio 设备及其索引。
func (r *OutputCaptureRecorder) ListDevices() ([]*portaudio.HostApiInfo, error) {
	apis, err := portaudio.HostApis()
	if err != nil {
		return nil, err
	}
	return apis, nil
}

// macOS上的输出设备捕获需要特殊处理
// 在macOS上，标准PortAudio无法直接捕获输出设备
// 我们需要使用BlackHole这样的虚拟设备或者系统聚合设备

// Start 启动系统音频输出捕获。
// deviceName: 要捕获的输出设备名称，通常是扬声器或耳机
// 注意：在macOS上，要成功捕获系统输出，您可能需要:
// 1. 安装BlackHole等虚拟音频设备（brew install blackhole-2ch）
// 2. 创建包含系统输出设备的聚合设备
// 3. 将系统音频输出设置为BlackHole或聚合设备
func (r *OutputCaptureRecorder) Start(deviceName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 枚举所有设备
	var selected *portaudio.DeviceInfo
	apis, err := portaudio.HostApis()
	if err != nil {
		return err
	}

	// 1. 首先尝试查找环回设备（如BlackHole）
	for _, api := range apis {
		for _, dev := range api.Devices {
			if contains(dev.Name, "BlackHole") || contains(dev.Name, "Loopback") {
				selected = dev
				break
			}
		}
		if selected != nil {
			break
		}
	}

	// 2. 如果没有找到环回设备，查找指定的设备
	if selected == nil && deviceName != "" {
		for _, api := range apis {
			for _, dev := range api.Devices {
				if dev.MaxInputChannels > 0 && (dev.Name == deviceName || contains(dev.Name, deviceName)) {
					selected = dev
					break
				}
			}
			if selected != nil {
				break
			}
		}
	}

	// 3. 如果仍未找到，尝试使用系统默认输入设备
	if selected == nil {
		defaultHostAPI, err := portaudio.DefaultHostApi()
		if err != nil {
			return fmt.Errorf("获取默认音频API失败: %w", err)
		}
		selected = defaultHostAPI.DefaultInputDevice
	}

	// 如果仍未找到可用设备，报错
	if selected == nil {
		return fmt.Errorf("未找到可用的音频环回设备，请安装BlackHole等虚拟音频设备")
	}

	// 设置输入参数 - 我们使用输入设备来捕获
	channelCount := selected.MaxInputChannels
	if channelCount == 0 {
		return fmt.Errorf("所选设备没有输入通道")
	}

	// 标准设置 - 使用高延迟可靠性更好
	params := portaudio.HighLatencyParameters(selected, nil)
	params.Input.Channels = channelCount
	params.SampleRate = selected.DefaultSampleRate

	// 缓冲区大小计算
	bufferSize := 1024 * channelCount

	// 创建输入缓冲区和处理回调
	in := make([]float32, bufferSize)

	stream, err := portaudio.OpenStream(params, func(input []float32) {
		// 复制输入数据到缓冲区
		copy(in, input)
		// 发送数据副本到通道
		r.Samples <- append([]float32(nil), in...)
	})

	if err != nil {
		return fmt.Errorf("打开流失败: %w", err)
	}
	r.stream = stream

	if err := r.stream.Start(); err != nil {
		r.stream.Close()
		return fmt.Errorf("启动流失败: %w", err)
	}

	return nil
}

// Stop 停止捕获并释放资源。
func (r *OutputCaptureRecorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stream != nil {
		r.stream.Stop()
		r.stream.Close()
		r.stream = nil
	}

	// 安全关闭通道，避免重复关闭
	if !r.isClosed {
		close(r.Samples)
		r.isClosed = true
	}

	SafePortAudioTerminate()
	return nil
}
