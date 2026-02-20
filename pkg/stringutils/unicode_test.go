// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeUnicode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Macrons (Japanese romanization)
		{"macron o", "Shōgun", "Shogun"},
		{"macron u", "Shippūden", "Shippuden"},
		{"macron multiple", "Tōkyō", "Tokyo"},
		{"jujutsu kaisen", "Jūjutsu Kaisen", "Jujutsu Kaisen"},

		// Accents (European)
		{"acute e", "Amélie", "Amelie"},
		{"acute e lowercase", "Pokémon", "Pokemon"},
		{"acute multiple", "Léon", "Leon"},
		{"tilde n", "Señorita", "Senorita"},
		{"tilde n el nino", "El Niño", "El Nino"},

		// Umlauts (German/Nordic)
		{"umlaut o", "Björk", "Bjork"},
		{"umlaut u", "Mötley Crüe", "Motley Crue"},
		{"umlaut a", "Händel", "Handel"},

		// Diaeresis
		{"diaeresis a", "Nausicaä", "Nausicaa"},
		{"diaeresis i", "naïve", "naive"},
		{"diaeresis e", "Noël", "Noel"},
		{"diaeresis o", "Zoë", "Zoe"},

		// Ligatures (NFKD decomposes these)
		{"ligature ae", "Encyclopædia", "Encyclopaedia"},
		{"ligature oe", "Cœur", "Coeur"},
		{"ligature fi", "ﬁlm", "film"},
		{"ligature fl", "ﬂower", "flower"},

		// Nordic characters
		{"slashed o", "Ørsted", "Orsted"},
		{"ring a", "Ångström", "Angstrom"},
		{"eszett", "Straße", "Strasse"},
		{"eth", "Iðunn", "Idunn"},
		{"thorn", "Þór", "THor"},

		// Mixed
		{"mixed accents", "Café Amélie", "Cafe Amelie"},
		{"full title", "The Shōgun S01E01 1080p", "The Shogun S01E01 1080p"},

		// No change needed
		{"plain ascii", "Hello World", "Hello World"},
		{"numbers", "2001 A Space Odyssey", "2001 A Space Odyssey"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeUnicode(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeForMatching(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Unicode normalization
		{"macron", "Shōgun S01", "shogun s01"},
		{"accent", "Amélie 2001", "amelie 2001"},
		{"umlaut", "Mötley Crüe The Dirt", "motley crue the dirt"},

		// Apostrophe handling
		{"straight apostrophe", "Bob's Burgers", "bobs burgers"},
		{"curly apostrophe right", "Don't Stop", "dont stop"},
		{"curly apostrophe left", "It's Fine", "its fine"},
		{"backtick", "Rock`n Roll", "rockn roll"},

		// Colon handling
		{"colon", "CSI: Miami", "csi miami"},
		{"colon with space", "City: Downtown", "city downtown"},

		// Hyphen handling
		{"hyphen", "Spider-Man", "spider man"},
		{"multiple hyphens", "X-Men: Days of Future Past", "x men days of future past"},

		// Ampersand handling
		{"ampersand", "His & Hers", "his and hers"},
		{"ampersand no spaces", "Law&Order", "law and order"},
		{"ampersand with spaces", "Will & Grace", "will and grace"},

		// Combined
		{"all punctuation", "Bob's Place: Spider-Man", "bobs place spider man"},
		{"unicode and punctuation", "Shōgun: The Warrior's Way", "shogun the warriors way"},

		// Whitespace
		{"extra spaces", "The   Show", "the show"},
		{"trim whitespace", "  Trimmed  ", "trimmed"},
		{"tabs and spaces", "Hello\t  World", "hello world"},

		// Real-world examples
		{"shogun full", "Shōgun S01E01 1080p DSNP WEB-DL", "shogun s01e01 1080p dsnp web dl"},
		{"pokemon", "Pokémon Journeys S01", "pokemon journeys s01"},
		{"leon", "Léon: The Professional 1994", "leon the professional 1994"},
		{"naruto", "Naruto Shippūden S01E01", "naruto shippuden s01e01"},

		// Edge cases
		{"empty string", "", ""},
		{"only spaces", "   ", ""},
		{"only punctuation", "'-:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeForMatching(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeForMatching_RealWorldPairs(t *testing.T) {
	// Test that real-world release name variations normalize to the same value
	pairs := []struct {
		name   string
		input1 string
		input2 string
	}{
		{
			"shogun macron vs plain",
			"Shōgun S01 1080p",
			"Shogun S01 1080p",
		},
		{
			"bobs burgers apostrophe",
			"Bob's Burgers S01",
			"Bobs Burgers S01",
		},
		{
			"pokemon accent",
			"Pokémon Journeys",
			"Pokemon Journeys",
		},
		{
			"spider-man hyphen",
			"Spider-Man Homecoming",
			"Spider Man Homecoming",
		},
		{
			"csi colon",
			"CSI: Miami S01",
			"CSI Miami S01",
		},
		{
			"leon accent and colon",
			"Léon: The Professional",
			"Leon The Professional",
		},
		{
			"motley crue umlauts",
			"Mötley Crüe The Dirt",
			"Motley Crue The Dirt",
		},
		{
			"naruto macron",
			"Naruto Shippūden",
			"Naruto Shippuden",
		},
		{
			"jujutsu kaisen macrons",
			"Jūjutsu Kaisen S01",
			"Jujutsu Kaisen S01",
		},
		{
			"amelie accent",
			"Amélie 2001",
			"Amelie 2001",
		},
		{
			"el nino tilde",
			"El Niño Documentary",
			"El Nino Documentary",
		},
		{
			"bjork umlaut",
			"Björk Live",
			"Bjork Live",
		},
		{
			"naive diaeresis",
			"Naïve Art",
			"Naive Art",
		},
		{
			"curly vs straight apostrophe",
			"It's Always Sunny",
			"It's Always Sunny",
		},
		{
			"ampersand vs and",
			"His & Hers S01",
			"His and Hers S01",
		},
		{
			"ampersand law and order",
			"Law & Order SVU",
			"Law and Order SVU",
		},
	}

	for _, tt := range pairs {
		t.Run(tt.name, func(t *testing.T) {
			norm1 := NormalizeForMatching(tt.input1)
			norm2 := NormalizeForMatching(tt.input2)
			require.Equal(t, norm1, norm2, "Expected %q and %q to normalize to the same value, got %q and %q",
				tt.input1, tt.input2, norm1, norm2)
		})
	}
}
