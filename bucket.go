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
	"unsafe"

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

// bucketFixedSize is the number of bytes that the fixed portion of a Bucket
// is marshalled to.
const bucketFixedSize = 8 + 8 + 8 + 1 // added + taken + elapsed + len(name)

// bucketPacketSize is the size of a UDP packet where a Bucket state update is sent.
// This is limited to this value so that it can be sent without fragmentation over IPv4
// networks with a small MTU of 256.
const bucketPacketSize = 256

// maxBucketNameLength is the maximum length of a Bucket's name that is allowed.
const maxBucketNameLength = bucketPacketSize - bucketFixedSize

// ErrNameTooLarge is returns by Bucket.MarshalBinary if the name of the
// Bucket exceeds the length of 231.
var ErrNameTooLarge = fmt.Errorf("bucket name larger than %d", maxBucketNameLength)

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (b *Bucket) MarshalBinary() ([]byte, error) {
	b.mu.RLock()

	if len(b.name) > maxBucketNameLength {
		b.mu.RUnlock()
		return nil, ErrNameTooLarge
	}

	data := make([]byte, bucketFixedSize+len(b.name))
	binary.BigEndian.PutUint64(data, math.Float64bits(b.added))
	binary.BigEndian.PutUint64(data[8:], math.Float64bits(b.taken))
	binary.BigEndian.PutUint64(data[16:], uint64(b.elapsed))
	data[24] = byte(len(b.name))
	copy(data[25:], *(*[]byte)(unsafe.Pointer(&b.name)))
	b.mu.RUnlock()

	return data, nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
func (b *Bucket) UnmarshalBinary(data []byte) error {
	if len(data) < bucketFixedSize {
		return io.ErrShortBuffer
	}

	b.mu.Lock()

	b.added = math.Float64frombits(binary.BigEndian.Uint64(data))
	b.taken = math.Float64frombits(binary.BigEndian.Uint64(data[8:]))
	b.elapsed = time.Duration(binary.BigEndian.Uint64(data[16:]))

	nameLen := int(byte(data[24]))
	if len(data[25:]) < nameLen {
		b.mu.Unlock()
		return io.ErrShortBuffer
	}
	b.name = string(data[25 : 25+nameLen])

	b.mu.Unlock()
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

// Take attempts to take n tokens out of the Bucket with the given filling Rate at time now.
// It returns the number of remaing tokens and if the take was successful.
func (b *Bucket) Take(now time.Time, r Rate, n uint64) (remaining uint64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Capacity is the number of tokens that can be taken out of the bucket in
	// a single Take call, also known as burstiness.
	capacity := float64(r.Freq)

	if b.added == 0 {
		b.added = capacity
	}

	last := b.created.Add(b.elapsed)
	if now.Before(last) {
		last = now
	}

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
	if have := tokens + added; taken > have {
		return uint64(have), false
	}

	b.elapsed += elapsed
	b.added += added
	b.taken += taken

	return uint64(b.added - b.taken), true
}

// String implements the Stringer interface.
func (b *Bucket) String() string {
	b.mu.RLock()
	s := fmt.Sprintf(
		"Bucket{name: %q, tokens: %f, elapsed: %s, created: %s}",
		b.name, b.added-b.taken, b.elapsed, b.created,
	)
	b.mu.RUnlock()
	return s
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
