// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command automationsgen writes the automation rule JSON reference for the docs
// and the condition field table for the query builder.
// Run it from the repository root with make generate.
package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/api/handlers"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
)

const (
	labelsPath    = "web/src/i18n/locales/en/automations.json"
	referencePath = "documentation/docs/_partials/_automations-json-reference.mdx"
	fieldsPath    = "web/src/components/query-builder/fields.generated.ts"
)

func main() {
	out, err := generate(".")
	if err == nil {
		err = os.WriteFile(filepath.FromSlash(referencePath), out, 0o644) //nolint:gosec // docs source file
	}
	if err == nil {
		err = os.WriteFile(filepath.FromSlash(fieldsPath), generateFields(), 0o644) //nolint:gosec // frontend source file
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "automationsgen:", err)
		os.Exit(1)
	}
}

// keyValues lists the allowed values of each string key that has a fixed set, by struct name and JSON key.
var keyValues = map[string][]string{
	"RuleCondition.groupId":                automations.BuiltinGroupIDs,
	"GroupingConfig.defaultGroupId":        automations.BuiltinGroupIDs,
	"GroupDefinition.keys":                 automations.GroupKeys,
	"GroupDefinition.ambiguousPolicy":      automations.GroupAmbiguousPolicies,
	"DeleteAction.mode":                    {models.DeleteModeKeepFiles, models.DeleteModeWithFiles, models.DeleteModeWithFilesPreserveCrossSeeds, models.DeleteModeWithFilesIncludeCrossSeeds},
	"DeleteAction.groupId":                 automations.BuiltinGroupIDs,
	"DeleteAction.atomic":                  {"all"},
	"TagAction.mode":                       {models.TagModeFull, models.TagModeAdd, models.TagModeRemove},
	"CategoryAction.groupId":               automations.BuiltinGroupIDs,
	"MoveAction.groupId":                   automations.BuiltinGroupIDs,
	"MoveAction.atomic":                    {"all"},
	"ShareLimitsAction.shareLimitAction":   {qbt.ShareLimitActionStop, qbt.ShareLimitActionRemove, qbt.ShareLimitActionRemoveWithContent, qbt.ShareLimitActionEnableSuperSeeding},
	"ShareLimitsAction.shareLimitsMode":    {qbt.ShareLimitsModeMatchAny, qbt.ShareLimitsModeMatchAll},
	"ExportToInstanceAction.contentLayout": automations.ContentLayouts,
	"SortingConfig.field":                  conditionFieldNames(func(f models.ConditionField) bool { return f.IsNumeric() || f.IsString() }),
	"FieldMultiplierScoreRule.field":       conditionFieldNames(models.ConditionField.IsNumeric),
	"FreeSpaceSource.type":                 {string(models.FreeSpaceSourceQBittorrent), string(models.FreeSpaceSourcePath)},
	"SortingConfig.type":                   {string(models.SortingTypeSimple), string(models.SortingTypeScore)},
	"SortingConfig.direction":              {string(models.SortDirectionASC), string(models.SortDirectionDESC)},
	"ScoreRule.type":                       {string(models.ScoreRuleTypeFieldMultiplier), string(models.ScoreRuleTypeConditional)},
}

// operatorDocs lists every operator in the order the reference shows them.
var operatorDocs = []struct {
	op  models.ConditionOperator
	doc string
}{
	{models.OperatorAnd, "Group. Matches when every child condition matches."},
	{models.OperatorOr, "Group. Matches when at least one child condition matches."},
	{models.OperatorEqual, "Equal to 'value'."},
	{models.OperatorNotEqual, "Not equal to 'value'."},
	{models.OperatorContains, "Contains 'value'."},
	{models.OperatorNotContains, "Does not contain 'value'."},
	{models.OperatorStartsWith, "Starts with 'value'."},
	{models.OperatorEndsWith, "Ends with 'value'."},
	{models.OperatorMatches, "Matches 'value' as a regular expression, anywhere in the field."},
	{models.OperatorGreaterThan, "Greater than 'value'."},
	{models.OperatorGreaterThanOrEqual, "Greater than or equal to 'value'."},
	{models.OperatorLessThan, "Less than 'value'."},
	{models.OperatorLessThanOrEqual, "Less than or equal to 'value'."},
	{models.OperatorBetween, "From 'minValue' to 'maxValue', both included. A range that wraps on a clock field does not include 'maxValue'."},
	{models.OperatorExistsIn, "A torrent with exactly this name is in the category 'value'."},
	{models.OperatorContainsIn, "A torrent with a similar name is in the category 'value'. Case, accents, and the characters '.', '_' and '-' do not matter."},
}

var unitText = map[automations.Unit]string{
	automations.UnitBytes:          "bytes",
	automations.UnitBytesPerSecond: "bytes per second",
	automations.UnitSeconds:        "seconds",
	automations.UnitFraction:       "0 to 1",
}

const (
	conditionTitle = "Condition object"
	operatorsTitle = "Operator names"
	fieldsTitle    = "Condition field names"
)

var (
	conditionType = reflect.TypeFor[models.RuleCondition]()
	// namedTypes are the Go types that the reference names in place of their JSON type, with the section that describes them.
	namedTypes = map[reflect.Type]struct{ name, section string }{
		conditionType:                               {"condition", conditionTitle},
		reflect.TypeFor[models.ConditionField]():    {"condition field", fieldsTitle},
		reflect.TypeFor[models.ConditionOperator](): {"operator", operatorsTitle},
	}
	quoted = regexp.MustCompile(`'([^'\s][^']*)'`)
	// markdownChars change the output in MDX or in a Markdown table. Doc text and labels cannot contain them outside quotes.
	markdownChars = "`|<>{}[]*_#\\'"
)

// conditionFieldNames returns the sorted names of the current condition fields for which keep returns true.
func conditionFieldNames(keep func(models.ConditionField) bool) []string {
	var names []string
	for field, spec := range automations.ConditionFields {
		if !spec.Legacy && keep(field) {
			names = append(names, string(field))
		}
	}
	slices.Sort(names)
	return names
}

func generate(root string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(labelsPath)))
	if err != nil {
		return nil, err
	}
	var labels struct {
		QueryBuilder struct {
			Fields map[string]string `json:"fields"`
		} `json:"queryBuilder"`
	}
	if err := json.Unmarshal(raw, &labels); err != nil {
		return nil, fmt.Errorf("%s: %w", labelsPath, err)
	}

	g := &generator{used: map[string]bool{}}
	g.line("## JSON reference")
	g.line("")
	g.text("This section lists every key of a rule as JSON, the condition fields and their operators. Legacy keys that qui still accepts in older rules are left out.")
	g.line("")
	if err := g.structSection(reflect.TypeFor[handlers.AutomationPayload](), "", "Rule", ""); err != nil {
		return nil, err
	}
	if err := g.conditionSections(labels.QueryBuilder.Fields); err != nil {
		return nil, err
	}
	if g.err != nil {
		return nil, g.err
	}
	for key := range keyValues {
		if !g.used[key] {
			return nil, fmt.Errorf("keyValues has %s, but no such key is in the reference", key)
		}
	}
	return g.buf.Bytes(), nil
}

type generator struct {
	buf  bytes.Buffer
	used map[string]bool
	err  error // the first text that markdown or plain rejected
}

func (g *generator) line(s string) { g.buf.WriteString(s + "\n") }

// text writes doc text as a paragraph.
func (g *generator) text(s string) { g.line(g.markdown(s)) }

// markdown returns doc text as Markdown. Text in single quotes becomes inline code.
func (g *generator) markdown(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range quoted.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(g.plain(s[last:m[0]]))
		b.WriteString(code(s[m[2]:m[3]]))
		last = m[1]
	}
	b.WriteString(g.plain(s[last:]))
	return b.String()
}

// plain returns s unchanged. It records an error when s has a character that changes the Markdown.
func (g *generator) plain(s string) string {
	if strings.ContainsAny(s, markdownChars) && g.err == nil {
		g.err = fmt.Errorf("%q: use single quotes only around code, and none of %s outside them", s, markdownChars)
	}
	return s
}

func code(s string) string { return "`" + s + "`" }

func codeList(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = code(v)
	}
	return strings.Join(out, ", ")
}

// structSection writes a table of the JSON keys of t, then a section for each nested object.
// path is the JSON path of t, empty for the rule itself.
func (g *generator) structSection(t reflect.Type, path, title, intro string) error {
	type nested struct {
		t    reflect.Type
		path string
	}
	var children []nested

	g.line("### " + title)
	g.line("")
	if intro != "" {
		g.text(intro)
		g.line("")
	}
	g.line("| Key | Type | Values | Description |")
	g.line("| --- | --- | --- | --- |")
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || name == "" {
			continue
		}
		doc, ok := f.Tag.Lookup("doc")
		if !ok {
			return fmt.Errorf("%s.%s has no doc tag", t.Name(), f.Name)
		}
		if doc == "-" {
			continue
		}

		ft := f.Type
		array := ""
		if ft.Kind() == reflect.Slice {
			array = "[]"
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		typeName := jsonType(ft)
		if array != "" {
			typeName = "array of " + typeName
		}

		keyPath := strings.TrimPrefix(path+"."+name, ".") + array
		if named, ok := namedTypes[ft]; ok {
			doc += " See " + named.section + "."
		} else if ft.Kind() == reflect.Struct {
			children = append(children, nested{ft, keyPath})
			doc += " See '" + keyPath + "'."
		}

		key := t.Name() + "." + name
		values := ""
		if v, ok := keyValues[key]; ok {
			values = codeList(v)
			g.used[key] = true
		}
		g.line(fmt.Sprintf("| %s | %s | %s | %s |", code(name), typeName, values, g.markdown(doc)))
	}
	g.line("")

	for _, c := range children {
		if err := g.structSection(c.t, c.path, code(c.path), ""); err != nil {
			return err
		}
	}
	return nil
}

func jsonType(t reflect.Type) string {
	if named, ok := namedTypes[t]; ok {
		return named.name
	}
	switch t.Kind() { //nolint:exhaustive // the rule structs use only these kinds
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int64:
		return "integer"
	case reflect.Float64:
		return "number"
	case reflect.Struct:
		return "object"
	}
	panic("automationsgen: no JSON type for " + t.String())
}

func (g *generator) conditionSections(labels map[string]string) error {
	if err := g.structSection(conditionType, "", conditionTitle, "A condition is a test or an 'AND' or 'OR' group of conditions. The 'value' of a test is always a JSON string. A number is written as a string, for example \"1073741824\", and a yes/no value as \"true\" or \"false\". 'BETWEEN' uses 'minValue' and 'maxValue' in place of 'value'."); err != nil {
		return err
	}

	fields := conditionFieldNames(func(models.ConditionField) bool { return true })
	usedOperators := map[models.ConditionOperator]bool{models.OperatorAnd: true, models.OperatorOr: true}
	for _, field := range fields {
		for _, op := range automations.ConditionFields[automations.ConditionField(field)].Operators {
			usedOperators[op] = true
		}
	}

	g.line("### " + operatorsTitle)
	g.line("")
	g.line("| Operator | Meaning |")
	g.line("| --- | --- |")
	for _, o := range operatorDocs {
		g.line(fmt.Sprintf("| %s | %s |", code(string(o.op)), g.markdown(o.doc)))
		delete(usedOperators, o.op)
	}
	if len(usedOperators) > 0 {
		return fmt.Errorf("operators with no entry in operatorDocs: %v", slices.Sorted(maps.Keys(usedOperators)))
	}
	g.line("")

	g.line("### " + fieldsTitle)
	g.line("")
	g.text("Label is the name of the field in the rule builder. With 'EQUAL' and 'NOT_EQUAL' and no regular expression, a field with values accepts only those values.")
	g.line("")
	g.line("| Field | Label | Type | Operators | Values | Requires |")
	g.line("| --- | --- | --- | --- | --- | --- |")
	for _, field := range fields {
		spec := automations.ConditionFields[automations.ConditionField(field)]
		label, ok := labels[field]
		if !ok {
			return fmt.Errorf("%s has no label in %s", field, labelsPath)
		}
		typeName := string(spec.Type)
		if unit := unitText[spec.Unit]; unit != "" {
			typeName += " (" + unit + ")"
		}
		operators := make([]string, len(spec.Operators))
		for i, op := range spec.Operators {
			operators[i] = string(op)
		}
		g.line(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |",
			code(field), g.plain(label), typeName, codeList(operators), codeList(spec.Values), g.plain(string(spec.Requires))))
	}
	return nil
}

// generateFields writes the condition field table as TypeScript for the query builder.
// It keeps the legacy fields, because the rule builder still shows them in stored rules.
func generateFields() []byte {
	var b bytes.Buffer
	b.WriteString(`/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Code generated by internal/automationsgen from the Go condition field table. DO NOT EDIT.
// Run "make generate" to update it.

import type { ConditionField, ConditionOperator } from "@/types"

export const CONDITION_FIELDS = {
`)
	for _, field := range slices.Sorted(maps.Keys(automations.ConditionFields)) {
		spec := automations.ConditionFields[field]
		props := []string{"type: " + tsString(string(spec.Type))}
		if spec.Unit != automations.UnitNone {
			props = append(props, "unit: "+tsString(string(spec.Unit)))
		}
		operators := make([]string, len(spec.Operators))
		for i, op := range spec.Operators {
			operators[i] = string(op)
		}
		props = append(props, "operators: "+tsArray(operators))
		if len(spec.Values) > 0 {
			props = append(props, "values: "+tsArray(spec.Values))
		}
		if spec.Requires != "" {
			props = append(props, "requires: "+tsString(string(spec.Requires)))
		}
		if spec.Legacy {
			props = append(props, "legacy: true")
		}
		fmt.Fprintf(&b, "  %s: { %s },\n", field, strings.Join(props, ", "))
	}
	b.WriteString(`} as const satisfies Record<ConditionField, {
  type: string
  unit?: string
  operators: readonly ConditionOperator[]
  values?: readonly string[]
  requires?: string
  legacy?: boolean
}>
`)
	return b.Bytes()
}

// tsString returns s as a TypeScript string literal. A JSON string is one.
func tsString(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(out)
}

func tsArray(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = tsString(v)
	}
	return "[" + strings.Join(out, ", ") + "]"
}
