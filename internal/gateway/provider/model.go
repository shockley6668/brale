package provider

import "context"

// 中文说明：
// 模型提供方（ModelProvider）抽象。可封装 DeepSeek/Qwen/自定义 OpenAI 兼容接口。

// ImagePayload 用于多模态输入。
type ImagePayload struct {
	DataURI     string
	Description string
}

// ChatPayload 描述一次推理请求的材料。
type ChatPayload struct {
	System     string
	User       string
	Images     []ImagePayload
	ExpectJSON bool
	MaxTokens  int
}

// ModelProvider 模型调用接口
type ModelProvider interface {
	ID() string
	Enabled() bool
	SupportsVision() bool
	ExpectsJSON() bool
	// Call 执行模型推理，返回完整文本（包含思维链与 JSON 决策数组）
	Call(ctx context.Context, payload ChatPayload) (string, error)
}
