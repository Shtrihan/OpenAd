package chwriter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type sink struct {
	mu    sync.Mutex
	rows  []int
	fails int // fail this many inserts first
}

func (s *sink) insert(_ context.Context, rows []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fails > 0 {
		s.fails--
		return errors.New("clickhouse down")
	}
	s.rows = append(s.rows, rows...)
	return nil
}

func (s *sink) got() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.rows...)
}

func newBatcher(s *sink, capacity int) *Batcher[int] {
	return &Batcher[int]{
		Name: "t", Insert: s.insert, FlushSize: 3, FlushInterval: 10 * time.Millisecond,
		Capacity: capacity, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestFlushPreservesOrderAcrossFailures(t *testing.T) {
	s := &sink{fails: 1}
	b := newBatcher(s, 100)
	for i := 0; i < 7; i++ {
		b.Add(i)
	}
	// Direct flush: first insert fails, everything stays buffered in order.
	if err := b.flush(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	b.Add(7)
	if err := b.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := s.got()
	if len(got) != 8 {
		t.Fatalf("got %v", got)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("order broken: %v", got)
		}
	}
}

func TestCapacity(t *testing.T) {
	b := newBatcher(&sink{}, 2)
	if !b.Add(1) || !b.Add(2) || b.Add(3) {
		t.Fatal("capacity not enforced")
	}
}

func TestRunFlushesOnShutdown(t *testing.T) {
	s := &sink{}
	b := newBatcher(s, 100)
	b.FlushInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	b.Add(1)
	cancel()
	<-done
	if got := s.got(); len(got) != 1 {
		t.Fatalf("final flush: %v", got)
	}
}
