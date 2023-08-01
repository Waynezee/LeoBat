package utils

import (
	"container/heap"
)

type Interface interface {
	Less(other interface{}) bool
}

type sorter []Interface

// Implement heap.Interface: Push, Pop, Len, Less, Swap
func (s *sorter) Push(x interface{}) {
	*s = append(*s, x.(Interface))
}

func (s *sorter) Pop() interface{} {
	n := len(*s)
	if n > 0 {
		x := (*s)[n-1]
		*s = (*s)[0 : n-1]
		return x
	}
	return nil
}

func (s *sorter) Len() int {
	return len(*s)
}

func (s *sorter) Less(i, j int) bool {
	return (*s)[i].Less((*s)[j])
}

func (s *sorter) Swap(i, j int) {
	(*s)[i], (*s)[j] = (*s)[j], (*s)[i]
}

// Define priority queue struct
type PriorityQueue struct {
	s *sorter
	//search map[interface{}]struct{}
}

func NewPriorityQueue() *PriorityQueue {
	q := &PriorityQueue{s: new(sorter)}
	heap.Init(q.s)
	return q
}

//func (q *PriorityQueue) Find(key interface{}) bool {
//	_, ok := q.search[key]
//	return ok
//}

func (q *PriorityQueue) Push(x Interface) {
	heap.Push(q.s, x)
	//q.search[x] = struct{}{}
}

func (q *PriorityQueue) Pop() Interface {
	item := heap.Pop(q.s)
	//delete(q.search, item)
	return item.(Interface)
}

func (q *PriorityQueue) Top() Interface {
	if len(*q.s) > 0 {
		return (*q.s)[0].(Interface)
	}
	return nil
}

func (q *PriorityQueue) Len() int {
	return q.s.Len()
}
