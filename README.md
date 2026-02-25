# Turnstile

Turnstile is a distributed gRPC rate limiter backed by Redis. It implements a token bucket algorithm using a Lua script executed atomically in Redis, ensuring consistent rate limiting across multiple application instances.

## Architecture

```
client
  │
  ▼
nginx (port 50051)
  │  round-robin gRPC load balancing
  ├──▶ turnstile instance 1
  ├──▶ turnstile instance 2
  └──▶ turnstile instance 3
              │
              ▼
           Redis
      (token bucket state)
```

Each turnstile instance is stateless. All rate limit state lives in Redis. The Lua script runs atomically, so concurrent requests from different instances against the same key are safe.

## Config

`config/config.yaml`:

```yaml
server:
  port: ":50051"       # gRPC listen address

redis:
  addr: "localhost:6379"  # default; overridden by REDIS_ADDR env var

defaults:              # fallback limits for unknown actions
  max_tokens: 10
  refill_rate: 1       # tokens per second (int64; fractional rates not supported in v1)

actions:               # per-action overrides
  login:
    max_tokens: 5
    refill_rate: 1
  api_call:
    max_tokens: 100
    refill_rate: 10
```

The config file path defaults to `config/config.yaml` and can be overridden with the `CONFIG_PATH` environment variable.

## Quick Start

```bash
docker compose up --build
```

This starts Redis, 3 turnstile instances, and nginx on port 50051.

Test with grpcurl:

```bash
grpcurl -plaintext \
  -d '{"identifier":"u1","action":"login","cost":1}' \
  localhost:50051 ratelimiter.RateLimiter/CheckRateLimit
```

Send 6 requests with `action: login` (max_tokens: 5) — the 6th returns `allowed: false` with a `retry_after` value.

## API

**Request:**

| Field | Type | Description |
|---|---|---|
| `identifier` | string | Required. Caller identity (user ID, IP, API key, etc.) |
| `action` | string | Required. The operation being rate-limited (e.g. `login`, `api_call`) |
| `cost` | int64 | Tokens to consume. Defaults to 1 if omitted or 0. Must not exceed `max_tokens` for the action. |

**Response:**

| Field | Type | Description |
|---|---|---|
| `allowed` | bool | Whether the request is permitted |
| `remaining` | int64 | Tokens left in the bucket after this request |
| `limit` | int64 | Bucket capacity (`max_tokens` for the action) |
| `retry_after` | int64 | Seconds until enough tokens are available. 0 when allowed. |

Rate limit keys are scoped as `identifier:action`, so each (identifier, action) pair has an independent bucket.

On Redis failure the service fails open: all requests are allowed and the error is logged.

## Running Tests

Unit tests (no external dependencies):

```bash
make test
# or: go test ./...
```

Integration tests (requires Docker):

```bash
make test-integration
# or: go test -tags integration ./internal/integration/...
```

## Load Testing

Install [ghz](https://ghz.sh/docs/install):

```bash
brew install ghz
```

Baseline — 500 RPS for 30s, single identifier:

```bash
make loadtest-baseline
```

Saturation — 100 concurrent workers for 30s:

```bash
make loadtest-saturation
```

Output includes p50/p95/p99 latency, RPS, and error rate. Run against `docker compose up --build` for realistic multi-instance results.

Results are saved to `loadtest/results/` as JSON (gitignored).

### Benchmark results

Measured on a MacBook Air M4, local 3-instance Docker Compose setup (nginx + 3 turnstile + Redis).

**Baseline** — 500 RPS, 30s, single identifier

| Metric | Value |
|---|---|
| RPS | 499.89 |
| p50 | 1.03 ms |
| p95 | 1.45 ms |
| p99 | 1.87 ms |
| Slowest | 11.84 ms |
| Errors | 0 (0%) |

**Saturation** — 100 concurrent workers, 50 connections, 30s, single identifier

| Metric | Value |
|---|---|
| RPS | 12,996 |
| p50 | 5.80 ms |
| p90 | 15.00 ms |
| p95 | 20.00 ms |
| p99 | 32.29 ms |
| Slowest | 127.10 ms |
| Errors | 213 / 389,882 (0.055%) |

86.5% of requests completed under 13ms at saturation. Errors are connection-cycling noise from nginx keepalive recycling, not application failures.

## Scaling Notes

- The nginx upstream uses Docker's internal DNS, which resolves all replicas automatically. Increasing `replicas` in `docker-compose.yml` requires no nginx config changes.
- Redis is the single point of coordination. For high availability, replace the single Redis instance with Redis Cluster or a sentinel setup.
- `refill_rate` is `int64` (tokens per second). Fractional rates (e.g. one token every 2 seconds) are not supported in v1 — the Lua script uses integer math.
