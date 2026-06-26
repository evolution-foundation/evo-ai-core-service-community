package repository

import (
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
