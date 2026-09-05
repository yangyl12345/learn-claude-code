// Package mcp 是 s14 的教学版 MCP mock，不建立网络连接，也不执行任意远端协议。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/yangyl12345/learn-claude-code/internal/ai"
)

// Tool 描述 mock server 暴露的工具。
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Handler     func(context.Context, map[string]any) (string, error)
}

// Client 表示一个已连接的教学 MCP server。
type Client struct {
	Name  string
	tools map[string]Tool
}

// NewMockClient 创建 MCP mock 客户端。
func NewMockClient(name string, tools []Tool) *Client {
	c := &Client{Name: name, tools: map[string]Tool{}}
	for _, t := range tools {
		c.tools[t.Name] = t
	}
	return c
}

// ListTools 实现教学版 tools/list。
func (c *Client) ListTools() []Tool {
	out := []Tool{}
	for _, t := range c.tools {
		out = append(out, t)
	}
	return out
}

// CallTool 实现教学版 tools/call。
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := c.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown MCP tool %s", name)
	}
	if t.Handler == nil {
		return "", errors.New("MCP tool has no handler")
	}
	return t.Handler(ctx, args)
}

// Pool 将多个 server 的工具加入统一命名空间。
type Pool struct {
	mu      sync.RWMutex
	clients map[string]*Client
	tools   map[string]Tool
}

// NewPool 创建空工具池。
func NewPool() *Pool { return &Pool{clients: map[string]*Client{}, tools: map[string]Tool{}} }

var safeName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func normalize(s string) string {
	s = safeName.ReplaceAllString(s, "_")
	if s == "" {
		return "server"
	}
	return s
}

// Connect 连接 mock server，并以 mcp__server__tool 形式注册工具。
func (p *Pool) Connect(c *Client) error {
	if c == nil || c.Name == "" {
		return errors.New("MCP client name is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	server := normalize(c.Name)
	for name, t := range c.tools {
		full := "mcp__" + server + "__" + normalize(name)
		if _, exists := p.tools[full]; exists {
			return fmt.Errorf("MCP tool name collision: %s", full)
		}
		p.tools[full] = t
	}
	p.clients[server] = c
	return nil
}

// Names 返回当前动态工具名。
func (p *Pool) Names() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []string{}
	for n := range p.tools {
		out = append(out, n)
	}
	return out
}

// Definitions 返回动态 MCP 工具的 OpenAI function 定义。
func (p *Pool) Definitions() []ai.ToolDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ai.ToolDefinition, 0, len(p.tools))
	for name, t := range p.tools {
		out = append(out, ai.ToolDefinition{Name: name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

// Handler 将动态工具池适配为 agent.ToolHandler 所需的函数签名。
func (p *Pool) Handler(name string) func(context.Context, []byte) (string, error) {
	return func(ctx context.Context, raw []byte) (string, error) {
		var args map[string]any
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
		return p.Call(ctx, name, args)
	}
}

// Call 根据前缀路由调用。
func (p *Pool) Call(ctx context.Context, full string, args map[string]any) (string, error) {
	p.mu.RLock()
	t, ok := p.tools[full]
	p.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown dynamic tool %s", full)
	}
	return t.Handler(ctx, args)
}
