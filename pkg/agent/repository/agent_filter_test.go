package repository

import (
	"reflect"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
)

func TestAgentFilterFragment(t *testing.T) {
	cases := []struct {
		name     string
		filter   model.AgentListFilter
		wantSQL  string
		wantArgs []interface{}
		wantOK   bool
	}{
		{
			name:   "unknown attribute is rejected (whitelist)",
			filter: model.AgentListFilter{AttributeKey: "instruction", FilterOperator: "contains", Values: []string{"x"}},
			wantOK: false,
		},
		{
			name:     "name contains",
			filter:   model.AgentListFilter{AttributeKey: "name", FilterOperator: "contains", Values: []string{"bot"}},
			wantSQL:  "name ILIKE ?",
			wantArgs: []interface{}{"%bot%"},
			wantOK:   true,
		},
		{
			name:     "type equal_to is case-insensitive",
			filter:   model.AgentListFilter{AttributeKey: "type", FilterOperator: "equal_to", Values: []string{"llm"}},
			wantSQL:  "LOWER(type) = LOWER(?)",
			wantArgs: []interface{}{"llm"},
			wantOK:   true,
		},
		{
			name:    "is_present needs no value",
			filter:  model.AgentListFilter{AttributeKey: "model", FilterOperator: "is_present"},
			wantSQL: "model IS NOT NULL",
			wantOK:  true,
		},
		{
			name:   "value-required operator with blank value is dropped",
			filter: model.AgentListFilter{AttributeKey: "name", FilterOperator: "contains", Values: []string{"   "}},
			wantOK: false,
		},
		{
			name:     "created_at equal_to compares by date",
			filter:   model.AgentListFilter{AttributeKey: "created_at", FilterOperator: "equal_to", Values: []string{"2026-01-01"}},
			wantSQL:  "DATE(created_at) = ?",
			wantArgs: []interface{}{"2026-01-01"},
			wantOK:   true,
		},
		{
			name:   "created_at rejects substring operators (no ILIKE on a timestamp)",
			filter: model.AgentListFilter{AttributeKey: "created_at", FilterOperator: "contains", Values: []string{"2026"}},
			wantOK: false,
		},
		{
			name:   "non-whitelisted uuid/text columns (folder_id) are rejected",
			filter: model.AgentListFilter{AttributeKey: "folder_id", FilterOperator: "equal_to", Values: []string{"x"}},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, ok := agentFilterFragment(tc.filter)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if sql != tc.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tc.wantSQL)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args len = %d, want %d", len(args), len(tc.wantArgs))
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Errorf("arg[%d] = %v, want %v", i, args[i], tc.wantArgs[i])
				}
			}
		})
	}
}

func TestQueryGlue(t *testing.T) {
	if got := queryGlue("or"); got != "OR" {
		t.Errorf("queryGlue(or) = %q, want OR", got)
	}
	if got := queryGlue("OR"); got != "OR" {
		t.Errorf("queryGlue(OR) = %q, want OR", got)
	}
	if got := queryGlue(""); got != "AND" {
		t.Errorf("queryGlue(empty) = %q, want AND", got)
	}
}

// A comma-separated value is a SET, which is what lets the Agents list send
// `(type IN ...) AND (model IN ...)` as two clauses instead of one per combination.
// Args hold a slice here, so these stay out of the table above (it compares with `!=`).
func TestAgentFilterFragmentValueSet(t *testing.T) {
	cases := []struct {
		name     string
		filter   model.AgentListFilter
		wantSQL  string
		wantArgs []string
	}{
		{
			name:     "equal_to with a set becomes IN, lowercased",
			filter:   model.AgentListFilter{AttributeKey: "type", FilterOperator: "equal_to", Values: []string{"llm, External ,sequential"}},
			wantSQL:  "LOWER(type) IN (?)",
			wantArgs: []string{"llm", "external", "sequential"},
		},
		{
			name:     "not_equal_to with a set becomes NOT IN, still null-safe",
			filter:   model.AgentListFilter{AttributeKey: "model", FilterOperator: "not_equal_to", Values: []string{"gpt-4o,claude"}},
			wantSQL:  "model IS NULL OR LOWER(model) NOT IN (?)",
			wantArgs: []string{"gpt-4o", "claude"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, ok := agentFilterFragment(tc.filter)
			if !ok {
				t.Fatal("fragment was dropped")
			}
			if sql != tc.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tc.wantSQL)
			}
			got, isSlice := args[0].([]string)
			if !isSlice {
				t.Fatalf("arg[0] = %T, want []string", args[0])
			}
			if !reflect.DeepEqual(got, tc.wantArgs) {
				t.Errorf("arg[0] = %v, want %v", got, tc.wantArgs)
			}
		})
	}
}

// Only the set operators split. `contains` is substring matching, so a comma there is
// part of the text the user typed.
func TestAgentFilterFragmentContainsKeepsComma(t *testing.T) {
	_, args, ok := agentFilterFragment(model.AgentListFilter{
		AttributeKey:   "name",
		FilterOperator: "contains",
		Values:         []string{"Bot, o atendente"},
	})

	if !ok || args[0] != "%Bot, o atendente%" {
		t.Fatalf("args = %v, ok = %v", args, ok)
	}
}

// A date set has no caller and no obvious meaning; the single-value form stays.
func TestAgentFilterFragmentDateStaysSingle(t *testing.T) {
	sql, _, ok := agentFilterFragment(model.AgentListFilter{
		AttributeKey:   "created_at",
		FilterOperator: "equal_to",
		Values:         []string{"2026-08-05,2026-08-04"},
	})

	if !ok || sql != "DATE(created_at) = ?" {
		t.Fatalf("sql = %q, ok = %v", sql, ok)
	}
}
