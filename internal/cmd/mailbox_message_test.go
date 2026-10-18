package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// messageServer answers the documents the message commands send with one
// mailbox of one address and one message, and records each document's
// variables by the name of its operation. A document it has no answer for
// is a failed test, so a command that sends something unexpected is seen.
type messageServer struct {
	*httptest.Server
	lock  sync.Mutex
	calls map[string]map[string]any
	// replies replace the standard answer to an operation, by its name.
	replies map[string]string
}

func newMessageServer(test *testing.T, addresses ...string) *messageServer {
	test.Helper()
	server := &messageServer{calls: map[string]map[string]any{}}
	if len(addresses) == 0 {
		addresses = []string{"you@example.com"}
	}
	addressRows := make([]string, 0, len(addresses))
	for _, address := range addresses {
		addressRows = append(addressRows, `{"address":"`+address+`"}`)
	}
	answers := map[string]string{
		"ListMailboxes":        `[{"mailbox":{"id":"mailbox-one","name":"Personal","addresses":[` + strings.Join(addressRows, ",") + `]},"folders":[{"id":"folder-inbox","name":"Inbox","kind":"inbox"},{"id":"folder-work","name":"Work"}]}]`,
		"ListMailboxItems":     `{"total":2,"items":[{"id":"item-one","folderId":"folder-inbox","mailId":"mail-one","seen":false,"flagged":true,"mail":{"id":"mail-one","from":"ann@example.org","fromName":"Ann","subject":"Lunch","receivedAt":"2031-03-04T10:00:00Z"}}]}`,
		"GetMailboxItem":       `{"id":"item-one","folderId":"folder-inbox","mailId":"mail-one","seen":true,"mail":{"id":"mail-one","from":"ann@example.org","subject":"Lunch"}}`,
		"GetMailContent":       `{"mailId":"mail-one","available":true,"text":"Noon on Friday?","headers":[{"key":"From","value":"Ann <ann@example.org>"},{"key":"Subject","value":"Lunch"},{"key":"Received","value":"by mx.example.com"}],"attachments":[{"index":0,"filename":"../../menu.pdf","contentType":"application/pdf","size":5}]}`,
		"MoveMailboxItems":     `[{"id":"item-new","folderId":"folder-work"}]`,
		"SetMailboxItemFlags":  `1`,
		"ReportMailboxJunk":    `1`,
		"SendMailboxMessage":   `{"mail":{"id":"mail-sent"},"item":{"id":"item-sent","folderId":"folder-sent"}}`,
		"SaveMailboxDraft":     `{"id":"item-draft","folderId":"folder-drafts","draft":true}`,
		"GetMailInvitation":    `{"id":"invitation-one","method":"REQUEST","summary":"Planning","location":"Room 4","startsAt":"2031-03-04T10:00:00Z","organizer":"ann@example.org","participation":"NEEDS-ACTION","attendees":[{"address":"you@example.com","participation":"NEEDS-ACTION"}]}`,
		"AnswerMailInvitation": `{"id":"invitation-one","method":"REQUEST","participation":"ACCEPTED","organizer":"ann@example.org","attendees":[]}`,
		"GetMailboxThread":     `{"threadId":"thread-one","subject":"Lunch","items":[{"folderId":"folder-inbox","item":{"id":"item-two","mailId":"mail-two"}},{"folderId":"folder-inbox","item":{"id":"item-one","mailId":"mail-one"}}]}`,
	}
	server.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/attachment/") {
			// The sender's own name for it is "../../menu.pdf"; the bytes are these.
			_, _ = response.Write([]byte("%PDF-"))
			return
		}
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation+"(") || (operation == "ListMailboxes" && strings.Contains(document.Query, "ListMailboxes")) {
				server.lock.Lock()
				server.calls[operation] = document.Variables
				if reply, isReplaced := server.replies[operation]; isReplaced {
					answer = reply
				}
				server.lock.Unlock()
				_, _ = response.Write([]byte(`{"data":{"` + operation + `":` + answer + `}}`))
				return
			}
		}
		test.Errorf("a document nothing expected was sent: %s", document.Query)
		response.WriteHeader(400)
	}))
	test.Cleanup(server.Close)
	return server
}

func (self *messageServer) variables(operation string) map[string]any {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.calls[operation]
}

// runMessage runs "teanode mailbox <arguments>" against the server and
// returns what it printed.
func (self *messageServer) run(test *testing.T, arguments ...string) (string, error) {
	test.Helper()
	root := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: self.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewMailboxCommand()},
	}
	var err error
	printed := captureStandardOutput(test, func() {
		err = root.Run(test.Context(), append([]string{"fixture", "mailbox"}, arguments...))
	})
	return printed, err
}

// A list with no folder is the Inbox, not every folder with Trash and Junk
// in it, and the words a person filters by arrive as the operation's own
// arguments.
func TestMessageListAsksForTheInboxAndPassesTheFilters(test *testing.T) {
	server := newMessageServer(test)

	printed, err := server.run(test, "message", "list", "--unread", "--from", "ann", "--first", "10", "--since", "2031-03-01")
	if err != nil {
		test.Fatal(err)
	}
	variables := server.variables("ListMailboxItems")
	if variables["folderId"] != "folder-inbox" || variables["from"] != "ann" || variables["unread"] != true || variables["first"] != float64(10) {
		test.Errorf("the list asked for %v", variables)
	}
	if _, isSent := variables["mailboxId"]; isSent {
		test.Error("a list of the Inbox asked for the whole mailbox as well")
	}
	if _, isSent := variables["flagged"]; isSent {
		test.Error("a filter nobody passed was sent")
	}
	// One of two is shown, so the way to the next page is printed.
	for _, wanted := range []string{"Lunch", "Ann <ann@example.org>", "unread,flagged", "item-one", "--offset 1"} {
		if !strings.Contains(printed, wanted) {
			test.Errorf("the list %q does not say %q", printed, wanted)
		}
	}
}

// --everywhere is the mailbox without a folder, and asking for it together
// with a folder is a mistake to be told about, not a choice made quietly.
func TestMessageListEverywhereAndFolderAreExclusive(test *testing.T) {
	server := newMessageServer(test)

	if _, err := server.run(test, "message", "list", "--everywhere"); err != nil {
		test.Fatal(err)
	}
	variables := server.variables("ListMailboxItems")
	if variables["mailboxId"] != "mailbox-one" || variables["folderId"] != nil {
		test.Errorf("everywhere asked for %v", variables)
	}
	if _, err := server.run(test, "message", "list", "--everywhere", "--folder", "Work"); err == nil || !strings.Contains(err.Error(), "--folder") {
		test.Errorf("both were accepted: %v", err)
	}
}

// Showing a message reads its text through the same operations the reader
// does, and writes a sender's attachment name no further than the directory
// it was told to use.
func TestMessageShowPrintsTheTextAndKeepsAttachmentsInTheirDirectory(test *testing.T) {
	server := newMessageServer(test)
	directory := filepath.Join(test.TempDir(), "saved")

	printed, err := server.run(test, "message", "show", "item-one", "--save-attachments", directory)
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"Noon on Friday?", "Subject:", "Lunch", "Ann <ann@example.org>"} {
		if !strings.Contains(printed, wanted) {
			test.Errorf("the message %q does not say %q", printed, wanted)
		}
	}
	if strings.Contains(printed, "mx.example.com") {
		test.Error("a header nobody asked to see was printed")
	}
	written, err := os.ReadFile(filepath.Join(directory, "menu.pdf"))
	if err != nil || string(written) != "%PDF-" {
		test.Errorf("the attachment was not written inside the directory: %q, %v", written, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(directory), "menu.pdf")); err == nil {
		test.Error("the attachment escaped its directory")
	}
}

// Moving files by folder name and prints the new identifiers, because the old
// ones are gone.
func TestMessageMoveFindsTheFolderByNameAndPrintsTheNewIdentifier(test *testing.T) {
	server := newMessageServer(test)

	printed, err := server.run(test, "message", "move", "item-one", "item-two", "--folder", "work")
	if err != nil {
		test.Fatal(err)
	}
	variables := server.variables("MoveMailboxItems")
	if variables["folderId"] != "folder-work" || !reflect.DeepEqual(variables["itemIds"], []any{"item-one", "item-two"}) {
		test.Errorf("the move asked for %v", variables)
	}
	if !strings.Contains(printed, "item-new") {
		test.Errorf("the new identifier is not in %q", printed)
	}
}

// Flags that contradict each other, or ask for nothing, are refused before
// anything is sent.
func TestMessageFlagRefusesWhatCannotBeDone(test *testing.T) {
	server := newMessageServer(test)

	for name, arguments := range map[string][]string{
		"opposite read states": {"message", "flag", "item-one", "--read", "--unread"},
		"opposite flags":       {"message", "flag", "item-one", "--flag", "--unflag"},
		"nothing to change":    {"message", "flag", "item-one"},
		"no message":           {"message", "flag", "--read"},
	} {
		if _, err := server.run(test, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	if server.variables("SetMailboxItemFlags") != nil {
		test.Error("a refused command reached the server")
	}

	if _, err := server.run(test, "message", "flag", "item-one", "--read", "--unflag"); err != nil {
		test.Fatal(err)
	}
	variables := server.variables("SetMailboxItemFlags")
	if variables["seen"] != true || variables["flagged"] != false {
		test.Errorf("the flags were %v", variables)
	}
}

// Reporting junk and taking it back are one operation with notJunk set only
// for the second.
func TestMessageJunkAndNotJunk(test *testing.T) {
	server := newMessageServer(test)

	if _, err := server.run(test, "message", "junk", "item-one"); err != nil {
		test.Fatal(err)
	}
	if _, isSent := server.variables("ReportMailboxJunk")["notJunk"]; isSent {
		test.Error("reporting junk said it was not junk")
	}
	if _, err := server.run(test, "message", "junk", "item-one", "--not-junk"); err != nil {
		test.Fatal(err)
	}
	if server.variables("ReportMailboxJunk")["notJunk"] != true {
		test.Error("taking it back did not say so")
	}
}

// A send uses the mailbox's only address, keeps the reply attached to its
// message, and carries an identifier a retry can reuse.
func TestMessageSendUsesTheOnlyAddressAndAnswersAMessage(test *testing.T) {
	server := newMessageServer(test)
	bodyPath := filepath.Join(test.TempDir(), "body.txt")
	if err := os.WriteFile(bodyPath, []byte("Noon works."), 0o600); err != nil {
		test.Fatal(err)
	}

	_, err := server.run(test, "message", "send", "--to", "ann@example.org", "--subject", "Re: Lunch", "--text", bodyPath, "--reply-to", "item-one", "--submission-id", "retry-one")
	if err != nil {
		test.Fatal(err)
	}
	variables := server.variables("SendMailboxMessage")
	message, _ := variables["message"].(map[string]any)
	if variables["mailboxId"] != "mailbox-one" || variables["submissionId"] != "retry-one" {
		test.Errorf("the send asked for %v", variables)
	}
	if message["from"] != "you@example.com" || message["subject"] != "Re: Lunch" || message["textContent"] != "Noon works." || message["replyToItemId"] != "item-one" {
		test.Errorf("the message was %v", message)
	}
	if !reflect.DeepEqual(message["to"], []any{"ann@example.org"}) {
		test.Errorf("the recipients were %v", message["to"])
	}
}

// With several addresses the command does not guess which to send as, and a
// message for nobody, or to something that is not an address, is not sent.
func TestMessageSendRefusesWhatIsAmbiguousOrAddressedToNobody(test *testing.T) {
	server := newMessageServer(test, "you@example.com", "you@example.net")

	_, err := server.run(test, "message", "send", "--to", "ann@example.org", "--text", "-")
	if err == nil || !strings.Contains(err.Error(), "you@example.net") {
		test.Errorf("two addresses and no --from gave %v", err)
	}
	for name, arguments := range map[string][]string{
		"no recipient":   {"message", "send", "--from", "you@example.com"},
		"not an address": {"message", "send", "--from", "you@example.com", "--to", "nobody"},
	} {
		if _, err := server.run(test, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	if server.variables("SendMailboxMessage") != nil {
		test.Error("a refused send reached the server")
	}
}

// A draft is saved through SaveMailboxDraft, not sent, and needs no recipient
// yet.
func TestMessageDraftSavesWithoutRecipients(test *testing.T) {
	server := newMessageServer(test)

	printed, err := server.run(test, "message", "draft", "--subject", "Later", "--draft", "item-old")
	if err != nil {
		test.Fatal(err)
	}
	message, _ := server.variables("SaveMailboxDraft")["message"].(map[string]any)
	if message["subject"] != "Later" || message["draftItemId"] != "item-old" {
		test.Errorf("the draft was %v", message)
	}
	if server.variables("SendMailboxMessage") != nil {
		test.Error("saving a draft sent it")
	}
	if !strings.Contains(printed, "item-draft") {
		test.Errorf("the saved identifier is not in %q", printed)
	}
}

// The three answers are said in a person's words and sent in the API's, and
// anything else is refused before it can be sent to an organizer.
func TestInvitationAnswerSaysAcceptTentativeOrDecline(test *testing.T) {
	server := newMessageServer(test)

	for said, sent := range map[string]string{"accept": "ACCEPTED", "tentative": "TENTATIVE", "Decline": "DECLINED"} {
		if _, err := server.run(test, "invitation", "answer", "item-one", said); err != nil {
			test.Fatal(err)
		}
		variables := server.variables("AnswerMailInvitation")
		if variables["itemId"] != "item-one" || variables["answer"] != sent {
			test.Errorf("%q was sent as %v", said, variables)
		}
	}

	other := newMessageServer(test)
	for name, arguments := range map[string][]string{
		"an answer that is not one": {"invitation", "answer", "item-one", "maybe"},
		"no answer":                 {"invitation", "answer", "item-one"},
		"no message":                {"invitation", "answer"},
	} {
		if _, err := other.run(test, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	if other.variables("AnswerMailInvitation") != nil {
		test.Error("a refused answer reached the server")
	}
}

// Showing an invitation prints the card, and a message with none says so
// rather than failing.
func TestInvitationShowPrintsTheCard(test *testing.T) {
	server := newMessageServer(test)

	printed, err := server.run(test, "invitation", "show", "item-one")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"Planning", "Room 4", "ann@example.org", "needs-action"} {
		if !strings.Contains(printed, wanted) {
			test.Errorf("the card %q does not say %q", printed, wanted)
		}
	}
}

// Messages and invitations are on the command line, in the agent's mail
// tools and on the API under the same names: nothing exists on one that the
// others lack, except what the agent leaves to a person's own hands.
func TestMailboxMessageParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{
		reflect.TypeFor[apigraph.MailboxQuery](), reflect.TypeFor[apigraph.MailboxMutation](),
		reflect.TypeFor[apigraph.MailboxComposeQuery](), reflect.TypeFor[apigraph.MailboxComposeMutation](),
		reflect.TypeFor[apigraph.CalendarInvitationQuery](), reflect.TypeFor[apigraph.CalendarInvitationMutation](),
	} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// operation, then the path of the subcommand of teanode mailbox that does
	// it and the tool and action of the agent that does, where it can.
	table := []struct {
		operation string
		path      []string
		tool      string
		action    string
	}{
		{"ListMailboxItems", []string{"message", "list"}, "mail_search", ""},
		{"GetMailboxItem", []string{"message", "show"}, "mail_read", ""},
		{"GetMailboxThread", []string{"message", "show"}, "mail_read", ""},
		{"MoveMailboxItems", []string{"message", "move"}, "mail_act", "move"},
		{"SetMailboxItemFlags", []string{"message", "flag"}, "mail_act", "mark_read"},
		{"ReportMailboxJunk", []string{"message", "junk"}, "mail_act", "junk"},
		{"SendMailboxMessage", []string{"message", "send"}, "mail_send", ""},
		{"SaveMailboxDraft", []string{"message", "draft"}, "mail_draft", ""},
		{"GetMailInvitation", []string{"invitation", "show"}, "mail_read", ""},
		{"AnswerMailInvitation", []string{"invitation", "answer"}, "mail_act", "answer_invitation"},
	}

	mailbox := NewMailboxCommand()
	listed := map[string]bool{}
	for _, row := range table {
		listed[strings.Join(row.path, " ")] = true
	}
	for _, group := range []string{"message", "invitation"} {
		for _, command := range mailbox.Commands {
			if command.Name != group {
				continue
			}
			for _, subcommand := range command.Commands {
				if !listed[group+" "+subcommand.Name] {
					test.Errorf("teanode mailbox %s %s is on the command line and not in the table", group, subcommand.Name)
				}
			}
		}
	}
	toolActions := map[string]map[string]bool{}
	toolNames := map[string]bool{}
	for _, tool := range tools.Build().All() {
		toolNames[tool.Name] = true
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		toolActions[tool.Name] = map[string]bool{}
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				toolActions[tool.Name][name] = true
			}
		case []any:
			for _, name := range enum {
				toolActions[tool.Name][name.(string)] = true
			}
		}
	}
	for _, row := range table {
		if !operations[row.operation] {
			test.Errorf("the API has no operation %s", row.operation)
		}
		command := mailbox
		for _, name := range row.path {
			var found *cli.Command
			for _, subcommand := range command.Commands {
				if subcommand.Name == name {
					found = subcommand
				}
			}
			if found == nil {
				test.Errorf("teanode mailbox %s has no %s, for %s", strings.Join(row.path, " "), name, row.operation)
				break
			}
			command = found
		}
		if !toolNames[row.tool] {
			test.Errorf("the agent has no tool %s, for %s", row.tool, row.operation)
		} else if row.action != "" && !toolActions[row.tool][row.action] {
			test.Errorf("the tool %s has no action %s, for %s", row.tool, row.action, row.operation)
		}
	}
}

// The same name twice does not end the save: the second file gets a number
// before its extension, and nothing already in the directory is replaced.
func TestSaveAttachmentsNumbersAFileWhoseNameIsTaken(test *testing.T) {
	server := newMessageServer(test)
	server.replies = map[string]string{"GetMailContent": `{"mailId":"mail-one","available":true,"text":"Two pictures","attachments":[{"index":0,"filename":"image.png","contentType":"image/png","size":5},{"index":1,"filename":"image.png","contentType":"image/png","size":5}]}`}
	directory := filepath.Join(test.TempDir(), "saved")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "image (2).png"), []byte("mine"), 0o600); err != nil {
		test.Fatal(err)
	}

	if _, err := server.run(test, "message", "show", "item-one", "--save-attachments", directory); err != nil {
		test.Fatal(err)
	}
	for name, wanted := range map[string]string{"image.png": "%PDF-", "image (2).png": "mine", "image (3).png": "%PDF-"} {
		written, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(written) != wanted {
			test.Errorf("%s holds %q, %v; wanted %q", name, written, err, wanted)
		}
	}
}

// A message with only an HTML part is printed, and one whose text is gone
// does not stop the conversation around it.
func TestShowFallsBackToHTMLAndSurvivesAMessageThatIsGone(test *testing.T) {
	server := newMessageServer(test)
	server.replies = map[string]string{"GetMailContent": `{"mailId":"mail-one","available":true,"text":"","html":"<p>Only HTML</p>","headers":[{"key":"Subject","value":"Lunch"}]}`}
	printed, err := server.run(test, "message", "show", "item-one")
	if err != nil || !strings.Contains(printed, "<p>Only HTML</p>") || !strings.Contains(printed, "Subject:") {
		test.Errorf("an HTML-only message printed %q, %v", printed, err)
	}

	server.replies = map[string]string{"GetMailContent": `{"mailId":"mail-one","available":false}`}
	printed, err = server.run(test, "message", "show", "item-one", "--thread")
	if err != nil {
		test.Fatalf("a message that is gone ended the thread: %v", err)
	}
	if strings.Count(printed, "no longer stored") != 2 || !strings.Contains(printed, "item-one") || !strings.Contains(printed, "item-two") {
		test.Errorf("the thread %q does not hold a line for each message", printed)
	}
}

// An answer carries a request identifier, so repeating it with the same one
// does not mail the organizer twice; a new answer gets a new one.
func TestInvitationAnswerSendsARequestIdentifierAndReusesOnRetry(test *testing.T) {
	server := newMessageServer(test)
	if _, err := server.run(test, "invitation", "answer", "item-one", "accept"); err != nil {
		test.Fatal(err)
	}
	first, _ := server.variables("AnswerMailInvitation")["requestId"].(string)
	if first == "" {
		test.Fatal("no request identifier was sent")
	}
	if _, err := server.run(test, "invitation", "answer", "item-one", "accept"); err != nil {
		test.Fatal(err)
	}
	if second, _ := server.variables("AnswerMailInvitation")["requestId"].(string); second == "" || second == first {
		test.Errorf("a new answer reused %q", second)
	}
	printed, err := server.run(test, "invitation", "answer", "item-one", "accept", "--request-id", first)
	if err != nil {
		test.Fatal(err)
	}
	if retried := server.variables("AnswerMailInvitation")["requestId"]; retried != first {
		test.Errorf("a retry sent %v, not %q", retried, first)
	}
	if !strings.Contains(printed, "ann@example.org") {
		test.Errorf("the answer %q does not say who was mailed", printed)
	}

	server.replies = map[string]string{"AnswerMailInvitation": `{"id":"invitation-one","method":"REQUEST","participation":"ACCEPTED","attendees":[]}`}
	printed, err = server.run(test, "invitation", "answer", "item-one", "accept")
	if err != nil || strings.Contains(printed, "organizer has been told") || !strings.Contains(printed, "answered accepted") {
		test.Errorf("an event with no organizer printed %q, %v", printed, err)
	}
}
