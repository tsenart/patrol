# Patrol

Patrol is a zero-dependencies, operator friendly
distributed rate limiting HTTP side-car API with eventually
consistent asynchronous state replication. It uses a modified version of
the [Token Bucket](https://en.wikipedia.org/wiki/Token_bucket) algorithm
underneath to support CRDT semantics.

## Status

This project is **alpha status**. Don't use it in production yet.

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
- Fault tolerant: Eventually consistent, best-effort state synchronization between cluster nodes.

## Deployment

### Integration with edge load balancers via Lua

Patrol is meant to be deployed as a side-car to edge load balancers
and reverse proxies that have dynamic routing capabilities with
Lua.

The load balancer or reverse proxy needs to be extended so that it asks
the side-car Patrol instance if it should pass or block a given request.

### State synchronization

Nodes in the cluster periodically poll other nodes for their `Buckets` and
perform a CRDT G-Counter style merge with their local `Buckets`.
This works because a `Bucket` is internally composed of strictly monotically
increasing counters. When merging, we simply pick the largest value for a field,
which is determined to be the latest value across the whole cluster.

### Cluster configuration

Cluster configuration can either be `static` or dynamic with `memberlist`.

With static configuration, the `ip:port` pairs of all cluster nodes need to
be specified with multiple `-node` flags.

A config management tool like Ansible is recommended to automate the provisioning
of the OS service scripts with this configuration pre-populated.

Memberlist support is experimental and untested.

### Failure modes

Under network partitions, nodes won't be able to get the latest `Buckets` from
the other side of the partition. This means that the there may be temporary
policy violations until the local `Bucket` gets depleted of tokens by direct requests.

Once a network partition is healed, nodes should gracefully resume background synchronization.

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
    "81.23.12.9": {"Added":56604,"Taken":56455,"Last":1539884470813616000},
    "81.92.1.33": {"Added":56590,"Taken":56440,"Last":1539884470813616000}
  }
```

### GET /bucket/:name

Used between Patrol nodes to query the cluster for a Bucket by its name when isn't found locally on `POST /take/:bucket`.

### POST /bucket/:name

Used between Patrol nodes to update or insert a single bucket.

### POST /buckets

Used between Patrol nodes to update all the buckets of another node.

## Testing

```console
go test -v ./...
```

## Future work

- More comprehensive tests.
- Experiment with Memberlist cluster mode in a realistic environment.
- Load test on a real cluster and iterate on results.
- Write and publish Docker image.
- Provide working examples of Lua integrations with nginx and Apache Traffic Server.
- Instrument with Prometheus.
- Structured logging.
- Exponential back-off + circuit breaker when polling state from cluster nodes.
- Explore alternative communication patterns for state propagation (push + Gossip)
