// Package ai 将课程运行时与具体的大模型 SDK 隔离开。
//
// 课程只依赖 ModelClient，因此单元测试可以使用 FakeClient，而不会发出
// 真实请求。OpenAIClient 是唯一直接依赖 openai-go 的实现。
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

// InputItem 是 Responses API 会话中的一个原始输入项。
// 保存原始 JSON 是为了完整保留 reasoning、assistant message 和 function call。
type InputItem = json.RawMessage

// Request 描述一次无状态的 Responses API 请求。
type Request struct {
	Model        string
	Instructions string
	Input        []InputItem
	Tools        []ToolDefinition
	MaxTokens    int
}

// ToolDefinition 是模型可见的 function tool 定义。
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// OutputItem 是从 OpenAI response 中抽取出的稳定内部表示。
type OutputItem struct {
	Type          string
	ID            string
	CallID        string
	Name          string
	ArgumentsJSON string
	Text          string
	Raw           any
}

// Response 是 Agent Loop 所需的最小模型结果。
type Response struct {
	Output       []OutputItem
	Text         string
	InputTokens  int
	OutputTokens int
}

// ErrorKind 是模型请求失败的可诊断分类。
type ErrorKind string

const (
	ErrorAPI           ErrorKind = "api"
	ErrorTimeout       ErrorKind = "timeout"
	ErrorContextLength ErrorKind = "context_length"
	ErrorInvalidJSON   ErrorKind = "invalid_json"
)

// RequestError 保留底层错误，同时向 CLI 和测试提供稳定分类。
type RequestError struct {
	Kind ErrorKind
	Err  error
}

func (e *RequestError) Error() string { return fmt.Sprintf("%s: %v", e.Kind, e.Err) }
func (e *RequestError) Unwrap() error { return e.Err }

// ClassifyError 将常见 SDK/解码错误映射为稳定分类，不包含请求头和密钥。
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &RequestError{Kind: ErrorTimeout, Err: err}
	}
	if strings.Contains(strings.ToLower(err.Error()), "context length") || strings.Contains(strings.ToLower(err.Error()), "maximum context") {
		return &RequestError{Kind: ErrorContextLength, Err: err}
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return &RequestError{Kind: ErrorInvalidJSON, Err: err}
	}
	return &RequestError{Kind: ErrorAPI, Err: err}
}

// ModelClient 是模型调用边界。所有课程和测试都通过这个接口访问模型。
type ModelClient interface {
	CreateResponse(context.Context, Request) (Response, error)
}

// Config 从环境变量读取 OpenAI 配置，不读取或打印密钥内容。
type Config struct {
	APIKey         string
	Model          string
	EvaluatorModel string
}

// LoadConfig 校验运行所需的 OpenAI 环境变量。
func LoadConfig() (Config, error) {
	c := Config{
		APIKey:         os.Getenv("OPENAI_API_KEY"),
		Model:          os.Getenv("OPENAI_MODEL"),
		EvaluatorModel: os.Getenv("OPENAI_EVALUATOR_MODEL"),
	}
	if c.APIKey == "" {
		return Config{}, errors.New("OPENAI_API_KEY is required")
	}
	if c.Model == "" {
		return Config{}, errors.New("OPENAI_MODEL is required")
	}
	if c.EvaluatorModel == "" {
		c.EvaluatorModel = c.Model
	}
	return c, nil
}

// OpenAIClient 使用官方 openai-go SDK 的 Responses API。
type OpenAIClient struct {
	client openai.Client
}

// NewOpenAIClient 创建真实 OpenAI 客户端。SDK 从 OPENAI_API_KEY 获取密钥。
func NewOpenAIClient() (*OpenAIClient, error) {
	if _, err := LoadConfig(); err != nil {
		return nil, err
	}
	return &OpenAIClient{client: openai.NewClient()}, nil
}

// NewOpenAIClientWithOptions 主要用于兼容 OpenAI-compatible 网关和测试。
// 生产环境仍建议只设置 OPENAI_API_KEY 与 OPENAI_MODEL。
func NewOpenAIClientWithOptions(opts ...option.RequestOption) *OpenAIClient {
	return &OpenAIClient{client: openai.NewClient(opts...)}
}

// CreateResponse 将内部消息转换为 Responses API 请求，并把完整 output 保留下来。
func (c *OpenAIClient) CreateResponse(ctx context.Context, request Request) (Response, error) {
	if request.Model == "" {
		return Response{}, errors.New("model is required")
	}
	input := make(responses.ResponseInputParam, 0, len(request.Input))
	for _, raw := range request.Input {
		var item responses.ResponseInputItemUnionParam
		if err := json.Unmarshal(raw, &item); err != nil {
			return Response{}, fmt.Errorf("decode input item: %w", err)
		}
		input = append(input, item)
	}
	tools := make([]responses.ToolUnionParam, 0, len(request.Tools))
	for _, tool := range request.Tools {
		if tool.Name == "" {
			return Response{}, errors.New("tool name cannot be empty")
		}
		tools = append(tools, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name:        tool.Name,
			Description: param.NewOpt(tool.Description),
			Parameters:  tool.Parameters,
			Strict:      param.NewOpt(false),
		}})
	}
	params := responses.ResponseNewParams{
		Model: request.Model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Tools: tools,
	}
	if request.Instructions != "" {
		params.Instructions = param.NewOpt(request.Instructions)
	}
	if request.MaxTokens > 0 {
		params.MaxOutputTokens = param.NewOpt(int64(request.MaxTokens))
	}
	resp, err := c.client.Responses.New(ctx, params)
	if err != nil {
		return Response{}, ClassifyError(err)
	}
	out := Response{Text: resp.OutputText()}
	for _, item := range resp.Output {
		raw := json.RawMessage(item.RawJSON())
		parsed := OutputItem{Type: item.Type, ID: item.ID, CallID: item.CallID, Name: item.Name, Raw: raw}
		parsed.ArgumentsJSON = item.Arguments.OfString
		if item.Type == "message" {
			for _, part := range item.Content {
				if part.Type == "output_text" {
					parsed.Text += part.Text
				}
			}
		}
		out.Output = append(out.Output, parsed)
	}
	// v3 SDK 将 usage 建模为值类型；即使服务端没有返回 usage，零值也安全。
	out.InputTokens = int(resp.Usage.InputTokens)
	out.OutputTokens = int(resp.Usage.OutputTokens)
	return out, nil
}

// UserMessage 将普通文本构造成 Responses 输入项。
func UserMessage(text string) InputItem {
	b, _ := json.Marshal(map[string]any{"role": "user", "content": text})
	return b
}

// FunctionCallOutput 将工具结果构造成 Responses function_call_output 输入项。
func FunctionCallOutput(callID, output string) InputItem {
	b, _ := json.Marshal(map[string]any{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  output,
	})
	return b
}

// AssistantOutputItems 返回 response output 的原始 JSON，供下一轮继续使用。
func AssistantOutputItems(response Response) []InputItem {
	items := make([]InputItem, 0, len(response.Output))
	for _, item := range response.Output {
		if raw, ok := item.Raw.(json.RawMessage); ok {
			items = append(items, raw)
			continue
		}
		// FakeClient 和第三方适配器可能只填充稳定字段；仍然要构造
		// 合法的 Responses input item，避免下一轮丢失 function call 上下文。
		if item.Type == "function_call" {
			raw, _ := json.Marshal(map[string]any{"type": item.Type, "id": item.ID, "call_id": item.CallID, "name": item.Name, "arguments": item.ArgumentsJSON})
			items = append(items, raw)
		} else if item.Type == "message" && item.Text != "" {
			raw, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": item.Text}}})
			items = append(items, raw)
		}
	}
	return items
}

// HasFunctionCalls 判断本轮是否需要执行工具，不能只依赖 stop_reason。
func HasFunctionCalls(response Response) bool {
	for _, item := range response.Output {
		if item.Type == "function_call" {
			return true
		}
	}
	return false
}

// FakeClient 为离线测试提供可编程响应序列。
type FakeClient struct {
	Responses []Response
	Calls     []Request
}

// CreateResponse 返回预设结果，并记录请求副本。
func (f *FakeClient) CreateResponse(_ context.Context, req Request) (Response, error) {
	f.Calls = append(f.Calls, req)
	if len(f.Responses) == 0 {
		return Response{}, errors.New("fake client has no response")
	}
	r := f.Responses[0]
	f.Responses = f.Responses[1:]
	return r, nil
}
