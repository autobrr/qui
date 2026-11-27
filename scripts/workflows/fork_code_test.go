// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package workflows

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const ownRepository = "${{ github.repository }}"

var (
	ownRefs = []string{
		"${{ github.event.pull_request.base.sha }}",
		"${{ github.event.repository.default_branch }}",
	}
	storedSecret = regexp.MustCompile(`secrets(?:\.|\[')(\w+)`)
)

type workflow struct {
	Permissions yaml.Node            `yaml:"permissions"`
	Env         yaml.Node            `yaml:"env"`
	Jobs        map[string]yaml.Node `yaml:"jobs"`
}

type job struct {
	Permissions yaml.Node `yaml:"permissions"`
	Steps       []struct {
		Uses string `yaml:"uses"`
		Run  string `yaml:"run"`
		With struct {
			Repository string `yaml:"repository"`
			Ref        string `yaml:"ref"`
		} `yaml:"with"`
	} `yaml:"steps"`
}

func forkCodeWithAccess(t *testing.T, source string) []string {
	t.Helper()
	var wf workflow
	require.NoError(t, yaml.Unmarshal([]byte(source), &wf))

	var found []string
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		node := wf.Jobs[name]
		var j job
		require.NoError(t, node.Decode(&j))

		code := forkCode(j)
		if code == "" {
			continue
		}
		permissions := &j.Permissions
		if permissions.Kind == 0 {
			permissions = &wf.Permissions
		}
		access := slices.Concat(writeAccess(t, permissions), storedSecrets(&node), storedSecrets(&wf.Env))
		for _, a := range access {
			found = append(found, fmt.Sprintf("job %s runs %s and has %s", name, code, a))
		}
	}
	return found
}

func forkCode(j job) string {
	for _, step := range j.Steps {
		if strings.Contains(step.Run, "gh pr checkout") {
			return "gh pr checkout"
		}
		if !strings.HasPrefix(step.Uses, "actions/checkout@") {
			continue
		}
		if repository := step.With.Repository; repository != "" && repository != ownRepository {
			return "a checkout of repository " + repository
		}
		if ref := step.With.Ref; strings.Contains(ref, "${{") && !slices.Contains(ownRefs, ref) {
			return "a checkout of ref " + ref
		}
	}
	return ""
}

func writeAccess(t *testing.T, permissions *yaml.Node) []string {
	t.Helper()
	switch permissions.Kind {
	case 0:
		return []string{"the default token permissions"}
	case yaml.ScalarNode:
		if permissions.Value == "write-all" {
			return []string{"permissions: write-all"}
		}
		return nil
	default:
		var scopes map[string]string
		require.NoError(t, permissions.Decode(&scopes))
		var writes []string
		for _, scope := range slices.Sorted(maps.Keys(scopes)) {
			if scopes[scope] == "write" {
				writes = append(writes, scope+": write")
			}
		}
		return writes
	}
}

func storedSecrets(node *yaml.Node) []string {
	var secrets []string
	if node.Kind == yaml.ScalarNode {
		for _, match := range storedSecret.FindAllStringSubmatch(node.Value, -1) {
			if match[1] != "GITHUB_TOKEN" {
				secrets = append(secrets, "the secret "+match[1])
			}
		}
	}
	for _, child := range node.Content {
		secrets = append(secrets, storedSecrets(child)...)
	}
	return secrets
}

func TestNoJobRunsForkCodeWithWriteAccessOrASecret(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.y*ml"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 20)

	for _, file := range files {
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Empty(t, forkCodeWithAccess(t, string(source)), filepath.Base(file))
	}
}

func TestForkCodeWithAccess(t *testing.T) {
	const forkCheckout = `
      - uses: actions/checkout@v7.0.1
        with:
          repository: ${{ steps.pr.outputs.repo }}
          ref: ${{ steps.pr.outputs.sha }}
`
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{
			name: "a fork checkout with a write permission of the workflow",
			source: `
permissions:
  contents: read
  packages: write
jobs:
  docker:
    steps:` + forkCheckout,
			want: []string{"job docker runs a checkout of repository ${{ steps.pr.outputs.repo }} and has packages: write"},
		},
		{
			name: "a fork checkout in a job that can only read, beside a job that can write",
			source: `
permissions:
  contents: read
jobs:
  build:
    steps:` + forkCheckout + `
  push:
    permissions:
      packages: write
    steps:
      - uses: actions/download-artifact@v8
`,
		},
		{
			name: "the head of a pull request with a write permission of the job",
			source: `
permissions: {}
jobs:
  comment:
    permissions:
      pull-requests: write
    steps:
      - uses: actions/checkout@v7.0.1
        with:
          ref: ${{ github.event.pull_request.head.sha }}
`,
			want: []string{"job comment runs a checkout of ref ${{ github.event.pull_request.head.sha }} and has pull-requests: write"},
		},
		{
			name: "the base of a pull request with a write permission",
			source: `
jobs:
  comment:
    permissions:
      pull-requests: write
    steps:
      - uses: actions/checkout@v7.0.1
        with:
          ref: ${{ github.event.pull_request.base.sha }}
`,
		},
		{
			name: "a fork checkout with a stored secret",
			source: `
permissions:
  contents: read
jobs:
  build:
    steps:` + forkCheckout + `
      - run: make build
        env:
          SIGNING_KEY: ${{ secrets.SIGNING_KEY }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
`,
			want: []string{"job build runs a checkout of repository ${{ steps.pr.outputs.repo }} and has the secret SIGNING_KEY"},
		},
		{
			name: "a fork checkout with a stored secret in the bracket form",
			source: `
permissions:
  contents: read
jobs:
  build:
    steps:` + forkCheckout + `
      - run: make build
        env:
          SIGNING_KEY: ${{ secrets['SIGNING_KEY'] }}
`,
			want: []string{"job build runs a checkout of repository ${{ steps.pr.outputs.repo }} and has the secret SIGNING_KEY"},
		},
		{
			name: "gh pr checkout with no permissions block",
			source: `
jobs:
  build:
    steps:
      - run: gh pr checkout 12
`,
			want: []string{"job build runs gh pr checkout and has the default token permissions"},
		},
		{
			name: "gh pr checkout with every permission",
			source: `
permissions: write-all
jobs:
  build:
    steps:
      - run: gh pr checkout 12
`,
			want: []string{"job build runs gh pr checkout and has permissions: write-all"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, forkCodeWithAccess(t, tc.source))
		})
	}
}
