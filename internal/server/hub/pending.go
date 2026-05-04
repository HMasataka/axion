package hub

import (
	"sync"

	"github.com/HMasataka/axion/internal/proto"
)

// PendingRegistry は correlation_id → 応答 chan の map。
type PendingRegistry struct {
	mu sync.Mutex
	m  map[string]chan proto.Envelope
}

func NewPendingRegistry() *PendingRegistry {
	return &PendingRegistry{
		m: make(map[string]chan proto.Envelope),
	}
}

func (r *PendingRegistry) Register(corrID string) chan proto.Envelope {
	ch := make(chan proto.Envelope, 1)
	r.mu.Lock()
	r.m[corrID] = ch
	r.mu.Unlock()
	return ch
}

// Resolve は corrID に対応する待機 chan に env を送る。待機者がいれば true を返す。
func (r *PendingRegistry) Resolve(corrID string, env proto.Envelope) bool {
	r.mu.Lock()
	ch, ok := r.m[corrID]
	if ok {
		delete(r.m, corrID)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- env
	return true
}

func (r *PendingRegistry) Cancel(corrID string) {
	r.mu.Lock()
	delete(r.m, corrID)
	r.mu.Unlock()
}
