package asicio

import (
	"container/list"
	"sync"
)

type Fifo struct {
	_list *list.List
	_lock sync.Mutex
}

func NewFifo() *Fifo {
	myfifo := Fifo{
		_list: list.New(),
	}
	return &myfifo
}

func (ff *Fifo) Push(v interface{}) {
	ff._lock.Lock()
	defer ff._lock.Unlock()
	ff._list.PushBack(v)
}

func (ff *Fifo) Pop() interface{} {
	ff._lock.Lock()
	defer ff._lock.Unlock()
	e := ff._list.Front()
	if e != nil {
		ff._list.Remove(e)
	}
	if e != nil {
		return e.Value
	} else {
		return nil
	}
}

func (ff *Fifo) Clear() {
	ff._lock.Lock()
	defer ff._lock.Unlock()
	ff._list.Init()
}
