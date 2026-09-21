package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
)

// standingIn is a server that answers the protocol endpoint with whatever
// it is told to, and records what it was sent.
func standingIn(test *testing.T, answer func(body string) (int, string)) (*client.Client, *[]string) {
	test.Helper()
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != api.PathAgentMCP {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(request.Body)
		sent = append(sent, string(body))
		status, reply := answer(string(body))
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_, _ = response.Write([]byte(reply))
	}))
	test.Cleanup(server.Close)

	connection, err := client.New(client.Options{URL: server.URL, Token: "a-token"})
	if err != nil {
		test.Fatalf("client.New: %s", err)
	}
	return connection, &sent
}

// One message in, one line out, and the harness's own bytes reach the
// server untouched.
func TestThePipeCarriesAMessageEachWay(test *testing.T) {
	connection, sent := standingIn(test, func(string) (int, string) {
		// With the newline the server's own encoder puts on the end, which
		// is what the pipe has to cope with.
		return http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}` + "\n"
	})

	out := &strings.Builder{}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	if err := pipeMCP(context.Background(), connection, in, out, io.Discard); err != nil {
		test.Fatalf("the pipe stopped: %s", err)
	}

	if len(*sent) != 1 || !strings.Contains((*sent)[0], `"method":"initialize"`) {
		test.Fatalf("the server was sent %v", *sent)
	}
	// Exactly one line: a blank line between messages is a message some
	// harnesses will not read past.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		test.Fatalf("the pipe wrote %d lines: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-03-26"`) {
		test.Fatalf("the answer came back as %q", lines[0])
	}
}

// A notification is answered by the server with nothing at all, and the
// pipe must put nothing on the wire: a harness matches answers to requests
// by id, and a message it did not ask for has no id to match.
func TestANotificationPutsNothingOnThePipe(test *testing.T) {
	connection, sent := standingIn(test, func(string) (int, string) {
		return http.StatusAccepted, ""
	})

	out := &strings.Builder{}
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	if err := pipeMCP(context.Background(), connection, in, out, io.Discard); err != nil {
		test.Fatalf("the pipe stopped: %s", err)
	}

	if len(*sent) != 1 {
		test.Fatalf("the notification was not forwarded: %v", *sent)
	}
	if out.String() != "" {
		test.Fatalf("the pipe wrote %q for a notification", out.String())
	}
}

// Several messages keep their order and stay one to a line.
func TestMessagesKeepTheirOrderAndTheirLines(test *testing.T) {
	answers := map[string]string{
		"1": `{"jsonrpc":"2.0","id":1,"result":{"first":true}}`,
		"2": `{"jsonrpc":"2.0","id":2,"result":{"second":true}}`,
	}
	connection, _ := standingIn(test, func(body string) (int, string) {
		for id, answer := range answers {
			if strings.Contains(body, `"id":`+id) {
				return http.StatusOK, answer + "\n"
			}
		}
		// A message with no id, which the server accepts and says nothing
		// to, the way it does for a notification.
		return http.StatusAccepted, ""
	})

	out := &strings.Builder{}
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/cancelled"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	if err := pipeMCP(context.Background(), connection, in, out, io.Discard); err != nil {
		test.Fatalf("the pipe stopped: %s", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		test.Fatalf("the pipe wrote %d lines: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "first") || !strings.Contains(lines[1], "second") {
		test.Fatalf("the answers came back as %q", out.String())
	}
}

// Something in the way answering with a page rather than a message must
// not be written onto the pipe: a harness reading it would report the
// protocol as broken rather than the connection.
func TestAnAnswerThatIsNotAMessageIsNotWrittenOnThePipe(test *testing.T) {
	connection, _ := standingIn(test, func(string) (int, string) {
		return http.StatusBadGateway, "<html><body>502 Bad Gateway</body></html>"
	})

	out := &strings.Builder{}
	complaints := &strings.Builder{}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")

	err := pipeMCP(context.Background(), connection, in, out, complaints)
	if err == nil {
		test.Fatal("a page in place of a message was taken as an answer")
	}
	if out.String() != "" {
		test.Fatalf("the pipe wrote %q", out.String())
	}
	if !strings.Contains(complaints.String(), "not JSON") {
		test.Fatalf("what went wrong was not said: %q", complaints.String())
	}
}

// A blank line between messages is not a message.
func TestBlankLinesArePassedOver(test *testing.T) {
	connection, sent := standingIn(test, func(string) (int, string) {
		return http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`
	})

	out := &strings.Builder{}
	in := strings.NewReader("\n\n" + `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n\n")
	if err := pipeMCP(context.Background(), connection, in, out, io.Discard); err != nil {
		test.Fatalf("the pipe stopped: %s", err)
	}
	if len(*sent) != 1 {
		test.Fatalf("%d messages were forwarded, not 1: %v", len(*sent), *sent)
	}
}
