package patrol

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/streadway/handy/accept"
	"github.com/streadway/handy/encoding"
)

// API implements the Patrol service HTTP API.
type API struct {
	mu      sync.RWMutex
	log     *log.Logger
	buckets map[string]Bucket
}

// NewAPI returns a new Patrol API.
func NewAPI(l *log.Logger) *API {
	return &API{
		log:     l,
		buckets: map[string]Bucket{},
	}
}

// Handler returns the http.Handler of the API.
func (api *API) Handler() http.Handler {
	mediaTypes := []string{"application/x-gob", "application/json"}
	handleBuckets := accept.Middleware(mediaTypes...)(
		encoding.GzipTypes(mediaTypes, http.HandlerFunc(api.handleBuckets)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/take" && r.Method == http.MethodPost:
			api.handleTake(w, r)
		case r.URL.Path == "/buckets" && r.Method == http.MethodGet:
			handleBuckets.ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// handler for GET /buckets
func (api *API) handleBuckets(w http.ResponseWriter, r *http.Request) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Accept"))
	if err != nil {
		mt = "application/json"
	}

	var buf bytes.Buffer

	api.mu.RLock()
	switch mt {
	default:
		fallthrough
	case "application/json":
		err = json.NewEncoder(&buf).Encode(api.buckets)
	case "application/x-gob":
		err = gob.NewEncoder(&buf).Encode(api.buckets)
	}
	api.mu.RUnlock()

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		api.log.Printf("handleBuckets: encoding error: %v", err)
		return
	}

	w.Header().Set("Content-Type", mt)
	buf.WriteTo(w)
}

// handler for POST /take?bucket=my-bucket-name&count=1&rate=100:1s
func (api *API) handleTake(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("bucket")

	rate, err := ParseRate(q.Get("rate"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, err)
		return
	}

	count, err := strconv.ParseUint(q.Get("count"), 10, 64)
	if err != nil {
		count = 1
	}

	bucket := api.getBucket(name)
	ok := bucket.Take(time.Now(), rate, count)
	api.updateBucket(name, bucket)

	if !ok {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
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
	api.buckets[name] = Merge(b, current)
	api.mu.Unlock()
}
