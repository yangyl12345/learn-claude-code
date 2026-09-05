package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yangyl12345/learn-claude-code/internal/agent"
	"github.com/yangyl12345/learn-claude-code/internal/ai"
	"github.com/yangyl12345/learn-claude-code/internal/background"
	"github.com/yangyl12345/learn-claude-code/internal/contextx"
	"github.com/yangyl12345/learn-claude-code/internal/cron"
	"github.com/yangyl12345/learn-claude-code/internal/skills"
	"github.com/yangyl12345/learn-claude-code/internal/tasks"
)

// Runtime 聚合高阶课程所需的持久化组件；字段公开是为了教学演示和测试替换。
type Runtime struct {
	Tasks      *tasks.Store
	Background *background.Manager
	Cron       *cron.Scheduler
	Todo       *TodoStore
}

// NewFullRegistry 在基础文件工具之上注册任务、todo、后台和定时工具。
func NewFullRegistry(workdir string) (*agent.Registry, *Runtime, error) {
	r := NewBaseRegistry(workdir)
	taskStore, err := tasks.NewStore(workdir + "/.tasks")
	if err != nil {
		return nil, nil, err
	}
	rt := &Runtime{Tasks: taskStore, Background: background.NewManager(), Cron: cron.NewScheduler(), Todo: &TodoStore{}}
	register := func(name, description string, props map[string]any, required []string, handler agent.ToolHandler) {
		_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: name, Description: description, Parameters: schema(props, required...)}, Handler: handler})
	}
	register("todo_write", "Replace the current session todo list.", map[string]any{"items": map[string]any{"type": "array"}}, []string{"items"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			Items []TodoItem `json:"items"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		if err := rt.Todo.Update(a.Items); err != nil {
			return "", err
		}
		return fmt.Sprintf("Todo updated: %d items", len(a.Items)), nil
	})
	register("create_task", "Create a durable task.", map[string]any{"subject": stringProp("Task subject"), "description": stringProp("Task details")}, []string{"subject"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			Subject     string `json:"subject"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		t, e := rt.Tasks.Create(a.Subject, a.Description)
		if e != nil {
			return "", e
		}
		return t.ID, nil
	})
	register("list_tasks", "List durable tasks.", map[string]any{}, nil, func(_ context.Context, _ json.RawMessage) (string, error) {
		v, e := rt.Tasks.List()
		if e != nil {
			return "", e
		}
		b, _ := json.Marshal(v)
		return string(b), nil
	})
	register("get_task", "Read one durable task.", map[string]any{"task_id": stringProp("Task ID")}, []string{"task_id"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		t, e := rt.Tasks.Get(a.TaskID)
		if e != nil {
			return "", e
		}
		b, _ := json.Marshal(t)
		return string(b), nil
	})
	register("update_task", "Add dependency edges to a task.", map[string]any{"task_id": stringProp("Task ID"), "blocked_by": map[string]any{"type": "array"}}, []string{"task_id"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			TaskID    string   `json:"task_id"`
			BlockedBy []string `json:"blocked_by"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		if err := rt.Tasks.AddDependencies(a.TaskID, a.BlockedBy...); err != nil {
			return "", err
		}
		return "task updated", nil
	})
	register("claim_task", "Atomically claim a ready task.", map[string]any{"task_id": stringProp("Task ID"), "owner": stringProp("Owner")}, []string{"task_id"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			TaskID string `json:"task_id"`
			Owner  string `json:"owner"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		t, e := rt.Tasks.Claim(a.TaskID, a.Owner)
		if e != nil {
			return "", e
		}
		return t.ID, nil
	})
	register("complete_task", "Complete a claimed task.", map[string]any{"task_id": stringProp("Task ID"), "owner": stringProp("Owner")}, []string{"task_id"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			TaskID string `json:"task_id"`
			Owner  string `json:"owner"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		t, e := rt.Tasks.Complete(a.TaskID, a.Owner)
		if e != nil {
			return "", e
		}
		return t.ID + " completed", nil
	})
	register("bash_background", "Run an independent shell command in background.", map[string]any{"command": stringProp("Command")}, []string{"command"}, func(ctx context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		id, e := rt.Background.Start(ctx, a.Command, 2*time.Minute)
		return id, e
	})
	register("schedule_cron", "Register a five-field cron command.", map[string]any{"spec": stringProp("Cron spec"), "command": stringProp("Command")}, []string{"spec", "command"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			Spec    string `json:"spec"`
			Command string `json:"command"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		j, e := rt.Cron.Add(a.Spec, a.Command)
		return j.ID, e
	})
	register("list_crons", "List scheduled jobs.", map[string]any{}, nil, func(_ context.Context, _ json.RawMessage) (string, error) {
		b, _ := json.Marshal(rt.Cron.Jobs())
		return string(b), nil
	})
	register("load_skill", "Load one skill body on demand.", map[string]any{"name": stringProp("Skill name")}, []string{"name"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		s, e := (skills.Loader{Dir: workdir + "/skills"}).Load(a.Name)
		if e != nil {
			return "", e
		}
		return contextx.Preview(s.Body, 20000), nil
	})
	// 保留教学工具名，避免每课重新复制一套注册逻辑。
	register("request_plan", "Ask the lead for an execution plan.", map[string]any{"summary": stringProp("Plan summary")}, []string{"summary"}, func(_ context.Context, raw json.RawMessage) (string, error) {
		return "Plan requested: " + strings.TrimSpace(string(raw)), nil
	})
	return r, rt, nil
}
