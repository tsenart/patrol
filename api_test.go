package patrol

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAPI(t *testing.T) {
	repo := NewLocalRepo(time.Now, &Bucket{
		name:    "foo",
		created: time.Now(),
	})

	log, err := zap.NewDevelopment()
	if err != nil {
		t.Fatal(err)
	}

	api := NewAPI(log, time.Now, repo)
	srv := httptest.NewServer(api)

	for _, tc := range []struct {
		name   string
		req    *http.Request
		assert func(testing.TB, *http.Response)
	}{
		{
			name: "bucket name too long",
			req:  request("POST", srv.URL+"/take/"+strings.Repeat("A", maxBucketNameLength+1)),
			assert: response(
				code(http.StatusBadRequest),
				body([]byte(ErrNameTooLarge.Error())),
			),
		},
		{
			name: "default rate is zero",
			req:  request("POST", srv.URL+"/take/default-rate"),
			assert: response(
				code(http.StatusTooManyRequests),
				body([]byte("0")),
			),
		},
		{
			name: "default count is one",
			req:  request("POST", srv.URL+"/take/default-count?rate=2:s"),
			assert: response(
				code(http.StatusOK),
				body([]byte("1")), // 1 remaining
			),
		},
		{
			name: "ok",
			req:  request("POST", srv.URL+"/take/pass?rate=2:s&count=1"),
			assert: response(
				code(http.StatusOK),
				body([]byte("1")), // 1 remaining
			),
		},
		{
			name: "too many requests",
			req:  request("POST", srv.URL+"/take/fail?rate=0:s&count=1"),
			assert: response(
				code(http.StatusTooManyRequests),
				body([]byte("0")),
			),
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := http.DefaultClient.Do(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			tc.assert(t, res)
		})
	}
}

func response(asserts ...func(testing.TB, *http.Response)) func(testing.TB, *http.Response) {
	return func(t testing.TB, r *http.Response) {
		t.Helper()
		for _, assert := range asserts {
			assert(t, r)
		}
	}
}

func code(want int) func(testing.TB, *http.Response) {
	return func(t testing.TB, r *http.Response) {
		t.Helper()
		if have := r.StatusCode; have != want {
			t.Errorf("have code %d, want %d", have, want)
		}
	}
}

func body(want []byte) func(testing.TB, *http.Response) {
	return func(t testing.TB, r *http.Response) {
		t.Helper()
		if have, err := ioutil.ReadAll(r.Body); err != nil {
			t.Fatal(err)
		} else if !bytes.Equal(have, want) {
			t.Errorf("have body %q, want %q", have, want)
		}
	}
}

func request(method, rawurl string) *http.Request {
	req, err := http.NewRequest(method, rawurl, nil)
	if err != nil {
		panic(err)
	}
	return req
}
