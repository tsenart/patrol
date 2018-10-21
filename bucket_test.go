package patrol

import (
	"testing"
	"testing/quick"
	"time"
)

func TestBucket_Marshaling(t *testing.T) {
	prop := func(b Bucket) bool {
		data, err := b.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}

		var decoded Bucket
		if err = decoded.UnmarshalBinary(data); err != nil {
			t.Fatal(err)
		}

		return b == decoded
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 1e5}); err != nil {
		t.Fatal(err)
	}
}
func TestBucket_Take(t *testing.T) {
	rate := Rate{Freq: 60, Per: time.Second} // 60 tokens per second
	interval := rate.Interval()
	bucket := NewBucket(60)
	now := time.Unix(0, 0)

	// Test successive takes from the same bucket.
	for i, tc := range []struct {
		elapsed time.Duration
		take    uint64
		ok      bool
		rem     uint64
	}{
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 4},
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 3},  // no tokens added. elapsed duration is before rate interval elapsed
		{elapsed: time.Millisecond, take: 3, ok: true, rem: 0},  // no tokens added, took 7
		{elapsed: interval, take: 1, ok: true, rem: 0},          // add 1, take 1
		{elapsed: interval, take: 2, ok: false, rem: 0},         // not enough tokens
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 0},  // take 1
		{elapsed: time.Millisecond, take: 1, ok: false, rem: 0}, // empty bucket, no tokens taken
		{elapsed: time.Second, take: 0, ok: true, rem: 5},       // tokens replenished
	} {
		now = now.Add(tc.elapsed)
		ok := bucket.Take(now, rate, tc.take)
		rem := uint64(bucket.Added - bucket.Taken)
		if ok != tc.ok || rem != tc.rem {
			t.Errorf(
				"step %d\nBucket%+v:\n\tTake elapsed: %s, rate: %v, n: %d\n\t\thave (%t, %d)\n\t\twant (%t, %d)",
				i, bucket, tc.elapsed, rate, tc.take, ok, rem, tc.ok, tc.rem,
			)
		}
	}
}
