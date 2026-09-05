package main

import (
	"context"
	"github.com/yangyl12345/learn-claude-code/internal/lesson"
	"os"
)

func main() {
	if err := lesson.BackgroundDemo(context.Background(), os.Stdout); err != nil {
		println("s11:", err.Error())
		os.Exit(1)
	}
}
