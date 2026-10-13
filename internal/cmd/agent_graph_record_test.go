package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// recordServer answers the record link operations and keeps the variables
// of the last call of each.
func recordServer(test *testing.T) (*httptest.Server, map[string]map[string]any) {
	test.Helper()
	calls := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		raw, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(raw, &body)
		link := `{"id":"l1","nodePath":"people/fixture-friend","nodeName":"Fixture Friend","nodeKind":"person","recordKind":"mail","recordId":"item-1",` +
			`"recordRelation":"received","linkStatus":"proposed","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"",` +
			`"createdAt":"2030-01-02T10:00:00Z","recordTitle":"Fixture subject","recordHappenedAt":"2030-01-02T10:00:00Z","dashboardPath":"/mailbox/starred/item-1","isUnavailable":false}`
		gone := strings.Replace(strings.Replace(link, `"recordId":"item-1"`, `"recordId":"item-2"`, 1), `"isUnavailable":false`, `"isUnavailable":true`, 1)
		for _, name := range []string{"ListAgentRecordLinks", "ReadRecordMemoryLinks", "LinkAgentRecord", "UnlinkAgentRecord", "SetAgentNodeContact"} {
			if !strings.Contains(body.Query, name+"(") {
				continue
			}
			calls[name] = body.Variables
			switch name {
			case "ListAgentRecordLinks":
				_, _ = response.Write([]byte(`{"data":{"ListAgentRecordLinks":{"recordLinks":[` + link + `,` + gone + `],"recordLinkCount":5,"nextOffset":2}}}`))
			case "ReadRecordMemoryLinks":
				_, _ = response.Write([]byte(`{"data":{"ReadRecordMemoryLinks":[` + link + `]}}`))
			case "LinkAgentRecord":
				_, _ = response.Write([]byte(`{"data":{"LinkAgentRecord":` + link + `}}`))
			case "UnlinkAgentRecord":
				_, _ = response.Write([]byte(`{"data":{"UnlinkAgentRecord":true}}`))
			case "SetAgentNodeContact":
				_, _ = response.Write([]byte(`{"data":{"SetAgentNodeContact":true}}`))
			}
			return
		}
		response.WriteHeader(http.StatusBadRequest)
	}))
	test.Cleanup(server.Close)
	return server, calls
}

func runRecordCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, string, error) {
	test.Helper()
	var written, errorsWritten bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &written, ErrWriter: &errorsWritten,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewAgentCommand()},
	}
	err := command.Run(test.Context(), append([]string{"fixture", "agent", "memory"}, arguments...))
	return written.String(), errorsWritten.String(), err
}

func TestLinksListsTheRecordsAPageIsLinkedTo(test *testing.T) {
	test.Parallel()
	server, calls := recordServer(test)
	output, notes, err := runRecordCommand(test, server, "links", "people/fixture-friend")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "perhaps received mail:item-1  Fixture subject  (2 Jan 2030) (the agent's guess)") {
		test.Errorf("a proposed link reads as a guess, with the record's title:\n%s", output)
	}
	if !strings.Contains(output, "mail:item-2 (no longer available)") {
		test.Errorf("a record that is gone says so:\n%s", output)
	}
	if !strings.Contains(notes, "--offset 2") {
		test.Errorf("a part says how to read the next:\n%s", notes)
	}
	if calls["ListAgentRecordLinks"]["path"] != "people/fixture-friend" {
		test.Errorf("the page was asked for: %v", calls)
	}
}

func TestBacklinksNameThePagesLinkedToARecord(test *testing.T) {
	test.Parallel()
	server, calls := recordServer(test)
	output, _, err := runRecordCommand(test, server, "backlinks", "--record", "recording_passage:rec-1@1000-2000")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "perhaps people/fixture-friend received this") {
		test.Errorf("the pages that link to it:\n%s", output)
	}
	variables := calls["ReadRecordMemoryLinks"]
	if variables["recordKind"] != "recording_passage" || variables["recordId"] != "rec-1" || variables["passageStartMS"] != float64(1000) || variables["passageEndMS"] != float64(2000) {
		test.Errorf("a passage is asked for by its range: %v", variables)
	}
	if _, _, err := runRecordCommand(test, server, "backlinks", "--record", "diary:1"); err == nil {
		test.Errorf("an unknown kind of record was accepted")
	}
}

func TestLinkAndUnlinkTakeARecordOrAPage(test *testing.T) {
	test.Parallel()
	server, calls := recordServer(test)
	if _, _, err := runRecordCommand(test, server, "link", "people/fixture-friend", "--record", "mail:item-1"); err == nil {
		test.Errorf("a link to a record needs to say what the page is to it: the page default means nothing here")
	}
	output, _, err := runRecordCommand(test, server, "link", "people/fixture-friend", "--record", "mail:item-1", "--relation", "received", "--note", "asked about the schedule")
	if err != nil || !strings.Contains(output, "people/fixture-friend received mail:item-1  Fixture subject") {
		test.Fatalf("link --record: %v\n%s", err, output)
	}
	variables := calls["LinkAgentRecord"]
	if variables["recordKind"] != "mail" || variables["recordId"] != "item-1" || variables["recordRelation"] != "received" || variables["note"] != "asked about the schedule" {
		test.Errorf("the record link was sent whole: %v", variables)
	}
	output, _, err = runRecordCommand(test, server, "unlink", "people/fixture-friend", "--record", "mail:item-1")
	if err != nil || !strings.Contains(output, "no longer linked to mail:item-1") {
		test.Fatalf("unlink --record: %v\n%s", err, output)
	}
	if _, present := calls["UnlinkAgentRecord"]["recordRelation"]; present {
		test.Errorf("no relation given is every relation: %v", calls["UnlinkAgentRecord"])
	}
	if _, _, err := runRecordCommand(test, server, "link", "people/fixture-friend", "projects/portal", "--record", "mail:item-1", "--relation", "about"); err == nil {
		test.Errorf("a page and a record in one link")
	}
	output, _, err = runRecordCommand(test, server, "contact", "people/fixture-friend", "card-1")
	if err != nil || !strings.Contains(output, "is now about contact card-1") || calls["SetAgentNodeContact"]["contactId"] != "card-1" {
		test.Fatalf("contact: %v\n%s", err, output)
	}
	if _, _, err := runRecordCommand(test, server, "contact", "people/fixture-friend", "--clear"); err != nil || calls["SetAgentNodeContact"]["contactId"] != "" {
		test.Errorf("clearing sends an empty card: %v %v", err, calls["SetAgentNodeContact"])
	}
}
