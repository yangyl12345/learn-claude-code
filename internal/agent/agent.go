// Package agent 提供所有课程共用的消息循环和工具分发器。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yangyl12345/learn-claude-code/internal/ai"
)

// Event 是 hook 可以观察的生命周期事件。
type Event string

const (
	UserPromptSubmit Event = "UserPromptSubmit"
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	Stop             Event = "Stop"
)

// ToolCall 是模型请求执行工具时的稳定表示。
type ToolCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments json.RawMessage
}

// ToolHandler 执行一个工具。错误会被转成模型可见的工具结果。
type ToolHandler func(context.Context, json.RawMessage) (string, error)

// Tool 是模型可见定义与本地 handler 的组合。
type Tool struct {
	Definition ai.ToolDefinition
	Handler    ToolHandler
	ReadOnly   bool
	Approval   bool
}

// Registry 是线程安全的工具注册表。
type Registry struct {
	mu    sync.RWMutex
	items map[string]Tool
}

// NewRegistry 创建空工具注册表。
func NewRegistry() *Registry { return &Registry{items: make(map[string]Tool)} }

// Register 添加或替换一个工具。
func (r *Registry) Register(tool Tool) error {
	if tool.Definition.Name == "" || tool.Handler == nil {
		return errors.New("tool name and handler are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[tool.Definition.Name] = tool
	return nil
}

// Get 返回一个工具及其存在状态。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[name]
	return t, ok
}

// Definitions 返回当前注册表的稳定快照。
func (r *Registry) Definitions() []ai.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ai.ToolDefinition, 0, len(r.items))
	for _, tool := range r.items {
		result = append(result, tool.Definition)
	}
	// 工具顺序会影响 prompt 缓存和测试快照，因此按名称排序。
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Name < result[i].Name {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// HookFunc 是一个可注册的生命周期 hook。
type HookFunc func(context.Context, Event, *ToolCall, string) error

// Hooks 管理 hook 注册和触发。一个 hook 失败不会跳过后续安全清理。
type Hooks struct {
	mu    sync.RWMutex
	items map[Event][]HookFunc
}

// NewHooks 创建 hook 注册表。
func NewHooks() *Hooks { return &Hooks{items: make(map[Event][]HookFunc)} }

// Register 注册一个 hook。
func (h *Hooks) Register(event Event, fn HookFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items[event] = append(h.items[event], fn)
}

// Trigger 按注册顺序运行 hook，并返回第一个错误。
func (h *Hooks) Trigger(ctx context.Context, event Event, call *ToolCall, output string) error {
	h.mu.RLock()
	items := append([]HookFunc(nil), h.items[event]...)
	h.mu.RUnlock()
	for _, fn := range items {
		if err := fn(ctx, event, call, output); err != nil {
			return err
		}
	}
	return nil
}

// Approver 决定需要人工确认的工具是否可以执行。
type Approver func(context.Context, ToolCall) (bool, error)

// Config 控制 Agent Loop。
type Config struct {
	WorkDir      string
	Model        string
	Instructions string
	MaxTokens    int
	MaxTurns     int
	Output       io.Writer
	Approver     Approver
}

// Loop 是一个可复用的 OpenAI function-calling Agent Loop。
type Loop struct {
	Client ai.ModelClient
	Tools  *Registry
	Hooks  *Hooks
	Config Config
}

// Result 是一次用户请求的最终结果和诊断信息。
type Result struct {
	Text         string
	History      []ai.InputItem
	InputTokens  int
	OutputTokens int
	Turns        int
}

// MessageStore 是可选的会话历史持久化边界。实现可以把 History 写入
// transcript 文件、数据库或外部队列，而 Agent Loop 不需要知道具体介质。
type MessageStore interface {
	Save(context.Context, []ai.InputItem) error
	Load(context.Context) ([]ai.InputItem, error)
}

// Run 执行模型、工具、工具结果之间的循环，直到模型没有 function call。
func (l *Loop) Run(ctx context.Context, prompt string) (Result, error) {
	if l.Client == nil || l.Tools == nil {
		return Result{}, errors.New("agent loop requires client and tools")
	}
	if l.Hooks == nil {
		l.Hooks = NewHooks()
	}
	if l.Config.Model == "" {
		return Result{}, errors.New("agent model is required")
	}
	if l.Config.MaxTurns <= 0 {
		l.Config.MaxTurns = 30
	}
	if l.Config.MaxTokens <= 0 {
		l.Config.MaxTokens = 8000
	}
	history := []ai.InputItem{ai.UserMessage(prompt)}
	_ = l.Hooks.Trigger(ctx, UserPromptSubmit, nil, prompt)
	var result Result
	for turn := 1; turn <= l.Config.MaxTurns; turn++ {
		response, err := l.Client.CreateResponse(ctx, ai.Request{
			Model: l.Config.Model, Instructions: l.Config.Instructions,
			Input: history, Tools: l.Tools.Definitions(), MaxTokens: l.Config.MaxTokens,
		})
		if err != nil {
			return result, fmt.Errorf("model response: %w", err)
		}
		result.Turns = turn
		result.InputTokens += response.InputTokens
		result.OutputTokens += response.OutputTokens
		history = append(history, ai.AssistantOutputItems(response)...)
		calls := make([]ToolCall, 0)
		for _, item := range response.Output {
			if item.Type == "function_call" {
				calls = append(calls, ToolCall{ID: item.ID, CallID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.ArgumentsJSON)})
			}
		}
		if len(calls) == 0 {
			_ = l.Hooks.Trigger(ctx, Stop, nil, response.Text)
			result.Text, result.History = response.Text, history
			return result, nil
		}
		for i := range calls {
			call := &calls[i]
			tool, ok := l.Tools.Get(call.Name)
			var output string
			if !ok {
				output = "Error: unknown tool " + call.Name
			} else {
				if err := l.Hooks.Trigger(ctx, PreToolUse, call, ""); err != nil {
					output = "Permission denied: " + err.Error()
				} else if tool.Approval {
					allowed := false
					if l.Config.Approver != nil {
						allowed, err = l.Config.Approver(ctx, *call)
					}
					if err != nil {
						output = "Permission error: " + err.Error()
					} else if !allowed {
						output = "Permission denied by user"
					} else {
						output = l.execute(ctx, tool, call)
					}
				} else {
					output = l.execute(ctx, tool, call)
				}
			}
			_ = l.Hooks.Trigger(ctx, PostToolUse, call, output)
			history = append(history, ai.FunctionCallOutput(call.CallID, output))
		}
	}
	return result, fmt.Errorf("agent loop exceeded max turns (%d)", l.Config.MaxTurns)
}

func (l *Loop) execute(ctx context.Context, tool Tool, call *ToolCall) string {
	output, err := tool.Handler(ctx, call.Arguments)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if output == "" {
		return "(no output)"
	}
	return output
}

// SafePath 将相对路径限制在 workspace 内，并拒绝最终路径的 symlink 逃逸。
func SafePath(workdir, name string) (string, error) {
	root, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", errors.New("path cannot be empty")
	}
	candidate, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path is outside workspace")
	}
	if realRoot, e := filepath.EvalSymlinks(root); e == nil {
		// 已存在的路径必须解析最终 symlink；否则 workspace/link 指向外部
		// 文件时，仅检查 parent 会误把它当成 workspace 内文件。
		realCandidate, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			parent := filepath.Dir(candidate)
			realParent, parentErr := filepath.EvalSymlinks(parent)
			if parentErr != nil {
				return "", parentErr
			}
			realCandidate = filepath.Join(realParent, filepath.Base(candidate))
		}
		realRel, relErr := filepath.Rel(realRoot, realCandidate)
		if relErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(os.PathSeparator)) {
			return "", errors.New("path symlink escapes workspace")
		}
	}
	return candidate, nil
}
