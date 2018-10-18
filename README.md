# Patrol

Patrol is a zero-dependencies, operator friendly
distributed rate limiting HTTP side-car API with eventually
consistent asynchronous state replication. Is uses a modified version of
the [Token Bucket](https://en.wikipedia.org/wiki/Token_bucket) algorithm
underneath to support CRDT semantics.

## Installation

```console
go get github.com/tsenart/patrol
```

## Usage

```console
Usage of patrol:
  -cluster string
    	Cluster mode [static | memberlist] (default "static")
  -host string
    	IP address to bind HTTP API to (default "0.0.0.0")
  -interval duration
    	Poller interval (default 1s)
  -node value
    	Static node for use with -cluster=static
  -port string
    	Port to bind HTTP API to (default "8080")
  -timeout duration
    	Poller HTTP client timeout (default 30s)
```

## Design

Patrol is designed to be:

- Easy to deploy: No dependencies on centralized stores.
- Operator friendly: Simple API and small configuration surface area.
- Performant: Minimal overhead, high concurrency support.

## Deployment

Patrol is meant to be deployed as a side-car to edge load balancers
and reverse proxies that have dynamic routing capabilities with
Lua.

```lua

```

## API

### POST /take/:bucket?rate=30:1m&count=1

Takes `count` number of tokens from the given `:bucket` (e.g. IP address) which is replenished
at the given `rate`. If the bucket doesn't exist, it creates one with `rate` initial number of tokens first.

If not enough tokens are available, an HTTP `429 Too Many Requests` response code is returned.
Otherwise, an HTTP `200 OK` is returned.

Here are examples of configuration values for the `rate` parameter:

- `1:1m`: 1 token per minute
- `100:1s`: 100 tokens per second
- `50:1h`: 50 tokens per hour

### GET /buckets

Used between Patrol nodes for periodic asynchronous bucket state synchronization (by default, every second).

```json
{
  "90.12.33.41": { ... }
}
```


## Benchmarks

```plaintext
TO BE DONE
```
