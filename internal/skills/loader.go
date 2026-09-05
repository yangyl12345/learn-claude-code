// Package skills 实现 s07 的“先列目录、按需加载”技能机制。
package skills

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill 是一个带 frontmatter 的 Markdown 技能。
type Skill struct {
	Name        string
	Description string
	Path        string
	Body        string
}

// Loader 扫描一个 skills 目录。
type Loader struct{ Dir string }

// List 只返回名称和描述，不把全文塞进模型上下文。
func (l Loader) List() ([]Skill, error) {
	es, err := os.ReadDir(l.Dir)
	if err != nil {
		return nil, err
	}
	out := []Skill{}
	for _, e := range es {
		path := filepath.Join(l.Dir, e.Name(), "SKILL.md")
		if e.IsDir() {
			if _, er := os.Stat(path); er == nil {
				b, er := os.ReadFile(path)
				if er != nil {
					return nil, er
				}
				name, desc := frontmatter(string(b))
				out = append(out, Skill{Name: name, Description: desc, Path: path})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Load 按技能名加载完整正文。
func (l Loader) Load(name string) (Skill, error) {
	if strings.TrimSpace(name) == "" {
		return Skill{}, errors.New("skill name is required")
	}
	list, err := l.List()
	if err != nil {
		return Skill{}, err
	}
	for _, s := range list {
		if s.Name == name || filepath.Base(filepath.Dir(s.Path)) == name {
			b, er := os.ReadFile(s.Path)
			if er != nil {
				return Skill{}, er
			}
			s.Body = string(b)
			return s, nil
		}
	}
	return Skill{}, errors.New("skill not found")
}
func frontmatter(s string) (string, string) {
	if !strings.HasPrefix(s, "---") {
		return "", ""
	}
	end := strings.Index(s[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	head := s[3 : 3+end]
	var n, d string
	for _, line := range strings.Split(head, "\n") {
		p := strings.SplitN(line, ":", 2)
		if len(p) != 2 {
			continue
		}
		switch strings.TrimSpace(p[0]) {
		case "name":
			n = strings.Trim(strings.TrimSpace(p[1]), "\"'")
		case "description":
			d = strings.Trim(strings.TrimSpace(p[1]), "\"'")
		}
	}
	return n, d
}
