// Package cache provides an optional Redis-backed cache, rate-limit store and
// vote queue used for Black Friday scale-out. When GOVOTE_REDIS_URL is empty the
// helpers no-op and the app falls back to in-process behaviour.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	rdb     *redis.Client
	once    sync.Once
	enabled bool
)

// Init connects to Redis when GOVOTE_REDIS_URL is set (e.g. redis://redis:6379/0).
// Safe to call multiple times; subsequent calls are no-ops.
func Init() {
	once.Do(func() {
		url := strings.TrimSpace(os.Getenv("GOVOTE_REDIS_URL"))
		if url == "" {
			log.Println("cache: Redis desabilitado (GOVOTE_REDIS_URL vazio) — rate-limit e cache em memória local")
			return
		}
		opt, err := redis.ParseURL(url)
		if err != nil {
			log.Printf("cache: URL Redis inválida (%v) — Redis desabilitado", err)
			return
		}
		c := redis.NewClient(opt)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.Ping(ctx).Err(); err != nil {
			log.Printf("cache: falha ao conectar Redis (%v) — Redis desabilitado", err)
			_ = c.Close()
			return
		}
		rdb = c
		enabled = true
		log.Println("cache: Redis conectado — rate-limit, cache e fila de votos ativos")
	})
}

// Enabled reports whether Redis is available.
func Enabled() bool {
	return enabled && rdb != nil
}

// Client returns the underlying Redis client (may be nil).
func Client() *redis.Client {
	return rdb
}

// Close releases the Redis connection.
func Close() {
	if rdb != nil {
		_ = rdb.Close()
	}
}

// ---------------------------------------------------------------------------
// Rate limit (sliding window via sorted set per IP)
// ---------------------------------------------------------------------------

// AllowRateLimit returns (allowed, retryAfter). When Redis is off, callers
// should fall back to the in-memory limiter.
func AllowRateLimit(ip string, max int, window time.Duration) (bool, time.Duration) {
	if !Enabled() || ip == "" {
		return true, 0
	}
	ctx := context.Background()
	key := "rl:" + ip
	now := time.Now()
	minScore := now.Add(-window).UnixMilli()

	pipe := rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(minScore, 10))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixMilli()), Member: fmt.Sprintf("%d-%d", now.UnixNano(), os.Getpid())})
	pipe.Expire(ctx, key, window+time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("cache: rate-limit redis error: %v", err)
		return true, 0 // fail-open to avoid blocking all traffic
	}
	n := countCmd.Val()
	if int(n) >= max {
		// Approximate retry: full window
		return false, window
	}
	return true, 0
}

// ---------------------------------------------------------------------------
// Generic JSON cache
// ---------------------------------------------------------------------------

func GetJSON(key string, dest any) bool {
	if !Enabled() {
		return false
	}
	ctx := context.Background()
	val, err := rdb.Get(ctx, key).Bytes()
	if err != nil {
		return false
	}
	if err := json.Unmarshal(val, dest); err != nil {
		return false
	}
	return true
}

func SetJSON(key string, v any, ttl time.Duration) {
	if !Enabled() {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	ctx := context.Background()
	_ = rdb.Set(ctx, key, b, ttl).Err()
}

func Del(keys ...string) {
	if !Enabled() || len(keys) == 0 {
		return
	}
	ctx := context.Background()
	_ = rdb.Del(ctx, keys...).Err()
}

// InvalidatePoll drops cached list + detail + results for a poll.
func InvalidatePoll(pollID int64) {
	Del(
		"polls:active",
		fmt.Sprintf("poll:%d", pollID),
		fmt.Sprintf("results:%d", pollID),
	)
}

// ---------------------------------------------------------------------------
// Already-voted fast path (avoids UNIQUE hit on every attempt)
// ---------------------------------------------------------------------------

func MarkVoted(pollID int64, voterHash string) {
	if !Enabled() {
		return
	}
	ctx := context.Background()
	key := fmt.Sprintf("voted:%d:%s", pollID, voterHash)
	_ = rdb.Set(ctx, key, "1", 48*time.Hour).Err()
}

func HasVoted(pollID int64, voterHash string) bool {
	if !Enabled() {
		return false
	}
	ctx := context.Background()
	key := fmt.Sprintf("voted:%d:%s", pollID, voterHash)
	n, err := rdb.Exists(ctx, key).Result()
	return err == nil && n > 0
}

// ---------------------------------------------------------------------------
// Vote queue (Redis Stream) — optional async path
// ---------------------------------------------------------------------------

const VoteStream = "govote:votes"

// VoteJob is the payload enqueued for the single writer worker.
type VoteJob struct {
	PollID    int64   `json:"poll_id"`
	CPF       string  `json:"cpf"`
	VoterHash string  `json:"voter_hash"`
	AnswerIDs []int64 `json:"answer_ids"`
	Enqueued  string  `json:"enqueued_at"`
}

// EnqueueVote pushes a vote job. Returns false if Redis is unavailable.
func EnqueueVote(job VoteJob) bool {
	if !Enabled() {
		return false
	}
	if job.Enqueued == "" {
		job.Enqueued = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.Marshal(job)
	if err != nil {
		return false
	}
	ctx := context.Background()
	_, err = rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: VoteStream,
		Values: map[string]any{"payload": string(b)},
	}).Result()
	return err == nil
}

// AsyncVotesEnabled is true when GOVOTE_VOTE_ASYNC=true and Redis is up.
func AsyncVotesEnabled() bool {
	return Enabled() && strings.EqualFold(os.Getenv("GOVOTE_VOTE_ASYNC"), "true")
}
