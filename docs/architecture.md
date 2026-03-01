# Architecture

## Overview

Turnstile is a standalone gRPC service that handles rate limiting.
It uses request count tracking rendering it agnostic to business logic.
By leveraging Redis for distributed state, it enables horizontal scaling
according to system demand.

## Scope

### Phase 1 (Current)

- Core rate limiting service with token bucket algorithm
- Single Redis instance
- Docker Compose deployment
- Hardcoded service endpoint
- Per-request structured logging (action, outcome, latency, gRPC status code)

### Out of Scope (Future Consideration)

- Metrics and distributed tracing
- Health check endpoints
- Redis clustering/replication
- Service discovery
- Additional algorithms (sliding window)

## System Components

### Component Diagram

```text
                         ┌─────────────────────┐
                         │   Client Services   │
                         │  (Service A, B, C)  │
                         └──────────┬──────────┘
                                    │ gRPC
                                    ▼
                         ┌─────────────────────┐
                         │        nginx        │
                         └──────────┬──────────┘
                                    │
              ┌─────────────────────┼─────────────────────┐
              │                     │                     │
              ▼                     ▼                     ▼
     ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
     │   Turnstile 1   │   │   Turnstile 2   │   │   Turnstile N   │
     │      (Go)       │   │      (Go)       │   │      (Go)       │
     └────────┬────────┘   └────────┬────────┘   └────────┬────────┘
              │                     │                     │
              └─────────────────────┼─────────────────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │       Redis         │
                         │  (Shared State)     │
                         └─────────────────────┘
```

### Components

- **Rate Limiter Service (Go):** Receives gRPC requests, executes the rate limit
algorithm via Redis Lua scripts, and returns allow/deny decisions.
- **Logging Interceptor:** gRPC unary interceptor that wraps every request,
emitting a structured log line with action, outcome (`allowed`/denied/error),
latency, and gRPC status code. Runs at `Debug` level for allowed requests,
`Info` for denied, and `Error` for failures.
- **Redis:** Holds the state of the system and uses atomic operations
via Lua scripts to avoid race conditions.
- **nginx Load Balancer:** Distributes traffic to all instances of the rate
limiter, uniting them under a single endpoint.
- **Configuration:** Rate limits, Redis connection, Service settings.

## Request Flow

### Step-by-Step Flow

1. Client sends gRPC request including an identifier and the action requested
2. nginx selects a Turnstile instance and forwards the request
3. The logging interceptor records the start time and extracts the action
4. Turnstile sends a Lua script to Redis containing the rate limit algorithm
5. Redis executes the Lua script atomically — checking available tokens,
decrementing if allowed, and returning the result
6. The logging interceptor emits a structured log line with action, outcome,
latency, and gRPC status code, then returns the response
7. Turnstile returns a response containing: allow/deny decision,
remaining tokens, and (if denied) a retry-after duration indicating when
tokens will be available
8. The client service processes the response — proceeding on allow,
or returning an error to the user on deny

### Sequence Diagram

```text
┌────────┐    ┌───────┐    ┌─────────────┐    ┌───────────┐    ┌───────┐
│ Client │    │ nginx │    │  Logging    │    │ Turnstile │    │ Redis │
│        │    │       │    │ Interceptor │    │  Handler  │    │       │
└───┬────┘    └───┬───┘    └──────┬──────┘    └─────┬─────┘    └───┬───┘
    │             │               │                  │              │
    │ CheckRate   │               │                  │              │
    │ Limit       │               │                  │              │
    │────────────>│               │                  │              │
    │             │               │                  │              │
    │             │ forward       │                  │              │
    │             │──────────────>│                  │              │
    │             │               │ record start     │              │
    │             │               │──────┐           │              │
    │             │               │      │           │              │
    │             │               │<─────┘           │              │
    │             │               │                  │              │
    │             │               │ invoke handler   │              │
    │             │               │─────────────────>│              │
    │             │               │                  │ EVALSHA(Lua) │
    │             │               │                  │─────────────>│
    │             │               │                  │              │
    │             │               │                  │ {allowed,    │
    │             │               │                  │  tokens,     │
    │             │               │                  │  retry_after}│
    │             │               │                  │<─────────────│
    │             │               │                  │              │
    │             │               │ response + nil   │              │
    │             │               │<─────────────────│              │
    │             │               │                  │              │
    │             │               │ log(action,      │              │
    │             │               │     allowed,     │              │
    │             │               │     latency,     │              │
    │             │               │     grpc_code)   │              │
    │             │               │──────┐           │              │
    │             │               │      │           │              │
    │             │               │<─────┘           │              │
    │             │               │                  │              │
    │             │ {allowed,     │                  │              │
    │             │  tokens,      │                  │              │
    │             │  retry_after} │                  │              │
    │             │<──────────────│                  │              │
    │             │               │                  │              │
    │ {allowed,   │               │                  │              │
    │  tokens,    │               │                  │              │
    │  retry_     │               │                  │              │
    │  after}     │               │                  │              │
    │<────────────│               │                  │              │
    │             │               │                  │              │
```

## Technology Choices

### Why Go?

Lightweight with a small memory footprint. Its concurrency model (goroutines)
is well-suited for handling many concurrent requests efficiently.

### Why gRPC?

Binary protocol with smaller bandwidth for faster communication. Not
human-readable like REST, but ideal for service-to-service communication
with strong typing and code generation.

### Why Redis?

In-memory key-value store supporting atomic operations via Lua scripts,
without the overhead, latency, and transactional locks required by a
traditional DBMS like PostgreSQL.

### Why nginx?

Simple, fast load balancer with native gRPC support and proven reliability
at scale.

## Key Design Decisions

### Atomic Operations with Lua

Without atomic operations, concurrent requests could read the same token count
before either decrements it, allowing more requests than the limit permits.
Lua scripts execute atomically in Redis - the entire check-and-decrement
happens as a single uninterruptible operation, eliminating race conditions.

### Fail-Open Strategy

When Redis is unavailable, Turnstile fails open - allowing all requests rather
than blocking them. Rate limiting is a protective layer, not core functionality;
temporary loss of rate limiting is preferable to total service disruption.
This tradeoff prioritizes availability over strict enforcement.

### Horizontal Scaling

Turnstile instances are stateless - all rate limit state lives in the shared
Redis instance. This allows horizontal scaling by simply adding more instances
behind nginx; they don't need to coordinate with each other since they all
read and write to the same Redis store.
