// Package tools 包含课程使用的本地工具实现。
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yangyl12345/learn-claude-code/internal/agent"
	"github.com/yangyl12345/learn-claude-code/internal/ai"
	"github.com/yangyl12345/learn-claude-code/internal/permission"
)

const maxToolOutput = 50000

func schema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// NewBaseRegistry 返回 s02 及以后课程共享的五个基础工具。
func NewBaseRegistry(workdir string) *agent.Registry {
	r := agent.NewRegistry()
	_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: "bash", Description: "Run a shell command.", Parameters: schema(map[string]any{"command": stringProp("Shell command to run")}, "command")}, Handler: bash(workdir)})
	_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: "read_file", Description: "Read a UTF-8 file inside the workspace.", Parameters: schema(map[string]any{"path": stringProp("Relative file path"), "limit": map[string]any{"type": "integer"}, "offset": map[string]any{"type": "integer"}}, "path")}, Handler: readFile(workdir), ReadOnly: true})
	_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: "write_file", Description: "Write a UTF-8 file inside the workspace.", Parameters: schema(map[string]any{"path": stringProp("Relative file path"), "content": stringProp("Full file content")}, "path", "content")}, Handler: writeFile(workdir)})
	_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: "edit_file", Description: "Replace one exact text occurrence in a workspace file.", Parameters: schema(map[string]any{"path": stringProp("Relative file path"), "old_text": stringProp("Exact text to replace"), "new_text": stringProp("Replacement text")}, "path", "old_text", "new_text")}, Handler: editFile(workdir)})
	_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: "glob", Description: "Find files matching a recursive glob.", Parameters: schema(map[string]any{"pattern": stringProp("Glob pattern relative to workspace")}, "pattern")}, Handler: globFiles(workdir), ReadOnly: true})
	return r
}

// NewBashRegistry 返回 s01 的最小工具池：只有 bash 一个行动接口。
func NewBashRegistry(workdir string) *agent.Registry {
	r := agent.NewRegistry()
	_ = r.Register(agent.Tool{Definition: ai.ToolDefinition{Name: "bash", Description: "Run a safe shell command.", Parameters: schema(map[string]any{"command": stringProp("Shell command to run")}, "command")}, Handler: bash(workdir)})
	return r
}

func decode[T any](raw []byte) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("invalid arguments: %w", err)
	}
	return value, nil
}

func trimOutput(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no output)"
	}
	if len(s) > maxToolOutput {
		return s[:maxToolOutput] + "\n[output truncated]"
	}
	return s
}

func bash(workdir string) agent.ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		args, err := decode[struct {
			Command string `json:"command"`
		}](raw)
		if err != nil {
			return "", err
		}
		if args.Command == "" {
			return "", errors.New("command is required")
		}
		// 工具边界的静态 deny 规则不依赖交互式 stdin，因此在后台和 CI 中
		// 也能阻止明显危险操作；需要人工确认的命令由上层 Approver 决定。
		if decision := permission.NewPolicy(workdir).Check(args.Command); decision == permission.Deny {
			return "", errors.New("command denied by safety policy")
		}
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", args.Command)
		cmd.Dir = workdir
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return trimOutput(string(out)), fmt.Errorf("command timeout: %w", ctx.Err())
		}
		if err != nil {
			return trimOutput(string(out)), fmt.Errorf("exit status: %w", err)
		}
		return trimOutput(string(out)), nil
	}
}

func readFile(workdir string) agent.ToolHandler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		args, err := decode[struct {
			Path   string `json:"path"`
			Limit  int    `json:"limit"`
			Offset int    `json:"offset"`
		}](raw)
		if err != nil {
			return "", err
		}
		path, err := agent.SafePath(workdir, args.Path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(data), "\n")
		start := args.Offset
		if start < 0 {
			start = 0
		}
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if args.Limit > 0 && start+args.Limit < end {
			end = start + args.Limit
		}
		return trimOutput(strings.Join(lines[start:end], "\n")), nil
	}
}

func writeFile(workdir string) agent.ToolHandler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		args, err := decode[struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}](raw)
		if err != nil {
			return "", err
		}
		path, err := agent.SafePath(workdir, args.Path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), args.Path), nil
	}
}

func editFile(workdir string) agent.ToolHandler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		args, err := decode[struct {
			Path string `json:"path"`
			Old  string `json:"old_text"`
			New  string `json:"new_text"`
		}](raw)
		if err != nil {
			return "", err
		}
		path, err := agent.SafePath(workdir, args.Path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		text := string(data)
		count := strings.Count(text, args.Old)
		if count != 1 {
			return "", fmt.Errorf("old_text must occur exactly once, found %d", count)
		}
		text = strings.Replace(text, args.Old, args.New, 1)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Edited %s", args.Path), nil
	}
}

func globFiles(workdir string) agent.ToolHandler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		args, err := decode[struct {
			Pattern string `json:"pattern"`
		}](raw)
		if err != nil {
			return "", err
		}
		if args.Pattern == "" {
			return "", errors.New("pattern is required")
		}
		var matches []string
		err = filepath.WalkDir(workdir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != workdir && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(workdir, path)
			normalized := filepath.ToSlash(rel)
			pattern := filepath.ToSlash(args.Pattern)
			ok, _ := filepath.Match(pattern, normalized)
			if !ok && strings.Contains(pattern, "**") {
				ok = recursiveMatch(pattern, normalized)
			}
			if ok {
				matches = append(matches, normalized)
			}
			if len(matches) >= 1000 {
				return errors.New("glob result limit exceeded")
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		return strings.Join(matches, "\n"), nil
	}
}

func recursiveMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return false
	}
	return strings.HasPrefix(value, strings.TrimSuffix(parts[0], "/")) && strings.HasSuffix(value, strings.TrimPrefix(parts[1], "/"))
}

// TodoItem 是会话计划中的一个条目。
type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// TodoStore 是线程安全的 Todo 状态存储。
type TodoStore struct {
	mu    sync.RWMutex
	Items []TodoItem
}

// Update 替换整个 Todo 列表并校验状态值。
func (s *TodoStore) Update(items []TodoItem) error {
	for _, item := range items {
		if item.Content == "" {
			return errors.New("todo content cannot be empty")
		}
		if item.Status != "pending" && item.Status != "in_progress" && item.Status != "completed" {
			return fmt.Errorf("invalid todo status %q", item.Status)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = append([]TodoItem(nil), items...)
	return nil
}

// Snapshot 返回 Todo 的副本。
func (s *TodoStore) Snapshot() []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]TodoItem(nil), s.Items...)
}
