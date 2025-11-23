package jackett

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
)

func TestRateLimiterRespectsCooldown(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	cooldown := 40 * time.Millisecond
	tolerance := 15 * time.Millisecond
	limiter.SetCooldown(indexer.ID, time.Now().Add(cooldown))

	start := time.Now()
	if err := limiter.BeforeRequest(context.Background(), indexer, nil); err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < cooldown-tolerance {
		t.Fatalf("expected to wait at least %v (cooldown %v - tolerance %v), waited %v", cooldown-tolerance, cooldown, tolerance, elapsed)
	}
}

func TestRateLimiterGetCooldownIndexers(t *testing.T) {
	limiter := NewRateLimiter(time.Millisecond)

	limiter.SetCooldown(1, time.Now().Add(100*time.Millisecond))
	limiter.SetCooldown(2, time.Now().Add(20*time.Millisecond))

	time.Sleep(40 * time.Millisecond)

	cooldowns := limiter.GetCooldownIndexers()

	if _, ok := cooldowns[1]; !ok {
		t.Fatalf("expected indexer 1 to still be in cooldown")
	}
	if _, ok := cooldowns[2]; ok {
		t.Fatalf("expected indexer 2 cooldown to expire")
	}
}

func TestRateLimiterIsInCooldown(t *testing.T) {
	limiter := NewRateLimiter(time.Millisecond)

	limiter.SetCooldown(1, time.Now().Add(20*time.Millisecond))

	inCooldown, resumeAt := limiter.IsInCooldown(1)
	if !inCooldown {
		t.Fatalf("expected indexer to be in cooldown immediately after SetCooldown")
	}
	if resumeAt.Before(time.Now()) {
		t.Fatalf("expected resumeAt to be in the future")
	}

	time.Sleep(30 * time.Millisecond)

	inCooldown, _ = limiter.IsInCooldown(1)
	if inCooldown {
		t.Fatalf("expected cooldown to expire")
	}
}

func TestRateLimiterMaxWaitBudget(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1, Name: "Test"}

	limiter.RecordRequest(indexer.ID, time.Now())

	opts := &RateLimitOptions{
		Priority:    RateLimitPriorityInteractive,
		MinInterval: 50 * time.Millisecond,
		MaxWait:     10 * time.Millisecond,
	}

	err := limiter.BeforeRequest(context.Background(), indexer, opts)
	if err == nil {
		t.Fatalf("expected RateLimitWaitError when required wait exceeds MaxWait")
	}

	var waitErr *RateLimitWaitError
	if !errors.As(err, &waitErr) {
		t.Fatalf("expected RateLimitWaitError, got %v", err)
	}

	if waitErr.Wait <= waitErr.MaxWait {
		t.Fatalf("expected wait to exceed max wait, got wait %v max %v", waitErr.Wait, waitErr.MaxWait)
	}
}
