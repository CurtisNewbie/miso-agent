package agents

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/curtisnewbie/miso/util/ptr"
)

var (
	DeepseekBaseURL       = "https://api.deepseek.com/v1"
	OpenAIBaseURL         = "https://api.openai.com/v1"
	AliBailianIntlBaseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	AliBailianCnBaseURL   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
)

type openAiModelConfig struct {
	maxToken    int
	temperature float32
	baseURL     string
}

func WithTemperature(n float32) func(o *openAiModelConfig) {
	return func(o *openAiModelConfig) {
		o.temperature = n
	}
}

func WithMaxToken(n int) func(o *openAiModelConfig) {
	return func(o *openAiModelConfig) {
		o.maxToken = n
	}
}

func NewOpenAIChatModel(model, apiKey string, ops ...func(o *openAiModelConfig)) (*openai.ChatModel, error) {
	o := &openAiModelConfig{
		maxToken:    4096,
		temperature: 0.7,
		baseURL:     AliBailianIntlBaseURL,
	}
	for _, op := range ops {
		op(o)
	}
	return openai.NewChatModel(context.TODO(), &openai.ChatModelConfig{
		BaseURL:             o.baseURL,
		APIKey:              apiKey,
		Model:               model,
		Temperature:         ptr.ValPtr(o.temperature),
		MaxCompletionTokens: ptr.ValPtr(o.maxToken),
	})
}
