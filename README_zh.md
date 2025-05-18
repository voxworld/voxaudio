[English](README.md) | [简体中文](README_zh.md)

# VoxAudio

![Version](https://img.shields.io/github/v/release/voxworld/voxaudio?style=flat-square)
![License](https://img.shields.io/github/license/voxworld/voxaudio?style=flat-square)

> [!IMPORTANT]
> 本项目目前处于研究阶段，主要用于探索 OpenAI Realtime API 在实时语音翻译领域的应用。代码和功能可能会随时发生变化，不建议在生产环境中使用。欢迎提交 Issue 和 Pull Request 来帮助改进。

VoxAudio 是一个基于 OpenAI Realtime API 的实时语音翻译工具。它能够实时捕获音频输入，通过 OpenAI 的 API 进行实时翻译，并输出翻译后的语音。

## 研究目标

- 探索 OpenAI Realtime API 在实时语音翻译中的应用
- 研究低延迟语音处理的最佳实践
- 测试不同音频设备和采样率对翻译质量的影响
- 捕获本地语音流输入，通过 WebRTC 传输到 OpenAI 服务器，并进行实时翻译
- 优化实时语音处理的性能

## 核心功能

- 实时音频捕获和处理
- 使用 OpenAI Realtime API 进行实时语音翻译
- 支持多种语言之间的互译
- 低延迟的实时语音处理
- 支持自定义音频设备选择
- 提供音频回环测试功能
- 提供保存为 wav 文件的方式用于验证

## 技术特点

- 使用 WebRTC 进行实时通信
- 支持多种音频格式和采样率
- 提供音频设备管理和选择功能
- 包含完整的测试套件
- 支持音频数据的平滑处理和降噪

## 环境要求

- Go 1.16 或更高版本
- OpenAI API 密钥
- 音频输入设备（麦克风）

## 配置

1. 设置环境变量：

```bash
export OPENAI_API_KEY="your-api-key"
export OPENAI_MODEL="gpt-4o-mini-realtime-preview"
export TEST_AUDIO_DEVICE="your-audio-device-name"  # 用于测试
```

2. 安装依赖：

```bash
go mod download
```

## 使用示例

```go
// 创建新的会话
session, err := NewSession(apiKey, model, "English", "alloy")
if err != nil {
    log.Fatal(err)
}
defer session.Stop()

// 建立 WebRTC 连接
err = session.Conn()
if err != nil {
    log.Fatal(err)
}

// 注册音频轨道
session.RegisterLocalTrack()

// 开始捕获音频
err = session.Start(deviceName)
if err != nil {
    log.Fatal(err)
}
```

## 测试

项目包含多个测试用例：

- `TestIntegratedRealtime`: 端到端集成测试
- `TestRealtimeConnection`: WebRTC 连接测试
- `TestLoopbackRecorder`: 音频回环测试

运行测试：

```bash
go test -v
```

## 项目状态

本项目目前处于研究阶段，主要关注以下方面：

- 实时语音翻译的准确性和延迟优化
- 不同语言对之间的翻译效果对比
- 音频处理算法的改进
- WebRTC 连接稳定性的提升

欢迎提交 Issue 和 Pull Request 来帮助改进，特别是：
- 新的语言对支持
- 音频处理算法的优化
- 性能改进建议
- 使用体验的反馈

## 贡献

1. Fork 本仓库
2. 创建你的特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交你的更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启一个 Pull Request

## 许可证

[MIT License](LICENSE) 