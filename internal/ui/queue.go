package ui

import "sync"

type qitem struct {
	line   string
	queued bool // submitted while a turn was running
}

// queue is the submitted-line queue shared by the input loop (producer) and
// the driver/agent (consumer).
type queue struct {
	mu    sync.Mutex
	items []qitem
	eof   bool
	wake  chan struct{}
}

func newQueue() *queue { return &queue{wake: make(chan struct{}, 1)} }

func (q *queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) push(line string, queued bool) {
	q.mu.Lock()
	q.items = append(q.items, qitem{line, queued})
	q.mu.Unlock()
	q.signal()
}

func (q *queue) setEOF() {
	q.mu.Lock()
	q.eof = true
	q.mu.Unlock()
	q.signal()
}

func (q *queue) peek() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return "", false
	}
	return q.items[0].line, true
}

func (q *queue) take() (qitem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return qitem{}, false
	}
	it := q.items[0]
	q.items = q.items[1:]
	return it, true
}

// waitTake blocks until a line is available or EOF.
func (q *queue) waitTake() (qitem, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			it := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return it, true
		}
		if q.eof {
			q.mu.Unlock()
			return qitem{}, false
		}
		q.mu.Unlock()
		<-q.wake
	}
}
