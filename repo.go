package patrol

import (
	"context"
	"sync"
)

// A Repo (repository) interface for retrieving and updating Buckets.
type Repo interface {
	// GetBucket returns a Bucket by its name.
	GetBucket(ctx context.Context, name string) (Bucket, error)

	// GetBuckets returns all Buckets.
	GetBuckets(ctx context.Context) (map[string]Bucket, error)

	// UpdateBucket updates the bucket with the given name.
	UpdateBucket(ctx context.Context, name string, b Bucket) error
}

var _ Repo = (*InMemoryRepo)(nil)

// A InMemoryRepo implements a thread safe Repo backed by an
// in-memory data structure.
type InMemoryRepo struct {
	mu      sync.RWMutex
	buckets map[string]Bucket
}

// NewInMemoryRepo return a new InMemoryRepo.
func NewInMemoryRepo() *InMemoryRepo {
	return &InMemoryRepo{
		buckets: map[string]Bucket{},
	}
}

// GetBucket gets a bucket by name.
func (s *InMemoryRepo) GetBucket(_ context.Context, name string) (Bucket, error) {
	s.mu.RLock()
	bucket := s.buckets[name]
	s.mu.RUnlock()
	return bucket, nil
}

// GetBuckets gets all the buckets.
func (s *InMemoryRepo) GetBuckets(context.Context) (map[string]Bucket, error) {
	s.mu.RLock()
	buckets := make(map[string]Bucket, len(s.buckets))
	for name, bucket := range s.buckets {
		buckets[name] = bucket
	}
	s.mu.RUnlock()
	return buckets, nil
}

// UpdateBucket updates the bucket with the given name.
func (s *InMemoryRepo) UpdateBucket(_ context.Context, name string, b Bucket) error {
	s.mu.Lock()
	current := s.buckets[name]
	s.buckets[name] = Merge(b, current)
	s.mu.Unlock()
	return nil
}
