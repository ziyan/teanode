// Package spamc provides a client for SpamAssassin spam checking.
package spamc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/connctx"
)

var log = logging.MustGetLogger("spamc") //nolint:unused

type Settings struct {
	Host string
	Port uint16
}

type Client interface {
	Close() error

	Check(ctx context.Context, reader io.Reader) (*Result, error)
}

type client struct {
	settings *Settings
}

func Open(settings *Settings) (Client, error) {
	return &client{
		settings: settings,
	}, nil
}

func (self *client) Close() error {
	return nil
}

type Result struct {
	Score   float64
	Symbols []string
}

func (self *client) Check(ctx context.Context, data io.Reader) (*Result, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", self.settings.Host, self.settings.Port))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	cleanUp := connctx.SetDeadlineAndWatchForCancel(ctx, conn)
	defer cleanUp()

	// send request
	if _, err := fmt.Fprintf(conn, "SYMBOLS SPAMC/1.5\r\n\r\n"); err != nil {
		return nil, err
	}
	if _, err := io.Copy(conn, data); err != nil {
		return nil, err
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return nil, err
	}

	// receive response
	reader := bufio.NewReader(conn)
	text := textproto.NewReader(reader)

	// SPAMD/1.1 0 EX_OK
	statusLine, err := text.ReadLine()
	if err != nil {
		return nil, err
	}
	statusParts := strings.SplitN(statusLine, " ", 3)
	if len(statusParts) != 3 || !strings.HasPrefix(statusParts[0], "SPAMD/") {
		return nil, fmt.Errorf("spamc: failed to parse status line in response %q", statusLine)
	}
	code, err := strconv.Atoi(statusParts[1])
	if err != nil {
		return nil, fmt.Errorf("spamc: failed to parse status line in response %q", statusLine)
	}
	if code != 0 {
		return nil, fmt.Errorf("spamc: spamassassin returned error code %d: %s", code, statusParts[2])
	}

	// parse headers
	header, err := text.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	spamHeader := header.Get("Spam")
	if spamHeader == "" {
		return nil, fmt.Errorf("spamc: missing spam header in response")
	}
	spamParts := strings.Split(spamHeader, ";")
	if len(spamParts) != 2 {
		return nil, fmt.Errorf("spamc: invalid spam header in response %q", spamHeader)
	}
	switch strings.ToLower(strings.TrimSpace(spamParts[0])) {
	case "true", "yes":
	case "false", "no":
	default:
		return nil, fmt.Errorf("spamc: invalid spam header in response %q", spamHeader)
	}

	scoreParts := strings.Split(spamParts[1], "/")
	if len(scoreParts) != 2 {
		return nil, fmt.Errorf("spamc: invalid spam header in response %q", spamHeader)
	}
	score, err := strconv.ParseFloat(strings.TrimSpace(scoreParts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("spamc: invalid spam header in response %q", spamHeader)
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(scoreParts[1]), 64); err != nil {
		return nil, fmt.Errorf("spamc: invalid spam header in response %q", spamHeader)
	}

	// read body
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var symbols []string
	if content := strings.ToUpper(strings.TrimSpace(string(body))); content != "" {
		symbols = strings.Split(content, ",")
	}

	return &Result{
		Score:   score,
		Symbols: symbols,
	}, nil
}
