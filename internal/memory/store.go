// Package memory 提供简单、可审计的 Markdown 记忆文件和关键词检索。
package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Record 是一条记忆。
type Record struct {
	Title string
	Body  string
	Tags  []string
}

// Store 将记忆按独立 Markdown 文件保存。
type Store struct {
	Dir string
	mu  sync.RWMutex
}

// NewStore 创建记忆目录。
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

// Save 写入一条记忆。
func (s *Store) Save(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(strings.ToLower(r.Title))
	if name == "" {
		name = "memory"
	}
	return os.WriteFile(filepath.Join(s.Dir, name+".md"), []byte("# "+r.Title+"\n\n"+r.Body+"\n"), 0o644)
}

// Search 返回包含全部关键词的记忆文件路径。
func (s *Store) Search(query string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	words := strings.Fields(strings.ToLower(query))
	es, _ := os.ReadDir(s.Dir)
	var out []string
	for _, e := range es {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		text := strings.ToLower(string(b))
		ok := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, filepath.Join(s.Dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// Read 读取记忆。
func (s *Store) Read(path string) (string, error)  { return stringMust(os.ReadFile(path)) }
func stringMust(b []byte, e error) (string, error) { return string(b), e }
