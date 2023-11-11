package memory

import (
	"container/heap"
	"time"
)

type trackedItem struct {
	expires time.Time
	key     string
}

type trackedItems []*trackedItem

func (ti trackedItems) Len() int {
	return len(ti)
}

func (ti trackedItems) Less(i, j int) bool {
	return ti[i].expires.Before(ti[j].expires)
}

func (ti trackedItems) Swap(i, j int) {
	ti[i], ti[j] = ti[j], ti[i]
}

func (ti *trackedItems) Push(e any) {
	*ti = append(*ti, e.(*trackedItem))
}

func (ti *trackedItems) Pop() any {
	n := len(*ti)
	e := (*ti)[n-1]
	(*ti)[n-1] = nil
	*ti = (*ti)[:n-1]
	return e
}

type evictionQueue struct {
	items trackedItems
}

func newEvictionQueue() *evictionQueue {
	eq := new(evictionQueue)
	heap.Init(&eq.items)
	return eq
}

func (eq *evictionQueue) Push(key string, expires time.Time) {
	heap.Push(&eq.items, &trackedItem{
		expires: expires,
		key:     key,
	})
}

func (eq *evictionQueue) Pop() *trackedItem {
	return heap.Pop(&eq.items).(*trackedItem)
}

func (eq *evictionQueue) Peek() *trackedItem {
	return eq.items[0]
}

func (eq *evictionQueue) Len() int {
	return eq.items.Len()
}
