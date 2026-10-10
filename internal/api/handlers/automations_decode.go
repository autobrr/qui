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
// It reports an unknown key at any depth and a value of the wrong JSON type, each with its JSON path.
// It does not check that a referenced instance or external program exists.
func decodeAutomationPayload(body io.Reader, instanceID int, instance *models.Instance) (*AutomationPayload, error) {
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
	if problems := checkJSONShape(nil, "", raw, reflect.TypeFor[AutomationPayload]()); len(problems) > 0 {
		return nil, ruleErrors(problems)
	}

	var payload AutomationPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, ruleErrors{"body: " + err.Error()}
	}
	if err := automations.ValidateRule(payload.toModel(instanceID, 0), instance); err != nil {
		return nil, ruleErrors{err.Error()}
	}
	return &payload, nil
}

// checkJSONShape compares a value decoded with UseNumber against the Go type that it decodes into.
// Like encoding/json, it accepts null for every type.
func checkJSONShape(problems []string, path string, v any, t reflect.Type) []string {
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
				problems = checkJSONShape(problems, keyPath, obj[key], field.Type)
			case !slices.Contains(ignoredRuleKeys[t], key):
				problems = append(problems, keyPath+": unknown key")
			}
		}
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return wrongType("an array")
		}
		for i, item := range arr {
			problems = checkJSONShape(problems, fmt.Sprintf("%s[%d]", path, i), item, t.Elem())
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
