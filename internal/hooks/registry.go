// Package hooks 提供与 Agent Loop 解耦的生命周期扩展点。
package hooks

import (
	"context"
	"sync"
)

// Event 是 hook 触发阶段。
type Event string

const (
	UserPromptSubmit Event = "UserPromptSubmit"
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	Stop             Event = "Stop"
)

// Registry 维护按注册顺序执行的回调。
type Registry struct {
	mu    sync.RWMutex
	items map[Event][]func(context.Context, any) error
}

// NewRegistry 创建 registry。
func NewRegistry() *Registry { return &Registry{items: map[Event][]func(context.Context, any) error{}} }

// Register 注册回调。
func (r *Registry) Register(e Event, fn func(context.Context, any) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[e] = append(r.items[e], fn)
}

// Trigger 触发回调，并隔离注册表读锁与执行过程。
func (r *Registry) Trigger(ctx context.Context, e Event, value any) error {
	r.mu.RLock()
	fns := append([]func(context.Context, any) error(nil), r.items[e]...)
	r.mu.RUnlock()
	for _, fn := range fns {
		if err := fn(ctx, value); err != nil {
			return err
		}
	}
	return nil
}
