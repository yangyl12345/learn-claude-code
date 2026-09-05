// Package contextx 提供轻量的上下文预算、输出持久化和历史压缩原语。
package contextx

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EstimateTokens 是教学用途的保守估算：中文/英文混合文本按约四字节一个 token。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len([]byte(text)) + 3) / 4
}

// Preview 保留文本头尾，避免工具输出吞掉整个上下文。
func Preview(text string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(text) <= max {
		return text
	}
	head := max * 2 / 3
	return text[:head] + "\n...[snipped]...\n" + text[len(text)-(max-head):]
}

// PersistLargeOutput 将完整输出存入目录并返回可放入对话的引用。
func PersistLargeOutput(dir, name, text string) (string, error) {
	if name == "" {
		sum := sha1.Sum([]byte(text))
		name = hex.EncodeToString(sum[:]) + ".txt"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("[完整输出已保存至 %s]\n%s", path, Preview(text, 4000)), nil
}

// Compact 将历史按窗口保留，并用摘要代替较早内容。
func Compact(history []string, keep int) []string {
	if keep < 0 {
		keep = 0
	}
	if len(history) <= keep {
		return append([]string(nil), history...)
	}
	old := strings.Join(history[:len(history)-keep], "\n")
	out := []string{"[历史摘要]\n" + Preview(old, 4000)}
	return append(out, history[len(history)-keep:]...)
}
