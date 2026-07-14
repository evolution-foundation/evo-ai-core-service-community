package model

import (
	"encoding/json"
	"testing"

	"evo-ai-core-service/internal/utils/stringutils"
)

// The Custom Tools wizard stores the "what it receives / what it returns"
// descriptions under the reserved __evo_modes_meta__ key of values, as an
// object. values must therefore accept non-string values.
func TestCustomToolRequestAcceptsObjectValues(t *testing.T) {
	payload := []byte(`{
		"name": "advice",
		"method": "GET",
		"endpoint": "https://api.adviceslip.com/advice",
		"headers": {},
		"path_params": {},
		"query_params": {},
		"body_params": {},
		"error_handling": {},
		"values": {
			"lang": "pt",
			"__evo_modes_meta__": {"input": "nothing", "output": "an advice"}
		}
	}`)

	var req CustomToolRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got := req.Values["lang"]; got != "pt" {
		t.Errorf("values[lang] = %v, want pt", got)
	}

	meta, ok := req.Values["__evo_modes_meta__"].(map[string]interface{})
	if !ok {
		t.Fatalf("values[__evo_modes_meta__] = %T, want object", req.Values["__evo_modes_meta__"])
	}
	if meta["output"] != "an advice" {
		t.Errorf("meta[output] = %v, want an advice", meta["output"])
	}
}

func TestToResponsePreservesObjectValues(t *testing.T) {
	tool := &CustomTool{
		Values: stringutils.InterfaceMapToJSON(map[string]interface{}{
			"lang":               "pt",
			"__evo_modes_meta__": map[string]interface{}{"input": "nothing"},
		}),
	}

	res := tool.ToResponse()

	if res.Values["lang"] != "pt" {
		t.Errorf("values[lang] = %v, want pt", res.Values["lang"])
	}
	if _, ok := res.Values["__evo_modes_meta__"].(map[string]interface{}); !ok {
		t.Errorf("values[__evo_modes_meta__] = %T, want object", res.Values["__evo_modes_meta__"])
	}
}

// values is binding:"required": an empty (non-nil) map must still round-trip.
func TestEmptyValuesRoundTrip(t *testing.T) {
	var req CustomToolRequest
	if err := json.Unmarshal([]byte(`{"values": {}}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.Values == nil {
		t.Fatal("values = nil, want empty map")
	}
	if got := stringutils.InterfaceMapToJSON(req.Values); got != "{}" {
		t.Errorf("serialized values = %s, want {}", got)
	}
}
