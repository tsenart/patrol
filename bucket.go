package patrol

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// Bucket implements a simple Token Bucket with underlying
// CRDT PN-Counter semantics which allow it to be merged without
// coordination with other Buckets.
type Bucket struct {
	mu sync.RWMutex
	// name of the Bucket.
	name string
	// added tokens.
	added float64
	// taken tokens.
	taken float64
	// elapsed time since creation until the last successful Take.
	elapsed time.Duration
	// Local created timestamp off of which all time deltas are calculated.
	created time.Time
}

// ErrNameTooLarge is returns by Bucket.MarshalBinary if the name of the
// Bucket exceeds math.MaxUint16 bytes.
var ErrNameTooLarge = fmt.Errorf("bucket name larger than %d", math.MaxUint16)

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (b *Bucket) MarshalBinary() ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.name) > math.MaxUint16 {
		return nil, ErrNameTooLarge
	}

	data := make([]byte, 26+len(b.name))
	binary.BigEndian.PutUint64(data, math.Float64bits(b.added))
	binary.BigEndian.PutUint64(data[8:], math.Float64bits(b.taken))
	binary.BigEndian.PutUint64(data[16:], uint64(b.elapsed))
	binary.BigEndian.PutUint16(data[24:], uint16(len(b.name)))
	copy(data[26:], []byte(b.name)) // TODO: Remove allocation

	return data, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (b *Bucket) UnmarshalBinary(data []byte) error {
	if len(data) < 26 {
		return io.ErrShortBuffer
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.added = math.Float64frombits(binary.BigEndian.Uint64(data))
	b.taken = math.Float64frombits(binary.BigEndian.Uint64(data[8:]))
	b.elapsed = time.Duration(binary.BigEndian.Uint64(data[16:]))

	nameLen := int(binary.BigEndian.Uint16(data[24:]))
	if len(data[26:]) < nameLen {
		return io.ErrShortBuffer
	}
	b.name = string(data[26 : 26+nameLen])

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
	return Bucket{added: float64(tokens)}
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

// String implements the Stringer interface.
func (r Rate) String() string {
	return strconv.Itoa(r.Freq) + ":" + r.Per.String()
}

// Tokens returns the number of tokens in the Bucket.
func (b *Bucket) Tokens() uint64 {
	b.mu.RLock()
	tokens := uint64(b.added - b.taken)
	b.mu.RUnlock()
	return tokens
}

// IsZero returns true if the Bucket's fields are zero valued
// (apart from the Name and Created timestamp).
func (b *Bucket) IsZero() bool {
	b.mu.RLock()
	zero := b.added == 0 && b.taken == 0 && b.elapsed == 0
	b.mu.RUnlock()
	return zero
}

// MarshalLogObject implements the zap.ObjectMarshaler interface
func (b *Bucket) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	enc.AddString("name", b.name)
	enc.AddFloat64("added", b.added)
	enc.AddFloat64("taken", b.taken)
	enc.AddDuration("elapsed", b.elapsed)
	enc.AddTime("created", b.created)
	return nil
}

// Take attempts to take n tokens out of the Bucket with the given filling Rate at time t.
func (b *Bucket) Take(now time.Time, r Rate, n uint64) (ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.created.IsZero() {
		b.created = now
	}

	last := b.created.Add(b.elapsed)
	if now.Before(last) {
		last = now
	}

	// Capacity is the number of tokens that can be taken out of the bucket in
	// a single Take call, also known as burstiness.
	capacity := float64(r.Freq)

	// Calculate the current number of tokens.
	tokens := b.added - b.taken

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

	b.elapsed += elapsed
	b.added += added
	b.taken += taken
	return true
}

// Merge merges multiple Buckets using PN-counter CRDT semantics with
// its counters, picking the largest value for each field.
func (b *Bucket) Merge(others ...*Bucket) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, other := range others {
		if other == b {
			continue
		}

		other.mu.RLock()
		if b.added < other.added { // Find the maximum added
			b.added = other.added
		}

		if b.taken < other.taken { // Find the maximum taken
			b.taken = other.taken
		}

		if b.elapsed < other.elapsed { // Find the largest elapsed time.
			b.elapsed = other.elapsed
		}
		other.mu.RUnlock()
	}
}
