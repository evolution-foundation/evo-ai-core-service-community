package model

// AgentListFilter is one advanced-filter clause sent by the Agents list screen
// (filters[i][attribute_key|filter_operator|values|query_operator]). Values is
// the comma-split list; QueryOperator ("and"/"or") joins this clause to the
// previous one.
type AgentListFilter struct {
	AttributeKey   string
	FilterOperator string
	QueryOperator  string
	Values         []string
}
