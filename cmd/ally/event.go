package main

import (
	"sync"
	"time"
)

// 不得import ally，所以Event必须copy

// Event 提供一个简单的一次性事件订阅和管理功能
type Event struct {
	mu   sync.Mutex
	Yes  chan struct{} // Emit触发事件后，会关闭此chan，实现事件通知效果
	Args []any         // 触发事件时提供的关联数据
	When *time.Time    // 在触发事件后才有时间，也可以根据时间来推断时间是否已经发生

	// 用于触发事件，支持提供可选的关联数据，只在首次触发时返回true
	Emit func(args ...any) bool
}

// NewEvent 初始化并返回一个*Event
func NewEvent() *Event {
	var evt = &Event{}
	evt.Yes = make(chan struct{})
	evt.Emit = func(args ...any) bool {
		evt.mu.Lock()
		if evt.When != nil {
			evt.mu.Unlock()
			return false
		}

		var t = time.Now()
		evt.Args = args
		evt.When = &t
		close(evt.Yes)
		evt.Emit = func(...any) bool { return false }
		evt.mu.Unlock()
		return true
	}
	return evt
}
