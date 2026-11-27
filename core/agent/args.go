package agent

// Tool arguments. The SDK checks arguments against the input schema and
// returns the validator's text ("validating root: required: missing
// properties: [\"action\"]"), which reads like an internal error. Twinnel
// checks them itself first and says what is wrong in the terms of the tool:
// "missing action; valid actions: …".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// toolSchema is a tool's input schema, inferred from its argument type.
type toolSchema struct {
	s *jsonschema.Schema
	r *jsonschema.Resolved
}

// newToolSchema infers In's schema and adds the allowed values in enums.
func newToolSchema[In any](enums map[string][]string) toolSchema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("tool input schema: %v", err))
	}
	for k, vals := range enums {
		p := s.Properties[k]
		if p == nil {
			panic(fmt.Sprintf("tool input schema: enum for unknown property %q", k))
		}
		p.Enum = nil
		for _, v := range vals {
			p.Enum = append(p.Enum, v)
		}
	}
	r, err := s.Resolve(nil)
	if err != nil {
		panic(fmt.Sprintf("tool input schema: %v", err))
	}
	return toolSchema{s, r}
}

// check validates raw (a tools/call "arguments" value) and returns it with
// the schema's defaults applied, or an error written for the agent.
func (ts toolSchema) check(tool string, raw json.RawMessage) (json.RawMessage, error) {
	m := map[string]any{}
	if t := bytes.TrimSpace(raw); len(t) > 0 && !bytes.Equal(t, []byte("null")) {
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, fmt.Errorf("%s takes its arguments as a JSON object, like %s", tool, ts.example())
		}
	}
	var problems []string
	for _, k := range sortedKeys(m) {
		if _, ok := ts.s.Properties[k]; !ok {
			problems = append(problems, ts.unknownArg(k))
		}
	}
	for _, k := range ts.s.Required {
		if _, ok := m[k]; !ok {
			problems = append(problems, "missing "+k+ts.hint(k))
		}
	}
	for _, k := range sortedKeys(m) {
		p := ts.s.Properties[k]
		if p == nil {
			continue
		}
		v := m[k]
		if want := types(p); len(want) > 0 && !typeMatches(v, want) {
			problems = append(problems, fmt.Sprintf("%s must be %s%s", k, typeWords(want), ts.hint(k)))
			continue
		}
		if len(p.Enum) > 0 && !inEnum(v, p.Enum) {
			problems = append(problems, fmt.Sprintf("unknown %s %s; %s", k, compact(v), validList(k, p.Enum)))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s: %s", tool, strings.Join(problems, "; "))
	}
	var inst any = m
	if err := ts.r.ApplyDefaults(&inst); err != nil {
		return nil, fmt.Errorf("%s: %v", tool, err)
	}
	if err := ts.r.Validate(inst); err != nil {
		// Something the checks above do not cover (nested values).
		return nil, fmt.Errorf("%s: the arguments do not match its input schema (see tools/list): %v", tool, err)
	}
	return json.Marshal(inst)
}

// hint is " (description)" or "; valid <k>s: a, b" for property k.
func (ts toolSchema) hint(k string) string {
	p := ts.s.Properties[k]
	if p == nil {
		return ""
	}
	if len(p.Enum) > 0 {
		return "; " + validList(k, p.Enum)
	}
	if p.Description != "" {
		return " (" + p.Description + ")"
	}
	return ""
}

func (ts toolSchema) unknownArg(k string) string {
	names := sortedKeys(ts.s.Properties)
	if len(names) == 0 {
		return fmt.Sprintf("unknown argument %q; this tool takes no arguments", k)
	}
	return fmt.Sprintf("unknown argument %q; valid arguments: %s", k, strings.Join(names, ", "))
}

// example is a minimal arguments object: the required properties.
func (ts toolSchema) example() string {
	parts := []string{}
	for _, k := range ts.s.Required {
		v := `"…"`
		if p := ts.s.Properties[k]; p != nil {
			if len(p.Enum) > 0 {
				v = compact(p.Enum[0])
			} else if w := types(p); len(w) > 0 && w[0] != "string" {
				v = "…"
			}
		}
		parts = append(parts, fmt.Sprintf("%q: %s", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func validList(k string, enum []any) string {
	vals := make([]string, len(enum))
	for i, v := range enum {
		if s, ok := v.(string); ok {
			vals[i] = s
		} else {
			vals[i] = compact(v)
		}
	}
	label := "valid values for " + k
	if !strings.ContainsAny(k, "_-") && !strings.HasSuffix(k, "s") {
		label = "valid " + k + "s"
	}
	return label + ": " + strings.Join(vals, ", ")
}

func types(p *jsonschema.Schema) []string {
	if p.Type != "" {
		return []string{p.Type}
	}
	return p.Types
}

func typeMatches(v any, want []string) bool {
	for _, w := range want {
		switch w {
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "number":
			if _, ok := v.(float64); ok {
				return true
			}
		case "integer":
			if f, ok := v.(float64); ok && f == math.Trunc(f) {
				return true
			}
		case "array":
			if _, ok := v.([]any); ok {
				return true
			}
		case "object":
			if _, ok := v.(map[string]any); ok {
				return true
			}
		case "null":
			if v == nil {
				return true
			}
		}
	}
	return false
}

func typeWords(want []string) string {
	words := map[string]string{"string": "a string", "boolean": "true or false", "number": "a number",
		"integer": "a whole number", "array": "a list", "object": "an object", "null": "null"}
	var out []string
	for _, w := range want {
		if w == "null" && len(want) > 1 {
			continue // optional pointer fields: the null is not what to say
		}
		out = append(out, words[w])
	}
	return strings.Join(out, " or ")
}

func inEnum(v any, enum []any) bool {
	for _, e := range enum {
		if e == v {
			return true
		}
	}
	return false
}

func compact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
