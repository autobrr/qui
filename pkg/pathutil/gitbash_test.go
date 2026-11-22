package pathutil

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeGitBashDrivePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase drive root without trailing slash",
			input:    "/c",
			expected: `C:\`,
		},
		{
			name:     "lowercase drive root with trailing slash",
			input:    "/c/",
			expected: `C:\`,
		},
		{
			name:     "lowercase drive with path",
			input:    "/c/backups",
			expected: `C:\backups`,
		},
		{
			name:     "non-c drive with trailing slash",
			input:    "/d/qui/backups/",
			expected: `D:\qui\backups`,
		},
		{
			name:     "uppercase drive with dot segments and double slashes",
			input:    "/D/a//b/../c",
			expected: `D:\a\c`,
		},
		{
			name:     "drive with whitespace padding preserved vs trimmed",
			input:    "  /e/data  ",
			expected: "  /e/data  ",
		},
		{
			name:     "unix multi-letter path left unchanged",
			input:    "/etc/passwd",
			expected: "/etc/passwd",
		},
		{
			name:     "unc path left unchanged",
			input:    "//server/share",
			expected: "//server/share",
		},
		{
			name:     "native windows path left unchanged",
			input:    `C:\backups`,
			expected: `C:\backups`,
		},
		{
			name:     "relative path left unchanged",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "empty string left unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "single slash left unchanged",
			input:    "/",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, NormalizeGitBashDrivePath(tt.input))
		})
	}
}

func TestNormalizeWindowsDrivePath(t *testing.T) {
	input := "/d/qui/backups"
	got := NormalizeWindowsDrivePath(input)
	if runtime.GOOS == "windows" {
		assert.Equal(t, `D:\qui\backups`, got)
	} else {
		assert.Equal(t, input, got)
	}
}
