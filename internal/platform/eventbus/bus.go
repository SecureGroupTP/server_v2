package eventbus

import (
	"encoding/hex"
	"sync"
	"sync/atomic"
)

// Bus is an in-process best-effort notifier keyed by user public key.
//
// It is used to wake up active connections so they can immediately flush
// queued events via PullEvents without requiring client-side polling.
//
// NOTE: This does not provide cross-process delivery. In multi-instance
// deployments you'll want to back this with a shared pub/sub (e.g. Postgres
// LISTEN/NOTIFY).
type Bus struct {
	mu   sync.RWMutex
	subs map[string]map[uint64]chan struct{}
	next atomic.Uint64
}

func New() *Bus {
	return &Bus{subs: make(map[string]map[uint64]chan struct{})}
}

func (b *Bus) Subscribe(userPublicKey []byte) (<-chan struct{}, func()) {
	key := keyString(userPublicKey)
	return b.SubscribeKey(key)
}

func (b *Bus) SubscribeKey(key string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	id := b.next.Add(1)

	b.mu.Lock()
	m := b.subs[key]
	if m == nil {
		m = make(map[uint64]chan struct{})
		b.subs[key] = m
	}
	m[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if m := b.subs[key]; m != nil {
			if c := m[id]; c != nil {
				delete(m, id)
				close(c)
			}
			if len(m) == 0 {
				delete(b.subs, key)
			}
		}
		b.mu.Unlock()
	}
}

func (b *Bus) Notify(userPublicKey []byte) {
	key := keyString(userPublicKey)
	b.NotifyKey(key)
}

func (b *Bus) NotifyKey(key string) {
	b.mu.RLock()
	m := b.subs[key]
	// Copy channels to avoid holding the lock while sending.
	chs := make([]chan struct{}, 0, len(m))
	for _, ch := range m {
		chs = append(chs, ch)
	}
	b.mu.RUnlock()

	for _, ch := range chs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func keyString(userPublicKey []byte) string {
	if len(userPublicKey) == 0 {
		return ""
	}
	return hex.EncodeToString(userPublicKey)
}
