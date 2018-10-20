package patrol

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/streadway/handy/accept"
)

// A Doer captures the Do method of an *http.Client
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// A Client is a high level HTTP client of the Patrol API.
// It implements the Repo interface.
type Client struct {
	log     *log.Logger
	cli     Doer
	cluster Cluster
	timeout time.Duration
}

// A ClientError is an aggregate error type that holds potential errors of
// multiple calls to cluster nodes.
type ClientError struct {
	Errors map[string]string
}

// Error implements the error interface.
func (e ClientError) Error() string {
	var b strings.Builder
	b.WriteString("client: ")
	for node, err := range e.Errors {
		b.WriteString(node + ": " + err + "; ")
	}
	return b.String()
}

// NewClient returns a new Client for the given Cluster with the given underlying Doer.
func NewClient(log *log.Logger, cli Doer, cluster Cluster, timeout time.Duration) *Client {
	return &Client{log: log, cli: cli, cluster: cluster, timeout: timeout}
}

// GetBucket gets a bucket by name from all the nodes in the cluster and
// returns the merged result.
func (c *Client) GetBucket(ctx context.Context, name string) (Bucket, error) {
	var b Bucket
	rpc := call{
		method: "GET",
		url:    c.url("/bucket/" + name),
		recv:   func() interface{} { return Bucket{} },
	}
	err := c.scatter(ctx, &rpc, func(v interface{}) {
		if other, ok := v.(*Bucket); ok {
			b.Merge(other)
		}
	})
	return b, err
}

// GetBuckets gets all Buckets from all the nodes in the cluster and
// returns the merged results.
func (c *Client) GetBuckets(ctx context.Context) (Buckets, error) {
	bs := make(Buckets)
	rpc := call{
		method: "GET",
		url:    c.url("/buckets"),
		recv:   func() interface{} { return Buckets{} },
	}
	err := c.scatter(ctx, &rpc, func(v interface{}) {
		if other, ok := v.(*Buckets); ok {
			bs.Merge(*other)
		}
	})
	return bs, err
}

// UpsertBucket updates or inserts the bucket with the given name across
// all nodes.
func (c *Client) UpsertBucket(ctx context.Context, name string, b *Bucket) error {
	rpc := call{method: "POST", url: c.url("/bucket/" + name), send: b}
	return c.scatter(ctx, &rpc, nil)
}

// UpsertBuckets updates or inserts all the given Buckets.
func (c *Client) UpsertBuckets(ctx context.Context, bs Buckets) error {
	rpc := call{method: "POST", url: c.url("/buckets"), send: bs}
	return c.scatter(ctx, &rpc, nil)
}

// an RPC call
type call struct {
	method string
	url    *url.URL
	hdr    http.Header
	send   interface{}
	recv   func() interface{}
	err    error
}

func (c *Client) scatter(ctx context.Context, rpc *call, gather func(interface{})) error {
	nodes, err := c.cluster.Nodes()
	if err != nil {
		return err
	}

	rpcs := make(chan *call, len(nodes))
	for _, node := range nodes {
		go c.rpc(ctx, node, rpcs, rpc)
	}

	cerr := ClientError{Errors: make(map[string]string, len(nodes))}
	for range nodes {
		if rpc := <-rpcs; rpc.err != nil {
			cerr.Errors[rpc.url.Host] = rpc.err.Error()
		} else if gather != nil {
			gather(rpc.recv)
		}
	}

	if len(cerr.Errors) > 0 {
		return cerr
	}

	return nil
}

func (c *Client) rpc(ctx context.Context, node string, rpcs chan *call, rpc *call) {
	rpc.url.Host = node
	rpc.err = c.do(ctx, rpc)
	rpcs <- rpc
}

const (
	gobMediaType  = "application/x-gob"
	jsonMediaType = "application/json"
)

func (c *Client) do(ctx context.Context, rpc *call) (err error) {
	defer func() { rpc.err = err }()

	if rpc.hdr == nil {
		rpc.hdr = make(http.Header)
	}

	var send io.Reader
	if rpc.send != nil {
		var buf bytes.Buffer
		ct := bestContentType(rpc.hdr.Get("Content-Type"), gobMediaType)
		if err = encode(ct, rpc.send, &buf); err != nil {
			return err
		}
		send = &buf
	}

	hdr := make(http.Header, len(rpc.hdr))
	for header, vs := range rpc.hdr {
		for _, v := range vs {
			hdr.Add(header, v)
		}
	}

	if hdr.Get("Accept") == "" {
		hdr.Set("Accept", gobMediaType)
	}

	req, err := http.NewRequest(rpc.method, rpc.url.String(), send)
	if err != nil {
		return err
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	res, err := c.cli.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}

	defer func() {
		if err == nil {
			err = res.Body.Close()
		}
	}()

	if rpc.recv == nil {
		return err
	}

	body := io.Reader(res.Body)
	switch res.Header.Get("Content-Encoding") {
	case "gzip":
		if body, err = gzip.NewReader(res.Body); err != nil {
			return err
		}
	}

	ct := bestContentType(res.Header.Get("Content-Type"), "")
	return decode(ct, body, &Response{Result: rpc.recv()})
}

func (Client) url(path string) *url.URL {
	return &url.URL{Scheme: "http", Path: path}
}

func bestContentType(mediaType, fallback string) string {
	mt, _, err := mime.ParseMediaType(mediaType)
	switch {
	case err != nil:
		return fallback
	case mt == accept.ALL:
		return jsonMediaType
	default:
		return mt
	}
}

func encode(mediaType string, v interface{}, w io.Writer) error {
	switch mediaType {
	case gobMediaType:
		return gob.NewEncoder(w).Encode(v)
	case jsonMediaType:
		return json.NewEncoder(w).Encode(v)
	default:
		return fmt.Errorf("unknown encoding for %q", mediaType)
	}
}

func decode(mediaType string, r io.Reader, v interface{}) error {
	switch mediaType {
	case gobMediaType:
		return gob.NewDecoder(r).Decode(v)
	case jsonMediaType:
		return json.NewDecoder(r).Decode(v)
	default:
		return fmt.Errorf("unknown decoding for %q", mediaType)
	}
}
