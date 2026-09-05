// Package lesson 负责组装 17 个课程入口；具体机制留在 internal 公共包中。
package lesson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangyl12345/learn-claude-code/internal/agent"
	"github.com/yangyl12345/learn-claude-code/internal/ai"
	"github.com/yangyl12345/learn-claude-code/internal/background"
	"github.com/yangyl12345/learn-claude-code/internal/cron"
	"github.com/yangyl12345/learn-claude-code/internal/goal"
	"github.com/yangyl12345/learn-claude-code/internal/mcp"
	"github.com/yangyl12345/learn-claude-code/internal/tasks"
	"github.com/yangyl12345/learn-claude-code/internal/teams"
	"github.com/yangyl12345/learn-claude-code/internal/tools"
	"github.com/yangyl12345/learn-claude-code/internal/workflow"
)

// Run 启动指定课程。课程 1-15、17 需要 OPENAI_API_KEY 和 OPENAI_MODEL；
// s10/s12/s13/s14/s16 也包含可离线执行的机制演示，便于教学和测试。
func Run(ctx context.Context, id string, args []string, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	if id == "s10" {
		return demoTasks(out)
	}
	if id == "s12" {
		return demoCron(out)
	}
	if id == "s13" {
		return demoTeams(out)
	}
	if id == "s14" {
		return demoMCP(ctx, out)
	}
	if id == "s16" {
		return demoWorkflow(out, args)
	}
	if id == "s15" {
		fmt.Fprintln(out, "s15 integrated harness: memory + skills + tasks + teams + cron + background + MCP + hooks")
	}
	if id == "s17" && len(args) > 0 && args[0] == "--demo" {
		return demoGoal(ctx, out)
	}
	if id == "s17" {
		return runGoal(ctx, args, out)
	}
	config, err := ai.LoadConfig()
	if err != nil {
		return err
	}
	client, err := ai.NewOpenAIClient()
	if err != nil {
		return err
	}
	registry := tools.NewBaseRegistry(".")
	if id == "s01" {
		registry = tools.NewBashRegistry(".")
	}
	if id == "s15" {
		registry, _, err = tools.NewFullRegistry(".")
		if err != nil {
			return err
		}
	}
	loop := &agent.Loop{Client: client, Tools: registry, Config: agent.Config{WorkDir: ".", Model: config.Model, MaxTurns: 20, Output: out}}
	prompt := strings.Join(args, " ")
	if prompt == "" {
		prompt = "请用一句话说明本课程的核心机制。"
	}
	result, err := loop.Run(ctx, prompt)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, result.Text)
	return nil
}

func demoTasks(out io.Writer) error {
	dir, err := os.MkdirTemp("", "lesson-tasks-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	s, err := tasks.NewStore(dir)
	if err != nil {
		return err
	}
	a, err := s.Create("准备工作", "")
	if err != nil {
		return err
	}
	b, err := s.Create("执行工作", "")
	if err != nil {
		return err
	}
	if err = s.AddDependencies(b.ID, a.ID); err != nil {
		return err
	}
	_, err = s.Claim(a.ID, "demo")
	if err != nil {
		return err
	}
	_, err = s.Complete(a.ID, "demo")
	if err != nil {
		return err
	}
	t, err := s.Claim(b.ID, "demo")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "s10 task graph: %s is %s after dependency unlock\n", t.ID, t.Status)
	return nil
}
func demoCron(out io.Writer) error {
	s := cron.NewScheduler()
	j, err := s.Add("*/5 * * * *", "echo scheduled")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "s12 cron: %s (%s)\n", j.ID, j.Spec)
	return nil
}
func demoTeams(out io.Writer) error {
	b := teams.NewMessageBus()
	m, err := b.Send("lead", "worker", "task", "implement lesson")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "s13 mailbox: #%d %s -> %s\n", m.ID, m.From, m.To)
	return nil
}
func demoMCP(ctx context.Context, out io.Writer) error {
	p := mcp.NewPool()
	c := mcp.NewMockClient("docs", []mcp.Tool{{Name: "search", Description: "search docs", Handler: func(context.Context, map[string]any) (string, error) { return "mock result", nil }}})
	if err := p.Connect(c); err != nil {
		return err
	}
	names := p.Names()
	if len(names) == 0 {
		return errors.New("MCP pool is empty")
	}
	v, err := p.Call(ctx, names[0], map[string]any{"query": "go"})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "s14 MCP: %s => %s\n", names[0], v)
	return nil
}
func demoWorkflow(out io.Writer, args []string) error {
	dir := filepath.Join(".runtime", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "resume" {
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return errors.New("resume requires a run id")
		}
		state, err := workflow.NewRunner(dir).Resume(args[1])
		if err != nil {
			return err
		}
		b, _ := json.Marshal(state)
		fmt.Fprintf(out, "s16 workflow resumed: %s\n", b)
		return nil
	}
	r := workflow.NewRunner(dir)
	id := fmt.Sprintf("run_%d", time.Now().UnixNano())
	state, err := r.Execute("demo", id, []workflow.Step{{Name: "agent", Run: func() (any, error) { return map[string]string{"status": "ok"}, nil }}, {Name: "parallel", Run: func() (any, error) { return 2, nil }}})
	if err != nil {
		return err
	}
	b, _ := json.Marshal(state)
	fmt.Fprintf(out, "s16 workflow: %s\nresume with: go run ./s16_workflow_runtime resume %s\n", b, id)
	return nil
}

// runGoal 将主模型与无工具 evaluator 串接在同一个用户请求中。
// 主模型每次停止后，evaluator 只读取已记录的对话；若目标未完成，
// 原因会作为新的用户提示回到 loop。这样“模型想停”和“目标已完成”不会混为一谈。
func runGoal(ctx context.Context, args []string, out io.Writer) error {
	config, err := ai.LoadConfig()
	if err != nil {
		return err
	}
	client, err := ai.NewOpenAIClient()
	if err != nil {
		return err
	}
	registry, _, err := tools.NewFullRegistry(".")
	if err != nil {
		return err
	}
	controller := goal.NewController(goal.ModelEvaluator{Client: client, Model: config.EvaluatorModel}, 3, 20)
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if strings.HasPrefix(prompt, "/goal ") {
		condition := strings.TrimSpace(strings.TrimPrefix(prompt, "/goal "))
		if err := controller.Set(condition); err != nil {
			return err
		}
		prompt = "请开始工作，并持续执行工具直到满足目标：" + condition
	}
	if prompt == "" {
		prompt = "请说明当前任务状态。"
	}
	for attempts := 0; attempts < 20; attempts++ {
		loop := &agent.Loop{Client: client, Tools: registry, Config: agent.Config{WorkDir: ".", Model: config.Model, MaxTurns: 20, MaxTokens: 8000, Output: out}}
		result, err := loop.Run(ctx, prompt)
		if err != nil {
			return err
		}
		decision, evaluation, err := controller.Evaluate(ctx, inputTranscript(result.History))
		if err != nil {
			return err
		}
		if decision == "allow" {
			fmt.Fprintln(out, result.Text)
			return nil
		}
		if decision == "impossible" {
			return fmt.Errorf("goal cannot continue: %s", evaluation.Reason)
		}
		prompt = "Goal evaluator says the goal is not complete: " + evaluation.Reason + "\nContinue from the previous response. Do not repeat completed work."
	}
	return errors.New("goal loop reached its continuation limit")
}

func inputTranscript(history []ai.InputItem) string {
	var lines []string
	for _, item := range history {
		var value any
		if json.Unmarshal(item, &value) == nil {
			b, _ := json.Marshal(value)
			lines = append(lines, string(b))
		} else {
			lines = append(lines, string(item))
		}
	}
	return strings.Join(lines, "\n")
}

type demoEvaluator struct{}

func (demoEvaluator) Evaluate(context.Context, string, string) (goal.Evaluation, error) {
	return goal.Evaluation{OK: true, Reason: "demo evaluator accepted"}, nil
}
func demoGoal(ctx context.Context, out io.Writer) error {
	c := goal.NewController(demoEvaluator{}, 3, 5)
	if err := c.Set("demo completes"); err != nil {
		return err
	}
	decision, ev, err := c.Evaluate(ctx, "completed")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "s17 goal: %s (%s)\n", decision, ev.Reason)
	return nil
}

// BackgroundDemo 暴露 s11 的可复用离线演示，供入口和测试调用。
func BackgroundDemo(ctx context.Context, out io.Writer) error {
	m := background.NewManager()
	id, err := m.Start(ctx, "printf background-ok", time.Second)
	if err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	for _, r := range m.Collect() {
		fmt.Fprintf(out, "%s: %s\n", id, r.Output)
	}
	return nil
}

// ArgsFromEnv 是课程入口共用的安全参数读取器；它不会读取任何密钥环境变量。
func ArgsFromEnv() []string {
	if v := os.Getenv("COURSE_PROMPT"); v != "" {
		return []string{v}
	}
	return nil
}

// EnsureWorkspace 用于测试路径行为。
func EnsureWorkspace(path string) error {
	if path == "" {
		return errors.New("workspace is empty")
	}
	_, err := filepath.Abs(path)
	return err
}
