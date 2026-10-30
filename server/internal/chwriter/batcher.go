// Package chwriter buffers rows in memory and writes them to ClickHouse in
// batches, which is how ClickHouse expects to be fed.
package chwriter

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Batcher collects rows of one table and flushes them by size or interval.
// Failed flushes are retried; while ClickHouse is down rows accumulate up to
// Capacity, after which Add reports false so callers can push back.
type Batcher[T any] struct {
	Name          string
	Insert        func(ctx context.Context, rows []T) error
	FlushSize     int
	FlushInterval time.Duration
	Capacity      int
	Log           *slog.Logger

	mu      sync.Mutex
	buf     []T
	kick    chan struct{}
	dropped atomic.Int64
}

func (b *Batcher[T]) init() {
	if b.kick == nil {
		b.kick = make(chan struct{}, 1)
	}
}

// Add queues a row. It returns false if the buffer is full.
func (b *Batcher[T]) Add(rows ...T) bool {
	b.mu.Lock()
	b.init()
	if len(b.buf)+len(rows) > b.Capacity {
		b.mu.Unlock()
		b.dropped.Add(int64(len(rows)))
		return false
	}
	b.buf = append(b.buf, rows...)
	full := len(b.buf) >= b.FlushSize
	b.mu.Unlock()
	if full {
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
	return true
}

// Run flushes until ctx is cancelled, then makes a final flush attempt.
func (b *Batcher[T]) Run(ctx context.Context) {
	b.mu.Lock()
	b.init()
	b.mu.Unlock()

	ticker := time.NewTicker(b.FlushInterval)
	defer ticker.Stop()
	backoff := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := b.flush(final); err != nil {
				b.Log.Error("final flush failed; rows lost", "table", b.Name, "err", err, "rows", b.pending())
			}
			cancel()
			return
		case <-ticker.C:
		case <-b.kick:
		}
		if backoff > 0 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				continue
			}
		}
		if err := b.flush(ctx); err != nil {
			backoff = min(max(backoff*2, time.Second), 30*time.Second)
			b.Log.Warn("flush failed; will retry", "table", b.Name, "err", err, "rows", b.pending(), "retry_in", backoff.String())
			continue
		}
		backoff = 0
		if n := b.dropped.Swap(0); n > 0 {
			b.Log.Error("rows dropped because buffer was full", "table", b.Name, "rows", n)
		}
	}
}

// Free returns how many more rows fit in the buffer.
func (b *Batcher[T]) Free() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return max(b.Capacity-len(b.buf), 0)
}

func (b *Batcher[T]) pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

// flush writes everything buffered so far. On failure rows go back to the
// front of the buffer, preserving order.
func (b *Batcher[T]) flush(ctx context.Context) error {
	b.mu.Lock()
	rows := b.buf
	b.buf = nil
	b.mu.Unlock()
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += b.FlushSize {
		end := min(start+b.FlushSize, len(rows))
		if err := b.Insert(ctx, rows[start:end]); err != nil {
			b.mu.Lock()
			b.buf = append(append([]T(nil), rows[start:]...), b.buf...)
			b.mu.Unlock()
			return err
		}
	}
	return nil
}
