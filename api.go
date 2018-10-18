package patrol

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// API implements the Patrol service HTTP API.
type API struct {
	mu      sync.RWMutex
	buckets map[string]Bucket
}

// ServeHTTP implements the http.Handler interface.
func (api *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/take" && r.Method == http.MethodPost:
		api.take(w, r)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// handler for POST /take?bucket=my-bucket-name&count=1&rate=100:1s
func (api *API) take(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("bucket")

	rate, err := ParseRate(q.Get("rate"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	count, err := strconv.ParseUint(q.Get("count"), 10, 64)
	if err != nil {
		count = 1
	}

	bucket := api.getBucket(name)
	ok, _ := bucket.Take(time.Now(), rate, count)
	if !ok {
		w.WriteHeader(http.StatusTooManyRequests)
	}

	api.updateBucket(name, bucket)
}

func (api *API) getBucket(name string) Bucket {
	api.mu.RLock()
	bucket := api.buckets[name]
	api.mu.RUnlock()
	return bucket
}

func (api *API) updateBucket(name string, b Bucket) {
	api.mu.Lock()
	current := api.buckets[name]
	api.buckets[name] = Merge(bucket, after)
	api.mu.Unlock()
}
