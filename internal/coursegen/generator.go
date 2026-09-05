// Package coursegen 从 Go 课程目录生成教学站使用的轻量元数据。
package coursegen

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
)

// Function 描述一个 Go 函数或方法。
type Function struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

// Lesson 描述一个课程版本。
type Lesson struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Source    string     `json:"source"`
	LOC       int        `json:"loc"`
	Functions []Function `json:"functions"`
	Types     []string   `json:"types"`
	Tools     []string   `json:"tools"`
}

// Generator 扫描仓库并把结果输出为 JSON。
type Generator struct {
	RepoRoot  string
	OutputDir string
}

// Generate 生成 versions.json 和 docs.json。
func (g Generator) Generate() error {
	if g.RepoRoot == "" || g.OutputDir == "" {
		return fmt.Errorf("repo root and output dir are required")
	}
	lessons, err := scan(g.RepoRoot)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(g.OutputDir, 0o755); err != nil {
		return err
	}
	diffs := make([]map[string]any, 0)
	for i := 1; i < len(lessons); i++ {
		previous := map[string]bool{}
		for _, name := range lessons[i-1].Tools {
			previous[name] = true
		}
		var added []string
		for _, name := range lessons[i].Tools {
			if !previous[name] {
				added = append(added, name)
			}
		}
		diffs = append(diffs, map[string]any{"from": lessons[i-1].ID, "to": lessons[i].ID, "newTools": added})
	}
	index := map[string]any{"versions": lessons, "diffs": diffs}
	if err = write(filepath.Join(g.OutputDir, "versions.json"), index); err != nil {
		return err
	}
	// docs.json 保持前端所需的数组形状；章节 Markdown 由 web 包装脚本
	// 继续读取三语 README，再把内容与这里的 Go source metadata 合并。
	docs := []any{}
	return write(filepath.Join(g.OutputDir, "docs.json"), docs)
}
func scan(root string) ([]Lesson, error) {
	var out []Lesson
	fset := token.NewFileSet()
	sharedTypes := collectSharedTypes(filepath.Join(root, "internal"))
	for i := 1; i <= 17; i++ {
		id := fmt.Sprintf("s%02d", i)
		matches, err := filepath.Glob(filepath.Join(root, id+"_*", "main.go"))
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			continue
		}
		file := matches[0]
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, file, b, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		lesson := Lesson{ID: id, Title: id, Source: mustRel(root, file), LOC: countLines(b)}
		lesson.Tools = lessonTools(id)
		lesson.Types = append(lesson.Types, sharedTypes...)
		for _, decl := range f.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok.String() == "type" {
				for _, spec := range gen.Specs {
					if named, ok := spec.(*ast.TypeSpec); ok {
						lesson.Types = append(lesson.Types, named.Name.Name)
					}
				}
			}
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			lesson.Functions = append(lesson.Functions, Function{Name: fn.Name.Name, File: lesson.Source, Line: fset.Position(fn.Pos()).Line, Exported: fn.Name.IsExported()})
		}
		out = append(out, lesson)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func collectSharedTypes(dir string) []string {
	seen := map[string]bool{}
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return nil
		}
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok.String() == "type" {
				for _, spec := range gen.Specs {
					if named, ok := spec.(*ast.TypeSpec); ok {
						seen[named.Name.Name] = true
					}
				}
			}
		}
		return nil
	})
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func lessonTools(id string) []string {
	tools := map[string][]string{
		"s01": {"bash"}, "s02": {"bash", "read_file", "write_file", "edit_file", "glob"},
		"s03": {"bash", "permission"}, "s04": {"bash", "hooks"}, "s05": {"bash", "todo_write"},
		"s06": {"bash", "task"}, "s07": {"bash", "load_skill"}, "s08": {"bash", "compact"},
		"s09": {"bash", "memory"}, "s10": {"create_task", "update_task", "list_tasks", "claim_task", "complete_task"},
		"s11": {"bash", "background"}, "s12": {"schedule_cron", "list_crons", "cancel_cron"},
		"s13": {"spawn_teammate", "send_message", "create_worktree"}, "s14": {"connect_mcp", "dynamic_mcp"},
		"s15": {"bash", "memory", "tasks", "teams", "cron", "background", "MCP"},
		"s16": {"agent", "parallel", "pipeline", "snapshot", "resume"}, "s17": {"bash", "goal", "evaluator"},
	}
	return append([]string(nil), tools[id]...)
}
func mustRel(root, path string) string {
	r, e := filepath.Rel(root, path)
	if e != nil {
		return path
	}
	return filepath.ToSlash(r)
}
func countLines(b []byte) int {
	n := 1
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
func write(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
