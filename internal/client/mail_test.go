package client

import (
	"encoding/json"
	"testing"
)

// The filters a command collects have to arrive as the pipeline the API
// declares: one match stage, with several tests joined by "and", because a
// second match stage would be applied after the first and read the same.
func TestStages(t *testing.T) {
	if stages(nil) != nil {
		t.Error("no filters should mean no pipeline, so the server lists everything")
	}

	one := stages([]Filter{{Field: "status", Value: "rejected"}})
	encoded, _ := json.Marshal(one)
	if string(encoded) != `[{"match":{"field":"status","operation":"equal","value":"rejected"}}]` {
		t.Errorf("one filter: %s", encoded)
	}

	two := stages([]Filter{{Field: "kind", Value: "incoming"}, {Field: "subject", Value: "invoice", Contains: true}})
	encoded, _ = json.Marshal(two)
	want := `[{"match":{"filters":[{"field":"kind","operation":"equal","value":"incoming"},{"field":"subject","operation":"contains","value":"invoice"}],"operation":"and"}}]`
	if string(encoded) != want {
		t.Errorf("two filters: %s", encoded)
	}
}
