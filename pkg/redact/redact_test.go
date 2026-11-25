// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain []string
		wantNotHave []string
	}{
		{
			name:        "empty string",
			input:       "",
			wantContain: nil,
			wantNotHave: nil,
		},
		{
			name:        "URL with apikey",
			input:       "http://example.com/api?apikey=SECRET123&other=value",
			wantContain: []string{"apikey=REDACTED", "other=value"},
			wantNotHave: []string{"SECRET123"},
		},
		{
			name:        "URL with api_key",
			input:       "http://example.com/api?api_key=SECRET456",
			wantContain: []string{"api_key=REDACTED"},
			wantNotHave: []string{"SECRET456"},
		},
		{
			name:        "URL with passkey",
			input:       "http://example.com/api?passkey=MYPASSKEY",
			wantContain: []string{"passkey=REDACTED"},
			wantNotHave: []string{"MYPASSKEY"},
		},
		{
			name:        "URL with token",
			input:       "http://example.com/api?token=MYTOKEN",
			wantContain: []string{"token=REDACTED"},
			wantNotHave: []string{"MYTOKEN"},
		},
		{
			name:        "URL with password",
			input:       "http://example.com/api?password=MYPASSWORD",
			wantContain: []string{"password=REDACTED"},
			wantNotHave: []string{"MYPASSWORD"},
		},
		{
			name:        "case insensitive - APIKEY",
			input:       "http://example.com/api?APIKEY=SECRET",
			wantContain: []string{"APIKEY=REDACTED"},
			wantNotHave: []string{"SECRET"},
		},
		{
			name:        "case insensitive - ApiKey",
			input:       "http://example.com/api?ApiKey=SECRET",
			wantContain: []string{"ApiKey=REDACTED"},
			wantNotHave: []string{"SECRET"},
		},
		{
			name:        "multiple sensitive params",
			input:       "http://example.com/api?apikey=KEY1&token=TOKEN1&passkey=PASS1",
			wantContain: []string{"apikey=REDACTED", "token=REDACTED", "passkey=REDACTED"},
			wantNotHave: []string{"KEY1", "TOKEN1", "PASS1"},
		},
		{
			name:        "non-sensitive params unchanged",
			input:       "http://example.com/api?format=json&limit=100&query=test",
			wantContain: []string{"format=json", "limit=100", "query=test"},
			wantNotHave: nil,
		},
		{
			name:        "mixed sensitive and non-sensitive",
			input:       "http://example.com/api?apikey=SECRET&format=json&t=search",
			wantContain: []string{"apikey=REDACTED", "format=json", "t=search"},
			wantNotHave: []string{"SECRET"},
		},
		{
			name:        "URL without query params",
			input:       "http://example.com/api",
			wantContain: []string{"http://example.com/api"},
			wantNotHave: nil,
		},
		{
			name:        "URL with userinfo password",
			input:       "http://admin:secretpass@example.com/api",
			wantContain: []string{"admin:REDACTED@example.com"},
			wantNotHave: []string{"secretpass"},
		},
		{
			name:        "URL with userinfo password and query params",
			input:       "http://user:hunter2@example.com/api?apikey=SECRET",
			wantContain: []string{"user:REDACTED@", "apikey=REDACTED"},
			wantNotHave: []string{"hunter2", "SECRET"},
		},
		{
			name:        "proxy path with API key",
			input:       "http://localhost:8080/proxy/abc123def456/api/v2/torrents/info",
			wantContain: []string{"/proxy/REDACTED/api/v2/torrents/info"},
			wantNotHave: []string{"abc123def456"},
		},
		{
			name:        "multi-valued apikey params",
			input:       "http://example.com/api?apikey=SECRET1&apikey=SECRET2",
			wantContain: []string{"REDACTED"},
			wantNotHave: []string{"SECRET1", "SECRET2"},
		},
		{
			name:        "empty apikey value",
			input:       "http://example.com/api?apikey=",
			wantContain: []string{"apikey=REDACTED"},
			wantNotHave: nil,
		},
		{
			name:        "empty apikey with other params",
			input:       "http://example.com/api?apikey=&t=caps",
			wantContain: []string{"apikey=REDACTED", "t=caps"},
			wantNotHave: nil,
		},
		{
			name:        "gazelle authkey and torrent_pass",
			input:       "https://tracker.example.com/torrents.php?action=download&id=12345&authkey=0123456789abcdef&torrent_pass=fedcba9876543210",
			wantContain: []string{"authkey=REDACTED", "torrent_pass=REDACTED", "id=12345"},
			wantNotHave: []string{"0123456789abcdef", "fedcba9876543210"},
		},
		{
			name:        "passkey as path segment",
			input:       "https://tracker.example.com/abcdef0123456789abcdef0123456789/announce",
			wantContain: []string{"/REDACTED/announce"},
			wantNotHave: []string{"abcdef0123456789abcdef0123456789"},
		},
		{
			name:        "discord webhook token in path",
			input:       "https://discord.com/api/webhooks/1234567890/aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789_aBcDeFgHiJkLmNoPqRsTuVwXyZ01",
			wantContain: []string{"/api/webhooks/1234567890/REDACTED"},
			wantNotHave: []string{"aBcDeFgHiJkLmNoPqRsTuVwXyZ"},
		},
		{
			name:        "magnet with passkey announce urls in tr params",
			input:       "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&dn=Some.Release&tr=https%3A%2F%2Ftracker.example.com%2Fannounce%3Fpasskey%3DDEADBEEF01&tr=https%3A%2F%2Fother.example.com%2Fabcdef0123456789abcdef0123456789%2Fannounce",
			wantContain: []string{"c12fe1c06bba254a9dc9f519b335aa7c1367a88a", "tracker.example.com", "other.example.com"},
			wantNotHave: []string{"DEADBEEF01", "abcdef0123456789abcdef0123456789"},
		},
		{
			name:        "magnet with passkey in udp announce url",
			input:       "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&tr=udp%3A%2F%2Ftracker.example.com%2Fabcdef0123456789abcdef0123456789%2Fannounce&tr=udp%3A%2F%2Fopen.example.org%3A1337%2Fannounce",
			wantContain: []string{"c12fe1c06bba254a9dc9f519b335aa7c1367a88a", "tracker.example.com", "open.example.org"},
			wantNotHave: []string{"abcdef0123456789abcdef0123456789"},
		},
		{
			name:        "short path segments untouched",
			input:       "http://localhost:8080/api/v2/torrents/info",
			wantContain: []string{"http://localhost:8080/api/v2/torrents/info"},
			wantNotHave: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := URLString(tt.input)
			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.wantNotHave {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain []string
		wantNotHave []string
	}{
		{
			name:        "empty string",
			input:       "",
			wantContain: nil,
			wantNotHave: nil,
		},
		{
			name:        "error message with apikey",
			input:       "error making request to http://example.com?apikey=SECRET123",
			wantContain: []string{"apikey=REDACTED"},
			wantNotHave: []string{"SECRET123"},
		},
		{
			name:        "error message with api_key",
			input:       "failed: http://x.com?api_key=ABC123&t=caps",
			wantContain: []string{"api_key=REDACTED", "t=caps"},
			wantNotHave: []string{"ABC123"},
		},
		{
			name:        "error message with token",
			input:       "connection failed token=SECRETTOKEN",
			wantContain: []string{"token=REDACTED"},
			wantNotHave: []string{"SECRETTOKEN"},
		},
		{
			name:        "error message with passkey",
			input:       "GET http://tracker.example.com/download?passkey=MYPASSKEY failed",
			wantContain: []string{"passkey=REDACTED"},
			wantNotHave: []string{"MYPASSKEY"},
		},
		{
			name:        "error message with password",
			input:       "auth failed: password=hunter2",
			wantContain: []string{"password=REDACTED"},
			wantNotHave: []string{"hunter2"},
		},
		{
			name:        "case insensitive in error",
			input:       "error: APIKEY=SECRET123 TOKEN=ABC",
			wantContain: []string{"APIKEY=REDACTED", "TOKEN=REDACTED"},
			wantNotHave: []string{"SECRET123", "ABC"},
		},
		{
			name:        "multiple params in error string",
			input:       "apikey=A token=B passkey=C password=D api_key=E",
			wantContain: []string{"apikey=REDACTED", "token=REDACTED", "passkey=REDACTED", "password=REDACTED", "api_key=REDACTED"},
			wantNotHave: []string{"=A", "=B", "=C", "=D", "=E"},
		},
		{
			name:        "unparseable URL fragment still redacted",
			input:       "error: ://broken-url?apikey=LEAK",
			wantContain: []string{"apikey=REDACTED"},
			wantNotHave: []string{"LEAK"},
		},
		{
			name:        "no sensitive params - unchanged",
			input:       "connection reset by peer",
			wantContain: []string{"connection reset by peer"},
			wantNotHave: nil,
		},
		{
			name:        "error with userinfo password",
			input:       "dial tcp http://user:secret@host.com:8080: connection refused",
			wantContain: []string{"user:REDACTED@host.com"},
			wantNotHave: []string{"secret"},
		},
		{
			name:        "error with proxy path",
			input:       "request to /proxy/mysecretkey/api/v2/torrents failed",
			wantContain: []string{"/proxy/REDACTED/api/v2/torrents"},
			wantNotHave: []string{"mysecretkey"},
		},
		{
			name:        "shoutrrr error with discord webhook url",
			input:       `making HTTP POST request: Post "https://discord.com/api/webhooks/1234567890/aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789_aBcDeFgHiJkLmNoPqRsTuVwXyZ01": dial tcp: i/o timeout`,
			wantContain: []string{"/api/webhooks/1234567890/REDACTED", "dial tcp: i/o timeout"},
			wantNotHave: []string{"aBcDeFgHiJkLmNoPqRsTuVwXyZ"},
		},
		{
			name:        "error with magnet link tr passkeys",
			input:       "torrent download redirected to magnet link: magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&tr=https%3A%2F%2Ftracker.example.com%2Fannounce%3Fpasskey%3DDEADBEEF01",
			wantContain: []string{"c12fe1c06bba254a9dc9f519b335aa7c1367a88a", "tracker.example.com"},
			wantNotHave: []string{"DEADBEEF01"},
		},
		{
			name:        "error with gazelle download url",
			input:       "download failed: https://tracker.example.com/torrents.php?action=download&id=1&authkey=SECRETAUTH&torrent_pass=SECRETPASS",
			wantContain: []string{"authkey=REDACTED", "torrent_pass=REDACTED"},
			wantNotHave: []string{"SECRETAUTH", "SECRETPASS"},
		},
		{
			name:        "udp announce url with passkey in path",
			input:       "tracker error: udp://tracker.example.test:1337/a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6/announce timed out",
			wantContain: []string{"udp://tracker.example.test:1337/REDACTED/announce", "timed out"},
			wantNotHave: []string{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"},
		},
		{
			name:        "http announce url with passkey in path",
			input:       "announce failed: http://tracker.example.test/a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6/announce",
			wantContain: []string{"http://tracker.example.test/REDACTED/announce"},
			wantNotHave: []string{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := String(tt.input)
			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.wantNotHave {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}

func TestProxyPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no proxy path",
			input: "/api/v2/torrents/info",
			want:  "/api/v2/torrents/info",
		},
		{
			name:  "proxy path with key",
			input: "/proxy/abc123def456/api/v2/torrents/info",
			want:  "/proxy/REDACTED/api/v2/torrents/info",
		},
		{
			name:  "proxy path at end",
			input: "/proxy/secretkey",
			want:  "/proxy/REDACTED",
		},
		{
			name:  "proxy path with trailing slash",
			input: "/proxy/secretkey/",
			want:  "/proxy/REDACTED/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ProxyPath(tt.input))
		})
	}
}

func TestBasicAuthUser(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "user:password",
			input: "admin:hunter2",
			want:  "admin:REDACTED",
		},
		{
			name:  "user only (no colon)",
			input: "admin",
			want:  "admin",
		},
		{
			name:  "user with empty password",
			input: "admin:",
			want:  "admin:REDACTED",
		},
		{
			name:  "user with complex password",
			input: "admin:p@ssw0rd!#$%",
			want:  "admin:REDACTED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BasicAuthUser(tt.input))
		})
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "json fields whose name holds a secret word",
			input: `{"apiKey":"abc123def","password": "hunter22","user":"bob","retries":3}`,
			want:  `{"apiKey":"REDACTED","password": "REDACTED","user":"bob","retries":3}`,
		},
		{
			name:  "json field with escaped quote and non-string value",
			input: `{"client_secret":"a\"b c","authToken":12345678,"level":"warn"}`,
			want:  `{"client_secret":"REDACTED","authToken":REDACTED,"level":"warn"}`,
		},
		{
			name:  "json field escaped inside a json log line",
			input: `{"level":"error","message":"body {\"apiKey\":\"abc123def\",\"name\":\"x\"}"}`,
			want:  `{"level":"error","message":"body {\"apiKey\":\"REDACTED\",\"name\":\"x\"}"}`,
		},
		{
			name:  "escaped json field with a backslash or a number in its value",
			input: `{"message":"body {\"apiKey\":\"ab\\\\cd\",\"authToken\":12345678}"}`,
			want:  `{"message":"body {\"apiKey\":\"REDACTED\",\"authToken\":REDACTED}"}`,
		},
		{
			name:  "json array or object under a secret name",
			input: `{"apiKeys":["abcdef123","uvwxyz987"],"auth":{"credential":"abcdef456"},"message":"body {\"tokens\":[\"abcdef789\"]}","user":"bob"}`,
			want:  `{"apiKeys":"REDACTED","auth":"REDACTED","message":"body {\"tokens\":\"REDACTED\"}","user":"bob"}`,
		},
		{
			name:  "json array or object under a secret name with a bracket inside a string",
			input: `{"apiKeys":["abc]def123","uvw[xyz987","a\"]b456"],"secrets":{"a":"x}y{z123"},"message":"body {\"tokens\":[\"abc]def789\",\"x[y\"]}","user":"bob"}`,
			want:  `{"apiKeys":"REDACTED","secrets":"REDACTED","message":"body {\"tokens\":\"REDACTED\"}","user":"bob"}`,
		},
		{
			name:  "host name in a url is not a key value pair",
			input: "dial https://auth.example.test:8443/login failed",
			want:  "dial https://auth.example.test:8443/login failed",
		},
		{
			name:  "key value pairs",
			input: "password: hunter22\nSessionSecret: s3cr3t-v4lue, user: bob\nX-Api-Key: abcdef123",
			want:  "password: REDACTED\nSessionSecret: REDACTED, user: bob\nX-Api-Key: REDACTED",
		},
		{
			name:  "authorization header bearer",
			input: "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig\nHost: example.test",
			want:  "Authorization: REDACTED\nHost: example.test",
		},
		{
			name:  "authorization header basic",
			input: "authorization: Basic dXNlcjpwYXNzd29yZA==",
			want:  "authorization: REDACTED",
		},
		{
			name:  "escaped json field with an escaped newline or quote in its value",
			input: `{"message":"body {\"password\":\"abc\\ndef\\\"gh\",\"name\":\"x\"}"}`,
			want:  `{"message":"body {\"password\":\"REDACTED\",\"name\":\"x\"}"}`,
		},
		{
			name:  "authorization header with quoted parameters",
			input: `Authorization: Digest username="alice", realm="test", response="abcdef123456"`,
			want:  "Authorization: REDACTED",
		},
		{
			name:  "authorization header with a quoted value",
			input: `Authorization: "Bearer abcdef123456"`,
			want:  "Authorization: REDACTED",
		},
		{
			name:  "authorization header with quoted parameters inside a json log line",
			input: `{"message":"request Authorization: Digest username=\"alice\", response=\"abcdef123456\"","level":"warn"}`,
			want:  `{"message":"request Authorization: REDACTED","level":"warn"}`,
		},
		{
			name:  "key value pair with an escaped quote in its quoted value",
			input: "password: \"abc\\\"def123456\"\nauthToken: 'abc\\'def123456'",
			want:  "password: REDACTED\nauthToken: REDACTED",
		},
		{
			name:  "quoted key value pair inside a json log line",
			input: `{"message":"config password: \"abc \\\"def\" loaded","level":"warn"}`,
			want:  `{"message":"config password: REDACTED loaded","level":"warn"}`,
		},
		{
			name:  "json field whose value holds a query parameter",
			input: `{"password":"s3cr3t&token=public","user":"bob"}`,
			want:  `{"password":"REDACTED","user":"bob"}`,
		},
		{
			name:  "unquoted key value pair with an ampersand in its value",
			input: "password: abc&def123456 user: bob",
			want:  "password: REDACTED user: bob",
		},
		{
			name:  "key value pair whose value is a flat list or object",
			input: "apiKeys: [\"abcdef123\",\"uvw]xyz987\"] user: bob\ntokens: [abcdef456, uvwxyz654]\nsecrets: {\"a\":\"abcdef789\"}\nmessage: \"body tokens: [\\\"abcdef321\\\"]\"",
			want:  "apiKeys: REDACTED user: bob\ntokens: REDACTED\nsecrets: REDACTED\nmessage: \"body tokens: REDACTED\"",
		},
		{
			name:  "key value pair whose value is a url with a comma in its password",
			input: "authURL: https://user:pa,ss@host.test\nproxyAuth: http://user:pa;ss@host.test/x",
			want:  "authURL: REDACTED\nproxyAuth: REDACTED",
		},
		{
			name:  "json field name with a space",
			input: `{"API Key":"abcdef123456","message":"body {\"Auth Token\":\"abcdef789\"}","user name":"bob"}`,
			want:  `{"API Key":"REDACTED","message":"body {\"Auth Token\":\"REDACTED\"}","user name":"bob"}`,
		},
		{
			name:  "plain text stays",
			input: "cross-seed search found 3 results for Some.Show.S01E01",
			want:  "cross-seed search found 3 results for Some.Show.S01E01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Text(tt.input))
		})
	}
}

func TestSecrets(t *testing.T) {
	secrets := []string{"s3ss10n-s3cr3t", "s3ss10n", "abc", "", "hunter22"}
	input := "cfg dump: <<s3ss10n-s3cr3t>>|pw=hunter22|s3ss10n;abc stays;hunter2 stays"

	got := Secrets(input, secrets)

	assert.Equal(t, "cfg dump: <<REDACTED>>|pw=REDACTED|REDACTED;abc stays;hunter2 stays", got)
}

func TestSecretsRemovesTheJSONEscapedForm(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		input  string
		want   string
	}{
		{"quote in a log line", `abc"defghi`, `{"level":"warn","detail":"login failed: abc\"defghi"}`, `{"level":"warn","detail":"login failed: REDACTED"}`},
		{"HTML escape of encoding/json", `pa<ss>word`, `{"body":"pa\u003css\u003eword"}`, `{"body":"REDACTED"}`},
		{"line separator escape of encoding/json", "pa<ss\u2028wo\u2029rd", `{"body":"pa\u003css\u2028wo\u2029rd"}`, `{"body":"REDACTED"}`},
		{"short secret stays", `ab"cd`, `{"detail":"ab\"cd"}`, `{"detail":"ab\"cd"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Secrets(tt.input, []string{tt.secret}))
		})
	}
}
