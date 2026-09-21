// Package clamav provides a client for ClamAV antivirus scanning.
package clamav

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/connctx"
)

var log = logging.MustGetLogger("clamav")

type Settings struct {
	Host string
	Port uint16
}

type Client interface {
	Close() error

	Scan(ctx context.Context, reader io.Reader) (string, error)
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

func (self *client) Scan(ctx context.Context, data io.Reader) (string, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", self.settings.Host, self.settings.Port))
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	cleanUp := connctx.SetDeadlineAndWatchForCancel(ctx, conn)
	defer cleanUp()

	// send command
	if _, err := fmt.Fprintf(conn, "nINSTREAM\n"); err != nil {
		return "", err
	}

	// send chunks
	chunkSize := make([]byte, 4)
	chunk := make([]byte, 8192)
	for {
		bytesRead, err := data.Read(chunk)
		if err != nil && err != io.EOF {
			return "", err
		}
		binary.BigEndian.PutUint32(chunkSize, uint32(bytesRead))
		if _, err := conn.Write(chunkSize); err != nil {
			return "", err
		}
		if bytesRead == 0 {
			break
		}
		if _, err := conn.Write(chunk[:bytesRead]); err != nil {
			return "", err
		}
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return "", err
	}

	// receive response
	reader := bufio.NewReader(conn)
	text := textproto.NewReader(reader)

	// stream: OK
	// stream: Win.Test.EICAR_HDB-1 FOUND
	line, err := text.ReadLine()
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 || parts[0] != "stream" {
		return "", fmt.Errorf("clamav: failed to parse status line in response %q", line)
	}
	result := strings.TrimSpace(parts[1])
	if result == "OK" {
		return "", nil
	}
	if strings.HasSuffix(result, " FOUND") {
		log.Warningf("found virus: %s", result)
		return result, nil
	}
	return "", fmt.Errorf("clamav: failed to scan for virus: %s", result)
}
