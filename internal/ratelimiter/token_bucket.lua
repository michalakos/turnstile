-- Token Bucket Rate Limiting Algorithm
-- Runs atomically in Redis via EVAL/EVALSHA
--
-- KEYS[1] = rate limit identifier (e.g., "user:123:api")
-- ARGV[1] = max_tokens (bucket capacity)
-- ARGV[2] = refill_rate (tokens per second)
-- ARGV[3] = cost (tokens to consume)
-- ARGV[4] = current_time (unix timestamp in seconds)
--
-- Returns: {allowed, remaining, limit, retry_after}

local key = KEYS[1]
local max_tokens = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

-- Get current state (nil if key doesn't exist)
local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(bucket[1])
local last_refill = tonumber(bucket[2])

-- Initialize new bucket with full tokens
if tokens == nil then
    tokens = max_tokens
    last_refill = now
end

-- Calculate tokens to add based on elapsed time
local elapsed = now - last_refill
local tokens_to_add = elapsed * refill_rate
tokens = math.min(tokens + tokens_to_add, max_tokens)

-- Check if request can be allowed
local allowed = 0
local retry_after = 0

if tokens >= cost then
    allowed = 1
    tokens = tokens - cost
else
    -- Calculate time until enough tokens are available
    local tokens_needed = cost - tokens
    retry_after = math.ceil(tokens_needed / refill_rate)
end

-- Persist state
redis.call('HSET', key, 'tokens', tokens, 'last_refill', now)

-- Set TTL: time to full refill * 2 buffer, minimum 60 seconds
local ttl = math.max(60, math.ceil((max_tokens / refill_rate) * 2))
redis.call('EXPIRE', key, ttl)

-- Return: allowed, remaining, limit, retry_after
return {allowed, math.floor(tokens), max_tokens, retry_after}
