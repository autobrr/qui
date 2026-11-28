package crossseed

import (
	"context"
	"slices"
	"sync"
)

type seasonPackAddStandIn struct {
	script func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error)

	mu       sync.Mutex
	requests []seasonPackRequest
}

func newSeasonPackAddStandIn(script func(context.Context, seasonPackRequest) (*seasonPackApplyResult, error)) *seasonPackAddStandIn {
	return &seasonPackAddStandIn{script: script}
}

func (s *seasonPackAddStandIn) applySeasonPack(ctx context.Context, req seasonPackRequest) (*seasonPackApplyResult, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return s.script(ctx, req)
}

func (s *seasonPackAddStandIn) Requests() []seasonPackRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}
