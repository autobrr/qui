package crossseed

import (
	"context"
	"slices"
	"sync"
)

type injectionStandIn struct {
	script func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error)

	mu       sync.Mutex
	requests []*CrossSeedRequest
}

func newInjectionStandIn(script func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error)) *injectionStandIn {
	return &injectionStandIn{script: script}
}

func (s *injectionStandIn) CrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return s.script(ctx, req)
}

func (s *injectionStandIn) Requests() []*CrossSeedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}
