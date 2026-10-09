package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A search sends the offset it was given, so that the command line reads
// the next page of a search and not the first one again, and reads back
// how many more there are and where the next page starts.
func TestSearchesSendTheirOffsetAndReadWhereTheNextPageStarts(test *testing.T) {
	variables := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		variables <- document.Variables
		response.Header().Set("Content-Type", "application/json")
		if document.Query == DocumentSearchAgentGraph {
			_, _ = response.Write([]byte(`{"data":{"SearchAgentGraph":{"nodes":[],"facts":[],` +
				`"moreNodeCount":3,"isMoreNodeCountLowerBound":false,"moreFactCount":40,"isMoreFactCountLowerBound":true,"nextOffset":60}}}`))
			return
		}
		_, _ = response.Write([]byte(`{"data":{"SearchAgentDocuments":{"passages":[],"definitions":[],"meaningful":true,` +
			`"moreCount":12,"isMoreCountLowerBound":true,"nextOffset":24}}}`))
	}))
	defer server.Close()
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}

	graphFound, err := SearchAgentGraph(test.Context(), connection, "the boat", 20, 40)
	if err != nil {
		test.Fatalf("SearchAgentGraph: %s", err)
	}
	if sent := <-variables; sent["offset"] != float64(40) || sent["first"] != float64(20) {
		test.Errorf("the memory search sends its offset and size: %v", sent)
	}
	if graphFound.NextOffset != 60 || graphFound.MoreFactCount != 40 || !graphFound.IsMoreFactCountLowerBound || graphFound.MoreNodeCount != 3 {
		test.Errorf("the memory search reads back what is left: %+v", graphFound)
	}

	documentsFound, err := SearchAgentDocuments(test.Context(), connection, &AgentDocumentQuery{Words: "kestrel", First: 12, Offset: 12})
	if err != nil {
		test.Fatalf("SearchAgentDocuments: %s", err)
	}
	if sent := <-variables; sent["offset"] != float64(12) {
		test.Errorf("the knowledge search sends its offset: %v", sent)
	}
	if documentsFound.NextOffset != 24 || documentsFound.MoreCount != 12 || !documentsFound.IsMoreCountLowerBound {
		test.Errorf("the knowledge search reads back what is left: %+v", documentsFound)
	}
}
