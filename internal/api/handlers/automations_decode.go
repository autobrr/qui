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

// ignoredRuleKeys are keys that the rule structs do not have but that clients send.
// The on/off switch sends the whole stored rule, with the server-set keys.
// The query builder adds clientId to each condition.
var ignoredRuleKeys = map[reflect.Type][]string{
	reflect.TypeFor[AutomationPayload]():    {"id", "instanceId", "createdAt", "updatedAt"},
	reflect.TypeFor[models.RuleCondition](): {"clientId"},
}

// ruleErrors is the list of problems in rule JSON, one line per problem.
type ruleErrors []string

func (p ruleErrors) Error() string { return strings.Join(p, "\n") }

// decodeAutomationPayload decodes rule JSON strictly and runs the rule check.
// It reports an unknown key at any depth, a value of the wrong JSON type, and a condition field, operator or enum value
// that automations.ConditionFields does not allow, each with its JSON path.
// stored is the rule that an update replaces, or nil. The field, operator and enum checks skip the conditions and the sorting
// when they equal those of stored, so a stored rule with a bad operator can still be switched on or off and renamed.
// It does not check that a referenced instance or external program exists.
func decodeAutomationPayload(body io.Reader, instanceID int, instance *models.Instance, stored *models.Automation) (*AutomationPayload, error) {
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
	if stored != nil && unmarshalErr == nil {
		payload.Conditions.Normalize()
		stored.Conditions.Normalize()
		if sameJSON(payload.Conditions, stored.Conditions) {
			unchanged = append(unchanged, "conditions")
		}
		if sameJSON(payload.SortingConfig, stored.SortingConfig) {
			unchanged = append(unchanged, "sortingConfig")
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
	return &payload, nil
}

func sameJSON(a, b any) bool {
	aJSON, errA := json.Marshal(a)
	bJSON, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(aJSON, bJSON)
}

// checkJSONShape compares a value decoded with UseNumber against the Go type that it decodes into.
// Like encoding/json, it accepts null for every type. It also runs checkCondition on each condition
// and checks exportToInstance.contentLayout, except under the top-level keys in unchanged.
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
			break
		}
		switch t {
		case reflect.TypeFor[models.RuleCondition]():
			problems = checkCondition(problems, path, obj)
		case reflect.TypeFor[models.ExportToInstanceAction]():
			if layout, _ := obj["contentLayout"].(string); layout != "" && !slices.Contains(automations.ContentLayouts, layout) {
				problems = append(problems, fmt.Sprintf("%s.contentLayout: contentLayout does not allow %q; it allows %s",
					path, layout, strings.Join(automations.ContentLayouts, ", ")))
			}
		}
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
		// No rule struct has a field of another kind. json.Unmarshal still checks it.
	}
	return problems
}

// checkCondition checks the field, operator and value of one condition against automations.ConditionFields.
// A group (AND or OR with child conditions) has no field, and checkJSONShape checks its children.
// The evaluator runs AND or OR without children as a condition, so it is checked as one.
func checkCondition(problems []string, path string, obj map[string]any) []string {
	operator, _ := obj["operator"].(string)
	op := models.ConditionOperator(operator)
	if children, _ := obj["conditions"].([]any); len(children) > 0 && (op == models.OperatorAnd || op == models.OperatorOr) {
		return problems
	}
	field, isString := obj["field"].(string)
	if !isString && obj["field"] != nil {
		return problems // checkJSONShape reported the wrong type
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

	// The evaluator ignores case. Another operator, or a regex on a string field, compares free text.
	// The other fields ignore regex. An empty value passes, except on a yes/no field, which reads it as false.
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

// jsonFields maps each JSON key of a struct to its field. Each rule struct field has a json tag, and none is embedded.
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
