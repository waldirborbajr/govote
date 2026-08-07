// Package worker runs the single-writer vote consumer when GOVOTE_VOTE_WORKER=true.
package worker

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/waldirborbajr/govote/internal/cache"
	"github.com/waldirborbajr/govote/internal/poll"
)

const consumerGroup = "govote-writers"

// RunVoteWorker blocks, consuming the Redis vote stream and calling CastVote.
// Intended to run in a dedicated process/container (single writer to SQLite).
func RunVoteWorker(ctx context.Context) {
	if !cache.Enabled() {
		log.Println("worker: Redis indisponível — vote worker não iniciado")
		return
	}
	rdb := cache.Client()
	ensureGroup(rdb)

	consumer := hostname()
	log.Printf("worker: vote worker iniciado (consumer=%s stream=%s)", consumer, cache.VoteStream)

	for {
		select {
		case <-ctx.Done():
			log.Println("worker: encerrando vote worker")
			return
		default:
		}

		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: consumer,
			Streams:  []string{cache.VoteStream, ">"},
			Count:    20,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if err == context.Canceled || err == redis.Nil {
				continue
			}
			log.Printf("worker: XReadGroup error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, st := range streams {
			for _, msg := range st.Messages {
				processMessage(ctx, rdb, msg)
			}
		}
	}
}

func ensureGroup(rdb *redis.Client) {
	ctx := context.Background()
	err := rdb.XGroupCreateMkStream(ctx, cache.VoteStream, consumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Printf("worker: create group: %v", err)
	}
}

func processMessage(ctx context.Context, rdb *redis.Client, msg redis.XMessage) {
	raw, _ := msg.Values["payload"].(string)
	var job cache.VoteJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		log.Printf("worker: payload inválido id=%s: %v", msg.ID, err)
		_ = rdb.XAck(ctx, cache.VoteStream, consumerGroup, msg.ID)
		return
	}

	if cache.HasVoted(job.PollID, job.VoterHash) {
		_ = rdb.XAck(ctx, cache.VoteStream, consumerGroup, msg.ID)
		return
	}

	if voteErr := poll.CastVote(job.PollID, job.CPF, job.AnswerIDs); voteErr != nil {
		// 409 = already voted — still ack; other errors: leave for retry (no ack)
		if voteErr.Status == 409 {
			cache.MarkVoted(job.PollID, job.VoterHash)
			cache.InvalidatePoll(job.PollID)
			_ = rdb.XAck(ctx, cache.VoteStream, consumerGroup, msg.ID)
			return
		}
		log.Printf("worker: CastVote falhou poll=%d: %s (status=%d)", job.PollID, voteErr.Message, voteErr.Status)
		// transient: do not ack so it can be retried
		if voteErr.Status >= 500 {
			time.Sleep(200 * time.Millisecond)
			return
		}
		// permanent client error — ack to avoid poison loop
		_ = rdb.XAck(ctx, cache.VoteStream, consumerGroup, msg.ID)
		return
	}

	cache.MarkVoted(job.PollID, job.VoterHash)
	cache.InvalidatePoll(job.PollID)
	_ = rdb.XAck(ctx, cache.VoteStream, consumerGroup, msg.ID)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "worker-1"
	}
	return h
}
