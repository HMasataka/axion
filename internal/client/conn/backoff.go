package conn

import (
	"math"
	"math/rand/v2"
	"time"
)

// ExponentialBackoff は 1s から開始し、最大 60s まで倍々で増加する。
// jitter は ±20% のランダム化。
type ExponentialBackoff struct {
	min        time.Duration
	max        time.Duration
	jitterFrac float64
	attempt    int
	rand       *rand.Rand
}

func NewExponentialBackoff() *ExponentialBackoff {
	src := rand.NewPCG(uint64(time.Now().UnixNano()), 0)
	return &ExponentialBackoff{
		min:        time.Second,
		max:        60 * time.Second,
		jitterFrac: 0.20,
		attempt:    0,
		rand:       rand.New(src),
	}
}

// Next は次の待機時間を返し、attempt をインクリメントする。
func (b *ExponentialBackoff) Next() time.Duration {
	delay := math.Min(float64(b.min)*math.Pow(2, float64(b.attempt)), float64(b.max))
	jitter := delay * b.jitterFrac * (b.rand.Float64()*2 - 1) // ±jitterFrac
	b.attempt++
	return time.Duration(delay + jitter)
}

// Reset は attempt を 0 に戻す（接続成功時に呼ぶ）。
func (b *ExponentialBackoff) Reset() {
	b.attempt = 0
}
