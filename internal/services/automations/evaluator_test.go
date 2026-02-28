// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestEvaluateCondition_StringFields(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		expected bool
	}{
		{
			name: "name equals",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorEqual,
				Value:    "Test.Torrent.2024",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "name equals case insensitive",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorEqual,
				Value:    "test.torrent.2024",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "name not equals",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorNotEqual,
				Value:    "Other.Torrent",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "name contains",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContains,
				Value:    "Torrent",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "name not contains",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorNotContains,
				Value:    "Movie",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "name starts with",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorStartsWith,
				Value:    "Test",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "name ends with",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorEndsWith,
				Value:    "2024",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "category equals",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorEqual,
				Value:    "movies",
			},
			torrent:  qbt.Torrent{Category: "movies"},
			expected: true,
		},
		{
			name: "category equals empty (uncategorized)",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorEqual,
				Value:    "",
			},
			torrent:  qbt.Torrent{Category: ""},
			expected: true,
		},
		{
			name: "category equals empty - no match when categorized",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorEqual,
				Value:    "",
			},
			torrent:  qbt.Torrent{Category: "movies"},
			expected: false,
		},
		{
			name: "category not equals empty - match when categorized",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorNotEqual,
				Value:    "",
			},
			torrent:  qbt.Torrent{Category: "movies"},
			expected: true,
		},
		{
			name: "category not equals empty - no match when uncategorized",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorNotEqual,
				Value:    "",
			},
			torrent:  qbt.Torrent{Category: ""},
			expected: false,
		},
		{
			name: "state equals uploading",
			cond: &RuleCondition{
				Field:    FieldState,
				Operator: OperatorEqual,
				Value:    "uploading",
			},
			torrent:  qbt.Torrent{State: qbt.TorrentStateUploading},
			expected: true,
		},
		{
			name: "state equals uploading matches queuedUP bucket",
			cond: &RuleCondition{
				Field:    FieldState,
				Operator: OperatorEqual,
				Value:    "uploading",
			},
			torrent:  qbt.Torrent{State: qbt.TorrentStateQueuedUp},
			expected: true,
		},
		{
			name: "state equals stalledUP",
			cond: &RuleCondition{
				Field:    FieldState,
				Operator: OperatorEqual,
				Value:    "stalledUP",
			},
			torrent:  qbt.Torrent{State: qbt.TorrentStateStalledUp},
			expected: true,
		},
		{
			name: "state equals errored matches error",
			cond: &RuleCondition{
				Field:    FieldState,
				Operator: OperatorEqual,
				Value:    "errored",
			},
			torrent:  qbt.Torrent{State: qbt.TorrentStateError},
			expected: true,
		},
		{
			name: "state equals errored matches missingFiles",
			cond: &RuleCondition{
				Field:    FieldState,
				Operator: OperatorEqual,
				Value:    "errored",
			},
			torrent:  qbt.Torrent{State: qbt.TorrentStateMissingFiles},
			expected: true,
		},
		{
			name: "state equals stopped matches pausedUP",
			cond: &RuleCondition{
				Field:    FieldState,
				Operator: OperatorEqual,
				Value:    "stopped",
			},
			torrent:  qbt.Torrent{State: qbt.TorrentStatePausedUp},
			expected: true,
		},
		{
			name: "regex matches",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorMatches,
				Value:    "^Test.*2024$",
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "regex with regex flag",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorEqual,
				Value:    ".*torrent.*",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "not_contains regex - false when regex matches",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorNotContains,
				Value:    "^Test.*2024$",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: false,
		},
		{
			name: "not_contains regex - true when regex does not match",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorNotContains,
				Value:    "^Movie.*2024$",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "not_equal regex - false when regex matches",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorNotEqual,
				Value:    ".*Torrent.*",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: false,
		},
		{
			name: "not_equal regex - true when regex does not match",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorNotEqual,
				Value:    "^Movie",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "contains regex - true when regex matches",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContains,
				Value:    "Torrent",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: true,
		},
		{
			name: "contains regex - false when regex does not match",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContains,
				Value:    "^Movie",
				Regex:    true,
			},
			torrent:  qbt.Torrent{Name: "Test.Torrent.2024"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, tt.torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_TrackerField_DisplayNameAndNegation(t *testing.T) {
	torrent := qbt.Torrent{Tracker: "https://beyond-hd.me/announce"}
	ctx := &EvalContext{
		TrackerDisplayNameByDomain: map[string]string{
			"beyond-hd.me": "BHD",
		},
	}

	t.Run("equals display name", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorEqual,
			Value:    "BHD",
		}
		if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})

	t.Run("not equals display name", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorNotEqual,
			Value:    "BHD",
		}
		if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); got != false {
			t.Fatalf("expected false, got %v", got)
		}
	})

	t.Run("domain still matches without ctx", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorEqual,
			Value:    "beyond-hd.me",
		}
		if got := EvaluateConditionWithContext(cond, torrent, nil, 0); got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})

	t.Run("display name does not match without ctx", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorEqual,
			Value:    "BHD",
		}
		if got := EvaluateConditionWithContext(cond, torrent, nil, 0); got != false {
			t.Fatalf("expected false, got %v", got)
		}
	})

	t.Run("not_equal regex - false when any candidate matches (display name)", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorNotEqual,
			Value:    "^BHD$",
			Regex:    true,
		}
		if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); got != false {
			t.Fatalf("expected false, got %v", got)
		}
	})

	t.Run("not_contains regex - false when any candidate matches (display name)", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorNotContains,
			Value:    "BHD",
			Regex:    true,
		}
		if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); got != false {
			t.Fatalf("expected false, got %v", got)
		}
	})

	t.Run("not_equal regex - true when no candidate matches", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorNotEqual,
			Value:    "^XYZ$",
			Regex:    true,
		}
		if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})

	t.Run("not_contains regex - true when no candidate matches", func(t *testing.T) {
		cond := &RuleCondition{
			Field:    FieldTracker,
			Operator: OperatorNotContains,
			Value:    "XYZ",
			Regex:    true,
		}
		if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})
}

func TestEvaluateCondition_NumericFields(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		evalCtx  *EvalContext
		expected bool
	}{
		{
			name: "ratio greater than",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorGreaterThan,
				Value:    "1.0",
			},
			torrent:  qbt.Torrent{Ratio: 2.5},
			expected: true,
		},
		{
			name: "ratio greater than or equal",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorGreaterThanOrEqual,
				Value:    "2.0",
			},
			torrent:  qbt.Torrent{Ratio: 2.0},
			expected: true,
		},
		{
			name: "ratio less than",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorLessThan,
				Value:    "1.0",
			},
			torrent:  qbt.Torrent{Ratio: 0.5},
			expected: true,
		},
		{
			name: "progress equals 1.0",
			cond: &RuleCondition{
				Field:    FieldProgress,
				Operator: OperatorEqual,
				Value:    "1",
			},
			torrent:  qbt.Torrent{Progress: 1.0},
			expected: true,
		},
		{
			name: "progress less than 100% (legacy percent value)",
			cond: &RuleCondition{
				Field:    FieldProgress,
				Operator: OperatorLessThan,
				Value:    "100",
			},
			torrent:  qbt.Torrent{Progress: 1.0},
			expected: false,
		},
		{
			name: "progress between 50-100% (legacy percent values)",
			cond: &RuleCondition{
				Field:    FieldProgress,
				Operator: OperatorBetween,
				MinValue: new(float64(50)),
				MaxValue: new(float64(100)),
			},
			torrent:  qbt.Torrent{Progress: 0.6},
			expected: true,
		},
		{
			name: "progress between 50-100% excludes lower progress",
			cond: &RuleCondition{
				Field:    FieldProgress,
				Operator: OperatorBetween,
				MinValue: new(float64(50)),
				MaxValue: new(float64(100)),
			},
			torrent:  qbt.Torrent{Progress: 0.2},
			expected: false,
		},
		{
			name: "seeding time greater than 1 hour",
			cond: &RuleCondition{
				Field:    FieldSeedingTime,
				Operator: OperatorGreaterThan,
				Value:    "3600",
			},
			torrent:  qbt.Torrent{SeedingTime: 7200},
			expected: true,
		},
		{
			name: "size greater than 1GB",
			cond: &RuleCondition{
				Field:    FieldSize,
				Operator: OperatorGreaterThan,
				Value:    "1073741824",
			},
			torrent:  qbt.Torrent{Size: 2147483648},
			expected: true,
		},
		{
			name: "free space greater than 1GB",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorGreaterThan,
				Value:    "1073741824",
			},
			evalCtx: &EvalContext{
				FreeSpace: 2147483648,
			},
			expected: true,
		},
		{
			name: "free space returns false with nil context",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorGreaterThan,
				Value:    "1073741824",
			},
			evalCtx:  nil,
			expected: false,
		},
		{
			name: "ratio between values",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: new(1.0),
				MaxValue: new(3.0),
			},
			torrent:  qbt.Torrent{Ratio: 2.0},
			expected: true,
		},
		{
			name: "ratio outside between range",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: new(1.0),
				MaxValue: new(2.0),
			},
			torrent:  qbt.Torrent{Ratio: 3.0},
			expected: false,
		},
		{
			name: "num seeds greater than",
			cond: &RuleCondition{
				Field:    FieldNumSeeds,
				Operator: OperatorGreaterThan,
				Value:    "5",
			},
			torrent:  qbt.Torrent{NumSeeds: 10},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateConditionWithContext(tt.cond, tt.torrent, tt.evalCtx, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_GroupFields_UseConditionGroupID(t *testing.T) {
	torrent := qbt.Torrent{Hash: "a"}

	defaultIdx := &groupIndex{
		sizeByHash: map[string]int{
			"a": 1,
		},
	}
	releaseIdx := &groupIndex{
		sizeByHash: map[string]int{
			"a": 3,
		},
	}

	ctx := &EvalContext{
		ActiveRuleID:        42,
		activeGroupIndex:    defaultIdx,
		groupIndexCache:     map[int]map[string]*groupIndex{42: {"release_item": releaseIdx}},
		FreeSpaceStates:     nil,
		CategoryIndex:       nil,
		CategoryNames:       nil,
		UnregisteredSet:     nil,
		TrackerDownSet:      nil,
		HardlinkScopeByHash: nil,
	}

	condWithGroupID := &RuleCondition{
		Field:    FieldGroupSize,
		GroupID:  "release_item",
		Operator: OperatorEqual,
		Value:    "3",
	}
	if got := EvaluateConditionWithContext(condWithGroupID, torrent, ctx, 0); !got {
		t.Fatalf("expected grouped condition with explicit groupId to use cached release_item index")
	}

	condCaseInsensitive := &RuleCondition{
		Field:    FieldGroupSize,
		GroupID:  "ReLeAsE_Item",
		Operator: OperatorEqual,
		Value:    "3",
	}
	if got := EvaluateConditionWithContext(condCaseInsensitive, torrent, ctx, 0); !got {
		t.Fatalf("expected groupId lookup to be case-insensitive")
	}

	condFallbackDefault := &RuleCondition{
		Field:    FieldGroupSize,
		Operator: OperatorEqual,
		Value:    "1",
	}
	if got := EvaluateConditionWithContext(condFallbackDefault, torrent, ctx, 0); !got {
		t.Fatalf("expected unscoped grouped condition to use active default group")
	}

	condMissingGroup := &RuleCondition{
		Field:    FieldIsGrouped,
		GroupID:  "does_not_exist",
		Operator: OperatorEqual,
		Value:    "true",
	}
	if got := EvaluateConditionWithContext(condMissingGroup, torrent, ctx, 0); got {
		t.Fatalf("expected false when requested groupId is not available")
	}
}

func TestEvaluateCondition_GroupFields_WorkWithZeroRuleID(t *testing.T) {
	torrent := qbt.Torrent{Hash: "a"}
	ctx := &EvalContext{
		ActiveRuleID:     0,
		groupIndexCache:  map[int]map[string]*groupIndex{0: {"release_item": {sizeByHash: map[string]int{"a": 2}}}},
		activeGroupIndex: nil,
	}

	cond := &RuleCondition{
		Field:    FieldGroupSize,
		GroupID:  "release_item",
		Operator: OperatorEqual,
		Value:    "2",
	}

	if got := EvaluateConditionWithContext(cond, torrent, ctx, 0); !got {
		t.Fatalf("expected grouped condition lookup to work for temporary rule ID 0")
	}
}

func TestEvaluateCondition_BooleanFields(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		expected bool
	}{
		{
			name: "private equals true",
			cond: &RuleCondition{
				Field:    FieldPrivate,
				Operator: OperatorEqual,
				Value:    "true",
			},
			torrent:  qbt.Torrent{Private: true},
			expected: true,
		},
		{
			name: "private equals false",
			cond: &RuleCondition{
				Field:    FieldPrivate,
				Operator: OperatorEqual,
				Value:    "false",
			},
			torrent:  qbt.Torrent{Private: false},
			expected: true,
		},
		{
			name: "private not equals true",
			cond: &RuleCondition{
				Field:    FieldPrivate,
				Operator: OperatorNotEqual,
				Value:    "true",
			},
			torrent:  qbt.Torrent{Private: false},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, tt.torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_Negate(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		expected bool
	}{
		{
			name: "negated equals becomes not equals",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorEqual,
				Value:    "movies",
				Negate:   true,
			},
			torrent:  qbt.Torrent{Category: "tv"},
			expected: true,
		},
		{
			name: "negated greater than",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorGreaterThan,
				Value:    "2.0",
				Negate:   true,
			},
			torrent:  qbt.Torrent{Ratio: 1.5},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, tt.torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_ANDGroup(t *testing.T) {
	torrent := qbt.Torrent{
		Name:        "Test.Movie.2024.1080p.BluRay",
		Category:    "movies",
		Ratio:       2.5,
		SeedingTime: 86400, // 1 day
		State:       qbt.TorrentStateStalledUp,
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		expected bool
	}{
		{
			name: "AND group all match",
			cond: &RuleCondition{
				Operator: OperatorAnd,
				Conditions: []*RuleCondition{
					{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
					{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "2.0"},
				},
			},
			expected: true,
		},
		{
			name: "AND group one fails",
			cond: &RuleCondition{
				Operator: OperatorAnd,
				Conditions: []*RuleCondition{
					{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
					{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "5.0"},
				},
			},
			expected: false,
		},
		{
			name: "AND group with three conditions",
			cond: &RuleCondition{
				Operator: OperatorAnd,
				Conditions: []*RuleCondition{
					{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
					{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "2.0"},
					{Field: FieldSeedingTime, Operator: OperatorGreaterThanOrEqual, Value: "86400"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_ORGroup(t *testing.T) {
	torrent := qbt.Torrent{
		Name:        "Test.Movie.2024.1080p.BluRay",
		Category:    "movies",
		Ratio:       1.5,
		SeedingTime: 3600, // 1 hour
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		expected bool
	}{
		{
			name: "OR group first matches",
			cond: &RuleCondition{
				Operator: OperatorOr,
				Conditions: []*RuleCondition{
					{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "1.0"},
					{Field: FieldSeedingTime, Operator: OperatorGreaterThan, Value: "86400"},
				},
			},
			expected: true,
		},
		{
			name: "OR group second matches",
			cond: &RuleCondition{
				Operator: OperatorOr,
				Conditions: []*RuleCondition{
					{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "5.0"},
					{Field: FieldSeedingTime, Operator: OperatorGreaterThan, Value: "1800"},
				},
			},
			expected: true,
		},
		{
			name: "OR group none match",
			cond: &RuleCondition{
				Operator: OperatorOr,
				Conditions: []*RuleCondition{
					{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "5.0"},
					{Field: FieldSeedingTime, Operator: OperatorGreaterThan, Value: "86400"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_NestedGroups(t *testing.T) {
	torrent := qbt.Torrent{
		Name:        "Test.Movie.2024.1080p.BluRay",
		Category:    "movies",
		Ratio:       2.5,
		SeedingTime: 172800, // 2 days
		State:       qbt.TorrentStateStalledUp,
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		expected bool
	}{
		{
			name: "AND with nested OR - matches",
			cond: &RuleCondition{
				Operator: OperatorAnd,
				Conditions: []*RuleCondition{
					{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
					{
						Operator: OperatorOr,
						Conditions: []*RuleCondition{
							{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "2.0"},
							{Field: FieldSeedingTime, Operator: OperatorGreaterThan, Value: "604800"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "OR with nested AND - matches",
			cond: &RuleCondition{
				Operator: OperatorOr,
				Conditions: []*RuleCondition{
					{
						Operator: OperatorAnd,
						Conditions: []*RuleCondition{
							{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
							{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "2.0"},
						},
					},
					{Field: FieldSeedingTime, Operator: OperatorGreaterThan, Value: "604800"},
				},
			},
			expected: true,
		},
		{
			name: "deeply nested - category AND (ratio > 2 OR (seeding > 1 day AND state = stalledUP))",
			cond: &RuleCondition{
				Operator: OperatorAnd,
				Conditions: []*RuleCondition{
					{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
					{
						Operator: OperatorOr,
						Conditions: []*RuleCondition{
							{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "2.0"},
							{
								Operator: OperatorAnd,
								Conditions: []*RuleCondition{
									{Field: FieldSeedingTime, Operator: OperatorGreaterThan, Value: "86400"},
									{Field: FieldState, Operator: OperatorEqual, Value: "stalledUP"},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_MaxDepth(t *testing.T) {
	// Create a deeply nested condition that exceeds max depth
	cond := &RuleCondition{
		Operator: OperatorAnd,
		Conditions: []*RuleCondition{
			{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"},
		},
	}

	// Build 25 levels of nesting (exceeds maxConditionDepth of 20)
	current := cond
	for range 25 {
		nested := &RuleCondition{
			Operator: OperatorAnd,
			Conditions: []*RuleCondition{
				{Field: FieldRatio, Operator: OperatorGreaterThan, Value: "1.0"},
			},
		}
		current.Conditions = append(current.Conditions, nested)
		current = nested
	}

	torrent := qbt.Torrent{Category: "movies", Ratio: 2.0}

	// Should return false because we hit max depth
	result := EvaluateCondition(cond, torrent, 0)
	if result {
		t.Error("expected false due to max depth, got true")
	}
}

func TestEvaluateCondition_NilCondition(t *testing.T) {
	torrent := qbt.Torrent{Name: "Test"}
	result := EvaluateCondition(nil, torrent, 0)
	if result {
		t.Error("expected false for nil condition")
	}
}

func TestEvaluateCondition_EmptyGroup(t *testing.T) {
	torrent := qbt.Torrent{Name: "Test"}

	// AND group with no conditions should return true (vacuous truth)
	andCond := &RuleCondition{
		Operator:   OperatorAnd,
		Conditions: []*RuleCondition{},
	}
	// Empty conditions means it's not a group, so evaluateLeaf is called with unknown field
	result := EvaluateCondition(andCond, torrent, 0)
	if result {
		t.Error("empty AND group should return false (not a valid group)")
	}
}

func TestEvaluateCondition_StateTrackerDown_WithContext(t *testing.T) {
	cond := &RuleCondition{
		Field:    FieldState,
		Operator: OperatorEqual,
		Value:    "tracker_down",
	}

	torrent := qbt.Torrent{
		Hash:  "hash1",
		State: qbt.TorrentStateUploading,
	}

	t.Run("matches when in TrackerDownSet", func(t *testing.T) {
		ctx := &EvalContext{
			TrackerDownSet: map[string]struct{}{"hash1": {}},
		}
		got := EvaluateConditionWithContext(cond, torrent, ctx, 0)
		if !got {
			t.Fatalf("expected true, got false")
		}
	})

	t.Run("does not match without TrackerDownSet", func(t *testing.T) {
		got := EvaluateConditionWithContext(cond, torrent, &EvalContext{}, 0)
		if got {
			t.Fatalf("expected false, got true")
		}
	})
}

//go:fix inline
func float64Ptr(v float64) *float64 {
	return new(v)
}

func TestEvaluateCondition_ExistsIn(t *testing.T) {
	// Build test torrents for the category index
	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
		{Hash: "hash2", Name: "Test.Show.S01E01.1080p", Category: "imported-tv"},
		{Hash: "hash3", Name: "Other.Show.S01E01.720p", Category: "imported-tv"},
		{Hash: "hash4", Name: "Movie.2024.BluRay", Category: "movies"},
		{Hash: "hash5", Name: "Uncategorized.File", Category: ""},
	}

	// Build the category index
	categoryIndex, categoryNames := BuildCategoryIndex(torrents)
	evalCtx := &EvalContext{
		CategoryIndex: categoryIndex,
		CategoryNames: categoryNames,
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		expected bool
	}{
		{
			name: "EXISTS_IN - exact match found in different category",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "imported-tv",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: true, // hash2 has the same name in imported-tv
		},
		{
			name: "EXISTS_IN - no match in target category",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "movies",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: false, // No torrent with this name in movies
		},
		{
			name: "EXISTS_IN - case insensitive matching",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "IMPORTED-TV",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "test.show.s01e01.1080p", Category: "tv"},
			expected: true, // Should match case-insensitively
		},
		{
			name: "EXISTS_IN - self-exclusion (same hash)",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "imported-tv",
			},
			torrent:  qbt.Torrent{Hash: "hash2", Name: "Test.Show.S01E01.1080p", Category: "imported-tv"},
			expected: false, // Only hash2 has this name in imported-tv, and it's the same torrent
		},
		{
			name: "EXISTS_IN - missing category returns false",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "nonexistent",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: false,
		},
		{
			name: "EXISTS_IN - empty category (uncategorized torrents)",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Uncategorized.File", Category: "tv"},
			expected: true, // hash5 has the same name with empty category
		},
		{
			name: "EXISTS_IN - whitespace-only category treated as no match",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "   ",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: false,
		},
		{
			name: "EXISTS_IN - with negation",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "imported-tv",
				Negate:   true,
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: false, // Negated: name DOES exist, so negated result is false
		},
		{
			name: "EXISTS_IN - only works with NAME field",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorExistsIn,
				Value:    "imported-tv",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: false, // EXISTS_IN only valid for NAME field
		},
		{
			name: "EXISTS_IN - regex flag ignored",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorExistsIn,
				Value:    "imported-tv",
				Regex:    true, // Should be ignored
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p", Category: "tv"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateConditionWithContext(tt.cond, tt.torrent, evalCtx, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_ContainsIn(t *testing.T) {
	// Build test torrents for the category index
	// Note: CONTAINS_IN requires names >= 10 chars normalized
	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Test.Show.S01E01.1080p.BluRay", Category: "tv"},
		{Hash: "hash2", Name: "Test.Show.S01E01.1080p", Category: "imported-tv"},
		{Hash: "hash3", Name: "Test.Show.S01E01.1080p.WEB-DL", Category: "imported-tv"},
		{Hash: "hash4", Name: "Short", Category: "movies"}, // Too short for CONTAINS_IN
		{Hash: "hash5", Name: "Another.Long.Enough.Name", Category: "movies"},
	}

	// Build the category index
	categoryIndex, categoryNames := BuildCategoryIndex(torrents)
	evalCtx := &EvalContext{
		CategoryIndex: categoryIndex,
		CategoryNames: categoryNames,
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		expected bool
	}{
		{
			name: "CONTAINS_IN - partial match found (current contains target)",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "imported-tv",
			},
			// "test show s01e01 1080p bluray" contains "test show s01e01 1080p"
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p.BluRay", Category: "tv"},
			expected: true,
		},
		{
			name: "CONTAINS_IN - partial match found (target contains current)",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "tv",
			},
			// hash1 has "test show s01e01 1080p bluray" which contains "test show s01e01 1080p"
			torrent:  qbt.Torrent{Hash: "hash2", Name: "Test.Show.S01E01.1080p", Category: "imported-tv"},
			expected: true,
		},
		{
			name: "CONTAINS_IN - self-exclusion",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "imported-tv",
			},
			torrent:  qbt.Torrent{Hash: "hash2", Name: "Test.Show.S01E01.1080p", Category: "imported-tv"},
			expected: true, // hash3 also has a similar name
		},
		{
			name: "CONTAINS_IN - short name skipped (current < 10 chars normalized)",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "movies",
			},
			torrent:  qbt.Torrent{Hash: "hashX", Name: "Tiny", Category: "tv"},
			expected: false, // "tiny" is too short
		},
		{
			name: "CONTAINS_IN - short target names skipped",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "movies",
			},
			// "Short.Movie.Extended.Cut.2024" contains "short" but "Short" in movies is too short (<10 chars)
			// so it's skipped and no match is found
			torrent:  qbt.Torrent{Hash: "hashX", Name: "Short.Movie.Extended.Cut.2024", Category: "tv"},
			expected: false, // Would match if short names weren't skipped
		},
		{
			name: "CONTAINS_IN - no match",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "movies",
			},
			torrent:  qbt.Torrent{Hash: "hashX", Name: "Completely.Different.Release", Category: "tv"},
			expected: false,
		},
		{
			name: "CONTAINS_IN - with negation",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "imported-tv",
				Negate:   true,
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p.BluRay", Category: "tv"},
			expected: false, // Match found, negated = false
		},
		{
			name: "CONTAINS_IN - only works with NAME field",
			cond: &RuleCondition{
				Field:    FieldCategory,
				Operator: OperatorContainsIn,
				Value:    "imported-tv",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p.BluRay", Category: "tv"},
			expected: false,
		},
		{
			name: "CONTAINS_IN - missing category returns false",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorContainsIn,
				Value:    "nonexistent",
			},
			torrent:  qbt.Torrent{Hash: "hash1", Name: "Test.Show.S01E01.1080p.BluRay", Category: "tv"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateConditionWithContext(tt.cond, tt.torrent, evalCtx, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBuildCategoryIndex(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "hash1", Name: "Test.Torrent.A", Category: "movies"},
		{Hash: "hash2", Name: "Test.Torrent.A", Category: "movies"}, // Same name, different hash
		{Hash: "hash3", Name: "Test.Torrent.B", Category: "MOVIES"}, // Different case category
		{Hash: "hash4", Name: "Uncategorized", Category: ""},        // Empty category
	}

	categoryIndex, categoryNames := BuildCategoryIndex(torrents)

	// Test CategoryIndex structure
	if categoryIndex == nil {
		t.Fatal("CategoryIndex should not be nil")
	}

	// Check that "movies" and "MOVIES" are normalized to same key
	moviesNames, ok := categoryIndex["movies"]
	if !ok {
		t.Error("CategoryIndex should have 'movies' key")
	}

	// Should have two distinct names under movies
	if len(moviesNames) != 2 {
		t.Errorf("expected 2 names under movies, got %d", len(moviesNames))
	}

	// "test.torrent.a" should have two hashes
	nameHashSet, ok := moviesNames["test.torrent.a"]
	if !ok {
		t.Error("CategoryIndex[movies] should have 'test.torrent.a'")
	}
	if len(nameHashSet) != 2 {
		t.Errorf("expected 2 hashes for test.torrent.a, got %d", len(nameHashSet))
	}

	// Test empty category
	emptyNames, ok := categoryIndex[""]
	if !ok {
		t.Error("CategoryIndex should have empty string key for uncategorized")
	}
	if len(emptyNames) != 1 {
		t.Errorf("expected 1 name under empty category, got %d", len(emptyNames))
	}

	// Test CategoryNames structure
	if categoryNames == nil {
		t.Fatal("CategoryNames should not be nil")
	}

	moviesEntries := categoryNames["movies"]
	if len(moviesEntries) != 3 {
		t.Errorf("expected 3 entries in movies CategoryNames, got %d", len(moviesEntries))
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Test.Torrent.2024", "test torrent 2024"},
		{"Test_Torrent_2024", "test torrent 2024"},
		{"Test-Torrent-2024", "test torrent 2024"},
		{"Test.Torrent_2024-Release", "test torrent 2024 release"},
		{"  Test  Torrent  ", "test torrent"},
		{"UPPERCASE.NAME", "uppercase name"},
		{"already normal", "already normal"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEvaluateCondition_ErrorCases(t *testing.T) {
	torrent := qbt.Torrent{
		Name:        "Test.Torrent",
		Size:        1073741824, // 1 GiB
		Ratio:       2.0,
		SeedingTime: 3600,
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		expected bool
	}{
		{
			name: "invalid regex pattern",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorMatches,
				Value:    "[invalid(",
			},
			expected: false,
		},
		{
			name: "invalid regex with regex flag",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorEqual,
				Value:    "[unclosed",
				Regex:    true,
			},
			expected: false,
		},
		{
			name: "non-numeric value for int64 field",
			cond: &RuleCondition{
				Field:    FieldSize,
				Operator: OperatorGreaterThan,
				Value:    "10GB",
			},
			expected: false,
		},
		{
			name: "non-numeric value for float64 field",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorGreaterThan,
				Value:    "high",
			},
			expected: false,
		},
		{
			name: "between with nil min value",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: nil,
				MaxValue: new(5.0),
			},
			expected: false,
		},
		{
			name: "between with nil max value",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: new(1.0),
				MaxValue: nil,
			},
			expected: false,
		},
		{
			name: "between with both nil values",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: nil,
				MaxValue: nil,
			},
			expected: false,
		},
		{
			name: "int64 between with nil min",
			cond: &RuleCondition{
				Field:    FieldSeedingTime,
				Operator: OperatorBetween,
				MinValue: nil,
				MaxValue: new(float64(7200)),
			},
			expected: false,
		},
		{
			name: "unknown field",
			cond: &RuleCondition{
				Field:    "UNKNOWN_FIELD",
				Operator: OperatorEqual,
				Value:    "test",
			},
			expected: false,
		},
		{
			name: "unsupported operator for string field",
			cond: &RuleCondition{
				Field:    FieldName,
				Operator: OperatorGreaterThan,
				Value:    "test",
			},
			expected: false,
		},
		{
			name: "unsupported operator for bool field",
			cond: &RuleCondition{
				Field:    FieldPrivate,
				Operator: OperatorContains,
				Value:    "true",
			},
			expected: false,
		},
		{
			name: "empty value parses as zero for numeric comparison",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorGreaterThan,
				Value:    "",
			},
			expected: true, // 2.0 > 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_AgeFields(t *testing.T) {
	// Fixed "now" for deterministic tests: 2024-01-15 12:00:00 UTC
	nowUnix := int64(1705320000)

	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		ctx      *EvalContext
		expected bool
	}{
		// ADDED_ON_AGE tests
		{
			name: "added_on_age less than 1 hour - matches",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorLessThan,
				Value:    "3600", // 1 hour in seconds
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix - 1800}, // added 30 minutes ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "added_on_age less than 1 hour - does not match",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorLessThan,
				Value:    "3600", // 1 hour in seconds
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix - 7200}, // added 2 hours ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name: "added_on_age greater than 1 day - matches",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorGreaterThan,
				Value:    "86400", // 1 day in seconds
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix - 172800}, // added 2 days ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "added_on_age between 1 hour and 2 hours - matches",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorBetween,
				MinValue: new(float64(3600)), // 1 hour
				MaxValue: new(float64(7200)), // 2 hours
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix - 5400}, // added 1.5 hours ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "added_on_age between 1 hour and 2 hours - outside range",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorBetween,
				MinValue: new(float64(3600)), // 1 hour
				MaxValue: new(float64(7200)), // 2 hours
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix - 10800}, // added 3 hours ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name: "added_on_age unset (0) - does not match",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorGreaterThan,
				Value:    "0",
			},
			torrent:  qbt.Torrent{AddedOn: 0},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},

		// COMPLETION_ON_AGE tests
		{
			name: "completion_on_age less than 1 hour - matches",
			cond: &RuleCondition{
				Field:    FieldCompletionOnAge,
				Operator: OperatorLessThan,
				Value:    "3600", // 1 hour
			},
			torrent:  qbt.Torrent{CompletionOn: nowUnix - 1800}, // completed 30 min ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "completion_on_age greater than 1 day - matches",
			cond: &RuleCondition{
				Field:    FieldCompletionOnAge,
				Operator: OperatorGreaterThan,
				Value:    "86400", // 1 day
			},
			torrent:  qbt.Torrent{CompletionOn: nowUnix - 172800}, // completed 2 days ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "completion_on_age unset (0) - does not match",
			cond: &RuleCondition{
				Field:    FieldCompletionOnAge,
				Operator: OperatorGreaterThan,
				Value:    "0", // any age
			},
			torrent:  qbt.Torrent{CompletionOn: 0}, // never completed
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name: "completion_on_age unset (-1 qBittorrent) - does not match",
			cond: &RuleCondition{
				Field:    FieldCompletionOnAge,
				Operator: OperatorGreaterThanOrEqual,
				Value:    "86400", // 1 day
			},
			torrent:  qbt.Torrent{CompletionOn: -1}, // qBittorrent uses -1 for incomplete torrents
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name: "completion_on_age between - matches",
			cond: &RuleCondition{
				Field:    FieldCompletionOnAge,
				Operator: OperatorBetween,
				MinValue: new(float64(3600)),
				MaxValue: new(float64(7200)),
			},
			torrent:  qbt.Torrent{CompletionOn: nowUnix - 5400}, // completed 1.5 hours ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},

		// LAST_ACTIVITY_AGE tests
		{
			name: "last_activity_age less than 1 hour - matches",
			cond: &RuleCondition{
				Field:    FieldLastActivityAge,
				Operator: OperatorLessThan,
				Value:    "3600", // 1 hour
			},
			torrent:  qbt.Torrent{LastActivity: nowUnix - 1800}, // active 30 min ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "last_activity_age greater than 1 day - matches",
			cond: &RuleCondition{
				Field:    FieldLastActivityAge,
				Operator: OperatorGreaterThan,
				Value:    "86400", // 1 day
			},
			torrent:  qbt.Torrent{LastActivity: nowUnix - 172800}, // active 2 days ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name: "last_activity_age unset (0) - does not match",
			cond: &RuleCondition{
				Field:    FieldLastActivityAge,
				Operator: OperatorGreaterThan,
				Value:    "0", // any age
			},
			torrent:  qbt.Torrent{LastActivity: 0}, // never had activity
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name: "last_activity_age between - matches",
			cond: &RuleCondition{
				Field:    FieldLastActivityAge,
				Operator: OperatorBetween,
				MinValue: new(float64(3600)),
				MaxValue: new(float64(7200)),
			},
			torrent:  qbt.Torrent{LastActivity: nowUnix - 5400}, // active 1.5 hours ago
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},

		// Clock skew handling (negative age clamped to 0)
		{
			name: "added_on_age with future timestamp (clock skew) - clamped to 0",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorEqual,
				Value:    "0",
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix + 3600}, // timestamp in the future
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true, // age clamped to 0
		},
		{
			name: "added_on_age with future timestamp - greater than fails",
			cond: &RuleCondition{
				Field:    FieldAddedOnAge,
				Operator: OperatorGreaterThan,
				Value:    "0",
			},
			torrent:  qbt.Torrent{AddedOn: nowUnix + 3600}, // timestamp in the future
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false, // clamped to 0, so not > 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateConditionWithContext(tt.cond, tt.torrent, tt.ctx, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_HardlinkScope(t *testing.T) {
	torrent := qbt.Torrent{
		Hash: "abc123",
		Name: "Test.Torrent",
	}

	tests := []struct {
		name     string
		cond     *RuleCondition
		evalCtx  *EvalContext
		expected bool
	}{
		{
			name: "scope is none - match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeNone,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeNone},
			},
			expected: true,
		},
		{
			name: "scope is none - no match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeNone,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeTorrentsOnly},
			},
			expected: false,
		},
		{
			name: "scope is torrents_only - match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeTorrentsOnly,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeTorrentsOnly},
			},
			expected: true,
		},
		{
			name: "scope is outside_qbittorrent - match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeOutsideQBitTorrent,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeOutsideQBitTorrent},
			},
			expected: true,
		},
		{
			name: "scope is not outside_qbittorrent - match (none)",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorNotEqual,
				Value:    HardlinkScopeOutsideQBitTorrent,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeNone},
			},
			expected: true,
		},
		{
			name: "scope is not outside_qbittorrent - match (torrents_only)",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorNotEqual,
				Value:    HardlinkScopeOutsideQBitTorrent,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeTorrentsOnly},
			},
			expected: true,
		},
		{
			name: "scope is not outside_qbittorrent - no match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorNotEqual,
				Value:    HardlinkScopeOutsideQBitTorrent,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeOutsideQBitTorrent},
			},
			expected: false,
		},
		{
			name: "unknown scope (not in map) - never matches",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeNone,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{}, // torrent not in map
			},
			expected: false, // Unknown scope should not match any condition
		},
		{
			name: "unknown scope (not in map) - NOT_EQUAL also fails",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorNotEqual,
				Value:    HardlinkScopeOutsideQBitTorrent,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{}, // torrent not in map
			},
			expected: false, // Unknown scope should not match any condition
		},
		{
			name: "nil context - no match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeNone,
			},
			evalCtx:  nil,
			expected: false,
		},
		{
			name: "no local access - no match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeNone,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: false,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeNone},
			},
			expected: false,
		},
		{
			name: "nil HardlinkScopeByHash - no match",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    HardlinkScopeNone,
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    nil,
			},
			expected: false,
		},
		{
			name: "case insensitive value matching",
			cond: &RuleCondition{
				Field:    FieldHardlinkScope,
				Operator: OperatorEqual,
				Value:    "OUTSIDE_QBITTORRENT", // uppercase
			},
			evalCtx: &EvalContext{
				InstanceHasLocalAccess: true,
				HardlinkScopeByHash:    map[string]string{"abc123": HardlinkScopeOutsideQBitTorrent},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateConditionWithContext(tt.cond, torrent, tt.evalCtx, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_FreeSpaceWithSpaceToClear(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		evalCtx  *EvalContext
		expected bool
	}{
		{
			name: "free space condition considers SpaceToClear - now above threshold",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorLessThan,
				Value:    "500000000000", // 500GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    400000000000, // 400GB actual
				SpaceToClear: 150000000000, // 150GB to be cleared
				// Effective: 550GB > 500GB, so condition (< 500GB) is false
			},
			expected: false,
		},
		{
			name: "free space condition considers SpaceToClear - still below threshold",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorLessThan,
				Value:    "500000000000", // 500GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    400000000000, // 400GB actual
				SpaceToClear: 50000000000,  // 50GB to be cleared
				// Effective: 450GB < 500GB, so condition is true
			},
			expected: true,
		},
		{
			name: "free space with zero SpaceToClear",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorLessThan,
				Value:    "500000000000", // 500GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    400000000000, // 400GB actual
				SpaceToClear: 0,
			},
			expected: true, // 400GB < 500GB
		},
		{
			name: "free space greater than with SpaceToClear",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorGreaterThan,
				Value:    "500000000000", // 500GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    400000000000, // 400GB actual
				SpaceToClear: 200000000000, // 200GB to be cleared
				// Effective: 600GB > 500GB
			},
			expected: true,
		},
		{
			name: "free space equal with SpaceToClear",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorEqual,
				Value:    "500000000000", // 500GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    300000000000, // 300GB actual
				SpaceToClear: 200000000000, // 200GB to be cleared
				// Effective: 500GB == 500GB
			},
			expected: true,
		},
		{
			name: "free space between with SpaceToClear",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorBetween,
				MinValue: new(float64(400000000000)), // 400GB
				MaxValue: new(float64(600000000000)), // 600GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    300000000000, // 300GB actual
				SpaceToClear: 200000000000, // 200GB to be cleared
				// Effective: 500GB, within [400GB, 600GB]
			},
			expected: true,
		},
		{
			name: "free space between with SpaceToClear - outside range",
			cond: &RuleCondition{
				Field:    FieldFreeSpace,
				Operator: OperatorBetween,
				MinValue: new(float64(400000000000)), // 400GB
				MaxValue: new(float64(500000000000)), // 500GB
			},
			evalCtx: &EvalContext{
				FreeSpace:    300000000000, // 300GB actual
				SpaceToClear: 300000000000, // 300GB to be cleared
				// Effective: 600GB, outside [400GB, 500GB]
			},
			expected: false,
		},
	}

	torrent := qbt.Torrent{Name: "Test"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateConditionWithContext(tt.cond, torrent, tt.evalCtx, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_Tags(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		expected bool
	}{
		// EQUAL operator - tag-aware
		{
			name: "tags equals - single tag match in list",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: true,
		},
		{
			name: "tags equals - case insensitive",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "NOHL",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: true,
		},
		{
			name: "tags equals - no match",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "missing",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: false,
		},
		{
			name: "tags equals - partial tag name does not match",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "cross",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: false,
		},
		{
			name: "tags equals - only tag",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "sonarr",
			},
			torrent:  qbt.Torrent{Tags: "sonarr"},
			expected: true,
		},
		{
			name: "tags equals - empty tags",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: ""},
			expected: false,
		},
		{
			name: "tags equals - whitespace only tags",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: "   "},
			expected: false,
		},
		{
			name: "tags equals - tag with spaces",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "my tag",
			},
			torrent:  qbt.Torrent{Tags: "other, my tag, another"},
			expected: true,
		},
		{
			name: "tags equals - empty condition value",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "",
			},
			torrent:  qbt.Torrent{Tags: "noHL"},
			expected: false,
		},
		{
			name: "tags equals - whitespace condition value",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "   ",
			},
			torrent:  qbt.Torrent{Tags: "noHL"},
			expected: false,
		},
		{
			name: "tags equals - tag with leading/trailing spaces trimmed",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: "  noHL  , other"},
			expected: true,
		},

		// NOT_EQUAL operator - tag-aware
		{
			name: "tags not equals - tag not present",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorNotEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, racing"},
			expected: true,
		},
		{
			name: "tags not equals - tag present",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorNotEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: false,
		},
		{
			name: "tags not equals - empty tags",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorNotEqual,
				Value:    "noHL",
			},
			torrent:  qbt.Torrent{Tags: ""},
			expected: true,
		},
		{
			name: "tags not equals - case insensitive",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorNotEqual,
				Value:    "NOHL",
			},
			torrent:  qbt.Torrent{Tags: "noHL"},
			expected: false,
		},

		// CONTAINS operator - tag-aware (any tag contains substring)
		{
			name: "tags contains - any tag contains substring",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorContains,
				Value:    "seed",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: true,
		},
		{
			name: "tags contains - no tag contains substring",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorContains,
				Value:    "missing",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: false,
		},
		{
			name: "tags contains - case insensitive",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorContains,
				Value:    "SEED",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: true,
		},

		// NOT_CONTAINS operator - tag-aware (no tag contains substring)
		{
			name: "tags not contains - no tag contains substring",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorNotContains,
				Value:    "missing",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: true,
		},
		{
			name: "tags not contains - some tag contains substring",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorNotContains,
				Value:    "seed",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: false,
		},

		// STARTS_WITH operator - tag-aware (any tag starts with)
		{
			name: "tags starts with - any tag starts with value",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorStartsWith,
				Value:    "cross",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: true,
		},
		{
			name: "tags starts with - no tag starts with value",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorStartsWith,
				Value:    "seed",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: false,
		},
		{
			name: "tags starts with - case insensitive",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorStartsWith,
				Value:    "CROSS",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: true,
		},

		// ENDS_WITH operator - tag-aware (any tag ends with)
		{
			name: "tags ends with - any tag ends with value",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEndsWith,
				Value:    "seed",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: true,
		},
		{
			name: "tags ends with - no tag ends with value",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEndsWith,
				Value:    "cross",
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL"},
			expected: false,
		},

		// MATCHES (regex) operator - operates on full string
		{
			name: "tags regex - word boundary match",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorMatches,
				Value:    `\bnoHL\b`,
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: true,
		},
		{
			name: "tags regex - full string anchored no match",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorMatches,
				Value:    `^noHL$`,
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: false,
		},
		{
			name: "tags regex flag - operates on full string",
			cond: &RuleCondition{
				Field:    FieldTags,
				Operator: OperatorEqual,
				Value:    `.*noHL.*`,
				Regex:    true,
			},
			torrent:  qbt.Torrent{Tags: "cross-seed, noHL, racing"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateCondition(tt.cond, tt.torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluateCondition_GoQBitTorrentAdditionalFields(t *testing.T) {
	nowUnix := int64(1705320000)

	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
		ctx      *EvalContext
		expected bool
	}{
		{
			name:     "infohash v1",
			cond:     &RuleCondition{Field: FieldInfohashV1, Operator: OperatorEqual, Value: "abc123"},
			torrent:  qbt.Torrent{InfohashV1: "abc123"},
			expected: true,
		},
		{
			name:     "infohash v2",
			cond:     &RuleCondition{Field: FieldInfohashV2, Operator: OperatorEqual, Value: "def456"},
			torrent:  qbt.Torrent{InfohashV2: "def456"},
			expected: true,
		},
		{
			name:     "magnet uri contains",
			cond:     &RuleCondition{Field: FieldMagnetURI, Operator: OperatorContains, Value: "btih"},
			torrent:  qbt.Torrent{MagnetURI: "magnet:?xt=urn:btih:abc123"},
			expected: true,
		},
		{
			name:     "download path",
			cond:     &RuleCondition{Field: FieldDownloadPath, Operator: OperatorEqual, Value: "/data/downloading"},
			torrent:  qbt.Torrent{DownloadPath: "/data/downloading"},
			expected: true,
		},
		{
			name:     "created by",
			cond:     &RuleCondition{Field: FieldCreatedBy, Operator: OperatorEqual, Value: "mktorrent"},
			torrent:  qbt.Torrent{CreatedBy: "mktorrent"},
			expected: true,
		},
		{
			name: "trackers list contains domain",
			cond: &RuleCondition{Field: FieldTrackers, Operator: OperatorContains, Value: "trackerb.org"},
			torrent: qbt.Torrent{
				Trackers: []qbt.TorrentTracker{
					{Url: "https://trackera.org/announce"},
					{Url: "udp://trackerb.org:1337/announce"},
				},
			},
			expected: true,
		},
		{
			name: "trackers list matches customization display name",
			cond: &RuleCondition{Field: FieldTrackers, Operator: OperatorEqual, Value: "BHD"},
			torrent: qbt.Torrent{
				Trackers: []qbt.TorrentTracker{
					{Url: "https://beyond-hd.me/announce"},
				},
			},
			ctx: &EvalContext{
				TrackerDisplayNameByDomain: map[string]string{
					"beyond-hd.me": "BHD",
				},
			},
			expected: true,
		},
		{
			name:     "completed bytes",
			cond:     &RuleCondition{Field: FieldCompleted, Operator: OperatorEqual, Value: "1024"},
			torrent:  qbt.Torrent{Completed: 1024},
			expected: true,
		},
		{
			name:     "downloaded session bytes",
			cond:     &RuleCondition{Field: FieldDownloadedSession, Operator: OperatorEqual, Value: "2048"},
			torrent:  qbt.Torrent{DownloadedSession: 2048},
			expected: true,
		},
		{
			name:     "uploaded session bytes",
			cond:     &RuleCondition{Field: FieldUploadedSession, Operator: OperatorEqual, Value: "3072"},
			torrent:  qbt.Torrent{UploadedSession: 3072},
			expected: true,
		},
		{
			name:     "added on timestamp evaluated as age duration",
			cond:     &RuleCondition{Field: FieldAddedOn, Operator: OperatorGreaterThan, Value: "3600"},
			torrent:  qbt.Torrent{AddedOn: nowUnix - 7200},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name:     "completion on timestamp evaluated as age duration",
			cond:     &RuleCondition{Field: FieldCompletionOn, Operator: OperatorLessThan, Value: "3600"},
			torrent:  qbt.Torrent{CompletionOn: nowUnix - 1800},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name:     "completion on unset does not match",
			cond:     &RuleCondition{Field: FieldCompletionOn, Operator: OperatorGreaterThan, Value: "0"},
			torrent:  qbt.Torrent{CompletionOn: 0},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name:     "last activity timestamp evaluated as age duration",
			cond:     &RuleCondition{Field: FieldLastActivity, Operator: OperatorGreaterThanOrEqual, Value: "3600"},
			torrent:  qbt.Torrent{LastActivity: nowUnix - 3600},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name:     "last activity unset does not match",
			cond:     &RuleCondition{Field: FieldLastActivity, Operator: OperatorGreaterThan, Value: "0"},
			torrent:  qbt.Torrent{LastActivity: 0},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name:     "seen complete timestamp evaluated as age duration",
			cond:     &RuleCondition{Field: FieldSeenComplete, Operator: OperatorBetween, MinValue: new(float64(3600)), MaxValue: new(float64(7200))},
			torrent:  qbt.Torrent{SeenComplete: nowUnix - 5400},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: true,
		},
		{
			name:     "seen complete unset does not match",
			cond:     &RuleCondition{Field: FieldSeenComplete, Operator: OperatorGreaterThan, Value: "0"},
			torrent:  qbt.Torrent{SeenComplete: 0},
			ctx:      &EvalContext{NowUnix: nowUnix},
			expected: false,
		},
		{
			name:     "eta duration",
			cond:     &RuleCondition{Field: FieldETA, Operator: OperatorEqual, Value: "600"},
			torrent:  qbt.Torrent{ETA: 600},
			expected: true,
		},
		{
			name:     "reannounce duration",
			cond:     &RuleCondition{Field: FieldReannounce, Operator: OperatorEqual, Value: "1200"},
			torrent:  qbt.Torrent{Reannounce: 1200},
			expected: true,
		},
		{
			name:     "max seeding time",
			cond:     &RuleCondition{Field: FieldMaxSeedingTime, Operator: OperatorEqual, Value: "3600"},
			torrent:  qbt.Torrent{MaxSeedingTime: 3600},
			expected: true,
		},
		{
			name:     "max inactive seeding time",
			cond:     &RuleCondition{Field: FieldMaxInactiveSeedingTime, Operator: OperatorEqual, Value: "7200"},
			torrent:  qbt.Torrent{MaxInactiveSeedingTime: 7200},
			expected: true,
		},
		{
			name:     "seeding time limit",
			cond:     &RuleCondition{Field: FieldSeedingTimeLimit, Operator: OperatorEqual, Value: "1800"},
			torrent:  qbt.Torrent{SeedingTimeLimit: 1800},
			expected: true,
		},
		{
			name:     "inactive seeding time limit",
			cond:     &RuleCondition{Field: FieldInactiveSeedingTimeLimit, Operator: OperatorEqual, Value: "900"},
			torrent:  qbt.Torrent{InactiveSeedingTimeLimit: 900},
			expected: true,
		},
		{
			name:     "ratio limit",
			cond:     &RuleCondition{Field: FieldRatioLimit, Operator: OperatorEqual, Value: "2.5"},
			torrent:  qbt.Torrent{RatioLimit: 2.5},
			expected: true,
		},
		{
			name:     "max ratio",
			cond:     &RuleCondition{Field: FieldMaxRatio, Operator: OperatorEqual, Value: "5.0"},
			torrent:  qbt.Torrent{MaxRatio: 5.0},
			expected: true,
		},
		{
			name:     "popularity",
			cond:     &RuleCondition{Field: FieldPopularity, Operator: OperatorEqual, Value: "0.75"},
			torrent:  qbt.Torrent{Popularity: 0.75},
			expected: true,
		},
		{
			name:     "download limit",
			cond:     &RuleCondition{Field: FieldDlLimit, Operator: OperatorEqual, Value: "1048576"},
			torrent:  qbt.Torrent{DlLimit: 1048576},
			expected: true,
		},
		{
			name:     "upload limit",
			cond:     &RuleCondition{Field: FieldUpLimit, Operator: OperatorEqual, Value: "524288"},
			torrent:  qbt.Torrent{UpLimit: 524288},
			expected: true,
		},
		{
			name:     "priority",
			cond:     &RuleCondition{Field: FieldPriority, Operator: OperatorEqual, Value: "3"},
			torrent:  qbt.Torrent{Priority: 3},
			expected: true,
		},
		{
			name:     "auto managed",
			cond:     &RuleCondition{Field: FieldAutoManaged, Operator: OperatorEqual, Value: "true"},
			torrent:  qbt.Torrent{AutoManaged: true},
			expected: true,
		},
		{
			name:     "first last piece priority",
			cond:     &RuleCondition{Field: FieldFirstLastPiecePrio, Operator: OperatorEqual, Value: "true"},
			torrent:  qbt.Torrent{FirstLastPiecePrio: true},
			expected: true,
		},
		{
			name:     "force start",
			cond:     &RuleCondition{Field: FieldForceStart, Operator: OperatorEqual, Value: "true"},
			torrent:  qbt.Torrent{ForceStart: true},
			expected: true,
		},
		{
			name:     "sequential download",
			cond:     &RuleCondition{Field: FieldSequentialDownload, Operator: OperatorEqual, Value: "true"},
			torrent:  qbt.Torrent{SequentialDownload: true},
			expected: true,
		},
		{
			name:     "super seeding",
			cond:     &RuleCondition{Field: FieldSuperSeeding, Operator: OperatorEqual, Value: "true"},
			torrent:  qbt.Torrent{SuperSeeding: true},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateConditionWithContext(tt.cond, tt.torrent, tt.ctx, 0)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
