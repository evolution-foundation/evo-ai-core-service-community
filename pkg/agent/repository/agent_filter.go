package repository

import (
	"fmt"
	"strings"

	"evo-ai-core-service/pkg/agent/model"

	"gorm.io/gorm"
)

// agentFilterColumns whitelists which attribute keys the Agents list screen may
// filter on, mapped to their physical column. Only keys present here reach SQL;
// every value is bound as a parameter, so user input never enters the query
// string — only the (whitelisted) column name and operator shape do.
var agentFilterColumns = map[string]string{
	"name":        "name",
	"description": "description",
	"type":        "type",
	"model":       "model",
	"created_at":  "created_at",
}

// applyAgentFilters adds the free-text search (name/description) and the advanced
// filter clauses to the query. Clauses are combined via each clause's
// query_operator (and/or); search is always AND-combined.
func applyAgentFilters(query *gorm.DB, filters []model.AgentListFilter, search string) *gorm.DB {
	if trimmed := strings.TrimSpace(search); trimmed != "" {
		like := "%" + trimmed + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", like, like)
	}

	conditions := make([]string, 0, len(filters))
	args := make([]interface{}, 0)

	for _, filter := range filters {
		fragment, fragmentArgs, ok := agentFilterFragment(filter)
		if !ok {
			continue
		}

		glue := ""
		if len(conditions) > 0 {
			glue = queryGlue(filter.QueryOperator) + " "
		}
		conditions = append(conditions, fmt.Sprintf("%s(%s)", glue, fragment))
		args = append(args, fragmentArgs...)
	}

	if len(conditions) > 0 {
		query = query.Where(strings.Join(conditions, " "), args...)
	}

	return query
}

func queryGlue(operator string) string {
	if strings.EqualFold(operator, "or") {
		return "OR"
	}
	return "AND"
}

// splitFilterValues reads a clause value as a SET: `llm,external` is two values, not one
// literal. Only the set operators call it, so a name filtered with `contains` keeps its
// commas. Without it the Agents list has to send one clause per (type, model) pair —
// the clause list is flat, so `(type IN ...) AND (model IN ...)` has no other spelling.
func splitFilterValues(raw string) []string {
	values := make([]string, 0, 1)
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func loweredValues(values []string) []string {
	lowered := make([]string, len(values))
	for i, value := range values {
		lowered[i] = strings.ToLower(value)
	}
	return lowered
}

func agentFilterFragment(filter model.AgentListFilter) (string, []interface{}, bool) {
	column, ok := agentFilterColumns[filter.AttributeKey]
	if !ok {
		return "", nil, false
	}

	switch filter.FilterOperator {
	case "is_present":
		return fmt.Sprintf("%s IS NOT NULL", column), nil, true
	case "is_not_present":
		return fmt.Sprintf("%s IS NULL", column), nil, true
	}

	value := ""
	if len(filter.Values) > 0 {
		value = strings.TrimSpace(filter.Values[0])
	}
	if value == "" {
		return "", nil, false
	}

	switch filter.FilterOperator {
	case "equal_to":
		// A date set has no caller and no obvious meaning; keep the single-value form.
		if column == "created_at" {
			return "DATE(created_at) = ?", []interface{}{value}, true
		}
		if set := splitFilterValues(value); len(set) > 1 {
			return fmt.Sprintf("LOWER(%s) IN (?)", column), []interface{}{loweredValues(set)}, true
		}
		return fmt.Sprintf("LOWER(%s) = LOWER(?)", column), []interface{}{value}, true
	case "not_equal_to":
		if column == "created_at" {
			return "DATE(created_at) <> ?", []interface{}{value}, true
		}
		if set := splitFilterValues(value); len(set) > 1 {
			return fmt.Sprintf("%s IS NULL OR LOWER(%s) NOT IN (?)", column, column),
				[]interface{}{loweredValues(set)}, true
		}
		return fmt.Sprintf("%s IS NULL OR LOWER(%s) <> LOWER(?)", column, column), []interface{}{value}, true
	case "contains":
		// Substring matching is text-only; ILIKE on the timestamp column errors in Postgres.
		if column == "created_at" {
			return "", nil, false
		}
		return fmt.Sprintf("%s ILIKE ?", column), []interface{}{"%" + value + "%"}, true
	case "does_not_contain":
		if column == "created_at" {
			return "", nil, false
		}
		return fmt.Sprintf("%s IS NULL OR %s NOT ILIKE ?", column, column), []interface{}{"%" + value + "%"}, true
	}

	return "", nil, false
}
