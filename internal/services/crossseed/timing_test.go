package crossseed

import "testing"

func TestNormalizeSearchTiming(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		interval     int
		cooldown     int
		wantInterval int
		wantCooldown int
	}{
		{
			name:         "clamps interval to minimum",
			interval:     1,
			cooldown:     1,
			wantInterval: minSearchIntervalSecondsTorznab,
			wantCooldown: minSearchCooldownMinutes,
		},
		{
			name:         "preserves higher interval",
			interval:     minSearchIntervalSecondsTorznab + 5,
			cooldown:     minSearchCooldownMinutes + 5,
			wantInterval: minSearchIntervalSecondsTorznab + 5,
			wantCooldown: minSearchCooldownMinutes + 5,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotInterval, gotCooldown := normalizeSearchTiming(tt.interval, tt.cooldown)
			if gotInterval != tt.wantInterval {
				t.Fatalf("interval: got %d want %d", gotInterval, tt.wantInterval)
			}
			if gotCooldown != tt.wantCooldown {
				t.Fatalf("cooldown: got %d want %d", gotCooldown, tt.wantCooldown)
			}
		})
	}
}

func TestNormalizeSearchRunTimingUsesGazelleFloorWhenTorznabDisabled(t *testing.T) {
	t.Parallel()

	gotInterval, gotCooldown := normalizeSearchRunTiming(1, 1, true)

	if gotInterval != minSearchIntervalSecondsGazelleOnly {
		t.Fatalf("interval: got %d want %d", gotInterval, minSearchIntervalSecondsGazelleOnly)
	}
	if gotCooldown != minSearchCooldownMinutes {
		t.Fatalf("cooldown: got %d want %d", gotCooldown, minSearchCooldownMinutes)
	}
}
