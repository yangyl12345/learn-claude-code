// Package teams 定义 lead 与 teammate 之间的持久化无关消息协议。
package teams

import (
	"errors"
	"sync"
	"time"
)

// Message 是 mailbox 中的协议消息。
type Message struct {
	ID        uint64    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Kind      string    `json:"kind"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
}

// MessageBus 是并发安全的进程内 mailbox；上层可以用 Snapshot 做持久化。
type MessageBus struct {
	mu    sync.Mutex
	next  uint64
	boxes map[string][]Message
}

// NewMessageBus 创建消息总线。
func NewMessageBus() *MessageBus { return &MessageBus{boxes: map[string][]Message{}} }

// Send 发送一条消息。
func (b *MessageBus) Send(from, to, kind, body string) (Message, error) {
	if from == "" || to == "" || kind == "" {
		return Message{}, errors.New("from, to and kind are required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	m := Message{ID: b.next, From: from, To: to, Kind: kind, Body: body, CreatedAt: time.Now().UTC()}
	b.boxes[to] = append(b.boxes[to], m)
	return m, nil
}

// Receive 读取并标记收件箱中的未读消息。
func (b *MessageBus) Receive(to string) []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	all := b.boxes[to]
	out := []Message{}
	for i := range all {
		if !all[i].Read {
			all[i].Read = true
			out = append(out, all[i])
		}
	}
	b.boxes[to] = all
	return out
}

// Snapshot 返回所有消息的副本。
func (b *MessageBus) Snapshot() []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Message{}
	for _, box := range b.boxes {
		out = append(out, box...)
	}
	return out
}
