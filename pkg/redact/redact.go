// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package redact

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var sensitiveParams = []string{"apikey", "api_key", "passkey", "token", "password", "authkey", "torrent_pass"}

var sensitiveParamRegex = regexp.MustCompile(`(?i)(apikey|api_key|passkey|token|password|authkey|torrent_pass)=([^&\s]*)`)

var proxyPathRegex = regexp.MustCompile(`(/proxy/)([^/]+)(/|$)`)

// pathTokenRegex matches path segments that look like credentials: long,
// unbroken alphanumeric tokens (tracker passkeys, Discord webhook tokens).
// Real path segments (file names, endpoints) virtually always contain dots,
// spaces, or are shorter than this.
var pathTokenRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{24,}$`)

var urlInTextRegex = regexp.MustCompile(`(?:https?|udp|wss?)://[^\s"'<>]+|magnet:\?[^\s"'<>]+`)

func URLString(raw string) string {
	if raw == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return regexRedact(raw)
	}

	modified := false

	if parsed.User != nil {
		if _, hasPass := parsed.User.Password(); hasPass {
			parsed.User = url.UserPassword(parsed.User.Username(), "REDACTED")
			modified = true
		}
	}

	if strings.Contains(parsed.Path, "/proxy/") {
		newPath := proxyPathRegex.ReplaceAllString(parsed.Path, "${1}REDACTED${3}")
		if newPath != parsed.Path {
			parsed.Path = newPath
			parsed.RawPath = ""
			modified = true
		}
	}

	if segments := strings.Split(parsed.Path, "/"); len(segments) > 0 {
		segmentModified := false
		for i, segment := range segments {
			if pathTokenRegex.MatchString(segment) {
				segments[i] = "REDACTED"
				segmentModified = true
			}
		}
		if segmentModified {
			parsed.Path = strings.Join(segments, "/")
			parsed.RawPath = ""
			modified = true
		}
	}

	// Redact sensitive query parameters, and recurse into query values that
	// are themselves URLs (magnet tr= announce URLs carry passkeys)
	query := parsed.Query()
	for key, values := range query {
		redactedWholeParam := false
		for _, param := range sensitiveParams {
			if strings.EqualFold(key, param) {
				query[key] = []string{"REDACTED"}
				modified = true
				redactedWholeParam = true
				break
			}
		}
		if redactedWholeParam {
			continue
		}
		for i, value := range values {
			// Any scheme, not just http(s): magnet tr= values are often
			// udp:// announce URLs carrying a passkey in the path.
			if strings.Contains(value, "://") {
				if redacted := URLString(value); redacted != value {
					values[i] = redacted
					modified = true
				}
			}
		}
	}

	if !modified {
		return raw
	}

	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func URLError(err error) error {
	if err == nil {
		return nil
	}

	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return &url.Error{
			Op:  urlErr.Op,
			URL: URLString(urlErr.URL),
			Err: urlErr.Err,
		}
	}

	return err
}

var userinfoPasswordRegex = regexp.MustCompile(`(://[^/:@\s]+):([^@\s]+)@`)

func String(s string) string {
	if s == "" {
		return s
	}
	result := urlInTextRegex.ReplaceAllStringFunc(s, URLString)
	return regexRedact(result)
}

const sensitiveFieldName = `[A-Za-z0-9_.-]*(?i:key|pass|token|secret|auth)[A-Za-z0-9_.-]*`

// sensitiveJSONFieldName also allows a space, for a quoted JSON name such as
// "API Key".
const sensitiveJSONFieldName = `[A-Za-z0-9_. -]*(?i:key|pass|token|secret|auth)[A-Za-z0-9_. -]*`

// escapedQuotedValue matches a quoted value inside a JSON string, \"...\",
// up to the first escaped quote that is not itself escaped.
const escapedQuotedValue = `\\"(?:[^"\\]|\\[^"\\]|\\\\(?:\\\\|\\"|[^"\\]))*\\"`

// flatJSONItem matches one part of a flat list or object: a character, an
// escape such as \n, or a whole quoted string, so that a bracket inside a
// string does not end the list.
const flatJSONItem = `(?:[^\[\]{}"\\]|\\[^"\[\]{}]|"(?:[^"\\]|\\.)*"|` + escapedQuotedValue + `)`

var (
	jsonSecretFlatRegex    = regexp.MustCompile(`((\\?)"` + sensitiveJSONFieldName + `\\?"\s*:\s*)(?:\[` + flatJSONItem + `*\]|\{` + flatJSONItem + `*\})`)
	jsonSecretStringRegex  = regexp.MustCompile(`("` + sensitiveJSONFieldName + `"\s*:\s*)"(?:[^"\\]|\\.)*"`)
	jsonSecretScalarRegex  = regexp.MustCompile(`(\\?"` + sensitiveJSONFieldName + `\\?"\s*:\s*)[^\s,}\]"{\[\\]+`)
	escapedJSONSecretRegex = regexp.MustCompile(`(\\"` + sensitiveJSONFieldName + `\\"\s*:\s*)` + escapedQuotedValue)
	authorizationRegex     = regexp.MustCompile(`(?i)(authorization:[ \t]*)(?:[^\s"\\]|\\.)(?:="[^"\r\n]*"|[^\r\n"\\]|\\.)*`)
	keyValueSecretRegex    = regexp.MustCompile(`(^|[^A-Za-z0-9_.\-/@])(` + sensitiveFieldName + `:[ \t]*)(?:\[` + flatJSONItem + `*\]|\{` + flatJSONItem + `*\}|"(?:[^"\\\r\n]|\\.)*"|'(?:[^'\\\r\n]|\\.)*'|` + escapedQuotedValue + `|[^\s,;"'}\]]+)`)
)

// Text redacts what String does, plus the value of each JSON field and
// `key: value` pair whose name holds key, pass, token, secret or auth (any
// case), and the value of an Authorization header of any scheme. It is meant
// for log lines and text that a user pastes, so it may redact a value that
// is not a secret.
func Text(s string) string {
	if s == "" {
		return s
	}
	// The field passes run before String, because a query parameter pattern
	// in String would also take the quote that ends a field value.
	result := jsonSecretFlatRegex.ReplaceAllString(s, `${1}${2}"REDACTED${2}"`)
	result = jsonSecretStringRegex.ReplaceAllString(result, `${1}"REDACTED"`)
	result = jsonSecretScalarRegex.ReplaceAllString(result, `${1}REDACTED`)
	result = escapedJSONSecretRegex.ReplaceAllString(result, `${1}\"REDACTED\"`)
	// URLs are redacted before the key: value pass, because that pass stops
	// at a comma and would cut a URL whose password holds one.
	result = urlInTextRegex.ReplaceAllStringFunc(result, URLString)
	result = authorizationRegex.ReplaceAllString(result, `${1}REDACTED`)
	result = keyValueSecretRegex.ReplaceAllString(result, `${1}${2}REDACTED`)
	return String(result)
}

// minSecretLength is the shortest value that Secrets removes. A shorter value
// also matches common words.
const minSecretLength = 6

func Secrets(s string, secrets []string) string {
	if s == "" {
		return s
	}
	if replacer := SecretsReplacer(secrets); replacer != nil {
		return replacer.Replace(s)
	}
	return s
}

func SecretsReplacer(secrets []string) *strings.Replacer {
	values := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if utf8.RuneCountInString(secret) >= minSecretLength {
			values = append(values, secret)
			values = append(values, jsonEscapedForms(secret)...)
		}
	}
	if len(values) == 0 {
		return nil
	}
	// Longest first, so a secret that holds another secret is removed whole.
	slices.SortFunc(values, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b))
	})
	values = slices.Compact(values)

	pairs := make([]string, 0, 2*len(values))
	for _, value := range values {
		pairs = append(pairs, value, "REDACTED")
	}
	return strings.NewReplacer(pairs...)
}

// jsonEscapedForms returns how a JSON string writes secret, when that differs
// from secret: a log line of zerolog escapes a quote or a backslash, and
// encoding/json also escapes <, >, & and the line separators U+2028 and U+2029.
func jsonEscapedForms(secret string) []string {
	var forms []string
	for _, escapeHTML := range []bool{false, true} {
		for _, escapeJS := range []bool{false, true} {
			quoted, err := json.Marshal(secret, jsontext.EscapeForHTML(escapeHTML), jsontext.EscapeForJS(escapeJS))
			if err != nil {
				return nil // invalid UTF-8, which a JSON string does not hold as is
			}
			if form := string(quoted[1 : len(quoted)-1]); form != secret {
				forms = append(forms, form)
			}
		}
	}
	return forms
}

func regexRedact(s string) string {
	result := sensitiveParamRegex.ReplaceAllString(s, "${1}=REDACTED")
	result = userinfoPasswordRegex.ReplaceAllString(result, "${1}:REDACTED@")
	result = proxyPathRegex.ReplaceAllString(result, "${1}REDACTED${3}")
	return result
}

func ProxyPath(path string) string {
	if path == "" || !strings.Contains(path, "/proxy/") {
		return path
	}
	return proxyPathRegex.ReplaceAllString(path, "${1}REDACTED${3}")
}

func BasicAuthUser(cred string) string {
	if cred == "" {
		return cred
	}
	idx := strings.Index(cred, ":")
	if idx < 0 {
		return cred
	}
	return cred[:idx+1] + "REDACTED"
}
