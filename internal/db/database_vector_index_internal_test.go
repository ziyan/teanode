package db

import (
	"strings"
	"testing"
)

func TestVectorIndexNamesKeepDistinctModels(test *testing.T) {
	indexNames := map[string]bool{}
	for _, modelName := range []string{"fixture:a", "fixture-a", "FIXTURE:a", strings.Repeat("prefix", 20) + "a", strings.Repeat("prefix", 20) + "b"} {
		for _, dimension := range []int{32, 64} {
			indexName := vectorIndexName(AgentNodeTable, modelName, dimension)
			if len(indexName) > 63 || indexNames[indexName] {
				test.Fatalf("colliding or oversized index name: %q", indexName)
			}
			indexNames[indexName] = true
			if indexName != vectorIndexName(AgentNodeTable, modelName, dimension) {
				test.Fatal("index name is not stable")
			}
		}
	}
}
