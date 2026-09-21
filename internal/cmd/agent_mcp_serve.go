package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
)

// The protocol on a terminal's standard input and output, for a harness
// that would rather run a command than hold a URL and a token.
//
// It is a pipe, not a second implementation. Every message read here is
// posted to the same endpoint the server answers over HTTP, with the
// profile's own credential, and the answer is written back. A harness on
// the same machine as somebody's command line is therefore connected by:
//
//	claude mcp add teanode -- teanode agent mcp serve
//
// with nothing pasted and nothing to keep up to date. Whatever the server
// offers, this offers, including anything added to it later.

// mcpLineBytes bounds one message read from the harness. The same bound
// the server puts on one it receives.
const mcpLineBytes = 1 << 20

func newAgentMCPServeCommand() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "answer the Model Context Protocol on this terminal, so a program here can use your agent's tools",
		Description: "Speaks the protocol on standard input and output and passes it to your server, " +
			"as you. For a coding harness or an editor on this machine: point it at this command " +
			"rather than at a URL, and it uses the credential this command line already has.\n\n" +
			"This is the opposite direction from the rest of `teanode agent mcp`, which is about " +
			"servers your agent connects out to.",
		Action: runAgentMCPServe,
	}
}

func runAgentMCPServe(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	// Nothing is printed to standard output but protocol, because the
	// harness is reading it. Anything worth saying goes to standard error,
	// where a harness shows it as the server's log.
	return pipeMCP(ctx, connection, os.Stdin, os.Stdout, command.ErrWriter)
}

// pipeMCP carries messages between a harness and the server until the
// harness closes its end.
func pipeMCP(ctx context.Context, connection *client.Client, in io.Reader, out io.Writer, complaints io.Writer) error {
	reader := bufio.NewReaderSize(in, mcpLineBytes)
	writer := bufio.NewWriter(out)

	for {
		line, err := readMCPLine(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		answer, err := forwardMCP(ctx, connection, line)
		if err != nil {
			// A failure to reach the server is this program's to report,
			// not the protocol's: a harness that gets a JSON-RPC error
			// shows it as the tool failing, and the tool is fine. Said on
			// standard error, and the exchange ends, which is what a
			// harness treats as the server going away.
			_, _ = fmt.Fprintf(complaints, "teanode: %s\n", err)
			return err
		}
		if len(answer) == 0 {
			// A notification. Nothing goes back, and writing anything
			// would put a message on the pipe the harness is not
			// expecting and cannot match to a request.
			continue
		}
		if _, err := writer.Write(answer); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
		// Flushed every message, because the harness is waiting for this
		// one before it sends the next. A buffer that holds it is a
		// harness that hangs.
		if err := writer.Flush(); err != nil {
			return err
		}
	}
}

// readMCPLine reads one message, refusing one longer than the bound rather
// than growing to hold it.
func readMCPLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if len(line) > 0 && err == io.EOF {
		// A last message with no newline after it.
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if len(line) >= mcpLineBytes {
		return nil, fmt.Errorf("a message longer than %d bytes arrived", mcpLineBytes)
	}
	return line, nil
}

// forwardMCP posts one message and returns the answer, or nothing at all
// where the server had nothing to say.
func forwardMCP(ctx context.Context, connection *client.Client, message []byte) ([]byte, error) {
	response, err := connection.PostJSON(ctx, api.PathAgentMCP, message)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, mcpLineBytes))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}
	// The body is passed through as it came rather than decoded and
	// re-encoded: whatever the server said is what the harness should
	// read, including anything this version of the command line has never
	// heard of. It is only checked for being JSON at all, so that an
	// error page from something in the way is reported as this program's
	// problem rather than written onto the protocol's pipe.
	if !json.Valid(body) {
		return nil, fmt.Errorf("the server answered HTTP %d with something that is not JSON", response.StatusCode)
	}
	// Trimmed, because the newline between messages is this pipe's to
	// write. The server's body ends with one of its own, and passing both
	// through puts a blank line on the pipe between every pair of
	// messages.
	return []byte(strings.TrimRight(string(body), "\r\n")), nil
}
