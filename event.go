package ally

import (
	"sync"
)

// Event 提供一个简单的一次性事件订阅和管理功能
type Event struct {
	mu   sync.Mutex
	done chan struct{} // Emit触发事件后，会关闭此chan，实现事件通知效果

	Args []any // 触发事件时提供的关联数据
}

// NewEvent 初始化并返回一个*Event
func NewEvent() *Event {
	return &Event{done: make(chan struct{})}
}

func (evt *Event) Yes() <-chan struct{} {
	return evt.done
}

// 用于触发事件，支持提供可选的关联数据，只在首次触发时返回true
func (evt *Event) Emit(args ...any) bool {
	evt.mu.Lock()
	defer evt.mu.Unlock()
	select {
	case <-evt.done:
		return false
	default:
		evt.Args = args
		close(evt.done)
		return true
	}
}

// 事件是否已经结束
func (evt *Event) Done() bool {
	evt.mu.Lock()
	defer evt.mu.Unlock()
	select {
	case <-evt.done:
		return true
	default:
		return false
	}
}
