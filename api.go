package patrol

import (
	"net/http"
	"strconv"
	"time"

	"net/http/pprof"

	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// API implements the Patrol service HTTP API.
type API struct {
	log   *zap.Logger
	clock func() time.Time
	repo  *Repo
	http.Handler
}

// NewAPI returns a new Patrol API.
func NewAPI(l *zap.Logger, clock func() time.Time, repo *Repo) *API {
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
	api.log.Error("api error", zap.Error(err))
}

func (api *API) takeBucket(w http.ResponseWriter, r *http.Request) {
	ps := httprouter.ParamsFromContext(r.Context())
	name := ps.ByName("name")

	q := r.URL.Query()
	rate, err := ParseRate(q.Get("rate"))
	if err != nil {
		api.error(w, http.StatusBadRequest, errors.Wrap(err, "rate"))
		return
	}

	count, err := strconv.ParseUint(q.Get("count"), 10, 64)
	if err != nil {
		count = 1
	}

	bucket, _ := api.repo.Bucket(r.Context(), name, rate.Freq)
	code := http.StatusOK
	if !bucket.Take(api.clock(), rate, count) {
		code = http.StatusTooManyRequests
	}

	api.log.Debug(
		"take",
		zap.Int("code", code),
		zap.Uint64("count", count),
		zap.Stringer("rate", rate),
		zap.Object("bucket", bucket),
	)

	if code == http.StatusOK {
		if err = api.repo.Broadcast(bucket); err != nil {
			api.error(w, http.StatusInternalServerError, err)
			return
		}
	}

	w.WriteHeader(code)
}
