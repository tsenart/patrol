# Patrol

Patrol is a zero-dependencies, operator friendly
distributed rate limiting HTTP side-car API with eventually
consistent asynchronous state replication. It uses a modified version of
the [Token Bucket](https://en.wikipedia.org/wiki/Token_bucket) algorithm
underneath to support CRDT PN-Counter semantics.

## Status

This project is **alpha status**. Don't use it in production yet.

## Installation

```console
go get github.com/tsenart/patrol
```

## Usage

```console
Usage of patrol:
  -api-addr string
    	Address to bind HTTP API to (default "0.0.0.0:8080")
  -cluster-discovery string
    	Cluster discovery [static] (default "static")
  -cluster-node value
    	Cluster node to broadcast to
  -replicator-addr string
    	Address to bind replication server to (default "0.0.0.0:16000")
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

Nodes in the cluster actively replicate state to all other nodes via UDP
unicast broadcasting. One message is sent to each cluster node per `Bucket`
take operation that is triggered via the HTTP API.

The full `Bucket` state is replicated which fits in less than 256 bytes.
Together with its merge semantics, this makes a `Bucket` a **state based**
*Convergent Replicated Data Type* (CvRDT) based on a [PN-Counter](https://en.wikipedia.org/wiki/Conflict-free_replicated_data_type#PN-Counter_(Positive-Negative_Counter)).

### Cluster discovery

#### `static`

With static configuration, the `ip:port` where the replicator service of cluster nodes is bound to
should be specified with multiple `-cluster-node` flags.

A config management tool like Ansible is recommended to automate the provisioning
of the OS service scripts with this configuration pre-populated.

### Failure modes

Under network partitions, nodes won't be able to get the latest `Buckets` from
the other side of the partition. This means that the there may be temporary
policy violations until the local `Bucket` gets depleted of tokens by direct requests.

Once a network partition is healed, nodes should gracefully resume active synchronization.

## API

### POST /take/:bucket?rate=30:1m&count=1

Takes `count` number of tokens from the given `:bucket` (e.g. IP address) which is replenished
at the given `rate`. If the bucket doesn't exist it creates one.

If not enough tokens are available, an HTTP `429 Too Many Requests` response code is returned.
Otherwise, an HTTP `200 OK` is returned.

Here are examples of configuration values for the `rate` parameter:

- `1:1m`: 1 token per minute
- `100:1s`: 100 tokens per second
- `50:1h`: 50 tokens per hour

## Testing

```console
go test -v ./...
```

## Future work

- More comprehensive tests.
- Load test on a real cluster and iterate on results.
- Write and publish Docker image.
- Provide working examples of Lua integrations with nginx and Apache Traffic Server.
- Instrument with Prometheus.
- Structured logging.
