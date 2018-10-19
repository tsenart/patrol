package patrol

import (
	"testing"
	"time"
)

func TestBucket_Take(t *testing.T) {
	rate := Rate{Freq: 60, Per: time.Second} // 60 tokens per second
	interval := rate.Interval()
	bucket := NewBucket(60)
	now := time.Unix(0, 0)

	// Test successive takes from the same bucket.
	for _, tc := range []struct {
		elapsed time.Duration
		take    uint64
		ok      bool
		rem     uint64
	}{
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 59},
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 58}, // no tokens added. elapsed duration is before rate interval elapsed
		{elapsed: time.Millisecond, take: 8, ok: true, rem: 50}, // no tokens added, took 7
		{elapsed: interval, take: 1, ok: true, rem: 50},         // add 1, take 1
		{elapsed: interval, take: 50, ok: true, rem: 1},         // add 1, take 50
		{elapsed: time.Millisecond, take: 2, ok: false, rem: 1}, // not enough tokens
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 0},  // not enough tokens
		{elapsed: time.Millisecond, take: 1, ok: false, rem: 0}, // empty bucket, no tokens taken
		{elapsed: time.Second / 2, take: 0, ok: true, rem: 30},  // 30 tokens replenished
		{elapsed: 5 * time.Minute, take: 0, ok: true, rem: 60},  // all tokens replenished, but capped at capacity
	} {
		now = now.Add(tc.elapsed)
		ok := bucket.Take(now, rate, tc.take)
		rem := uint64(bucket.Added - bucket.Taken)
		if ok != tc.ok || rem != tc.rem {
			t.Errorf(
				"\nBucket%+v:\n\tTake elapsed: %s, rate: %v, n: %d\n\t\thave (%t, %d)\n\t\twant (%t, %d)",
				bucket, tc.elapsed, rate, tc.take, ok, rem, tc.ok, tc.rem,
			)
		}
	}
}
