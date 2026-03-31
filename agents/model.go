package agents

import (
	"context"
	"encoding/json"

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
	OpenRouterBaseURL     = "https://openrouter.ai/api/v1"
)

const (
	maxToken32k = 32768
	maxToken64k = 65536
)

var (
	modelMaxToken = map[string]int{
		"qwen-flash":                     maxToken32k,
		"qwen-plus":                      maxToken32k,
		"qwen3-max":                      maxToken64k,
		"qwen3-coder-plus":               maxToken64k,
		"qwen3-next-80b-a3b-thinking":    maxToken32k,
		"qwen3-next-80b-a3b-instruct":    maxToken32k,
		"qwen3-coder-30b-a3b-instruct":   maxToken64k,
		"qwen3-30b-a3b-thinking-2507":    maxToken32k,
		"qwen3-30b-a3b-instruct-2507":    maxToken32k,
		"qwen3-235b-a22b-thinking-2507":  maxToken32k,
		"qwen3-235b-a22b-instruct-2507":  maxToken32k,
		"qwen3-coder-480b-a35b-instruct": maxToken64k,
	}
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

func WithBaseURL(url string) func(o *openAiModelConfig) {
	return func(o *openAiModelConfig) {
		o.baseURL = url
	}
}

// NewOpenAIChatModel creates a new OpenAI-compatible chat model.
//
// Example:
//
//	model, err := NewOpenAIChatModel("qwen3-max", apiKey,
//	    WithTemperature(0.3),
//	    WithMaxToken(32768),
//	    WithRetry(3),
//	)
func NewOpenAIChatModel(modelName, apiKey string, ops ...func(o *openAiModelConfig)) (model.ToolCallingChatModel, error) {
	o := &openAiModelConfig{
		maxToken:    0,
		temperature: 0.7,
		baseURL:     AliBailianIntlBaseURL,
		retry:       0,
	}
	for _, op := range ops {
		op(o)
	}

	if o.maxToken < 1 {
		if n, ok := modelMaxToken[modelName]; ok {
			o.maxToken = n
		}
	}
	if o.maxToken < 1 {
		o.maxToken = maxToken32k // default value for all models
	}

	cm, err := openai.NewChatModel(context.TODO(), &openai.ChatModelConfig{
		BaseURL:             o.baseURL,
		APIKey:              apiKey,
		Model:               modelName,
		Temperature:         ptr.ValPtr(o.temperature),
		MaxCompletionTokens: ptr.ValPtr(o.maxToken),
	})
	if err != nil {
		return nil, err
	}

	// wrap with retry
	var result model.ToolCallingChatModel = cm
	if o.retry > 0 {
		result = &retryChatModel{
			retry: o.retry,
			c:     result,
		}
	}

	// always wrap with content fix modifier to ensure "content" field is
	// present in every request message (required by some providers)
	result = &contentFixModel{inner: result}
	return result, nil
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

// RetryChatModel wraps c with a default retry count of 3.
func RetryChatModel(c model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &retryChatModel{
		c:     c,
		retry: 3,
	}
}

// ensureMessageContent is a RequestPayloadModifier that guarantees every
// message object in the serialized request body contains a "content" field.
//
// Some providers (e.g. DashScope) reject requests when the "content" field is
// absent from an assistant message, even though standard Go JSON encoding drops
// the field via omitempty when the content string is empty (which is normal for
// assistant messages that only contain tool_calls).  This modifier patches the
// raw body without re-encoding any unrelated fields, so precision of other
// numeric/string values is fully preserved.
func ensureMessageContent(_ context.Context, _ []*schema.Message, body []byte) ([]byte, error) {
	var reqMap map[string]json.RawMessage
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, nil
	}
	msgsRaw, ok := reqMap["messages"]
	if !ok {
		return body, nil
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil {
		return body, nil
	}
	modified := false
	for i, msgRaw := range msgs {
		var msgMap map[string]json.RawMessage
		if err := json.Unmarshal(msgRaw, &msgMap); err != nil {
			continue
		}
		if _, hasContent := msgMap["content"]; !hasContent {
			msgMap["content"] = json.RawMessage(`""`)
			newMsgBytes, err := json.Marshal(msgMap)
			if err != nil {
				continue
			}
			msgs[i] = json.RawMessage(newMsgBytes)
			modified = true
		}
	}
	if !modified {
		return body, nil
	}
	newMsgsBytes, err := json.Marshal(msgs)
	if err != nil {
		return body, nil
	}
	reqMap["messages"] = json.RawMessage(newMsgsBytes)
	newBody, err := json.Marshal(reqMap)
	if err != nil {
		return body, nil
	}
	return newBody, nil
}

// contentFixModel wraps a ToolCallingChatModel and always injects
// ensureMessageContent as a RequestPayloadModifier on every Generate and
// Stream call, ensuring compatibility with providers that require the
// "content" field to be present in every message.
type contentFixModel struct {
	inner model.ToolCallingChatModel
}

func (c *contentFixModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	opts = append(opts, openai.WithRequestPayloadModifier(ensureMessageContent))
	return c.inner.Generate(ctx, input, opts...)
}

func (c *contentFixModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	opts = append(opts, openai.WithRequestPayloadModifier(ensureMessageContent))
	return c.inner.Stream(ctx, input, opts...)
}

func (c *contentFixModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := c.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &contentFixModel{inner: inner}, nil
}
