// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// The rule builder reads the fields, operators and values from the condition field table. Build every condition
// that the table allows, and send them through Create, so that the check never rejects a rule that the UI can make.
func TestAutomationCheckAcceptsEveryRuleBuilderCondition(t *testing.T) {
	var leaves []map[string]any
	for field, spec := range automations.ConditionFields {
		for _, operator := range spec.Operators {
			leafValues := spec.Values
			switch {
			case operator == automations.OperatorBetween:
				leaves = append(leaves, map[string]any{"field": field, "operator": operator, "minValue": 2000, "maxValue": 2001})
				continue
			case len(leafValues) > 0 && (operator == automations.OperatorEqual || operator == automations.OperatorNotEqual):
			case spec.Type == automations.ValueString:
				leafValues = []string{"x"}
			default:
				leafValues = []string{"2000"}
			}
			for _, value := range leafValues {
				leaves = append(leaves, map[string]any{"field": field, "operator": operator, "value": value})
			}
		}
	}

	values := func(text string) []string {
		matches := regexp.MustCompile(`value: "([^"]*)"`).FindAllStringSubmatch(text, -1)
		out := make([]string, len(matches))
		for i, m := range matches {
			out[i] = m[1]
		}
		return out
	}
	layouts := values(regexp.MustCompile(`(?s)CONTENT_LAYOUT_OPTIONS = \[(.*?)\]`).FindString(
		readFile(t, "../../../web/src/components/instances/preferences/WorkflowDialog.tsx")))
	require.NotEmpty(t, layouts)

	db := testdb.NewMigratedSQLite(t, "automation-rule-builder")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	localAccess := true // the hardlink and missing files fields need it
	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)
	handler := NewAutomationHandler(models.NewAutomationStore(db), nil, instances, nil, &automations.Service{})

	for _, layout := range layouts {
		body, err := json.Marshal(map[string]any{
			"name":           "every rule builder condition",
			"trackerPattern": "*",
			"conditions": map[string]any{
				"schemaVersion":    "1",
				"pause":            map[string]any{"enabled": true, "condition": map[string]any{"operator": "OR", "conditions": leaves}},
				"exportToInstance": map[string]any{"enabled": false, "targetInstanceId": 0, "savePath": "", "contentLayout": layout},
			},
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(instance.ID))
		rec := httptest.NewRecorder()
		handler.Create(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
