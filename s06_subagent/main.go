package main

import (
	"context"
	"github.com/yangyl12345/learn-claude-code/internal/lesson"
	"os"
)

func main() {
	if err := lesson.Run(context.Background(), "s06", os.Args[1:], os.Stdout); err != nil {
		println("s06:", err.Error())
		os.Exit(1)
	}
}
