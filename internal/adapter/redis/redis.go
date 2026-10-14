//intenza:paradigm imperative-shell

// Package redis holds what Redis is good at here and nothing it must be
// trusted with: a GCRA rate limiter and a sorted-set index of deadlines.
// Idempotency does not live here: it comes from ids derived from the client's
// key and unique indexes in MongoDB. Every use has a fallback, because the
// guarantees live in MongoDB: losing Redis degrades latency, never correctness.
package redis

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/rueidis"
)

// Client wraps rueidis with the scripts this package needs.
type Client struct {
	c rueidis.Client
}

// Connect opens a client. Client-side caching is off: nothing here is read-mostly.
func Connect(addr string, timeout time.Duration) (*Client, error) {
	c, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:      []string{addr},
		DisableCache:     true,
		ConnWriteTimeout: timeout,
		Dialer:           netDialer(timeout),
	})
	if err != nil {
		return nil, err
	}
	return &Client{c: c}, nil
}

// Raw exposes the underlying client.
func (c *Client) Raw() rueidis.Client { return c.c }

// Close releases connections.
func (c *Client) Close() { c.c.Close() }

// Ping checks reachability.
func (c *Client) Ping(ctx context.Context) error { return c.c.Do(ctx, c.c.B().Ping().Build()).Error() }

// gcra is the generic cell rate algorithm: one key holds the theoretical
// arrival time; a request is allowed if it is not earlier than TAT - burst.
var gcra = rueidis.NewLuaScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local interval = tonumber(ARGV[2])
local burst = tonumber(ARGV[3])
local tat = tonumber(redis.call('GET', key) or now)
if tat < now then tat = now end
local allow_at = tat - burst * interval
if now < allow_at then
  return {0, allow_at - now}
end
local new_tat = tat + interval
redis.call('SET', key, new_tat, 'PX', math.ceil(new_tat - now + burst * interval))
return {1, 0}
`)

// Allow applies a GCRA limit of rate requests per second with a burst. The
// returned duration is how long to wait before retrying.
func (c *Client) Allow(ctx context.Context, key string, rate float64, burst int) (bool, time.Duration, error) {
	intervalMS := 1000 / rate
	now := time.Now().UnixMilli()
	res, err := gcra.Exec(ctx, c.c, []string{"intenza:rl:" + key},
		[]string{strconv.FormatInt(now, 10), strconv.FormatFloat(intervalMS, 'f', 3, 64), strconv.Itoa(burst)}).AsIntSlice()
	if err != nil {
		return false, 0, err
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond, nil
}

const deadlines = "intenza:deadlines"

// Schedule indexes a deadline (score = Unix milliseconds).
func (c *Client) Schedule(ctx context.Context, kind, id string, at time.Time) error {
	return c.c.Do(ctx, c.c.B().Zadd().Key(deadlines).ScoreMember().ScoreMember(float64(at.UnixMilli()), kind+":"+id).Build()).Error()
}

// claim pops due members atomically, so two workers never take the same one.
var claim = rueidis.NewLuaScript(`
local due = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
if #due > 0 then redis.call('ZREM', KEYS[1], unpack(due)) end
return due
`)

// ClaimDue returns up to limit deadlines due at now, as "kind:id".
func (c *Client) ClaimDue(ctx context.Context, now time.Time, limit int) ([]string, error) {
	return claim.Exec(ctx, c.c, []string{deadlines}, []string{strconv.FormatInt(now.UnixMilli(), 10), strconv.Itoa(limit)}).AsStrSlice()
}
