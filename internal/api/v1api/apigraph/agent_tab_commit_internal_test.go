package apigraph

import (
	"encoding/json"
	"testing"
)

// The extension reports a tab committing a document with these names (see
// web/extension/src/tabDocuments.ts), in the `commits` of its update, with
// `currentTab` beside them.
func TestTheExtensionsCommitReportHasTheNamesTheExtensionWrites(t *testing.T) {
	var update struct {
		CurrentTab *int64               `json:"currentTab"`
		Commits    []extensionTabCommit `json:"commits"`
	}
	text := `{"type":"update","currentTab":7,"commits":[{"tab":7,"documentId":"doc-1","url":"https://shop.example.test/a","isBackForward":true,"count":3},{"tab":8,"documentId":"","url":"u","isBackForward":false,"count":1}]}`
	if err := json.Unmarshal([]byte(text), &update); err != nil {
		t.Fatal(err)
	}
	if update.CurrentTab == nil || *update.CurrentTab != 7 || len(update.Commits) != 2 {
		t.Fatalf("read %+v", update)
	}
	if first := update.Commits[0]; first != (extensionTabCommit{Tab: 7, DocumentID: "doc-1", URL: "https://shop.example.test/a", IsBackForward: true, Count: 3}) {
		t.Fatalf("the first commit: %+v", first)
	}
}
