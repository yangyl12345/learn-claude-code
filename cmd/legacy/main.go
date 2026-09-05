// legacy 提供旧课程命令到 Go runtime 的兼容映射。
package main

import (
	"context"
	"fmt"
	"github.com/yangyl12345/learn-claude-code/internal/lesson"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/legacy s01|s12|full")
		os.Exit(2)
	}
	id := os.Args[1]
	if id == "full" {
		id = "s15"
	}
	if len(id) == 3 && id[0] == 's' {
		if err := lesson.Run(context.Background(), id, os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "unknown legacy lesson:", id)
	os.Exit(2)
}
