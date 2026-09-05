// Package goal 将“模型想停止”与“用户目标已完成”分离。
package goal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/yangyl12345/learn-claude-code/internal/ai"
)

// Evaluation 是独立 evaluator 的结构化判断。
type Evaluation struct {
	OK         bool   `json:"ok"`
	Reason     string `json:"reason"`
	Impossible bool   `json:"impossible,omitempty"`
}

// Evaluator 只读取对话，不暴露工具。
type Evaluator interface {
	Evaluate(context.Context, string, string) (Evaluation, error)
}

// ModelEvaluator 使用同一个 Responses 抽象调用独立 evaluator。
// 它故意不给 evaluator 传递工具定义，确保 evaluator 只能审查对话。
type ModelEvaluator struct {
	Client ai.ModelClient
	Model  string
}

// Evaluate 要求模型返回 JSON 对象，其中 ok/reason/impossible 是唯一判断字段。
func (e ModelEvaluator) Evaluate(ctx context.Context, condition, transcript string) (Evaluation, error) {
	if e.Client == nil || e.Model == "" {
		return Evaluation{}, errors.New("goal model evaluator is not configured")
	}
	prompt := fmt.Sprintf("Goal: %s\nConversation transcript:\n%s\nReturn JSON only with boolean ok, non-empty reason, and optional boolean impossible.", condition, transcript)
	r, err := e.Client.CreateResponse(ctx, ai.Request{
		Model: e.Model, Instructions: "You are an independent goal evaluator. You have no tools and must only judge the transcript.",
		Input: []ai.InputItem{ai.UserMessage(prompt)}, MaxTokens: 1000,
	})
	if err != nil {
		return Evaluation{}, err
	}
	var out Evaluation
	if err := json.Unmarshal([]byte(strings.TrimSpace(r.Text)), &out); err != nil {
		return Evaluation{}, fmt.Errorf("goal evaluator returned invalid JSON: %w", err)
	}
	if strings.TrimSpace(out.Reason) == "" {
		return Evaluation{}, errors.New("goal evaluator reason is required")
	}
	if out.OK && out.Impossible {
		return Evaluation{}, errors.New("goal evaluator cannot return ok and impossible together")
	}
	return out, nil
}

// State 是会话级目标状态。
type State struct {
	Condition  string
	Active     bool
	Blocks     int
	MaxBlocks  int
	Done       bool
	Failed     bool
	LastReason string
}

// Controller 把 evaluator 判断接入停止边界。
type Controller struct {
	mu       sync.Mutex
	Eval     Evaluator
	State    State
	MaxTurns int
	Turns    int
}

// NewController 创建目标控制器。
func NewController(e Evaluator, maxBlocks, maxTurns int) *Controller {
	if maxBlocks <= 0 {
		maxBlocks = 3
	}
	if maxTurns <= 0 {
		maxTurns = 50
	}
	return &Controller{Eval: e, State: State{MaxBlocks: maxBlocks}, MaxTurns: maxTurns}
}

// Set 设置或替换目标。
func (c *Controller) Set(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return errors.New("goal condition cannot be empty")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = State{Condition: condition, Active: true, MaxBlocks: c.State.MaxBlocks}
	c.Turns = 0
	return nil
}

// Clear 清除当前目标。
func (c *Controller) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.State = State{MaxBlocks: c.State.MaxBlocks}
}

// Evaluate 在模型准备停止时做独立判断；返回 allow、block 或 impossible。
func (c *Controller) Evaluate(ctx context.Context, transcript string) (string, Evaluation, error) {
	c.mu.Lock()
	if !c.State.Active {
		c.mu.Unlock()
		return "allow", Evaluation{OK: true, Reason: "no active goal"}, nil
	}
	if c.Turns >= c.MaxTurns {
		c.State.Active = false
		c.State.Failed = true
		c.State.LastReason = "global turn limit reached"
		c.mu.Unlock()
		return "impossible", Evaluation{Reason: c.State.LastReason, Impossible: true}, nil
	}
	evaluator := c.Eval
	condition := c.State.Condition
	c.mu.Unlock()
	if evaluator == nil {
		return "defer", Evaluation{}, errors.New("goal evaluator is required")
	}
	ev, err := evaluator.Evaluate(ctx, condition, transcript)
	if err != nil {
		return "defer", ev, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Turns++
	c.State.LastReason = ev.Reason
	if ev.Impossible {
		c.State.Active = false
		c.State.Failed = true
		return "impossible", ev, nil
	}
	if ev.OK {
		c.State.Active = false
		c.State.Done = true
		return "allow", ev, nil
	}
	c.State.Blocks++
	if c.State.Blocks >= c.State.MaxBlocks {
		c.State.Active = false
		c.State.Failed = true
		return "impossible", Evaluation{Reason: fmt.Sprintf("blocked %d times", c.State.Blocks), Impossible: true}, nil
	}
	return "block", ev, nil
}
