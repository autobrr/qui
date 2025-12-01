// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRespondJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		data       any
		wantStatus int
		wantBody   string
	}{
		{
			name:       "success with data",
			status:     http.StatusOK,
			data:       map[string]string{"message": "hello"},
			wantStatus: http.StatusOK,
			wantBody:   `{"message":"hello"}`,
		},
		{
			name:       "nil data",
			status:     http.StatusNoContent,
			data:       nil,
			wantStatus: http.StatusNoContent,
			wantBody:   "",
		},
		{
			name:       "error status with data",
			status:     http.StatusBadRequest,
			data:       ErrorResponse{Error: "bad request"},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"bad request"}`,
		},
		{
			name:       "slice data",
			status:     http.StatusOK,
			data:       []int{1, 2, 3},
			wantStatus: http.StatusOK,
			wantBody:   `[1,2,3]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			RespondJSON(w, tt.status, tt.data)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, w.Body.String())
			}
		})
	}
}

func TestRespondError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		message    string
		wantStatus int
	}{
		{
			name:       "bad request",
			status:     http.StatusBadRequest,
			message:    "invalid input",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "internal server error",
			status:     http.StatusInternalServerError,
			message:    "something went wrong",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			message:    "resource not found",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			RespondError(w, tt.status, tt.message)

			assert.Equal(t, tt.wantStatus, w.Code)

			var resp ErrorResponse
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, tt.message, resp.Error)
		})
	}
}

func TestParseInstanceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		instanceID string
		wantID     int
		wantOK     bool
		wantStatus int
	}{
		{
			name:       "valid ID",
			instanceID: "123",
			wantID:     123,
			wantOK:     true,
		},
		{
			name:       "invalid ID - not a number",
			instanceID: "abc",
			wantID:     0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
		// Note: Empty instanceID doesn't match chi route pattern, so the handler is never called
		// This is expected chi behavior - the route simply won't match
		{
			name:       "valid negative ID",
			instanceID: "-1",
			wantID:     -1,
			wantOK:     true,
		},
		{
			name:       "zero ID",
			instanceID: "0",
			wantID:     0,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := chi.NewRouter()
			var gotID int
			var gotOK bool

			r.Get("/instances/{instanceID}", func(w http.ResponseWriter, r *http.Request) {
				gotID, gotOK = ParseInstanceID(w, r)
			})

			req := httptest.NewRequest("GET", "/instances/"+tt.instanceID, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantOK, gotOK)

			if !tt.wantOK {
				assert.Equal(t, tt.wantStatus, w.Code)
			}
		})
	}
}

func TestParseTorrentHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hash       string
		wantHash   string
		wantOK     bool
		wantStatus int
	}{
		{
			name:     "valid hash",
			hash:     "abc123def456",
			wantHash: "abc123def456",
			wantOK:   true,
		},
		// Note: Empty hash doesn't match chi route pattern, so handler is never called
		// This is expected chi behavior - the route simply won't match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := chi.NewRouter()
			var gotHash string
			var gotOK bool

			r.Get("/torrents/{hash}", func(w http.ResponseWriter, r *http.Request) {
				gotHash, gotOK = ParseTorrentHash(w, r)
			})

			req := httptest.NewRequest("GET", "/torrents/"+tt.hash, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantHash, gotHash)
			assert.Equal(t, tt.wantOK, gotOK)

			if !tt.wantOK {
				assert.Equal(t, tt.wantStatus, w.Code)
			}
		})
	}
}

func TestParseIndexerID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		indexerID  string
		wantID     int
		wantOK     bool
		wantStatus int
	}{
		{
			name:      "valid ID",
			indexerID: "42",
			wantID:    42,
			wantOK:    true,
		},
		{
			name:       "invalid ID",
			indexerID:  "abc",
			wantID:     0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := chi.NewRouter()
			var gotID int
			var gotOK bool

			r.Get("/indexers/{indexerID}", func(w http.ResponseWriter, r *http.Request) {
				gotID, gotOK = ParseIndexerID(w, r)
			})

			req := httptest.NewRequest("GET", "/indexers/"+tt.indexerID, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestParseRuleID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ruleID     string
		wantID     int
		wantOK     bool
		wantStatus int
	}{
		{
			name:   "valid ID",
			ruleID: "1",
			wantID: 1,
			wantOK: true,
		},
		{
			name:       "zero ID is invalid",
			ruleID:     "0",
			wantID:     0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative ID is invalid",
			ruleID:     "-1",
			wantID:     0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-numeric ID",
			ruleID:     "abc",
			wantID:     0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := chi.NewRouter()
			var gotID int
			var gotOK bool

			r.Get("/rules/{ruleID}", func(w http.ResponseWriter, r *http.Request) {
				gotID, gotOK = ParseRuleID(w, r)
			})

			req := httptest.NewRequest("GET", "/rules/"+tt.ruleID, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name       string
		body       string
		wantOK     bool
		wantResult testStruct
	}{
		{
			name:       "valid JSON",
			body:       `{"name":"test","value":42}`,
			wantOK:     true,
			wantResult: testStruct{Name: "test", Value: 42},
		},
		{
			name:   "invalid JSON",
			body:   `{invalid}`,
			wantOK: false,
		},
		{
			name:   "empty body",
			body:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			var result testStruct
			ok := DecodeJSON(w, req, &result)

			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantResult, result)
			} else {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestDecodeJSONOptional(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name       string
		body       string
		wantOK     bool
		wantResult testStruct
	}{
		{
			name:       "valid JSON",
			body:       `{"name":"test"}`,
			wantOK:     true,
			wantResult: testStruct{Name: "test"},
		},
		{
			name:       "empty body returns true",
			body:       "",
			wantOK:     true,
			wantResult: testStruct{},
		},
		{
			name:   "invalid JSON",
			body:   `{invalid}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			var result testStruct
			ok := DecodeJSONOptional(w, req, &result)

			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantResult, result)
			}
		})
	}
}

func TestParsePagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		query        string
		defaultLimit int
		maxLimit     int
		wantLimit    int
		wantOffset   int
	}{
		{
			name:         "defaults when no params",
			query:        "",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   0,
		},
		{
			name:         "custom limit and offset",
			query:        "?limit=50&offset=10",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    50,
			wantOffset:   10,
		},
		{
			name:         "limit capped at max",
			query:        "?limit=200",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    100,
			wantOffset:   0,
		},
		{
			name:         "invalid limit uses default",
			query:        "?limit=abc",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   0,
		},
		{
			name:         "negative limit uses default",
			query:        "?limit=-5",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   0,
		},
		{
			name:         "zero limit uses default",
			query:        "?limit=0",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   0,
		},
		{
			name:         "invalid offset uses default",
			query:        "?offset=abc",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   0,
		},
		{
			name:         "negative offset uses default",
			query:        "?offset=-5",
			defaultLimit: 20,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/"+tt.query, nil)
			params := ParsePagination(req, tt.defaultLimit, tt.maxLimit)

			assert.Equal(t, tt.wantLimit, params.Limit)
			assert.Equal(t, tt.wantOffset, params.Offset)
		})
	}
}

func TestParseIntParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		paramValue  string
		paramName   string
		displayName string
		wantValue   int
		wantOK      bool
	}{
		{
			name:        "valid int",
			paramValue:  "42",
			paramName:   "id",
			displayName: "item ID",
			wantValue:   42,
			wantOK:      true,
		},
		{
			name:        "invalid int",
			paramValue:  "abc",
			paramName:   "id",
			displayName: "item ID",
			wantValue:   0,
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := chi.NewRouter()
			var gotValue int
			var gotOK bool

			r.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
				gotValue, gotOK = ParseIntParam(w, r, tt.paramName, tt.displayName)
			})

			req := httptest.NewRequest("GET", "/items/"+tt.paramValue, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantValue, gotValue)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestParseIntParam64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		paramValue  string
		paramName   string
		displayName string
		wantValue   int64
		wantOK      bool
	}{
		{
			name:        "valid int64",
			paramValue:  "9223372036854775807",
			paramName:   "runID",
			displayName: "run ID",
			wantValue:   9223372036854775807,
			wantOK:      true,
		},
		{
			name:        "invalid int64",
			paramValue:  "abc",
			paramName:   "runID",
			displayName: "run ID",
			wantValue:   0,
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := chi.NewRouter()
			var gotValue int64
			var gotOK bool

			r.Get("/runs/{runID}", func(w http.ResponseWriter, r *http.Request) {
				gotValue, gotOK = ParseIntParam64(w, r, tt.paramName, tt.displayName)
			})

			req := httptest.NewRequest("GET", "/runs/"+tt.paramValue, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantValue, gotValue)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestRespondNotFoundIfNoRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		message    string
		wantResult bool
		wantStatus int
	}{
		{
			name:       "sql.ErrNoRows returns true",
			err:        sql.ErrNoRows,
			message:    "not found",
			wantResult: true,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "other error returns false",
			err:        sql.ErrConnDone,
			message:    "not found",
			wantResult: false,
		},
		{
			name:       "nil error returns false",
			err:        nil,
			message:    "not found",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			result := RespondNotFoundIfNoRows(w, tt.err, tt.message)

			assert.Equal(t, tt.wantResult, result)
			if tt.wantResult {
				assert.Equal(t, tt.wantStatus, w.Code)
			}
		})
	}
}

func TestRespondDBError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		err             error
		notFoundMessage string
		fallbackMessage string
		wantStatus      int
		wantError       string
	}{
		{
			name:            "sql.ErrNoRows gives 404",
			err:             sql.ErrNoRows,
			notFoundMessage: "resource not found",
			fallbackMessage: "database error",
			wantStatus:      http.StatusNotFound,
			wantError:       "resource not found",
		},
		{
			name:            "other error gives 500",
			err:             sql.ErrConnDone,
			notFoundMessage: "resource not found",
			fallbackMessage: "database error",
			wantStatus:      http.StatusInternalServerError,
			wantError:       "database error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			RespondDBError(w, tt.err, tt.notFoundMessage, tt.fallbackMessage)

			assert.Equal(t, tt.wantStatus, w.Code)

			var resp ErrorResponse
			err := json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, tt.wantError, resp.Error)
		})
	}
}

func TestRespondJSON_UnmarshalableData(t *testing.T) {
	t.Parallel()

	// Create data that cannot be marshaled to JSON
	type badStruct struct {
		Func func() `json:"func"` // functions can't be marshaled
	}

	w := httptest.NewRecorder()

	// This should not panic, even though it can't marshal
	assert.NotPanics(t, func() {
		RespondJSON(w, http.StatusOK, badStruct{Func: func() {}})
	})
}

func TestDecodeJSON_LargeBody(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Data string `json:"data"`
	}

	// Create a large but valid JSON body
	largeData := strings.Repeat("a", 1024*1024) // 1MB of data
	body := `{"data":"` + largeData + `"}`

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	var result testStruct
	ok := DecodeJSON(w, req, &result)

	assert.True(t, ok)
	assert.Equal(t, largeData, result.Data)
}
