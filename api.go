package patrol

import (
	"compress/gzip"
	"log"
	"net/http"
	"strconv"
	"time"

	"net/http/pprof"

	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	"github.com/streadway/handy/accept"
	"github.com/streadway/handy/encoding"
)

// API implements the Patrol service HTTP API.
type API struct {
	log     *log.Logger
	local   Repo
	cluster Repo
}

// NewAPI returns a new Patrol API.
func NewAPI(l *log.Logger, cluster, local Repo) *API {
	return &API{
		log:     l,
		local:   local,
		cluster: cluster,
	}
}

var supportedContentTypes = []string{accept.ALL, gobMediaType, jsonMediaType}

type middleware []func(http.Handler) http.Handler

func (mw middleware) Wrap(h http.Handler) http.Handler {
	wh := h
	for _, m := range mw {
		wh = m(wh)
	}
	return wh
}

// Handler returns the http.Handler of the API.
func (api *API) Handler() http.Handler {
	mw := middleware{
		accept.Middleware(supportedContentTypes...),
		encoding.Gzipper(gzip.DefaultCompression, supportedContentTypes...),
	}

	rt := httprouter.New()
	for _, r := range []struct {
		method string
		path   string
		handle func(*http.Request) *Response
	}{
		{"GET", "/buckets", api.getBuckets},
		{"POST", "/buckets", api.upsertBuckets},
		{"GET", "/bucket/:name", api.getBucket},
		{"POST", "/bucket/:name", api.upsertBucket},
		{"POST", "/take/:name", api.takeBucket},
	} {
		rt.Handler(r.method, r.path, mw.Wrap(api.handler(r.handle)))
	}

	rt.HandlerFunc("GET", "/debug/pprof/", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/cmdline", pprof.Cmdline)
	rt.HandlerFunc("GET", "/debug/pprof/profile", pprof.Profile)
	rt.HandlerFunc("GET", "/debug/pprof/symbol", pprof.Symbol)
	rt.HandlerFunc("GET", "/debug/pprof/trace", pprof.Trace)

	return rt
}

// A Response that is returned as the body of all handlers.
type Response struct {
	Code   int         `json:"code"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func respond(code int, result interface{}) *Response {
	switch v := result.(type) {
	case error:
		return &Response{Code: code, Error: v.Error()}
	default:
		return &Response{Code: code, Result: v}
	}
}

func (api *API) handler(handle func(*http.Request) *Response) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := handle(r)
		ct := bestContentType(r.Header.Get("Accept"), jsonMediaType)
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(resp.Code)
		if err := encode(ct, resp, w); err != nil {
			api.log.Printf("response error: %v", err)
		}
	})
}

func (api *API) getBuckets(r *http.Request) *Response {
	buckets, err := api.local.GetBuckets(r.Context())
	if err != nil {
		return respond(http.StatusInternalServerError, err)
	}
	return respond(http.StatusOK, buckets)
}

func (api *API) getBucket(r *http.Request) *Response {
	ps := httprouter.ParamsFromContext(r.Context())
	bucket, err := api.local.GetBucket(r.Context(), ps.ByName("name"))
	switch err {
	case nil:
		return respond(http.StatusOK, bucket)
	case ErrBucketNotFound:
		return respond(http.StatusNotFound, err)
	default:
		return respond(http.StatusInternalServerError, err)
	}
}

func (api *API) upsertBucket(r *http.Request) *Response {
	ps := httprouter.ParamsFromContext(r.Context())
	name := ps.ByName("name")
	var b Bucket
	if err := decode(r.Header.Get("Content-Type"), r.Body, &b); err != nil {
		return respond(http.StatusBadRequest, err)
	} else if err = api.local.UpsertBucket(r.Context(), name, &b); err != nil {
		return respond(http.StatusInternalServerError, err)
	}
	return respond(http.StatusOK, nil)
}

func (api *API) upsertBuckets(r *http.Request) *Response {
	var bs Buckets
	if err := decode(r.Header.Get("Content-Type"), r.Body, &bs); err != nil {
		return respond(http.StatusBadRequest, err)
	} else if err = api.local.UpsertBuckets(r.Context(), bs); err != nil {
		return respond(http.StatusInternalServerError, err)
	}
	return respond(http.StatusOK, nil)
}

// TakeResponse is returned by the takeBucket handler.
type TakeResponse struct {
	Taken     uint64
	Remaining uint64
}

func (api *API) takeBucket(r *http.Request) *Response {
	ps := httprouter.ParamsFromContext(r.Context())
	name := ps.ByName("name")

	q := r.URL.Query()
	rate, err := ParseRate(q.Get("rate"))
	if err != nil {
		return respond(http.StatusBadRequest, errors.Wrap(err, "rate"))
	}

	count, err := strconv.ParseUint(q.Get("count"), 10, 64)
	if err != nil {
		count = 1
	}

	var bucket *Bucket
	for _, rp := range []struct {
		name string
		Repo
	}{
		{name: "local", Repo: api.local},
		{name: "cluster", Repo: api.cluster},
	} {
		b, err := rp.GetBucket(r.Context(), name)
		if err == nil {
			bucket = &b
			break
		}

		switch err {
		case ErrBucketNotFound:
			api.log.Printf("Bucket %q not found in %q repo", name, rp.name)
		default:
			return respond(http.StatusInternalServerError, err)
		}
	}

	if bucket == nil {
		bucket = new(Bucket)
	}

	ok := bucket.Take(time.Now(), rate, count)

	if err = api.local.UpsertBucket(r.Context(), name, bucket); err != nil {
		return respond(http.StatusInternalServerError, errors.Wrap(err, "UpsertBucket"))
	}

	resp := &TakeResponse{Remaining: bucket.Tokens()}
	code := http.StatusTooManyRequests

	if ok {
		code, resp.Taken = http.StatusOK, count
	}

	return respond(code, resp)
}
