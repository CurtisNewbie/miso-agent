package agents

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/util/ptr"
	"github.com/curtisnewbie/miso/util/retry"
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
	retry       int
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

func WithRetry(n int) func(o *openAiModelConfig) {
	return func(o *openAiModelConfig) {
		o.retry = n
	}
}

func NewOpenAIChatModel(model, apiKey string, ops ...func(o *openAiModelConfig)) (model.ToolCallingChatModel, error) {
	o := &openAiModelConfig{
		maxToken:    4096,
		temperature: 0.7,
		baseURL:     AliBailianIntlBaseURL,
		retry:       0,
	}
	for _, op := range ops {
		op(o)
	}

	cm, err := openai.NewChatModel(context.TODO(), &openai.ChatModelConfig{
		BaseURL:             o.baseURL,
		APIKey:              apiKey,
		Model:               model,
		Temperature:         ptr.ValPtr(o.temperature),
		MaxCompletionTokens: ptr.ValPtr(o.maxToken),
	})
	if err != nil {
		return nil, err
	}

	// wrap with retry
	if o.retry > 0 {
		return &retryChatModel{
			retry: o.retry,
			c:     cm,
		}, nil
	}
	return cm, nil
}

type retryChatModel struct {
	retry int
	c     model.ToolCallingChatModel
}

func (r *retryChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return retry.GetOne(r.retry, func() (*schema.Message, error) {
		return r.c.Generate(ctx, input, opts...)
	})
}

func (r *retryChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return retry.GetOne(r.retry, func() (*schema.StreamReader[*schema.Message], error) {
		return r.c.Stream(ctx, input, opts...)
	})
}

func (r *retryChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return r.c.WithTools(tools)
}

func RetryChatModel(c model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &retryChatModel{
		c:     c,
		retry: 3,
	}
}
