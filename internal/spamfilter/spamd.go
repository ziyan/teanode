package spamfilter

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/bufferpool"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/spamc"
)

// spamd scores messages with an external SpamAssassin daemon.
type spamd struct {
	client spamc.Client
}

// NewSpamd returns a Filter backed by the daemon at host and port.
func NewSpamd(host string, port uint16) (Filter, error) {
	client, err := spamc.Open(&spamc.Settings{Host: host, Port: port})
	if err != nil {
		return nil, fmt.Errorf("cannot connect to spamd at %s:%d: %w", host, port, err)
	}
	return &spamd{client: client}, nil
}

func (self *spamd) Close() error {
	return self.client.Close()
}

// Check hands the message to the daemon.
//
// This is where the message is glued back together. The server split it into
// headers and body before any check ran, and the daemon takes a byte stream,
// so the two have to be rejoined here. Nothing else does this, and the
// strainer must not: it reads the parsed form.
func (self *spamd) Check(ctx context.Context, message *Message) (*models.SpamFilterResult, error) {
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()

	if err := mailparse.Unsplit(buffer, message.Body, message.Headers); err != nil {
		return nil, err
	}

	result, err := self.client.Check(ctx, buffer)
	if err != nil {
		return nil, err
	}

	// No Checks: the protocol reports symbol names without the points each
	// one contributed, so there is nothing honest to put there.
	return &models.SpamFilterResult{
		Score:   result.Score,
		Symbols: result.Symbols,
	}, nil
}
