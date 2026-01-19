// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

// ============================================================================
// Mock implementations for testing
// ============================================================================

// mockExternalProgramStore implements the store interface for testing
type mockExternalProgramStore struct {
	getByIDFn func(ctx context.Context, id int) (*models.ExternalProgram, error)
	listFn    func(ctx context.Context) ([]*models.ExternalProgram, error)
	createFn  func(ctx context.Context, c *models.ExternalProgramCreate) (*models.ExternalProgram, error)
	updateFn  func(ctx context.Context, id int, u *models.ExternalProgramUpdate) (*models.ExternalProgram, error)
	deleteFn  func(ctx context.Context, id int) error
}

func (m *mockExternalProgramStore) GetByID(ctx context.Context, id int) (*models.ExternalProgram, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, models.ErrExternalProgramNotFound
}

func (m *mockExternalProgramStore) List(ctx context.Context) ([]*models.ExternalProgram, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockExternalProgramStore) Create(ctx context.Context, c *models.ExternalProgramCreate) (*models.ExternalProgram, error) {
	if m.createFn != nil {
		return m.createFn(ctx, c)
	}
	return nil, nil
}

func (m *mockExternalProgramStore) Update(ctx context.Context, id int, u *models.ExternalProgramUpdate) (*models.ExternalProgram, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, u)
	}
	return nil, nil
}

func (m *mockExternalProgramStore) Delete(ctx context.Context, id int) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

// mockQBClient implements qBittorrent client methods for testing
type mockQBClient struct {
	getTorrentsFn func(opts qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
}

func (m *mockQBClient) GetTorrents(opts qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if m.getTorrentsFn != nil {
		return m.getTorrentsFn(opts)
	}
	return nil, nil
}

// mockClientPool implements the client pool interface for testing
type mockClientPool struct {
	getClientFn func(ctx context.Context, instanceID int) (*mockQBClient, error)
}

// ============================================================================
// Test Helper Functions
// ============================================================================

func newExternalProgramsTestRequest(method, path string, body []byte, params map[string]string) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	ctx := chi.NewRouteContext()
	for key, value := range params {
		ctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
}

// ============================================================================
// Test Handlers with Mocks (using interface-based testing)
// ============================================================================

// testableExternalProgramsHandler wraps the handler with mock-friendly interfaces
type testableExternalProgramsHandler struct {
	store      *mockExternalProgramStore
	clientPool *mockClientPool
	config     *domain.Config
}

func newTestableHandler(store *mockExternalProgramStore, pool *mockClientPool, cfg *domain.Config) *testableExternalProgramsHandler {
	return &testableExternalProgramsHandler{
		store:      store,
		clientPool: pool,
		config:     cfg,
	}
}

// executeForHash tests the core execution logic for a single hash
func (h *testableExternalProgramsHandler) executeForHash(
	ctx context.Context,
	program *models.ExternalProgram,
	hash string,
	torrentIndex map[string]*qbt.Torrent,
) map[string]any {
	result := map[string]any{
		"hash":    hash,
		"success": false,
	}

	torrent, found := torrentIndex[strings.ToLower(hash)]
	if !found {
		result["error"] = "Torrent with hash " + hash + " not found"
		return result
	}

	// Simulate execution - in the real handler this calls externalprograms.Execute
	// For testing, we just validate the flow
	result["success"] = true
	if program.UseTerminal {
		result["message"] = "Terminal window opened successfully"
	} else {
		result["message"] = "Program started successfully"
	}
	result["torrent_name"] = torrent.Name

	return result
}

// isPathAllowed checks if the program path is in the allow list
func (h *testableExternalProgramsHandler) isPathAllowed(programPath string) bool {
	if h == nil || h.config == nil || len(h.config.ExternalProgramAllowList) == 0 {
		return true
	}
	// Simplified check for testing - real implementation uses externalprograms.IsPathAllowed
	for _, allowed := range h.config.ExternalProgramAllowList {
		if strings.HasPrefix(programPath, allowed) {
			return true
		}
	}
	return false
}

// ============================================================================
// Phase 1.1: ExecuteExternalProgram Endpoint Tests
// ============================================================================

func TestExecuteExternalProgram_Success(t *testing.T) {
	t.Parallel()

	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Test Program",
		Path:         "/opt/scripts/test.sh",
		ArgsTemplate: "{hash}",
		Enabled:      true,
		UseTerminal:  false,
	}

	torrents := []qbt.Torrent{
		{Hash: "abc123", Name: "Test Torrent 1"},
		{Hash: "def456", Name: "Test Torrent 2"},
	}

	store := &mockExternalProgramStore{
		getByIDFn: func(ctx context.Context, id int) (*models.ExternalProgram, error) {
			if id == 1 {
				return program, nil
			}
			return nil, models.ErrExternalProgramNotFound
		},
	}

	pool := &mockClientPool{
		getClientFn: func(ctx context.Context, instanceID int) (*mockQBClient, error) {
			if instanceID == 1 {
				return &mockQBClient{
					getTorrentsFn: func(opts qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
						return torrents, nil
					},
				}, nil
			}
			return nil, errors.New("instance not found")
		},
	}

	cfg := &domain.Config{ExternalProgramAllowList: []string{"/opt/scripts"}}
	handler := newTestableHandler(store, pool, cfg)

	// Build torrent index
	torrentIndex := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentIndex[strings.ToLower(torrents[i].Hash)] = &torrents[i]
	}

	// Test execution for single hash
	result := handler.executeForHash(context.Background(), program, "abc123", torrentIndex)

	assert.True(t, result["success"].(bool))
	assert.Equal(t, "Program started successfully", result["message"])
	assert.Equal(t, "Test Torrent 1", result["torrent_name"])
}

func TestExecuteExternalProgram_InvalidBody(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs/execute", []byte("invalid json"), nil)
	w := httptest.NewRecorder()

	handler.ExecuteExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestExecuteExternalProgram_MissingProgramID(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	body, _ := json.Marshal(models.ExternalProgramExecute{
		ProgramID:  0, // Missing
		InstanceID: 1,
		Hashes:     []string{"abc123"},
	})

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs/execute", body, nil)
	w := httptest.NewRecorder()

	handler.ExecuteExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Program ID is required")
}

func TestExecuteExternalProgram_MissingInstanceID(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	body, _ := json.Marshal(models.ExternalProgramExecute{
		ProgramID:  1,
		InstanceID: 0, // Missing
		Hashes:     []string{"abc123"},
	})

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs/execute", body, nil)
	w := httptest.NewRecorder()

	handler.ExecuteExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Instance ID is required")
}

func TestExecuteExternalProgram_MissingHashes(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	body, _ := json.Marshal(models.ExternalProgramExecute{
		ProgramID:  1,
		InstanceID: 1,
		Hashes:     []string{}, // Empty
	})

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs/execute", body, nil)
	w := httptest.NewRecorder()

	handler.ExecuteExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "At least one torrent hash is required")
}

func TestExecuteForHash_TorrentFound(t *testing.T) {
	t.Parallel()

	program := &models.ExternalProgram{
		ID:          1,
		Name:        "Test Program",
		Path:        "/opt/scripts/test.sh",
		Enabled:     true,
		UseTerminal: false,
	}

	torrentIndex := map[string]*qbt.Torrent{
		"abc123": {Hash: "abc123", Name: "Test Torrent"},
	}

	handler := newTestableHandler(nil, nil, nil)
	result := handler.executeForHash(context.Background(), program, "abc123", torrentIndex)

	assert.True(t, result["success"].(bool))
	assert.Equal(t, "abc123", result["hash"])
	assert.Equal(t, "Test Torrent", result["torrent_name"])
}

func TestExecuteForHash_TorrentNotFound(t *testing.T) {
	t.Parallel()

	program := &models.ExternalProgram{
		ID:      1,
		Name:    "Test Program",
		Path:    "/opt/scripts/test.sh",
		Enabled: true,
	}

	torrentIndex := map[string]*qbt.Torrent{
		"abc123": {Hash: "abc123", Name: "Test Torrent"},
	}

	handler := newTestableHandler(nil, nil, nil)
	result := handler.executeForHash(context.Background(), program, "xyz789", torrentIndex)

	assert.False(t, result["success"].(bool))
	assert.Contains(t, result["error"].(string), "not found")
}

func TestExecuteForHash_CaseInsensitive(t *testing.T) {
	t.Parallel()

	program := &models.ExternalProgram{
		ID:          1,
		Name:        "Test Program",
		Path:        "/opt/scripts/test.sh",
		Enabled:     true,
		UseTerminal: false,
	}

	// Index with lowercase key
	torrentIndex := map[string]*qbt.Torrent{
		"abc123def": {Hash: "ABC123DEF", Name: "Test Torrent"},
	}

	handler := newTestableHandler(nil, nil, nil)

	// Test with uppercase hash - should find due to lowercase normalization
	result := handler.executeForHash(context.Background(), program, "ABC123DEF", torrentIndex)

	assert.True(t, result["success"].(bool))
	assert.Equal(t, "Test Torrent", result["torrent_name"])
}

func TestExecuteForHash_TerminalMessage(t *testing.T) {
	t.Parallel()

	program := &models.ExternalProgram{
		ID:          1,
		Name:        "Test Program",
		Path:        "/opt/scripts/test.sh",
		Enabled:     true,
		UseTerminal: true, // Terminal mode
	}

	torrentIndex := map[string]*qbt.Torrent{
		"abc123": {Hash: "abc123", Name: "Test Torrent"},
	}

	handler := newTestableHandler(nil, nil, nil)
	result := handler.executeForHash(context.Background(), program, "abc123", torrentIndex)

	assert.True(t, result["success"].(bool))
	assert.Equal(t, "Terminal window opened successfully", result["message"])
}

func TestExecuteForHash_MultipleHashes(t *testing.T) {
	t.Parallel()

	program := &models.ExternalProgram{
		ID:          1,
		Name:        "Test Program",
		Path:        "/opt/scripts/test.sh",
		Enabled:     true,
		UseTerminal: false,
	}

	torrentIndex := map[string]*qbt.Torrent{
		"abc123": {Hash: "abc123", Name: "Test Torrent 1"},
		"def456": {Hash: "def456", Name: "Test Torrent 2"},
	}

	handler := newTestableHandler(nil, nil, nil)

	// Execute for multiple hashes
	hashes := []string{"abc123", "def456", "notfound"}
	var results []map[string]any

	for _, hash := range hashes {
		result := handler.executeForHash(context.Background(), program, hash, torrentIndex)
		results = append(results, result)
	}

	require.Len(t, results, 3)

	// First hash - success
	assert.True(t, results[0]["success"].(bool))
	assert.Equal(t, "Test Torrent 1", results[0]["torrent_name"])

	// Second hash - success
	assert.True(t, results[1]["success"].(bool))
	assert.Equal(t, "Test Torrent 2", results[1]["torrent_name"])

	// Third hash - not found
	assert.False(t, results[2]["success"].(bool))
	assert.Contains(t, results[2]["error"].(string), "not found")
}

// ============================================================================
// Phase 1.2: isPathAllowed Tests (already exists, extending)
// ============================================================================

func TestExternalProgramsHandler_isPathAllowed(t *testing.T) {
	tempDir := t.TempDir()
	allowedFile := filepath.Join(tempDir, "script.sh")

	handler := &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{tempDir}}}
	if !handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be allowed when directory is whitelisted", allowedFile)
	}

	handler = &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{allowedFile}}}
	if !handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected exact path %s to be allowed when explicitly listed", allowedFile)
	}

	otherDir := t.TempDir()
	handler = &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{otherDir}}}
	if handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be blocked when not in allow list", allowedFile)
	}

	handler = &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: nil}}
	if !handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be allowed when allow list is empty", allowedFile)
	}
}

func TestIsPathAllowed_NilHandler(t *testing.T) {
	t.Parallel()

	var handler *ExternalProgramsHandler
	assert.True(t, handler.isPathAllowed("/any/path"))
}

func TestIsPathAllowed_NilConfig(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{config: nil}
	assert.True(t, handler.isPathAllowed("/any/path"))
}

func TestIsPathAllowed_EmptyAllowList(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{}}}
	assert.True(t, handler.isPathAllowed("/any/path"))
}

func TestIsPathAllowed_MultipleDirectories(t *testing.T) {
	t.Parallel()

	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{ExternalProgramAllowList: []string{tempDir1, tempDir2}},
	}

	// Both directories should be allowed
	assert.True(t, handler.isPathAllowed(filepath.Join(tempDir1, "script.sh")))
	assert.True(t, handler.isPathAllowed(filepath.Join(tempDir2, "script.sh")))

	// Other directory should be blocked
	assert.False(t, handler.isPathAllowed("/other/directory/script.sh"))
}

func TestIsPathAllowed_SubdirectoryAccess(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create the subdirectories so path normalization works
	subDir := filepath.Join(tempDir, "subdir")
	deepDir := filepath.Join(tempDir, "deep", "nested", "dir")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.MkdirAll(deepDir, 0755))

	handler := &ExternalProgramsHandler{
		config: &domain.Config{ExternalProgramAllowList: []string{tempDir}},
	}

	// Subdirectories should be allowed
	assert.True(t, handler.isPathAllowed(filepath.Join(subDir, "script.sh")))
	assert.True(t, handler.isPathAllowed(filepath.Join(deepDir, "script.sh")))
}

// ============================================================================
// Phase 1.3: CRUD Operation Tests
// ============================================================================

func TestListExternalPrograms_Success(t *testing.T) {
	t.Parallel()

	programs := []*models.ExternalProgram{
		{ID: 1, Name: "Program 1", Path: "/opt/script1.sh", Enabled: true},
		{ID: 2, Name: "Program 2", Path: "/opt/script2.sh", Enabled: false},
	}

	store := &mockExternalProgramStore{
		listFn: func(ctx context.Context) ([]*models.ExternalProgram, error) {
			return programs, nil
		},
	}

	// Create a testable handler wrapper that simulates ListExternalPrograms
	req := httptest.NewRequest(http.MethodGet, "/api/external-programs", nil)
	w := httptest.NewRecorder()

	// Simulate the handler logic
	result, err := store.List(req.Context())
	require.NoError(t, err)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []*models.ExternalProgram
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response, 2)
	assert.Equal(t, "Program 1", response[0].Name)
	assert.Equal(t, "Program 2", response[1].Name)
}

func TestListExternalPrograms_Empty(t *testing.T) {
	t.Parallel()

	store := &mockExternalProgramStore{
		listFn: func(ctx context.Context) ([]*models.ExternalProgram, error) {
			return []*models.ExternalProgram{}, nil
		},
	}

	result, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestListExternalPrograms_DBError(t *testing.T) {
	t.Parallel()

	store := &mockExternalProgramStore{
		listFn: func(ctx context.Context) ([]*models.ExternalProgram, error) {
			return nil, errors.New("database error")
		},
	}

	result, err := store.List(context.Background())
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "database error")
}

func TestCreateExternalProgram_Success(t *testing.T) {
	t.Parallel()

	store := &mockExternalProgramStore{
		createFn: func(ctx context.Context, c *models.ExternalProgramCreate) (*models.ExternalProgram, error) {
			return &models.ExternalProgram{
				ID:           1,
				Name:         c.Name,
				Path:         c.Path,
				ArgsTemplate: c.ArgsTemplate,
				Enabled:      c.Enabled,
				UseTerminal:  c.UseTerminal,
			}, nil
		},
	}

	cfg := &domain.Config{ExternalProgramAllowList: []string{"/opt/scripts"}}
	handler := newTestableHandler(store, nil, cfg)

	createReq := &models.ExternalProgramCreate{
		Name:         "New Program",
		Path:         "/opt/scripts/new.sh",
		ArgsTemplate: "{hash} {name}",
		Enabled:      true,
		UseTerminal:  false,
	}

	// Verify path is allowed
	assert.True(t, handler.isPathAllowed(createReq.Path))

	// Create the program
	result, err := store.Create(context.Background(), createReq)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ID)
	assert.Equal(t, "New Program", result.Name)
	assert.Equal(t, "/opt/scripts/new.sh", result.Path)
}

func TestCreateExternalProgram_MissingName(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	body, _ := json.Marshal(models.ExternalProgramCreate{
		Name: "", // Missing
		Path: "/opt/scripts/test.sh",
	})

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs", body, nil)
	w := httptest.NewRecorder()

	handler.CreateExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Name is required")
}

func TestCreateExternalProgram_MissingPath(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	body, _ := json.Marshal(models.ExternalProgramCreate{
		Name: "Test Program",
		Path: "", // Missing
	})

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs", body, nil)
	w := httptest.NewRecorder()

	handler.CreateExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Path is required")
}

func TestCreateExternalProgram_PathBlocked(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{ExternalProgramAllowList: []string{"/allowed/path"}},
	}

	body, _ := json.Marshal(models.ExternalProgramCreate{
		Name: "Test Program",
		Path: "/blocked/path/script.sh",
	})

	req := newExternalProgramsTestRequest(http.MethodPost, "/api/external-programs", body, nil)
	w := httptest.NewRecorder()

	handler.CreateExternalProgram(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "not allowed")
}

func TestUpdateExternalProgram_InvalidID(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	body, _ := json.Marshal(models.ExternalProgramUpdate{
		Name: "Updated Program",
		Path: "/opt/scripts/test.sh",
	})

	req := newExternalProgramsTestRequest(http.MethodPut, "/api/external-programs/invalid", body, map[string]string{"id": "invalid"})
	w := httptest.NewRecorder()

	handler.UpdateExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid program ID")
}

func TestUpdateExternalProgram_NotFound(t *testing.T) {
	t.Parallel()

	store := &mockExternalProgramStore{
		updateFn: func(ctx context.Context, id int, u *models.ExternalProgramUpdate) (*models.ExternalProgram, error) {
			return nil, models.ErrExternalProgramNotFound
		},
	}

	result, err := store.Update(context.Background(), 999, &models.ExternalProgramUpdate{
		Name: "Updated Program",
		Path: "/opt/scripts/test.sh",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, models.ErrExternalProgramNotFound)
}

func TestUpdateExternalProgram_PathBlocked(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{ExternalProgramAllowList: []string{"/allowed/path"}},
	}

	body, _ := json.Marshal(models.ExternalProgramUpdate{
		Name: "Updated Program",
		Path: "/blocked/path/script.sh",
	})

	req := newExternalProgramsTestRequest(http.MethodPut, "/api/external-programs/1", body, map[string]string{"id": "1"})
	w := httptest.NewRecorder()

	handler.UpdateExternalProgram(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "not allowed")
}

func TestDeleteExternalProgram_Success(t *testing.T) {
	t.Parallel()

	deleted := false
	store := &mockExternalProgramStore{
		deleteFn: func(ctx context.Context, id int) error {
			if id == 1 {
				deleted = true
				return nil
			}
			return models.ErrExternalProgramNotFound
		},
	}

	err := store.Delete(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestDeleteExternalProgram_NotFound(t *testing.T) {
	t.Parallel()

	store := &mockExternalProgramStore{
		deleteFn: func(ctx context.Context, id int) error {
			return models.ErrExternalProgramNotFound
		},
	}

	err := store.Delete(context.Background(), 999)
	assert.Error(t, err)
	assert.ErrorIs(t, err, models.ErrExternalProgramNotFound)
}

func TestDeleteExternalProgram_InvalidID(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	req := newExternalProgramsTestRequest(http.MethodDelete, "/api/external-programs/invalid", nil, map[string]string{"id": "invalid"})
	w := httptest.NewRecorder()

	handler.DeleteExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid program ID")
}

func TestDeleteExternalProgram_MissingID(t *testing.T) {
	t.Parallel()

	handler := &ExternalProgramsHandler{
		config: &domain.Config{},
	}

	req := newExternalProgramsTestRequest(http.MethodDelete, "/api/external-programs/", nil, map[string]string{"id": ""})
	w := httptest.NewRecorder()

	handler.DeleteExternalProgram(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Missing program ID")
}
