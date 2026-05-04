package conn

import (
	"testing"
	"time"
)

func TestBackoff_StartsAtMin(t *testing.T) {
	b := NewExponentialBackoff()
	// Given: fresh backoff with min=1s, jitterFrac=0.20

	d := b.Next()

	// Then: first delay is within ±20% of 1s
	low := time.Duration(float64(time.Second) * 0.80)
	high := time.Duration(float64(time.Second) * 1.20)
	if d < low || d > high {
		t.Fatalf("first Next() = %v, expected [%v, %v]", d, low, high)
	}
}

func TestBackoff_DoublesPerAttempt(t *testing.T) {
	b := NewExponentialBackoff()

	prev := b.Next() // attempt 0 → ~1s
	for i := 1; i <= 4; i++ {
		cur := b.Next()
		// jitter is ±20%; after doubling, even the low end of cur > high end of prev
		// prev_high = prev_nominal * 1.20, cur_low = cur_nominal * 0.80 = prev_nominal*2*0.80
		// 2*0.80 > 1.20 holds, so cur_low > prev_high strictly for uncapped attempts
		if cur <= prev {
			t.Fatalf("attempt %d: Next()=%v did not increase over previous %v", i, cur, prev)
		}
		prev = cur
	}
}

func TestBackoff_CapsAtMax(t *testing.T) {
	b := NewExponentialBackoff()
	// Drive well past the cap
	for i := 0; i < 20; i++ {
		b.Next()
	}

	d := b.Next()
	high := time.Duration(float64(60*time.Second) * 1.20)
	if d > high {
		t.Fatalf("Next() = %v after many attempts, expected <= %v (max+jitter)", d, high)
	}
}

func TestBackoff_Reset(t *testing.T) {
	b := NewExponentialBackoff()

	// Advance a few attempts
	for i := 0; i < 5; i++ {
		b.Next()
	}

	b.Reset()
	d := b.Next()

	// Then: back to min range
	low := time.Duration(float64(time.Second) * 0.80)
	high := time.Duration(float64(time.Second) * 1.20)
	if d < low || d > high {
		t.Fatalf("after Reset(), Next() = %v, expected [%v, %v]", d, low, high)
	}
}
