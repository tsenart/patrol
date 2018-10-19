package patrol

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Buckets is a map from bucket name to Bucket
type Buckets map[string]Bucket

// Bucket implements a simple Token Bucket with underlying
// CRDT G-Counter semantics which allow it to be merged without
// coordination with other Buckets.
type Bucket struct {
	Added float64
	Taken float64
	Last  int64 // Unix nanoseconds timestamp since epoch
}

// Rate defines the maximum frequency of some events.
// Rate is represented as number of events per unit of time.
// A zero Rate allows no events.
type Rate struct {
	Freq int
	Per  time.Duration
}

// ParseRate returns a new Rate parsed from the give string.
func ParseRate(v string) (r Rate, err error) {
	ps := strings.SplitN(v, ":", 2)
	switch len(ps) {
	case 1:
		ps = append(ps, "1s")
	case 0:
		return Rate{}, fmt.Errorf("format %q doesn't match the \"freq:duration\" format (i.e. 50:1s)", v)
	}

	r.Freq, err = strconv.Atoi(ps[0])
	if err != nil {
		return r, err
	}

	switch ps[1] {
	case "ns", "us", "µs", "ms", "s", "m", "h":
		ps[1] = "1" + ps[1]
	}

	r.Per, err = time.ParseDuration(ps[1])
	return r, err
}

// IsZero returns true if either Freq or Per are zero valued.
func (r Rate) IsZero() bool {
	return r.Freq == 0 || r.Per == 0
}

// Tokens is a unit conversion function from a time duration to the number of tokens
// which could be accumulated during that duration at the given rate.
func (r Rate) Tokens(d time.Duration) float64 {
	if r.IsZero() {
		return 0
	}

	interval := r.Interval()
	if interval == 0 {
		return 0
	}

	return float64(d) / float64(interval)
}

// Interval returns the Rate's interval between events.
func (r Rate) Interval() time.Duration {
	return r.Per / time.Duration(r.Freq)
}

// NewBucket returns a new Bucket with the given pre-filled tokens.
func NewBucket(tokens uint64) *Bucket {
	return &Bucket{Added: float64(tokens)}
}

// Take attempts to take n tokens out of the Bucket with the given capacity and
// filling Rate at time t.
func (b *Bucket) Take(t time.Time, r Rate, n uint64) (ok bool) {
	last, now := b.Last, t.UnixNano()
	if now < last {
		last = now
	}

	// Token bucket capacity
	capacity := float64(r.Freq)

	// Calculate the current number of tokens.
	tokens := b.Added - b.Taken

	// Calculate the added number of tokens due to elapsed time.
	added := r.Tokens(time.Duration(now - last))
	if missing := capacity - tokens; added > missing {
		added = missing
	}

	taken := float64(n)
	if ok = taken <= tokens+added; ok {
		b.Last = now
		b.Added += added
		b.Taken += taken
	} else {
		b.Last = last
	}

	return ok
}

// Merge merges multiple Buckets, using G-counter CRDT semantics
// with its counters, picking the largest values for each field.
func Merge(bs ...Bucket) Bucket {
	var max Bucket
	for _, b := range bs {
		if max.Added < b.Added {
			max.Added = b.Added
		}

		if max.Taken < b.Taken {
			max.Taken = b.Taken
		}

		if max.Last < b.Last {
			max.Last = b.Last
		}
	}
	return max
}
