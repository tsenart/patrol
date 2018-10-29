package patrol

import (
	"math/rand"
	"testing"
	"testing/quick"
	"time"
)

func TestBucket_Marshaling(t *testing.T) {
	prop := func(name string, added, taken float64, elapsed time.Duration) bool {
		b := Bucket{
			name:    name,
			added:   added,
			taken:   taken,
			elapsed: elapsed,
		}
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
	rate := Rate{Freq: 5, Per: time.Second} // 60 tokens per second
	interval := rate.Interval()
	bucket := Bucket{created: time.Now()} // 5 initial tokens
	now := bucket.created

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
		{elapsed: interval, take: 2, ok: false, rem: 1},         // not enough tokens
		{elapsed: time.Millisecond, take: 1, ok: true, rem: 0},  // take 1
		{elapsed: time.Millisecond, take: 1, ok: false, rem: 0}, // empty bucket, no tokens taken
		{elapsed: time.Second, take: 0, ok: true, rem: 5},       // tokens replenished
	} {
		now = now.Add(tc.elapsed)
		rem, ok := bucket.Take(now, rate, tc.take)
		if ok != tc.ok || rem != tc.rem {
			t.Errorf(
				"step %d\nBucket%+v:\n\tTake elapsed: %s, rate: %v, n: %d\n\t\thave (%t, %d)\n\t\twant (%t, %d)",
				i, &bucket, tc.elapsed, rate, tc.take, ok, rem, tc.ok, tc.rem,
			)
		}
	}
}

func TestBucket_Merge(t *testing.T) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	buckets := make([]*Bucket, 100)
	for i := range buckets {
		buckets[i] = &Bucket{
			added:   rng.Float64(),              // The P of the PN counter "tokens".
			taken:   rng.Float64(),              // The N of the PN counter "tokens".
			elapsed: time.Duration(rng.Int63()), // A separate "elapsed" duration G-Counter.
		}
	}

	// Compute the result of a merged bucket with sequential operations.
	var sequential Bucket
	for _, bucket := range buckets {
		sequential.Merge(&sequential, bucket)
	}

	// Compute multiple random sequences of merge operations and compare with
	// the sequential result. With this, we test that the Merge operation
	// makes the Bucket a CRDT by holding the following properties independently
	// of merge order:
	//
	// - Associativity (a+(b+c)=(a+b)+c)
	// - Commutativity (a+b=b+a)
	// - Idempotence (a+a=a)
	//
	for i := 0; i < 10000; i++ { // Test 10000 random permutations of Merge order.
		rng.Shuffle(len(buckets), func(i, j int) {
			buckets[i], buckets[j] = buckets[j], buckets[i]
		})

		random := buckets[rng.Int()%len(buckets)]
		for _, bucket := range buckets {
			// Explicitly test idempotence by merging the same bucket twice.
			random.Merge(bucket, bucket)
		}

		if *random != sequential {
			t.Fatalf(
				"Buckets merged in random order diverged from sequential result:\nhave: %v\nwant: %v\nbuckets: %v",
				random,
				&sequential,
				buckets,
			)
		}
	}
}
