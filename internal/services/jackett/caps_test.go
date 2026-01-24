// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"strings"
	"testing"
)

func TestParseTorznabCaps_Limits(t *testing.T) {
	tests := []struct {
		name        string
		xml         string
		wantDefault int
		wantMax     int
	}{
		{
			name: "limits present with both values",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="50" max="100"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 50,
			wantMax:     100,
		},
		{
			name: "limits missing",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 100,
			wantMax:     100,
		},
		{
			name: "limits with invalid values",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="abc" max="xyz"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 100,
			wantMax:     100,
		},
		{
			name: "only max present",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits max="100"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 100,
			wantMax:     100,
		},
		{
			name: "only default present",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="50"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 50,
			wantMax:     100,
		},
		{
			name: "limits with zero values",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="0" max="0"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 100,
			wantMax:     100,
		},
		{
			name: "limits with negative values",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="-10" max="-5"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 100,
			wantMax:     100,
		},
		{
			name: "real-world MTV example",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="50" max="100"/>
	<searching>
		<search available="yes" supportedParams="q,imdbid"/>
		<tv-search available="yes" supportedParams="q,tvdbid,imdbid,season,ep"/>
		<movie-search available="yes" supportedParams="q,imdbid"/>
	</searching>
	<categories>
		<category id="2000" name="Movies">
			<subcat id="2010" name="Foreign"/>
			<subcat id="2020" name="Other"/>
		</category>
	</categories>
</caps>`,
			wantDefault: 50,
			wantMax:     100,
		},
		{
			name: "limits with large values",
			xml: `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="500" max="1000"/>
	<searching>
		<search available="yes" supportedParams="q"/>
	</searching>
	<categories></categories>
</caps>`,
			wantDefault: 500,
			wantMax:     1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps, err := parseTorznabCaps(strings.NewReader(tt.xml))
			if err != nil {
				t.Fatalf("parseTorznabCaps() error = %v", err)
			}

			if caps.LimitDefault != tt.wantDefault {
				t.Errorf("LimitDefault = %d, want %d", caps.LimitDefault, tt.wantDefault)
			}
			if caps.LimitMax != tt.wantMax {
				t.Errorf("LimitMax = %d, want %d", caps.LimitMax, tt.wantMax)
			}
		})
	}
}

func TestParseTorznabCaps_FullParsing(t *testing.T) {
	// Test that limits parsing doesn't interfere with other cap parsing
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<limits default="50" max="100"/>
	<searching>
		<search available="yes" supportedParams="q,imdbid"/>
		<tv-search available="yes" supportedParams="q,tvdbid,season,ep"/>
		<movie-search available="no"/>
	</searching>
	<categories>
		<category id="2000" name="Movies">
			<subcat id="2010" name="Foreign"/>
		</category>
		<category id="5000" name="TV"/>
	</categories>
</caps>`

	caps, err := parseTorznabCaps(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("parseTorznabCaps() error = %v", err)
	}

	// Check limits
	if caps.LimitDefault != 50 {
		t.Errorf("LimitDefault = %d, want 50", caps.LimitDefault)
	}
	if caps.LimitMax != 100 {
		t.Errorf("LimitMax = %d, want 100", caps.LimitMax)
	}

	// Check capabilities are still parsed correctly
	expectedCaps := map[string]bool{
		"search":           true,
		"search-q":         true,
		"search-imdbid":    true,
		"tv-search":        true,
		"tv-search-q":      true,
		"tv-search-tvdbid": true,
		"tv-search-season": true,
		"tv-search-ep":     true,
	}

	for _, cap := range caps.Capabilities {
		if !expectedCaps[cap] {
			t.Errorf("unexpected capability: %s", cap)
		}
		delete(expectedCaps, cap)
	}

	for cap := range expectedCaps {
		t.Errorf("missing expected capability: %s", cap)
	}

	// Check categories are still parsed correctly
	if len(caps.Categories) != 3 { // 2 parents + 1 subcat
		t.Errorf("expected 3 categories, got %d", len(caps.Categories))
	}
}
