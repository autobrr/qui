// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// WarningResponse represents a success response with optional warnings
type WarningResponse struct {
	Warning string `json:"warning,omitempty"`
}

// RespondJSON sends a JSON response.
// For 204 No Content and 304 Not Modified, no body or Content-Type is sent per HTTP spec.
func RespondJSON(w http.ResponseWriter, status int, data any) {
	// 204 and 304 must not have a body per RFC 7230/9110
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}

	if data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Error().Err(err).Msg("Failed to encode JSON response")
		}
		return
	}

	w.WriteHeader(status)
}

// RespondError sends an error response
func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, ErrorResponse{
		Error: message,
	})
}

func respondIfInstanceDisabled(w http.ResponseWriter, err error, instanceID int, context string) bool {
	if errors.Is(err, internalqbittorrent.ErrInstanceDisabled) {
		log.Trace().
			Int("instanceID", instanceID).
			Str("context", context).
			Msg("Ignoring request for disabled instance")
		RespondError(w, http.StatusConflict, "Instance is disabled")
		return true
	}

	return false
}

var countryNameToCodeMap = map[string]string{
	"afghanistan": "af", "albania": "al", "algeria": "dz", "andorra": "ad", "angola": "ao",
	"argentina": "ar", "armenia": "am", "australia": "au", "austria": "at", "azerbaijan": "az",
	"bahamas": "bs", "bahrain": "bh", "bangladesh": "bd", "barbados": "bb", "belarus": "by",
	"belgium": "be", "belize": "bz", "benin": "bj", "bhutan": "bt", "bolivia": "bo",
	"bosnia and herzegovina": "ba", "botswana": "bw", "brazil": "br", "brasil": "br",
	"brunei": "bn", "bulgaria": "bg", "burkina faso": "bf", "burundi": "bi", "cambodia": "kh",
	"cameroon": "cm", "canada": "ca", "chile": "cl", "china": "cn", "colombia": "co",
	"costa rica": "cr", "croatia": "hr", "cuba": "cu", "cyprus": "cy", "czech republic": "cz",
	"czechia": "cz", "denmark": "dk", "dominican republic": "do", "ecuador": "ec", "egypt": "eg",
	"el salvador": "sv", "estonia": "ee", "ethiopia": "et", "finland": "fi", "france": "fr",
	"georgia": "ge", "germany": "de", "deutschland": "de", "ghana": "gh", "greece": "gr",
	"guatemala": "gt", "haiti": "ht", "honduras": "hn", "hong kong": "hk", "hungary": "hu",
	"iceland": "is", "india": "in", "indonesia": "id", "iran": "ir", "iraq": "iq",
	"ireland": "ie", "israel": "il", "italy": "it", "italia": "it", "jamaica": "jm",
	"japan": "jp", "jordan": "jo", "kazakhstan": "kz", "kenya": "ke", "south korea": "kr",
	"korea, republic of": "kr", "republic of korea": "kr", "north korea": "kp", "kuwait": "kw",
	"latvia": "lv", "lebanon": "lb", "libya": "ly", "lithuania": "lt", "luxembourg": "lu",
	"malaysia": "my", "malta": "mt", "mexico": "mx", "moldova": "md", "monaco": "mc",
	"mongolia": "mn", "montenegro": "me", "morocco": "ma", "nepal": "np", "netherlands": "nl",
	"new zealand": "nz", "nicaragua": "ni", "nigeria": "ng", "north macedonia": "mk",
	"norway": "no", "oman": "om", "pakistan": "pk", "palestine": "ps", "panama": "pa",
	"paraguay": "py", "peru": "pe", "philippines": "ph", "poland": "pl", "portugal": "pt",
	"qatar": "qa", "romania": "ro", "russia": "ru", "russian federation": "ru",
	"saudi arabia": "sa", "senegal": "sn", "serbia": "rs", "singapore": "sg",
	"slovakia": "sk", "slovenia": "si", "south africa": "za", "spain": "es", "españa": "es",
	"sri lanka": "lk", "sudan": "sd", "sweden": "se", "switzerland": "ch", "syria": "sy",
	"taiwan": "tw", "thailand": "th", "tunisia": "tn", "turkey": "tr", "türkiye": "tr",
	"uganda": "ug", "ukraine": "ua", "united arab emirates": "ae", "uae": "ae",
	"united kingdom": "gb", "uk": "gb", "great britain": "gb", "united states": "us",
	"united states of america": "us", "usa": "us", "uruguay": "uy", "uzbekistan": "uz",
	"venezuela": "ve", "vietnam": "vn", "viet nam": "vn", "yemen": "ye", "zambia": "zm", "zimbabwe": "zw",
}

func resolveCountryCode(countryCode, country string) string {
	code := strings.TrimSpace(countryCode)
	if len(code) == 2 {
		return strings.ToLower(code)
	}

	cntry := strings.TrimSpace(country)
	if len(cntry) == 2 {
		return strings.ToLower(cntry)
	}

	if code != "" {
		if resolved, ok := countryNameToCodeMap[strings.ToLower(code)]; ok {
			return resolved
		}
	}
	if cntry != "" {
		if resolved, ok := countryNameToCodeMap[strings.ToLower(cntry)]; ok {
			return resolved
		}
	}

	return strings.ToLower(code)
}
