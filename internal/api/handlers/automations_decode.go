// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
)

var ignoredRuleKeys = map[reflect.Type][]string{
	reflect.TypeFor[AutomationPayload]():    {"id", "instanceId", "createdAt", "updatedAt"},
	reflect.TypeFor[models.RuleCondition](): {"clientId"},
}

type ruleErrors []string

func (p ruleErrors) Error() string { return strings.Join(p, "\n") }

func decodeAutomationPayload(body io.Reader, instanceID int, instance *models.Instance, stored *models.Automation, saves bool) (*AutomationPayload, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, ruleErrors{"body: not valid JSON: " + err.Error()}
	}

	var payload AutomationPayload
	unmarshalErr := json.Unmarshal(data, &payload)
	var unchanged []string
	scoreGroupsUnchanged := false
	if stored != nil && unmarshalErr == nil {
		payload.Conditions.Normalize()
		stored.Conditions.Normalize()
		if sameJSON(payload.Conditions, stored.Conditions) {
			unchanged = append(unchanged, "conditions")
		}
		if sameJSON(payload.SortingConfig, stored.SortingConfig) {
			unchanged = append(unchanged, "sortingConfig")
			scoreGroupsUnchanged = payload.Conditions != nil && stored.Conditions != nil &&
				sameJSON(payload.Conditions.Grouping, stored.Conditions.Grouping)
		}
	}
	if problems := checkJSONShape(nil, "", raw, reflect.TypeFor[AutomationPayload](), unchanged); len(problems) > 0 {
		return nil, ruleErrors(problems)
	}
	if unmarshalErr != nil {
		return nil, ruleErrors{"body: " + unmarshalErr.Error()}
	}
	if err := automations.ValidateRule(payload.toModel(instanceID, 0), instance); err != nil {
		return nil, ruleErrors{err.Error()}
	}
	if !slices.Contains(unchanged, "conditions") {
		if err := automations.ConditionGroupIDsError(payload.Conditions); err != nil {
			return nil, ruleErrors{err.Error()}
		}
		if saves {
			if problem := differentConditions(payload.Conditions); problem != "" {
				return nil, ruleErrors{problem}
			}
		}
	}
	if !scoreGroupsUnchanged && payload.Conditions != nil {
		if err := automations.ScoreRuleGroupIDsError(payload.Conditions.Grouping, payload.SortingConfig); err != nil {
			return nil, ruleErrors{err.Error()}
		}
	}
	return &payload, nil
}

func differentConditions(conditions *models.ActionConditions) string {
	var first *models.ActionCondition
	for c := range conditions.Conditions() {
		switch {
		case !c.Enabled:
		case first == nil:
			first = &c
		case !sameJSON(first.Condition, c.Condition):
			return first.Path + " and " + c.Path + " differ. All actions in a workflow must use the same condition. Make one workflow for each condition."
		}
	}
	return ""
}

func sameJSON(a, b any) bool {
	aJSON, errA := json.Marshal(a)
	bJSON, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(aJSON, bJSON)
}

func checkJSONShape(problems []string, path string, v any, t reflect.Type, unchanged []string) []string {
	if v == nil {
		return problems
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	wrongType := func(want string) []string {
		label := path
		if label == "" {
			label = "body"
		}
		return append(problems, fmt.Sprintf("%s: expected %s, got %s", label, want, jsonTypeName(v)))
	}

	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			return wrongType("an object")
		}
		problems = checkJSONObject(problems, path, obj, t, unchanged)
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return wrongType("an array")
		}
		for i, item := range arr {
			problems = checkJSONShape(problems, fmt.Sprintf("%s[%d]", path, i), item, t.Elem(), unchanged)
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			return wrongType("a string")
		}
	case reflect.Bool:
		if _, ok := v.(bool); !ok {
			return wrongType("true or false")
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := v.(json.Number)
		if !ok {
			return wrongType("a whole number")
		}
		if _, err := strconv.ParseInt(n.String(), 10, t.Bits()); err != nil {
			return wrongType("a whole number")
		}
	case reflect.Float32, reflect.Float64:
		if _, ok := v.(json.Number); !ok {
			return wrongType("a number")
		}
	default:
	}
	return problems
}

func checkJSONObject(problems []string, path string, obj map[string]any, t reflect.Type, unchanged []string) []string {
	fields := jsonFields(t)
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		keyPath := key
		if path != "" {
			keyPath = path + "." + key
		}
		field, ok := fields[key]
		switch {
		case ok:
			problems = checkJSONShape(problems, keyPath, obj[key], field.Type, unchanged)
		case !slices.Contains(ignoredRuleKeys[t], key):
			problems = append(problems, keyPath+": unknown key")
		}
	}
	if top, _, _ := strings.Cut(path, "."); slices.Contains(unchanged, top) {
		return problems
	}
	return checkRuleValues(problems, path, obj, t)
}

func checkRuleValues(problems []string, path string, obj map[string]any, t reflect.Type) []string {
	switch t {
	case reflect.TypeFor[models.RuleCondition]():
		problems = checkCondition(problems, path, obj)
	case reflect.TypeFor[models.ExportToInstanceAction]():
		problems = checkEnumKey(problems, path, obj, "contentLayout", automations.ContentLayouts)
	case reflect.TypeFor[models.TransferToInstanceAction]():
		problems = checkEnumKey(problems, path, obj, "after", automations.TransferAfters)
		problems = checkEnumKey(problems, path, obj, "way", automations.TransferWays)
	case reflect.TypeFor[models.TagAction]():
		problems = checkEnumKey(problems, path, obj, "mode", automations.TagModes)
	case reflect.TypeFor[models.DeleteAction]():
		problems = checkEnumKey(problems, path, obj, "mode", automations.DeleteModes)
	case reflect.TypeFor[models.ShareLimitsAction]():
		if value, _ := obj["shareLimitAction"].(string); !strings.EqualFold(value, "Default") {
			problems = checkEnumKey(problems, path, obj, "shareLimitAction", automations.ShareLimitActions)
		}
		if value, _ := obj["shareLimitsMode"].(string); !strings.EqualFold(value, "Default") {
			problems = checkEnumKey(problems, path, obj, "shareLimitsMode", automations.ShareLimitsModes)
		}
	}
	return problems
}

func checkEnumKey(problems []string, path string, obj map[string]any, key string, allowed []string) []string {
	if value, _ := obj[key].(string); value != "" && !slices.Contains(allowed, value) {
		problems = append(problems, fmt.Sprintf("%s.%s: %s does not allow %q; it allows %s",
			path, key, key, value, strings.Join(allowed, ", ")))
	}
	return problems
}

func checkCondition(problems []string, path string, obj map[string]any) []string {
	operator, _ := obj["operator"].(string)
	op := models.ConditionOperator(operator)
	if children, _ := obj["conditions"].([]any); len(children) > 0 && (op == models.OperatorAnd || op == models.OperatorOr) {
		return problems
	}
	field, isString := obj["field"].(string)
	if !isString && obj["field"] != nil {
		return problems
	}
	spec, ok := automations.ConditionFields[models.ConditionField(field)]
	if !ok {
		return append(problems, fmt.Sprintf("%s.field: unknown field %q", path, field))
	}

	if !slices.Contains(spec.Operators, op) {
		allowed := joinOperators(spec.Operators)
		if knownOperator(op) {
			problems = append(problems, fmt.Sprintf("%s.operator: %s does not allow %s; it allows %s", path, field, op, allowed))
		} else {
			problems = append(problems, fmt.Sprintf("%s.operator: unknown operator %q; %s allows %s", path, operator, field, allowed))
		}
	}

	value, _ := obj["value"].(string)
	regex, _ := obj["regex"].(bool)
	pattern := regex && spec.Type == automations.ValueString
	if len(spec.Values) > 0 && (value != "" || spec.Type == automations.ValueBoolean) && !pattern && (op == models.OperatorEqual || op == models.OperatorNotEqual) &&
		!slices.ContainsFunc(spec.Values, func(allowed string) bool { return strings.EqualFold(allowed, value) }) {
		problems = append(problems, fmt.Sprintf("%s.value: %s does not allow %q; it allows %s", path, field, value, strings.Join(spec.Values, ", ")))
	}
	return problems
}

func knownOperator(op models.ConditionOperator) bool {
	for _, spec := range automations.ConditionFields {
		if slices.Contains(spec.Operators, op) {
			return true
		}
	}
	return false
}

func joinOperators(ops []models.ConditionOperator) string {
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = string(op)
	}
	return strings.Join(names, ", ")
}

func jsonFields(t reflect.Type) map[string]reflect.StructField {
	fields := make(map[string]reflect.StructField, t.NumField())
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		fields[name] = field
	}
	return fields
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "true or false"
	case json.Number:
		return "a number"
	default:
		return "null"
	}
}
