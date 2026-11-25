// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import "context"

type start struct {
	commit  string
	problem string
}

func startOf(ctx context.Context, r repo, at string, now snapshot, b bench) (start, error) {
	if pinned := now.config.Starts[b.String()]; pinned != "" {
		same, err := now.sameWorkload(ctx, r, pinned, b)
		if err != nil {
			return start{}, err
		}
		if !same {
			return start{commit: pinned, problem: "its workload is not the one at its starting commit " + short(pinned)}, nil
		}
		return start{commit: pinned}, nil
	}
	args := append([]string{"log", "--first-parent", "--format=%H", at, "--", b.dir}, now.config.WorkloadDirs...)
	out, err := r.git(ctx, "", args...)
	if err != nil {
		return start{}, err
	}
	first := at
	for _, commit := range split(out, "\n") {
		same, err := now.sameWorkload(ctx, r, commit, b)
		if err != nil {
			return start{}, err
		}
		if !same {
			break
		}
		first = commit
	}
	return start{commit: first}, nil
}
