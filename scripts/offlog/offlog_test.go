// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package offlog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLines(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want []int
	}{
		{"a DEBUG message that says disabled", "p.go", `log.Debug().Msg("completion search disabled")`, []int{4}},
		{"an INFO message that says not configured", "p.go", `log.Info().Str("k", v).Msgf("%s is not configured", name)`, []int{4}},
		{"a DEBUG line in the body of a negated on switch", "p.go", "if !cfg.Enabled {\n\tlog.Debug().\n\t\tInt(\"id\", id).\n\t\tMsg(\"skipping\")\n}", []int{5}},
		{"a DEBUG line in the body of an off switch", "p.go", "if cfg.Disabled && verbose {\n\tlog.Debug().Msg(\"skipping\")\n}", []int{5}},
		{"a DEBUG line where each side of an or is off", "p.go", "if !cfg.Enabled || keysOff {\n\tlog.Debug().Msg(\"skipping\")\n}", []int{5}},
		{"a DEBUG line where one side of an or is not off", "p.go", "if cfg.Disabled || force {\n\tlog.Debug().Msg(\"running\")\n}", nil},
		{"a DEBUG line in the body of an off call", "p.go", "if s.lookupsOff() {\n\tlogger.Debug().Send()\n}", []int{5}},
		{"a DEBUG line in the else of an on switch", "p.go", "if instance.IsActive {\n\tlog.Debug().Msg(\"syncing\")\n} else {\n\tlog.Debug().Msg(\"skipping\")\n}", []int{7}},
		{"a DEBUG line in the else of an on switch or another test", "p.go", "if cfg.Enabled || force {\n\tlog.Debug().Msg(\"running\")\n} else {\n\tlog.Debug().Msg(\"skipping\")\n}", []int{7}},
		{"a DEBUG line in the else of an on switch and another test", "p.go", "if cfg.Enabled && ready {\n\tlog.Debug().Msg(\"running\")\n} else {\n\tlog.Debug().Msg(\"waiting\")\n}", nil},
		{"a DEBUG line in the else of a negated off switch", "p.go", "if !cfg.Disabled {\n\tlog.Debug().Msg(\"running\")\n} else {\n\tlog.Debug().Msg(\"skipping\")\n}", []int{7}},
		{"a DEBUG line in a closure inside an off branch", "p.go", "if !cfg.Enabled {\n\tdefer func() { log.Debug().Msg(\"skipped\") }()\n}", []int{5}},
		{"a DEBUG line with an error field", "p.go", `log.Debug().Err(err).Msg("feature disabled")`, []int{4}},
		{"an off word in a format value", "p.go", `log.Info().Msgf("state changed from %s to on", "disabled")`, nil},
		{"a TRACE line", "p.go", "if !cfg.Enabled {\n\tlog.Trace().Msg(\"feature disabled\")\n}", nil},
		{"a warning", "p.go", "if !cfg.Enabled {\n\tlog.Warn().Msg(\"running with auth-disabled\")\n}", nil},
		{"an error with an error field", "p.go", `log.Error().Err(err).Msg("feature disabled")`, nil},
		{"a DEBUG line in the body of an on switch", "p.go", "if cfg.Enabled && !ready {\n\tlog.Debug().Msg(\"waiting\")\n}", nil},
		{"a name that only ends in off", "p.go", "if backoff {\n\tlog.Debug().Msg(\"waiting\")\n}", nil},
		{"a test file", "p_test.go", `log.Debug().Msg("feature disabled")`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Lines(tt.path, []byte("package p\n\nfunc f() {\n"+tt.body+"\n}\n"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
