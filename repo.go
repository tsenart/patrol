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

	// GetBuckets returns all Buckets.
	GetBuckets(ctx context.Context) (Buckets, error)

	// UpsertBucket updates or inserts the bucket with the given name.
	UpsertBucket(ctx context.Context, name string, b *Bucket) error

	// UpsertBuckets updates or inserts all the given Buckets.
	UpsertBuckets(ctx context.Context, bs Buckets) error
}

// ErrBucketNotFound is returned by GetBucket when a Bucket is missing.
var ErrBucketNotFound = errors.New("repo: bucket not found")

var _ Repo = (*InMemoryRepo)(nil)

// A InMemoryRepo implements a thread safe Repo backed by an
// in-memory data structure.
type InMemoryRepo struct {
	mu      sync.RWMutex
	buckets Buckets
}

// NewInMemoryRepo return a new InMemoryRepo.
func NewInMemoryRepo() *InMemoryRepo {
	return &InMemoryRepo{buckets: Buckets{}}
}

// GetBucket gets a bucket by name.
func (s *InMemoryRepo) GetBucket(_ context.Context, name string) (Bucket, error) {
	s.mu.RLock()
	bucket := s.buckets[name]
	s.mu.RUnlock()

	if bucket == nil {
		return Bucket{}, ErrBucketNotFound
	}

	return *bucket, nil
}

// GetBuckets gets all the buckets for read-only operations. It's not thread safe
// to mutate any Bucket returned by this method.
//
// This is a memory optimization to avoid duplicating all the buckets on every
// call to this method.
func (s *InMemoryRepo) GetBuckets(context.Context) (Buckets, error) {
	s.mu.RLock()
	buckets := make(Buckets, len(s.buckets))
	for name, bucket := range s.buckets {
		buckets[name] = bucket
	}
	s.mu.RUnlock()
	return buckets, nil
}

// UpsertBucket updates or inserts the bucket with the given name.
func (s *InMemoryRepo) UpsertBucket(_ context.Context, name string, b *Bucket) error {
	s.upsert(name, b)
	return nil
}

// UpsertBuckets updates or inserts all the given Buckets.
func (s *InMemoryRepo) UpsertBuckets(_ context.Context, bs Buckets) error {
	for name, bucket := range bs {
		s.upsert(name, bucket)
	}
	return nil
}

func (s *InMemoryRepo) upsert(name string, b *Bucket) {
	s.mu.Lock()
	if current := s.buckets[name]; current == nil { // insert
		s.buckets[name] = b
	} else if current != b { // update
		var merged Bucket
		merged.Merge(current, b)
		s.buckets[name] = &merged
	}
	s.mu.Unlock()
}
