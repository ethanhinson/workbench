package store

import "sync"

// Broker is a tiny in-process pub/sub for "the board changed" signals. The store
// bumps a monotonic revision on every mutation and notifies subscribers; the SSE
// handler and any in-process consumer (e.g. a local TUI) subscribe and re-pull a
// Snapshot. It carries no payload — subscribers fetch the current snapshot — so a
// slow subscriber can never wedge a writer (notifications coalesce).
type Broker struct {
	mu   sync.Mutex
	rev  uint64
	subs map[int]chan uint64
	next int
}

func newBroker() *Broker {
	return &Broker{subs: map[int]chan uint64{}}
}

// Subscribe returns a channel that receives the latest revision on each change,
// plus an unsubscribe func. The channel is buffered depth 1 and coalescing: if a
// subscriber hasn't drained the previous tick, newer ticks overwrite it rather
// than block the writer.
func (b *Broker) Subscribe() (<-chan uint64, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan uint64, 1)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// Rev returns the current revision (subscribers use it to send an initial frame).
func (b *Broker) Rev() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rev
}

// notify bumps the revision and wakes subscribers without blocking.
func (b *Broker) notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rev++
	for _, ch := range b.subs {
		select {
		case ch <- b.rev:
		default:
			// Subscriber is behind; drop the stale value and post the newest.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- b.rev:
			default:
			}
		}
	}
}
