package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/bimi"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/safefetch"
)

// Fetching the logo a sending domain publishes for its mail.
//
// The address is in DNS and the file is on the sender's server, so fetching it
// when somebody opens a message would tell that sender which address read
// which message at what moment — the thing the remote image proxy exists to
// prevent. It is fetched here instead, once per domain, and the dashboard only
// ever asks this server for it.
//
// Nothing waits on this. A subscription whose logo has not been fetched yet
// shows a monogram, and the logo appears the next time the page is opened.
const (
	// How many domains one pass looks up. Small: each is a DNS query and
	// possibly an HTTPS fetch of somebody else's server.
	logoBatch = 10

	// How long a lookup stands before it is asked again. A brand changes its
	// mark rarely, and a domain that publishes nothing should not be asked
	// about on every pass.
	logoLifetime = 24 * time.Hour

	// Enough for a mark drawn as an SVG, and small enough that a hostile
	// server cannot use this to fill a disk.
	logoMaximumSize = 256 << 10
)

// fetchSenderLogos looks up the domains that have not been asked about lately
// and stores what they publish, including publishing nothing. It returns how
// many were looked at.
func fetchSenderLogos(ctx context.Context, database db.Database, nameserver string) (int, error) {
	var domains []string
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListSenderDomainsWithoutLogo(time.Now().Add(-logoLifetime), logoBatch)
		domains = found
		return err
	}); err != nil {
		return 0, err
	}
	if len(domains) == 0 {
		return 0, nil
	}

	found := 0
	for _, domain := range domains {
		if ctx.Err() != nil {
			return found, ctx.Err()
		}
		logo := &db.BimiLogo{
			Domain:    domain,
			Selector:  bimi.DefaultSelector,
			CheckedAt: time.Now(),
		}
		record, published, err := bimi.Lookup(ctx, nameserver, domain, bimi.DefaultSelector)
		switch {
		case err != nil:
			logo.Error = err.Error()
		case !published || record.Logo == "":
			// Nothing published, which is the ordinary answer and worth
			// remembering so it is not asked again today.
		default:
			logo.LogoURL = record.Logo
			content, contentType, err := fetchLogo(ctx, record.Logo)
			if err != nil {
				logo.Error = err.Error()
			} else {
				logo.Content = content
				logo.ContentType = contentType
				found++
			}
		}
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.SaveBimiLogo(logo)
		}); err != nil {
			return found, err
		}
	}
	if found > 0 {
		log.Noticef("fetched the published logo of %d of %d sending domains", found, len(domains))
	}
	return len(domains), nil
}

// fetchLogo gets the file itself, through the same guard every other fetch of
// a stranger's address goes through.
func fetchLogo(ctx context.Context, address string) ([]byte, string, error) {
	target, err := safefetch.ParseTarget(address)
	if err != nil {
		return nil, "", err
	}
	if target.Scheme != "https" {
		return nil, "", errors.New("a logo is only fetched over https")
	}
	timed, cancel := context.WithTimeout(ctx, safefetch.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(timed, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := safefetch.Client().Do(request)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Debugf("failed to close the logo response: %s", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("the sender's server answered %s", response.Status)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	// BIMI is SVG and nothing else. A server that answers with something else
	// is answering a different question, and rendering whatever came back
	// would be rendering a stranger's choice of file type.
	if contentType != "image/svg+xml" && contentType != "image/svg" {
		return nil, "", fmt.Errorf("the logo is %q, not an SVG", contentType)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, logoMaximumSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(content) > logoMaximumSize {
		return nil, "", errors.New("the logo is larger than this will store")
	}
	if len(content) == 0 {
		return nil, "", errors.New("the logo is empty")
	}
	return content, "image/svg+xml", nil
}
