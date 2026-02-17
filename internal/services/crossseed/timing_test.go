package crossseed

import "testing"

func TestNormalizeSearchRunTiming(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		interval       int
		cooldown       int
		disableTorznab bool
		wantInterval   int
		wantCooldown   int
	}{
		{
			name:           "torznab clamps interval",
			interval:       1,
			cooldown:       1,
			disableTorznab: false,
			wantInterval:   minSearchIntervalSecondsTorznab,
			wantCooldown:   minSearchCooldownMinutes,
		},
		{
			name:           "gazelle-only clamps interval",
			interval:       1,
			cooldown:       1,
			disableTorznab: true,
			wantInterval:   minSearchIntervalSecondsGazelleOnly,
			wantCooldown:   minSearchCooldownMinutes,
		},
		{
			name:           "gazelle-only preserves higher interval",
			interval:       minSearchIntervalSecondsGazelleOnly + 5,
			cooldown:       minSearchCooldownMinutes + 5,
			disableTorznab: true,
			wantInterval:   minSearchIntervalSecondsGazelleOnly + 5,
			wantCooldown:   minSearchCooldownMinutes + 5,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotInterval, gotCooldown := normalizeSearchRunTiming(tt.interval, tt.cooldown, tt.disableTorznab)
			if gotInterval != tt.wantInterval {
				t.Fatalf("interval: got %d want %d", gotInterval, tt.wantInterval)
			}
			if gotCooldown != tt.wantCooldown {
				t.Fatalf("cooldown: got %d want %d", gotCooldown, tt.wantCooldown)
			}
		})
	}
}
