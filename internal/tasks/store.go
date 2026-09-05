// Package tasks 提供带依赖关系、所有者和原子领取语义的持久化任务图。
package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Task 是任务图中的持久化节点。
type Task struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	Owner       string    `json:"owner,omitempty"`
	BlockedBy   []string  `json:"blocked_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store 是线程安全的文件任务仓库。
type Store struct {
	dir string
	mu  sync.RWMutex
}

// NewStore 创建任务目录；每个任务单独一个 JSON 文件，降低并发写冲突。
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}
func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }
func newID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("task_%d", time.Now().UnixNano())
	}
	return "task_" + hex.EncodeToString(b)
}

// Create 新建一个 pending 任务。
func (s *Store) Create(subject, description string) (Task, error) {
	if strings.TrimSpace(subject) == "" {
		return Task{}, errors.New("subject is required")
	}
	now := time.Now().UTC()
	t := Task{ID: newID(), Subject: subject, Description: description, Status: "pending", CreatedAt: now, UpdatedAt: now}
	return t, s.Save(t)
}

// Save 原子写入任务，避免进程中断留下半个 JSON。
func (s *Store) Save(t Task) error {
	if t.ID == "" {
		return errors.New("task id is required")
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := os.CreateTemp(s.dir, ".task-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0o644)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.path(t.ID))
}

// Get 读取任务。
func (s *Store) Get(id string) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var t Task
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("decode task: %w", err)
	}
	return t, nil
}

// List 按任务 ID 返回稳定排序的任务列表。
func (s *Store) List() ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var t Task
		b, er := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if er != nil {
			return nil, er
		}
		if er = json.Unmarshal(b, &t); er != nil {
			return nil, er
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) dependsLocked(id, target string, seen map[string]bool) bool {
	if id == target {
		return true
	}
	if seen[id] {
		return false
	}
	seen[id] = true
	t, err := s.Get(id)
	if err != nil {
		return false
	}
	for _, d := range t.BlockedBy {
		if d == target || s.dependsLocked(d, target, seen) {
			return true
		}
	}
	return false
}

// AddDependencies 添加依赖，并拒绝不存在任务和循环依赖。
func (s *Store) AddDependencies(id string, deps ...string) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	for _, d := range deps {
		if d == id {
			return errors.New("task cannot depend on itself")
		}
		if _, err = s.Get(d); err != nil {
			return fmt.Errorf("dependency %s: %w", d, err)
		}
		if s.dependsLocked(d, id, map[string]bool{}) {
			return fmt.Errorf("dependency would create a cycle")
		}
		found := false
		for _, old := range t.BlockedBy {
			if old == d {
				found = true
			}
		}
		if !found {
			t.BlockedBy = append(t.BlockedBy, d)
		}
	}
	t.UpdatedAt = time.Now().UTC()
	return s.Save(t)
}

// Claim 原子地领取一个就绪任务；同一 owner 同时只能持有一个进行中任务。
func (s *Store) Claim(id, owner string) (Task, error) {
	if owner == "" {
		owner = "agent"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.getUnlocked(id)
	if err != nil {
		return t, err
	}
	if t.Status != "pending" {
		return t, fmt.Errorf("task is %s", t.Status)
	}
	for _, d := range t.BlockedBy {
		dep, er := s.getUnlocked(d)
		if er != nil {
			return t, er
		}
		if dep.Status != "completed" {
			return t, fmt.Errorf("task blocked by %s", d)
		}
	}
	list, er := s.listUnlocked()
	if er != nil {
		return t, er
	}
	for _, other := range list {
		if other.Owner == owner && other.Status == "in_progress" {
			return t, fmt.Errorf("owner already has task %s", other.ID)
		}
	}
	t.Status = "in_progress"
	t.Owner = owner
	t.UpdatedAt = time.Now().UTC()
	return t, s.saveUnlocked(t)
}

// Complete 标记任务完成并校验领取者。
func (s *Store) Complete(id, owner string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.getUnlocked(id)
	if err != nil {
		return t, err
	}
	if t.Status != "in_progress" {
		return t, fmt.Errorf("task is %s", t.Status)
	}
	if owner != "" && t.Owner != owner {
		return t, errors.New("task owner mismatch")
	}
	t.Status = "completed"
	t.UpdatedAt = time.Now().UTC()
	return t, s.saveUnlocked(t)
}
func (s *Store) getUnlocked(id string) (Task, error) {
	var t Task
	b, e := os.ReadFile(s.path(id))
	if e != nil {
		return t, e
	}
	e = json.Unmarshal(b, &t)
	return t, e
}
func (s *Store) listUnlocked() ([]Task, error) {
	es, e := os.ReadDir(s.dir)
	if e != nil {
		return nil, e
	}
	var out []Task
	for _, x := range es {
		if x.IsDir() || !strings.HasSuffix(x.Name(), ".json") {
			continue
		}
		t, e := s.getUnlocked(strings.TrimSuffix(x.Name(), ".json"))
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, nil
}
func (s *Store) saveUnlocked(t Task) error {
	data, e := json.MarshalIndent(t, "", "  ")
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(s.dir, ".task-*.tmp")
	if e != nil {
		return e
	}
	n := tmp.Name()
	defer os.Remove(n)
	if _, e = tmp.Write(data); e == nil {
		e = tmp.Close()
	}
	if e != nil {
		return e
	}
	return os.Rename(n, s.path(t.ID))
}
