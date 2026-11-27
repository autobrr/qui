// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type server struct {
	name, url, key string
}

type announce struct {
	TorrentName            string `json:"torrentName"`
	InstanceIDs            []int  `json:"instanceIds,omitempty"`
	Size                   uint64 `json:"size,omitempty"`
	Indexer                string `json:"indexer,omitempty"`
	FindIndividualEpisodes *bool  `json:"findIndividualEpisodes,omitempty"`
}

type match struct {
	InstanceID  int    `json:"instanceId"`
	TorrentHash string `json:"torrentHash"`
	TorrentName string `json:"torrentName"`
	MatchType   string `json:"matchType"`
}

type seasonPack struct {
	InstanceID       int     `json:"instanceId"`
	MatchedEpisodes  int     `json:"matchedEpisodes"`
	TotalEpisodes    int     `json:"totalEpisodes"`
	Coverage         float64 `json:"coverage"`
	ThresholdSkipped bool    `json:"thresholdSkipped"`
}

type answer struct {
	Status         int         `json:"status"`
	CanCrossSeed   bool        `json:"canCrossSeed"`
	Recommendation string      `json:"recommendation"`
	Matches        []match     `json:"matches"`
	SeasonPack     *seasonPack `json:"seasonPack,omitempty"`
	Millis         float64     `json:"ms"`
	Error          string      `json:"error,omitempty"`
}

func readAnnounces(path string, instanceIDs []int) ([]announce, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var announces []announce
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		a, err := parseAnnounce(text)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if len(a.InstanceIDs) == 0 {
			a.InstanceIDs = instanceIDs
		}
		announces = append(announces, a)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(announces) == 0 {
		return nil, fmt.Errorf("%s holds no announce", path)
	}
	return announces, nil
}

func parseAnnounce(text string) (announce, error) {
	var a announce
	if strings.HasPrefix(text, "{") {
		if err := json.Unmarshal([]byte(text), &a); err != nil {
			return a, err
		}
	} else {
		name, size, found := strings.Cut(text, "\t")
		a.TorrentName = strings.TrimSpace(name)
		if found {
			parsed, err := strconv.ParseUint(strings.TrimSpace(size), 10, 64)
			if err != nil {
				return a, fmt.Errorf("size %q is not a number of bytes", size)
			}
			a.Size = parsed
		}
	}
	if a.TorrentName == "" {
		return a, errors.New("no torrent name")
	}
	return a, nil
}

func (s server) check(ctx context.Context, client *http.Client, a announce) answer {
	body, err := json.Marshal(a)
	if err != nil {
		return answer{Error: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/api/cross-seed/webhook/check", bytes.NewReader(body))
	if err != nil {
		return answer{Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if s.key != "" {
		req.Header.Set("X-API-Key", s.key)
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return answer{Error: err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	got := answer{Status: resp.StatusCode, Millis: float64(time.Since(started).Microseconds()) / 1000}
	if err != nil {
		got.Error = err.Error()
		return got
	}
	if !knownStatus(resp.StatusCode) {
		got.Error = fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		return got
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		got.Error = fmt.Sprintf("status %d with a body that is not a webhook check answer: %v", resp.StatusCode, err)
	}
	got.Status = resp.StatusCode
	return got
}

func knownStatus(status int) bool {
	return status == http.StatusOK || status == http.StatusAccepted || status == http.StatusNotFound
}

type result struct {
	Announce announce `json:"announce"`
	Old      answer   `json:"old"`
	New      answer   `json:"new"`
	Verdict  verdict  `json:"verdict"`
	Extra    []match  `json:"extra,omitempty"`
}

func replay(ctx context.Context, c config, announces []announce, out io.Writer) ([]result, error) {
	results := make([]result, len(announces))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var mu sync.Mutex
	var writeErr error
	done := 0
	for range c.workers {
		workers.Go(func() {
			for i := range jobs {
				a := announces[i]
				var old, now answer
				var pair sync.WaitGroup
				pair.Go(func() { old = c.old.check(ctx, c.client, a) })
				pair.Go(func() { now = c.new.check(ctx, c.client, a) })
				pair.Wait()
				r := compare(a, old, now)
				mu.Lock()
				results[i] = r
				if writeErr == nil {
					writeErr = json.MarshalWrite(out, r)
					if writeErr == nil {
						_, writeErr = io.WriteString(out, "\n")
					}
				}
				done++
				if done%100 == 0 {
					fmt.Fprintf(c.stderr, "%d of %d announces\n", done, len(announces))
				}
				mu.Unlock()
			}
		})
	}
	for i := range announces {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, writeErr
}

func waitReady(ctx context.Context, c config) error {
	probe := announce{TorrentName: c.ready, InstanceIDs: c.instanceIDs}
	deadline := time.Now().Add(c.readyTimeout)
	for {
		old, now := c.old.check(ctx, c.client, probe), c.new.check(ctx, c.client, probe)
		if len(old.Matches) > 0 && len(now.Matches) > 0 {
			fmt.Fprintf(c.stderr, "both servers match %q\n", c.ready)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("after %s, %q still gets no match: old %s, new %s", c.readyTimeout, c.ready, old.brief(), now.brief())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.readyPollInterval):
		}
	}
}

func (a answer) brief() string {
	if a.Error != "" {
		return a.Error
	}
	return fmt.Sprintf("status %d with %d matches", a.Status, len(a.Matches))
}
