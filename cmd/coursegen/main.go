// coursegen 是教学站元数据生成命令。
package main

import (
	"flag"
	"fmt"
	"github.com/yangyl12345/learn-claude-code/internal/coursegen"
	"log"
	"os"
)

func main() {
	root := flag.String("repo-root", ".", "repository root")
	out := flag.String("output", "web/src/data/generated", "output directory")
	flag.Parse()
	if err := (coursegen.Generator{RepoRoot: *root, OutputDir: *out}).Generate(); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "course metadata generated")
}
