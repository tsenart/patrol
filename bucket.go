package patrol

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Bucket implements a simple Token Bucket with underlying
// CRDT PN-Counter semantics which allow it to be merged without
// coordination with other Buckets.
type Bucket struct {
	// Name of the Bucket.
	Name string
	// Added tokens.
	Added float64
	// Taken tokens.
	Taken float64
	// Elapsed time since creation until the last successful Take.
	Elapsed time.Duration
	// Local created timestamp off of which all time deltas are calculated.
	Created time.Time
}

// ErrNameTooLarge is returns by Bucket.MarshalBinary if the name of the
// Bucket exceeds math.MaxUint16 bytes.
var ErrNameTooLarge = fmt.Errorf("bucket name larger than %d", math.MaxUint16)

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (b Bucket) MarshalBinary() ([]byte, error) {
	if len(b.Name) > math.MaxUint16 {
		return nil, ErrNameTooLarge
	}

	data := make([]byte, 26+len(b.Name))
	binary.BigEndian.PutUint64(data, math.Float64bits(b.Added))
	binary.BigEndian.PutUint64(data[8:], math.Float64bits(b.Taken))
	binary.BigEndian.PutUint64(data[16:], uint64(b.Elapsed))
	binary.BigEndian.PutUint16(data[24:], uint16(len(b.Name)))
	copy(data[26:], []byte(b.Name)) // TODO: Remove allocation

	return data, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (b *Bucket) UnmarshalBinary(data []byte) error {
	if len(data) < 26 {
		return io.ErrShortBuffer
	}

	b.Added = math.Float64frombits(binary.BigEndian.Uint64(data))
	b.Taken = math.Float64frombits(binary.BigEndian.Uint64(data[8:]))
	b.Elapsed = time.Duration(binary.BigEndian.Uint64(data[16:]))

	nameLen := int(binary.BigEndian.Uint16(data[24:]))
	if len(data[26:]) < nameLen {
		return io.ErrShortBuffer
	}
	b.Name = string(data[26 : 26+nameLen])

	return nil
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

// NewBucket returns a new Bucket with the given pre-filled tokens.
func NewBucket(tokens uint64) Bucket {
	return Bucket{Added: float64(tokens)}
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

// Tokens returns the number of tokens in the Bucket.
func (b Bucket) Tokens() uint64 {
	return uint64(b.Added - b.Taken)
}

// Take attempts to take n tokens out of the Bucket with the given filling Rate at time t.
func (b *Bucket) Take(now time.Time, r Rate, n uint64) (ok bool) {
	if b.Created.IsZero() {
		b.Created = now
	}

	last := b.Created.Add(b.Elapsed)
	if now.Before(last) {
		last = now
	}

	// Capacity is the number of tokens that can be taken out of the bucket in
	// a single Take call, also known as burstiness. However, it *also* determines
	// how quickly the bucket is depleted when the take rate is above the refill rate.
	//
	// The larger the bucket, the slower it'll be to empty it at take rates
	// that only slightly exceed the refill rate. e.g. refill: 100/s, take: 105/s
	// Empirically, a value of 5 results in short policy violation windows for these cases
	// but it is also large enough to not be too sensitve to variable burstiness.
	const capacity = float64(5)

	// Calculate the current number of tokens.
	tokens := b.Added - b.Taken

	// Calculate the elapsed time since the last successful Take.
	elapsed := now.Sub(last)

	// Calculate the added number of tokens due to elapsed time.
	added := r.Tokens(elapsed)
	if missing := capacity - tokens; added > missing {
		added = missing
	}

	taken := float64(n)
	if taken > tokens+added { // Not enough tokens.
		return false
	}

	b.Elapsed += elapsed
	b.Added += added
	b.Taken += taken
	return true
}

// Merge merges multiple Buckets into the given Bucket,
// using PN-counter CRDT semantics with its counters, picking
// the largest value for each field.
func (b *Bucket) Merge(others ...*Bucket) {
	for _, other := range others {
		if b.Added < other.Added { // Find the maximum added
			b.Added = other.Added
		}

		if b.Taken < other.Taken { // Find the maximum taken
			b.Taken = other.Taken
		}

		if b.Elapsed < other.Elapsed { // Find the largest elapsed time.
			b.Elapsed = other.Elapsed
		}
	}
}
