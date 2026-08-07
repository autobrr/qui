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

var countryAliasesMap = map[string]string{
	"uk": "gb", // United Kingdom -> Great Britain (ISO alpha-2 GB)
	"el": "gr", // Greece (EU language code alias -> ISO alpha-2 GR)
	"tp": "tl", // Timor-Leste / East Timor (legacy ISO alpha-2 -> TL)
	"su": "ru", // Soviet Union (legacy TLD / GeoIP code -> RU)
}

var isoAlpha3ToAlpha2Map = map[string]string{
	"afg": "af", "ala": "ax", "alb": "al", "dza": "dz", "asm": "as", "and": "ad", "ago": "ao", "aia": "ai", "ata": "aq", "atg": "ag", "arg": "ar",
	"arm": "am", "abw": "aw", "aus": "au", "aut": "at", "aze": "az", "bhs": "bs", "bhr": "bh", "bgd": "bd", "brb": "bb", "blr": "by",
	"bel": "be", "blz": "bz", "ben": "bj", "bmu": "bm", "btn": "bt", "bol": "bo", "bes": "bq", "bih": "ba", "bwa": "bw", "bvt": "bv",
	"bra": "br", "iot": "io", "brn": "bn", "bgr": "bg", "bfa": "bf", "bdi": "bi", "cpt": "cp", "khm": "kh", "cmr": "cm", "can": "ca",
	"cpv": "cv", "cym": "ky", "caf": "cf", "tcd": "td", "chl": "cl", "chn": "cn", "cxr": "cx", "cck": "cc", "col": "co", "com": "km",
	"cog": "cg", "cod": "cd", "cok": "ck", "cri": "cr", "civ": "ci", "hrv": "hr", "cub": "cu", "cuw": "cw", "cyp": "cy", "cze": "cz",
	"dnk": "dk", "dji": "dj", "dma": "dm", "dom": "do", "ecu": "ec", "egy": "eg", "slv": "sv", "gnq": "gq", "eri": "er", "est": "ee",
	"swz": "sz", "eth": "et", "flk": "fk", "fro": "fo", "fji": "fj", "fin": "fi", "fra": "fr", "guf": "gf", "pyf": "pf", "atf": "tf",
	"gab": "ga", "gmb": "gm", "geo": "ge", "deu": "de", "gha": "gh", "gib": "gi", "grc": "gr", "grl": "gl", "grd": "gd", "glp": "gp",
	"gum": "gu", "gtm": "gt", "ggy": "gg", "gin": "gn", "gnb": "gw", "guy": "gy", "hti": "ht", "hmd": "hm", "vat": "va", "hnd": "hn",
	"hkg": "hk", "hun": "hu", "isl": "is", "ind": "in", "idn": "id", "irn": "ir", "irq": "iq", "irl": "ie", "imn": "im", "isr": "il",
	"ita": "it", "jam": "jm", "jpn": "jp", "jey": "je", "jor": "jo", "kaz": "kz", "ken": "ke", "kir": "ki", "prk": "kp", "kor": "kr",
	"kwt": "kw", "kgz": "kg", "lao": "la", "lva": "lv", "lbn": "lb", "lso": "ls", "lbr": "lr", "lby": "ly", "lie": "li", "ltu": "lt",
	"lux": "lu", "mac": "mo", "mkd": "mk", "mdg": "mg", "mwi": "mw", "mys": "my", "mdv": "mv", "mli": "ml", "mlt": "mt", "mhl": "mh",
	"mtq": "mq", "mrt": "mr", "mus": "mu", "myt": "yt", "mex": "mx", "fsm": "fm", "mda": "md", "mco": "mc", "mng": "mn", "mne": "me",
	"msr": "ms", "mar": "ma", "moz": "mz", "mmr": "mm", "nam": "na", "nru": "nr", "npl": "np", "nld": "nl", "ncl": "nc", "nzl": "nz",
	"nic": "ni", "ner": "ne", "nga": "ng", "niu": "nu", "nfk": "nf", "mnp": "mp", "nor": "no", "omn": "om", "pak": "pk", "plw": "pw",
	"pse": "ps", "pan": "pa", "png": "pg", "pry": "py", "per": "pe", "phl": "ph", "pcn": "pn", "pol": "pl", "prt": "pt", "pri": "pr",
	"qat": "qa", "reu": "re", "rou": "ro", "rus": "ru", "rwa": "rw", "blm": "bl", "shn": "sh", "kna": "kn", "lca": "lc", "maf": "mf",
	"spm": "pm", "vct": "vc", "wsm": "ws", "smr": "sm", "stp": "st", "sau": "sa", "sen": "sn", "srb": "rs", "syc": "sc", "sle": "sl",
	"sgp": "sg", "sxm": "sx", "svk": "sk", "svn": "si", "slb": "sb", "som": "so", "zaf": "za", "sgs": "gs", "ssd": "ss", "esp": "es",
	"lka": "lk", "sdn": "sd", "sur": "sr", "sjm": "sj", "swe": "se", "che": "ch", "syr": "sy", "twn": "tw", "tjk": "tj", "tza": "tz",
	"tha": "th", "tls": "tl", "tgo": "tg", "tkl": "tk", "ton": "to", "tto": "tt", "tun": "tn", "tur": "tr", "tkm": "tm", "tca": "tc",
	"tuv": "tv", "uga": "ug", "ukr": "ua", "are": "ae", "gbr": "gb", "usa": "us", "umi": "um", "ury": "uy", "uzb": "uz", "vut": "vu",
	"ven": "ve", "vnm": "vn", "vgb": "vg", "vir": "vi", "wlf": "wf", "esh": "eh", "yem": "ye", "zmb": "zm", "zwe": "zw",
}

var validAlpha2CodesMap = map[string]bool{
	"ad": true, "ae": true, "af": true, "ag": true, "ai": true, "al": true, "am": true, "ao": true, "aq": true, "ar": true,
	"as": true, "at": true, "au": true, "aw": true, "ax": true, "az": true, "ba": true, "bb": true, "bd": true, "be": true,
	"bf": true, "bg": true, "bh": true, "bi": true, "bj": true, "bl": true, "bm": true, "bn": true, "bo": true, "bq": true,
	"br": true, "bs": true, "bt": true, "bv": true, "bw": true, "by": true, "bz": true, "ca": true, "cc": true, "cd": true,
	"cf": true, "cg": true, "ch": true, "ci": true, "ck": true, "cl": true, "cm": true, "cn": true, "co": true, "cp": true,
	"cr": true, "cu": true, "cv": true, "cw": true, "cx": true, "cy": true, "cz": true, "de": true, "dg": true, "dj": true,
	"dk": true, "dm": true, "do": true, "dz": true, "ec": true, "ee": true, "eg": true, "eh": true, "er": true, "es": true,
	"et": true, "eu": true, "fi": true, "fj": true, "fk": true, "fm": true, "fo": true, "fr": true, "ga": true, "gb": true,
	"gd": true, "ge": true, "gf": true, "gg": true, "gh": true, "gi": true, "gl": true, "gm": true, "gn": true, "gp": true,
	"gq": true, "gr": true, "gs": true, "gt": true, "gu": true, "gw": true, "gy": true, "hk": true, "hm": true, "hn": true,
	"hr": true, "ht": true, "hu": true, "id": true, "ie": true, "il": true, "im": true, "in": true, "io": true, "iq": true,
	"ir": true, "is": true, "it": true, "je": true, "jm": true, "jo": true, "jp": true, "ke": true, "kg": true, "kh": true,
	"ki": true, "km": true, "kn": true, "kp": true, "kr": true, "kw": true, "ky": true, "kz": true, "la": true, "lb": true,
	"lc": true, "li": true, "lk": true, "lr": true, "ls": true, "lt": true, "lu": true, "lv": true, "ly": true, "ma": true,
	"mc": true, "md": true, "me": true, "mf": true, "mg": true, "mh": true, "mk": true, "ml": true, "mm": true, "mn": true,
	"mo": true, "mp": true, "mq": true, "mr": true, "ms": true, "mt": true, "mu": true, "mv": true, "mw": true, "mx": true,
	"my": true, "mz": true, "na": true, "nc": true, "ne": true, "nf": true, "ng": true, "ni": true, "nl": true, "no": true,
	"np": true, "nr": true, "nu": true, "nz": true, "om": true, "pa": true, "pe": true, "pf": true, "pg": true, "ph": true,
	"pk": true, "pl": true, "pm": true, "pn": true, "pr": true, "ps": true, "pt": true, "pw": true, "py": true, "qa": true,
	"re": true, "ro": true, "rs": true, "ru": true, "rw": true, "sa": true, "sb": true, "sc": true, "sd": true, "se": true,
	"sg": true, "sh": true, "si": true, "sj": true, "sk": true, "sl": true, "sm": true, "sn": true, "so": true, "sr": true,
	"ss": true, "st": true, "sv": true, "sx": true, "sy": true, "sz": true, "tc": true, "td": true, "tf": true, "tg": true,
	"th": true, "tj": true, "tk": true, "tl": true, "tm": true, "tn": true, "to": true, "tr": true, "tt": true, "tv": true,
	"tw": true, "tz": true, "ua": true, "ug": true, "um": true, "un": true, "us": true, "uy": true, "uz": true, "va": true,
	"vc": true, "ve": true, "vg": true, "vi": true, "vn": true, "vu": true, "wf": true, "ws": true, "xk": true, "ye": true,
	"yt": true, "za": true, "zm": true, "zw": true,
}

var countryNameToCodeMap = map[string]string{
	"afghanistan": "af", "aland islands": "ax", "åland islands": "ax", "albania": "al", "algeria": "dz", "andorra": "ad", "angola": "ao",
	"argentina": "ar", "armenia": "am", "australia": "au", "austria": "at", "azerbaijan": "az",
	"bahamas": "bs", "bahrain": "bh", "bangladesh": "bd", "barbados": "bb", "belarus": "by",
	"belgium": "be", "belize": "bz", "benin": "bj", "bhutan": "bt", "bolivia": "bo",
	"bosnia and herzegovina": "ba", "botswana": "bw", "brazil": "br", "brasil": "br",
	"brunei": "bn", "bulgaria": "bg", "burkina faso": "bf", "burundi": "bi", "cabo verde": "cv", "cambodia": "kh",
	"cameroon": "cm", "canada": "ca", "cape verde": "cv", "chile": "cl", "china": "cn", "clipperton island": "cp", "colombia": "co",
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
	candidates := []string{countryCode, country}

	for _, raw := range candidates {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)

		if alias, ok := countryAliasesMap[lower]; ok {
			return alias
		}

		if len(lower) == 2 && validAlpha2CodesMap[lower] {
			return lower
		}

		if alpha2, ok := isoAlpha3ToAlpha2Map[lower]; ok {
			return alpha2
		}

		if code, ok := countryNameToCodeMap[lower]; ok {
			return code
		}
	}

	return ""
}
