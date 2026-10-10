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

// The rule builder keeps its own copy of the fields, operators and values. Read it, build every condition that it
// offers, and send them through Create, so that the check never rejects a rule that the UI can make.
func TestAutomationCheckAcceptsEveryRuleBuilderCondition(t *testing.T) {
	source := readFile(t, "../../../web/src/components/query-builder/constants.ts")
	block := func(name string) string {
		t.Helper()
		match := regexp.MustCompile(`(?s)` + name + `\b[^=]*= [\[{](.*?)\n[\]}]`).FindStringSubmatch(source)
		require.NotNil(t, match, "no %s in constants.ts", name)
		return match[1]
	}
	values := func(text string) []string {
		matches := regexp.MustCompile(`value: "([^"]*)"`).FindAllStringSubmatch(text, -1)
		out := make([]string, len(matches))
		for i, m := range matches {
			out[i] = m[1]
		}
		return out
	}

	fieldTypes := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^  ([A-Z_0-9]+): "(\w+)",`).FindAllStringSubmatch(block("CONDITION_FIELD_TYPES"), -1) {
		fieldTypes[m[1]] = m[2]
	}
	operatorsByType := map[string][]string{}
	for _, m := range regexp.MustCompile(`(?s)  (\w+): \[(.*?)\n  \]`).FindAllStringSubmatch(block("OPERATORS_BY_TYPE"), -1) {
		operatorsByType[m[1]] = values(m[2])
	}
	valuesByType := map[string][]string{
		"state":            values(block("TORRENT_STATES")),
		"trackerStatus":    values(block("TRACKER_STATUS_VALUES")),
		"hardlinkScope":    values(block("HARDLINK_SCOPE_VALUES")),
		"seasonPackStatus": values(block("SEASON_PACK_STATUS_VALUES")),
		"boolean":          {"true", "false"},
	}
	contentTypes := values(block("CONTENT_TYPE_VALUES"))
	require.Len(t, fieldTypes, len(automations.ConditionFields))

	var leaves []map[string]any
	for field, fieldType := range fieldTypes {
		operators := operatorsByType[fieldType]
		require.NotEmpty(t, operators, "no operators for type %s", fieldType)
		if field == "NAME" {
			operators = append(operators, values(block("NAME_SPECIAL_OPERATORS"))...)
		}
		for _, operator := range operators {
			leafValues, ok := valuesByType[fieldType]
			switch {
			case field == "CONTENT_TYPE" && (operator == "EQUAL" || operator == "NOT_EQUAL"):
				leafValues = contentTypes
			case operator == "BETWEEN":
				leaves = append(leaves, map[string]any{"field": field, "operator": operator, "minValue": 2000, "maxValue": 2001})
				continue
			case !ok && fieldType == "string":
				leafValues = []string{"x"}
			case !ok:
				leafValues = []string{"2000"}
			}
			for _, value := range leafValues {
				leaves = append(leaves, map[string]any{"field": field, "operator": operator, "value": value})
			}
		}
	}

	// The rule builder does not offer "both", but it is a distinct scope that the evaluator matches exactly.
	leaves = append(leaves, map[string]any{"field": "HARDLINK_SCOPE", "operator": "EQUAL", "value": "both"})

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
