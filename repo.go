package patrol

import (
	"context"
	"errors"
	"sync"
	"time"
)

// A Repo (repository) interface for retrieving and updating Buckets.
type Repo interface {
	// GetBucket returns a Bucket by its name.
	GetBucket(ctx context.Context, name string) (Bucket, error)

	// UpsertBucket updates or inserts the bucket with the given name.
	UpsertBucket(ctx context.Context, b *Bucket) error
}

// ErrBucketNotFound is returned by GetBucket when a Bucket is missing.
var ErrBucketNotFound = errors.New("repo: bucket not found")

var _ Repo = (*InMemoryRepo)(nil)

// A InMemoryRepo implements a thread safe Repo backed by an
// in-memory data structure.
type InMemoryRepo struct {
	mu      sync.RWMutex
	buckets map[string]Bucket
	clock   func() time.Time
}

// NewInMemoryRepo return a new InMemoryRepo.
func NewInMemoryRepo(clock func() time.Time) *InMemoryRepo {
	return &InMemoryRepo{
		buckets: map[string]Bucket{},
		clock:   clock,
	}
}

// GetBucket gets a bucket by name.
func (r *InMemoryRepo) GetBucket(_ context.Context, name string) (Bucket, error) {
	r.mu.RLock()
	bucket, ok := r.buckets[name]
	r.mu.RUnlock()

	if !ok {
		bucket.Name = name
		bucket.Added = 1
		bucket.Created = r.clock()
	}

	return bucket, nil
}

// UpsertBucket updates or inserts the bucket.
func (r *InMemoryRepo) UpsertBucket(_ context.Context, b *Bucket) error {
	r.mu.Lock()
	if current, ok := r.buckets[b.Name]; !ok { // insert
		if b.Created.IsZero() {
			b.Created = r.clock()
		}
		r.buckets[b.Name] = *b
	} else { // update
		current.Merge(b)
		r.buckets[b.Name] = current
	}
	r.mu.Unlock()
	return nil
}
