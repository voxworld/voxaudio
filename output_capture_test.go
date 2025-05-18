package voxaudio

import (
	"fmt"
	"testing"
	"time"
)

func TestOutputCaptureRecorder_Start(t *testing.T) {
	// 初始化输出捕获器
	rec, err := NewOutputCaptureRecorder()
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Stop()

	// 列举音频设备并显示
	apis, err := rec.ListDevices()
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("可用音频设备:")
	for _, api := range apis {
		fmt.Printf("API: %s\n", api.Name)
		for idx, dev := range api.Devices {
			fmt.Printf("  [%d] %d %s (in:%d out:%d, rate:%.0f)\n",
				idx, dev.Index, dev.Name, dev.MaxInputChannels, dev.MaxOutputChannels, dev.DefaultSampleRate)
		}
	}

	// 创建一个缓冲通道来收集样本
	sampleBuffer := make([][]float32, 0)
	doneCh := make(chan struct{})

	// 设置一个goroutine来收集样本
	go func() {
		for samples := range rec.Samples {
			sampleBuffer = append(sampleBuffer, samples)
		}
		close(doneCh)
	}()

	// 开始录制 - 使用默认输出设备（传空字符串）
	// 也可以指定设备名如"MacBook Pro扬声器"
	if err := rec.Start(""); err != nil {
		t.Fatal(err)
	}

	t.Log("开始捕获系统音频输出，请在10秒内播放一些声音...")

	// 捕获10秒
	time.Sleep(10 * time.Second)

	// 停止捕获
	t.Log("停止捕获...")
	rec.Stop()

	// 等待样本收集完成
	<-doneCh

	// 检查是否捕获到了音频数据
	if len(sampleBuffer) == 0 {
		t.Fatal("未捕获到任何音频数据")
	}

	t.Logf("成功捕获了 %d 个音频样本块", len(sampleBuffer))

	// 创建一个新的通道来传递给saveToWav
	sampleCh := make(chan []float32)
	go func() {
		for _, samples := range sampleBuffer {
			sampleCh <- samples
		}
		close(sampleCh)
	}()

	// 保存为WAV文件
	outputFile := "output_capture_test.wav"
	if err := saveToWav(outputFile, sampleCh, 48000, 2); err != nil {
		t.Fatalf("保存WAV文件失败: %v", err)
	}

	t.Logf("成功将捕获的系统音频输出保存到文件: %s", outputFile)
}
