// Package workflow 提供可恢复的固定形状 workflow 运行时。
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Step 是 workflow 的一个可执行节点。
type Step struct {
	Name string
	Run  func() (any, error)
}

// Run 是一次 workflow 的状态。
type Run struct {
	ID        string         `json:"id"`
	Workflow  string         `json:"workflow"`
	Status    string         `json:"status"`
	Outputs   map[string]any `json:"outputs"`
	Error     string         `json:"error,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Journal 以追加事件的方式记录可恢复状态。
type Journal struct {
	mu   sync.Mutex
	Path string
}

// NewJournal 创建 journal。
func NewJournal(path string) *Journal { return &Journal{Path: path} }

// Append 原子追加一条 JSONL 事件。
func (j *Journal) Append(event any) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(j.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(j.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// Read 返回 journal 全部事件；损坏事件会阻止恢复。
func (j *Journal) Read() ([]json.RawMessage, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	b, err := os.ReadFile(j.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var raw json.RawMessage
		if !json.Valid(line) {
			return nil, errors.New("workflow journal is corrupt")
		}
		raw = append([]byte(nil), line...)
		out = append(out, raw)
	}
	return out, nil
}
func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// Runner 执行串行步骤，并在每一步后写 snapshot 和 journal。
type Runner struct {
	Dir string
	mu  sync.Mutex
}

// NewRunner 创建 workflow runner。
func NewRunner(dir string) *Runner { return &Runner{Dir: dir} }

// Execute 执行 workflow；run ID 已存在时拒绝覆盖原结果。
func (r *Runner) Execute(name, id string, steps []Step) (Run, error) {
	if name == "" || id == "" {
		return Run{}, errors.New("workflow name and run id are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return Run{}, err
	}
	path := filepath.Join(r.Dir, id+".snapshot.json")
	if _, err := os.Stat(path); err == nil {
		return Run{}, fmt.Errorf("run %s already exists", id)
	}
	state := Run{ID: id, Workflow: name, Status: "running", Outputs: map[string]any{}, UpdatedAt: time.Now().UTC()}
	j := NewJournal(filepath.Join(r.Dir, id+".journal"))
	if err := j.Append(state); err != nil {
		return Run{}, err
	}
	for _, step := range steps {
		if step.Name == "" || step.Run == nil {
			return state, errors.New("invalid workflow step")
		}
		value, err := step.Run()
		if err != nil {
			state.Status = "failed"
			state.Error = err.Error()
			state.UpdatedAt = time.Now().UTC()
			_ = j.Append(state)
			_ = writeJSON(path, state)
			return state, err
		}
		state.Outputs[step.Name] = value
		state.UpdatedAt = time.Now().UTC()
		if err := j.Append(map[string]any{"step": step.Name, "output": value}); err != nil {
			return state, err
		}
		if err := writeJSON(path, state); err != nil {
			return state, err
		}
	}
	state.Status = "completed"
	state.UpdatedAt = time.Now().UTC()
	_ = j.Append(state)
	if err := writeJSON(path, state); err != nil {
		return state, err
	}
	return state, nil
}

// Parallel 并发执行相互独立的步骤，并等待全部结果；任一步失败都会返回错误。
func (r *Runner) Parallel(ctx context.Context, steps []Step) (map[string]any, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	values := make(map[string]any)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, len(steps))
	for _, step := range steps {
		step := step
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				errCh <- err
				return
			}
			value, err := step.Run()
			if err != nil {
				errCh <- fmt.Errorf("parallel step %s: %w", step.Name, err)
				cancel()
				return
			}
			mu.Lock()
			values[step.Name] = value
			mu.Unlock()
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return nil, err
	default:
		return values, nil
	}
}

// Pipeline 按顺序执行步骤；每一步可读取前一步输出。
func Pipeline(ctx context.Context, steps []func(context.Context, any) (any, error)) (any, error) {
	var value any
	var err error
	for _, step := range steps {
		if e := ctx.Err(); e != nil {
			return value, e
		}
		value, err = step(ctx, value)
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

// Resume 读取已完成或失败的 snapshot，不会重复调用步骤。
func (r *Runner) Resume(id string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var state Run
	b, err := os.ReadFile(filepath.Join(r.Dir, id+".snapshot.json"))
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(b, &state); err != nil {
		return state, fmt.Errorf("decode workflow snapshot: %w", err)
	}
	return state, nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".snapshot-*.tmp")
	if e != nil {
		return e
	}
	n := tmp.Name()
	defer os.Remove(n)
	if _, e = tmp.Write(b); e == nil {
		e = tmp.Close()
	}
	if e != nil {
		return e
	}
	return os.Rename(n, path)
}
