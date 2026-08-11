package web

import (
	"container/heap"
	"sync"
	"time"

	"github.com/brijbyte/orc/internal/session"
)

type wakeEntry struct {
	id  string
	at  time.Time
	gen uint64
}

type wakeHeap []wakeEntry

func (h wakeHeap) Len() int           { return len(h) }
func (h wakeHeap) Less(i, j int) bool { return h[i].at.Before(h[j].at) }
func (h wakeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *wakeHeap) Push(v any)        { *h = append(*h, v.(wakeEntry)) }
func (h *wakeHeap) Pop() any {
	old := *h
	v := old[len(old)-1]
	*h = old[:len(old)-1]
	return v
}

type wakeUpdate struct {
	id   string
	wake string
}

type wakeScheduler struct {
	server  *Server
	updates chan wakeUpdate
	stop    chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	started bool
	closed  bool
}

func newWakeScheduler(server *Server) *wakeScheduler {
	return &wakeScheduler{
		server: server, updates: make(chan wakeUpdate, 256),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (s *wakeScheduler) start() {
	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.loop()
}

func (s *wakeScheduler) arm(id, wake string) {
	if id == "" {
		return
	}
	select {
	case s.updates <- wakeUpdate{id: id, wake: wake}:
	case <-s.stop:
	}
}

func (s *wakeScheduler) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	started := s.started
	close(s.stop)
	s.mu.Unlock()
	if started {
		<-s.done
	}
}

func (s *wakeScheduler) loop() {
	defer close(s.done)
	var wakes wakeHeap
	current := map[string]wakeEntry{}
	var generation uint64
	set := func(id, wake string) {
		generation++
		delete(current, id)
		at, err := time.Parse(time.RFC3339, wake)
		if err != nil {
			return
		}
		entry := wakeEntry{id: id, at: at, gen: generation}
		current[id] = entry
		heap.Push(&wakes, entry)
	}
	rows, _ := session.ListAll()
	for _, row := range rows {
		if row.Routine != "" && row.Wake != "" {
			set(row.ID, row.Wake)
		}
	}

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		for wakes.Len() > 0 {
			next := wakes[0]
			if cur, ok := current[next.id]; ok && cur.gen == next.gen {
				break
			}
			heap.Pop(&wakes)
		}
		wait := time.Hour
		if wakes.Len() > 0 {
			wait = max(time.Until(wakes[0].at), 0)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)

		select {
		case update := <-s.updates:
			set(update.id, update.wake)
		case <-timer.C:
			if wakes.Len() == 0 {
				continue
			}
			entry := heap.Pop(&wakes).(wakeEntry)
			cur, ok := current[entry.id]
			if !ok || cur.gen != entry.gen {
				continue
			}
			delete(current, entry.id)
			rt, err := s.server.openRuntime(entry.id)
			if err != nil {
				set(entry.id, time.Now().UTC().Add(2*time.Minute).Format(time.RFC3339))
				continue
			}
			if rt.Cfg.Routine != "" {
				rt.IO.q.push("/wake", nil, false)
			}
		case <-s.stop:
			return
		}
	}
}
