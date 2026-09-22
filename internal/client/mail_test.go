package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSendMailKeepsTheLegacyWireShape(test *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			response.WriteHeader(400)
			return
		}
		requests <- string(body)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"SendMail":{"mail":{"id":"fixture-mail"}}}}`))
	}))
	defer server.Close()
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}
	mail, err := SendMail(test.Context(), connection, "fixture-domain", &MessageParameters{From: "sender@example.com", To: []string{"recipient@example.net"}, TextContent: "Fixture"})
	if err != nil || mail == nil || mail.ID != "fixture-mail" {
		test.Fatalf("legacy send=%+v, %v", mail, err)
	}
	if request := <-requests; strings.Contains(request, "submissionId") {
		test.Fatal("legacy call requires the new submission API")
	}
}
