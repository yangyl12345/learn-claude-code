// Package cron 提供教学用途的五字段 cron 调度器。
package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job 是一个定时任务。
type Job struct {
	ID      string `json:"id"`
	Spec    string `json:"spec"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
}
type schedule struct {
	job    Job
	fields [5]map[int]bool
}

// Scheduler 是线程安全的内存调度器；持久化可以由调用者把 Jobs 写入 JSON。
type Scheduler struct {
	mu     sync.Mutex
	jobs   map[string]schedule
	next   int
	cancel context.CancelFunc
}

// NewScheduler 创建调度器。
func NewScheduler() *Scheduler { return &Scheduler{jobs: map[string]schedule{}} }

// Add 校验并加入五字段任务（分 时 日 月 周）。
func (s *Scheduler) Add(spec, command string) (Job, error) {
	f, e := Parse(spec)
	if e != nil {
		return Job{}, e
	}
	if command == "" {
		return Job{}, fmt.Errorf("command is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	j := Job{ID: fmt.Sprintf("cron_%04d", s.next), Spec: spec, Command: command, Enabled: true}
	s.jobs[j.ID] = schedule{job: j, fields: f}
	return j, nil
}

// Remove 删除任务。
func (s *Scheduler) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; ok {
		delete(s.jobs, id)
		return true
	}
	return false
}

// Jobs 返回稳定排序的任务快照。
func (s *Scheduler) Jobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Job{}
	for _, x := range s.jobs {
		out = append(out, x.job)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].ID < out[i].ID {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Due 返回给定时间应触发的任务。
func (s *Scheduler) Due(t time.Time) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Job{}
	for _, x := range s.jobs {
		if x.job.Enabled && match(x.fields, t) {
			out = append(out, x.job)
		}
	}
	return out
}

// Run 启动每分钟一次的调度循环，ctx 结束时退出。
func (s *Scheduler) Run(ctx context.Context, trigger func(Job)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			for _, j := range s.Due(t) {
				trigger(j)
			}
		}
	}
}

// Parse 解析标准五字段 cron；支持 *、逗号、范围和步长。
func Parse(spec string) ([5]map[int]bool, error) {
	var out [5]map[int]bool
	parts := strings.Fields(spec)
	if len(parts) != 5 {
		return out, fmt.Errorf("cron requires five fields")
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, p := range parts {
		m, e := parseField(p, ranges[i][0], ranges[i][1])
		if e != nil {
			return out, fmt.Errorf("field %d: %w", i+1, e)
		}
		out[i] = m
	}
	return out, nil
}
func parseField(s string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		step := 1
		if strings.Contains(part, "/") {
			a := strings.Split(part, "/")
			if len(a) != 2 {
				return nil, fmt.Errorf("bad step")
			}
			part = a[0]
			var e error
			step, e = strconv.Atoi(a[1])
			if e != nil || step <= 0 {
				return nil, fmt.Errorf("bad step")
			}
		}
		lo, hi := min, max
		if part != "*" {
			if strings.Contains(part, "-") {
				a := strings.Split(part, "-")
				if len(a) != 2 {
					return nil, fmt.Errorf("bad range")
				}
				var e error
				lo, e = strconv.Atoi(a[0])
				if e != nil {
					return nil, e
				}
				hi, e = strconv.Atoi(a[1])
				if e != nil {
					return nil, e
				}
			} else {
				var e error
				lo, e = strconv.Atoi(part)
				if e != nil {
					return nil, e
				}
				hi = lo
			}
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("value out of range")
		}
		for n := lo; n <= hi; n += step {
			out[n] = true
		}
	}
	return out, nil
}
func match(f [5]map[int]bool, t time.Time) bool {
	return f[0][t.Minute()] && f[1][t.Hour()] && f[2][t.Day()] && f[3][int(t.Month())] && f[4][int(t.Weekday())]
}
