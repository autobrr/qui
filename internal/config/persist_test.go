// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"strings"
	"testing"
)

func TestUpdateLogSettingsInTOMLUpdatesCommentedKeysInPlace(t *testing.T) {
	content := `# config.toml - Auto-generated on first run

# Log file path
# If not defined, logs to stdout
# Optional
#logPath = "log/qui.log"

# Log rotation
# Maximum log file size in megabytes before rotation
# Default: 50
#logMaxSize = 50

# Number of rotated log files to retain (0 keeps all)
# Default: 3
#logMaxBackups = 3

# Log level
# Default: "INFO"
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
logLevel = "INFO"

# HTTP Timeouts
[httpTimeouts]
#readTimeout = 60
`
	updated := updateLogSettingsInTOML(content, "DEBUG", "/config/qui.log", 50, 3)

	if strings.Contains(updated, "# Log settings") {
		t.Fatalf("unexpected appended log settings section:\n%s", updated)
	}

	httpIndex := strings.Index(updated, "[httpTimeouts]")
	if httpIndex == -1 {
		t.Fatalf("missing httpTimeouts section:\n%s", updated)
	}

	lastLogPath := strings.LastIndex(updated, "logPath")
	if lastLogPath == -1 {
		t.Fatalf("missing logPath setting:\n%s", updated)
	}
	if lastLogPath > httpIndex {
		t.Fatalf("logPath appended after httpTimeouts section:\n%s", updated)
	}

	if !strings.Contains(updated, `logPath = "/config/qui.log"`) {
		t.Fatalf("logPath not updated in place:\n%s", updated)
	}
	if !strings.Contains(updated, "logMaxSize = 50") {
		t.Fatalf("logMaxSize not updated in place:\n%s", updated)
	}
	if !strings.Contains(updated, "logMaxBackups = 3") {
		t.Fatalf("logMaxBackups not updated in place:\n%s", updated)
	}
	if !strings.Contains(updated, `logLevel = "DEBUG"`) {
		t.Fatalf("logLevel not updated in place:\n%s", updated)
	}
}
