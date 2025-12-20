// Copyright (c) 2025, s0up and the autobrr contributors.
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

func TestEvaluateCondition_NumericFields(t *testing.T) {
	tests := []struct {
		name     string
		cond     *RuleCondition
		torrent  qbt.Torrent
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
			name: "ratio between values",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: float64Ptr(1.0),
				MaxValue: float64Ptr(3.0),
			},
			torrent:  qbt.Torrent{Ratio: 2.0},
			expected: true,
		},
		{
			name: "ratio outside between range",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: float64Ptr(1.0),
				MaxValue: float64Ptr(2.0),
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
			result := EvaluateCondition(tt.cond, tt.torrent, 0)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
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
	for i := 0; i < 25; i++ {
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

func float64Ptr(v float64) *float64 {
	return &v
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
				MaxValue: float64Ptr(5.0),
			},
			expected: false,
		},
		{
			name: "between with nil max value",
			cond: &RuleCondition{
				Field:    FieldRatio,
				Operator: OperatorBetween,
				MinValue: float64Ptr(1.0),
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
				MaxValue: float64Ptr(7200),
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
