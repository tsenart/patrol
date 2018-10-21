package patrol

import (
	"context"
	"errors"
	"sync"
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
}

// NewInMemoryRepo return a new InMemoryRepo.
func NewInMemoryRepo() *InMemoryRepo {
	return &InMemoryRepo{buckets: map[string]Bucket{}}
}

// GetBucket gets a bucket by name.
func (s *InMemoryRepo) GetBucket(_ context.Context, name string) (Bucket, error) {
	s.mu.RLock()
	bucket, ok := s.buckets[name]
	s.mu.RUnlock()

	if !ok {
		bucket.Name = name
	}

	return bucket, nil
}

// UpsertBucket updates or inserts the bucket.
func (s *InMemoryRepo) UpsertBucket(_ context.Context, b *Bucket) error {
	s.mu.Lock()
	if current, ok := s.buckets[b.Name]; !ok { // insert
		s.buckets[b.Name] = *b
	} else { // update
		b.Merge(&current)
		s.buckets[b.Name] = *b
	}
	s.mu.Unlock()
	return nil
}
