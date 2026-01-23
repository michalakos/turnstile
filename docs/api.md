# API Reference

## gRPC Service

### Service Definition

```protobuf
syntax = "proto3";

package ratelimiter;

service RateLimiter {
  rpc CheckRateLimit(RateLimitRequest) returns (RateLimitResponse);
}

message RateLimitRequest {
  string identifier = 1;  // Who is being rate limited
  string action = 2;      // What action is being performed
  int64 cost = 3;         // Tokens to consume (default: 1)
}

message RateLimitResponse {
  bool allowed = 1;       // Whether the request can proceed
  int64 remaining = 2;    // Tokens remaining after this request
  int64 limit = 3;        // Maximum tokens for this bucket
  int64 retry_after = 4;  // Seconds to wait if denied (0 if allowed)
}
```

> **Note:** `retry_after` is a duration (seconds) rather than a Unix
> timestamp to avoid clock synchronization between services.

### CheckRateLimit

Checks whether a request should be allowed based on the configured
rate limit for the given identifier and action.

**Request Fields:**

| Field        | Type   | Required | Description                    |
|--------------|--------|----------|--------------------------------|
| `identifier` | string | Yes      | User ID, IP, or API key        |
| `action`     | string | Yes      | Action name (e.g., "api.post") |
| `cost`       | int64  | No       | Tokens to consume (default: 1) |

**Response Fields:**

| Field         | Type  | Description                        |
|---------------|-------|------------------------------------|
| `allowed`     | bool  | Whether the request can proceed    |
| `remaining`   | int64 | Tokens remaining after this call   |
| `limit`       | int64 | Maximum tokens for this bucket     |
| `retry_after` | int64 | Seconds to wait if denied (else 0) |

**Example Request (grpcurl):**

```bash
grpcurl -plaintext \
  -d '{"identifier":"user123","action":"api.post","cost":1}' \
  localhost:50051 \
  ratelimiter.RateLimiter/CheckRateLimit
```

**Example Response (Allowed):**

```json
{
  "allowed": true,
  "remaining": 99,
  "limit": 100,
  "retryAfter": 0
}
```

**Example Response (Denied):**

```json
{
  "allowed": false,
  "remaining": 0,
  "limit": 100,
  "retryAfter": 30
}
```

## Error Codes

A rate limit denial is not an error - it is a valid response
indicating the request should not proceed. Error codes are
reserved for actual problems.

| Code               | When                                       |
|--------------------|--------------------------------------------|
| `OK`               | Request processed (check `allowed` field)  |
| `INVALID_ARGUMENT` | Empty identifier, empty action, cost < 1   |
| `INTERNAL`         | Lua script error, unexpected Redis failure |
