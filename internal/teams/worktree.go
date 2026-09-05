package teams

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager 把 teammate 的工作目录绑定到一个 Git worktree。
type WorktreeManager struct {
	Root string
	Base string
}

// ValidateWorktreeName 检查名称，防止用路径片段逃逸到 worktree 根目录外。
func ValidateWorktreeName(name string) error {
	if name == "" || strings.Contains(name, "..") || filepath.Base(name) != name {
		return errors.New("invalid worktree name")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return errors.New("invalid worktree name")
		}
	}
	return nil
}

// Create 创建一个新分支和工作树，并返回绝对路径。
func (m WorktreeManager) Create(ctx context.Context, name, branch string) (string, error) {
	if err := ValidateWorktreeName(name); err != nil {
		return "", err
	}
	if branch == "" {
		branch = "teammate/" + name
	}
	root, err := filepath.Abs(m.Root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, name)
	if !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return "", errors.New("worktree path escaped root")
	}
	if err := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, dir, m.Base).Run(); err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}
	return dir, nil
}

// Remove 删除一个已创建的工作树。
func (m WorktreeManager) Remove(ctx context.Context, name string) error {
	if err := ValidateWorktreeName(name); err != nil {
		return err
	}
	root, _ := filepath.Abs(m.Root)
	return exec.CommandContext(ctx, "git", "worktree", "remove", filepath.Join(root, name)).Run()
}
