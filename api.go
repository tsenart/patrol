package patrol

import (
	"net/http"
	"strconv"
	"time"

	"net/http/pprof"

	"github.com/julienschmidt/httprouter"
	"go.uber.org/zap"
)

// API implements the Patrol service HTTP API.
type API struct {
	log   *zap.Logger
	clock func() time.Time
	repo  Repo
	http.Handler
}

// NewAPI returns a new Patrol API.
func NewAPI(l *zap.Logger, clock func() time.Time, repo Repo) *API {
	api := API{log: l, clock: clock, repo: repo}

	rt := httprouter.New()
	rt.HandlerFunc("POST", "/take/:name", api.takeBucket)

	rt.HandlerFunc("GET", "/debug/pprof/", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/allocs", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/block", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/goroutine", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/heap", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/mutex", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/threadcreate", pprof.Index)
	rt.HandlerFunc("GET", "/debug/pprof/cmdline", pprof.Cmdline)
	rt.HandlerFunc("GET", "/debug/pprof/profile", pprof.Profile)
	rt.HandlerFunc("GET", "/debug/pprof/symbol", pprof.Symbol)
	rt.HandlerFunc("GET", "/debug/pprof/trace", pprof.Trace)

	api.Handler = rt
	return &api
}

func (api *API) error(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	w.Write([]byte(err.Error()))
	api.log.Error("api error", zap.Error(err))
}

func (api *API) takeBucket(w http.ResponseWriter, r *http.Request) {
	ps := httprouter.ParamsFromContext(r.Context())
	name := ps.ByName("name")

	if len(name) > maxBucketNameLength {
		api.error(w, http.StatusBadRequest, ErrNameTooLarge)
		return
	}

	q := r.URL.Query()
	rate, _ := ParseRate(q.Get("rate"))
	count, _ := strconv.ParseUint(q.Get("count"), 10, 64)
	if count == 0 {
		count = 1
	}

	bucket, _ := api.repo.GetBucket(r.Context(), name)

	code := http.StatusOK
	remaining, ok := bucket.Take(api.clock(), rate, count)
	if !ok {
		code = http.StatusTooManyRequests
	}
	api.repo.UpsertBucket(r.Context(), bucket)

	api.log.Debug(
		"take",
		zap.Int("code", code),
		zap.Uint64("count", count),
		zap.Stringer("rate", rate),
		zap.Object("bucket", bucket),
	)

	w.WriteHeader(code)
	w.Write([]byte(strconv.FormatUint(remaining, 10)))
}
