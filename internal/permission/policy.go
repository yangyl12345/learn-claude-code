// Package permission 定义 Agent 执行外部副作用时的最小权限边界。
package permission

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
)

// Decision 表示策略对一次操作的决定。
type Decision string

const (
	Allow   Decision = "allow"
	Deny    Decision = "deny"
	Confirm Decision = "confirm"
)

// Policy 对命令和路径做静态判断。
type Policy struct {
	Workspace string
	Denylist  []*regexp.Regexp
	ConfirmOn []*regexp.Regexp
}

// NewPolicy 创建包含常见破坏性命令规则的策略。
func NewPolicy(workspace string) Policy {
	patterns := []string{`(^|[;&|])\s*rm\s+-rf`, `\bmkfs\b`, `\bdd\s+if=`, `\bshutdown\b`, `\breboot\b`, `:\(\)\s*\{`}
	deny := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		deny = append(deny, regexp.MustCompile(p))
	}
	return Policy{Workspace: workspace, Denylist: deny, ConfirmOn: []*regexp.Regexp{regexp.MustCompile(`(^|[;&|])\s*rm\b`), regexp.MustCompile(`\bgit\s+push\b`), regexp.MustCompile(`\bchmod\b`)}}
}

// Check 判断 shell 命令是否可执行。
func (p Policy) Check(command string) Decision {
	for _, re := range p.Denylist {
		if re.MatchString(command) {
			return Deny
		}
	}
	for _, re := range p.ConfirmOn {
		if re.MatchString(command) {
			return Confirm
		}
	}
	return Allow
}

// ConsoleBroker 串行化所有审批问题，避免后台任务同时读取 stdin。
type ConsoleBroker struct {
	mu  sync.Mutex
	In  io.Reader
	Out io.Writer
}

// Ask 在锁内读取一次确认答案；空答案视为拒绝。
func (b *ConsoleBroker) Ask(ctx context.Context, question string) (bool, error) {
	if b == nil || b.In == nil {
		return false, errors.New("interactive approval is unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if b.Out != nil {
		_, _ = fmt.Fprint(b.Out, question+" [y/N] ")
	}
	s := bufio.NewScanner(b.In)
	if !s.Scan() {
		if err := s.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(s.Text()))
	return answer == "y" || answer == "yes", nil
}
