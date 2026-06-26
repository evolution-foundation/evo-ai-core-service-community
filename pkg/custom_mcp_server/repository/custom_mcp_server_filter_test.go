package repository

import (
	"testing"

	"evo-ai-core-service/pkg/custom_mcp_server/model"
)

func TestCustomMcpServerFilterFragment(t *testing.T) {
	cases := []struct {
		name     string
		filter   model.CustomMcpServerListFilter
		wantSQL  string
		wantArgs []interface{}
		wantOK   bool
	}{
		{
			name:   "unknown attribute is rejected (whitelist)",
			filter: model.CustomMcpServerListFilter{AttributeKey: "headers", FilterOperator: "contains", Values: []string{"x"}},
			wantOK: false,
		},
		{
			name:     "name contains",
			filter:   model.CustomMcpServerListFilter{AttributeKey: "name", FilterOperator: "contains", Values: []string{"weather"}},
			wantSQL:  "name ILIKE ?",
			wantArgs: []interface{}{"%weather%"},
			wantOK:   true,
		},
		{
			name:     "url equal_to is case-insensitive",
			filter:   model.CustomMcpServerListFilter{AttributeKey: "url", FilterOperator: "equal_to", Values: []string{"https://x"}},
			wantSQL:  "LOWER(url) = LOWER(?)",
			wantArgs: []interface{}{"https://x"},
			wantOK:   true,
		},
		{
			name:     "timeout equal_to binds an integer",
			filter:   model.CustomMcpServerListFilter{AttributeKey: "timeout", FilterOperator: "equal_to", Values: []string{"30"}},
			wantSQL:  "timeout = ?",
			wantArgs: []interface{}{30},
			wantOK:   true,
		},
		{
			name:   "timeout with a non-numeric value is rejected (no text-to-int error)",
			filter: model.CustomMcpServerListFilter{AttributeKey: "timeout", FilterOperator: "equal_to", Values: []string{"abc"}},
			wantOK: false,
		},
		{
			name:     "tags equal_to is case-insensitive array membership",
			filter:   model.CustomMcpServerListFilter{AttributeKey: "tags", FilterOperator: "equal_to", Values: []string{"prod"}},
			wantSQL:  "LOWER(?) = ANY(SELECT LOWER(t) FROM unnest(tags) AS t)",
			wantArgs: []interface{}{"prod"},
			wantOK:   true,
		},
		{
			name:     "created_at equal_to matches by date",
			filter:   model.CustomMcpServerListFilter{AttributeKey: "created_at", FilterOperator: "equal_to", Values: []string{"2026-01-01"}},
			wantSQL:  "DATE(created_at) = ?",
			wantArgs: []interface{}{"2026-01-01"},
			wantOK:   true,
		},
		{
			name:   "created_at rejects substring operators",
			filter: model.CustomMcpServerListFilter{AttributeKey: "created_at", FilterOperator: "contains", Values: []string{"2026"}},
			wantOK: false,
		},
		{
			name:   "created_at rejects a malformed date value (no Postgres cast error)",
			filter: model.CustomMcpServerListFilter{AttributeKey: "created_at", FilterOperator: "equal_to", Values: []string{"notadate"}},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, ok := customMcpServerFilterFragment(tc.filter)
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
