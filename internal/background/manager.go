// Package background 管理不阻塞主 Agent Loop 的后台 shell 任务。
package background

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Result 是后台任务完成后等待注入对话的结果。
type Result struct {
	ID         string
	Output     string
	Err        error
	FinishedAt time.Time
}
type job struct{ cancel context.CancelFunc }

// Manager 提供启动、收集和取消操作；结果只会被 Collect 取出一次。
type Manager struct {
	mu    sync.Mutex
	next  int
	jobs  map[string]job
	ready []Result
}

// NewManager 创建后台任务管理器。
func NewManager() *Manager { return &Manager{jobs: map[string]job{}} }

// Start 启动一个带上下文的 shell 命令并立即返回任务 ID。
func (m *Manager) Start(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	m.mu.Lock()
	m.next++
	id := fmt.Sprintf("bg_%04d", m.next)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	m.jobs[id] = job{cancel: cancel}
	m.mu.Unlock()
	go func() {
		cmd := exec.CommandContext(runCtx, "sh", "-c", command)
		out, err := cmd.CombinedOutput()
		cancel()
		m.mu.Lock()
		delete(m.jobs, id)
		m.ready = append(m.ready, Result{ID: id, Output: string(out), Err: err, FinishedAt: time.Now().UTC()})
		m.mu.Unlock()
	}()
	return id, nil
}

// Collect 取出已完成结果并清空结果队列。
func (m *Manager) Collect() []Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]Result(nil), m.ready...)
	m.ready = nil
	return out
}

// Running 返回当前正在运行的后台任务数量。
func (m *Manager) Running() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.jobs) }

// Cancel 停止指定任务。
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if ok {
		j.cancel()
		return true
	}
	return false
}
