package safefetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrNotFetched is an image the server could not get: no answer, or
	// an answer other than 200.
	ErrNotFetched = errors.New("safefetch: could not fetch it")

	// ErrNotImage is an answer that is not an image the caller shows.
	ErrNotImage = errors.New("safefetch: not an image")
)

// Image fetches an image somebody else named, for a reader who is looking
// at it: with no credentials, no cookies and no referer, so nothing tells
// the image's server whose request this is or what they were reading, and
// only when what comes back is a type the caller shows. Anything else
// would make this a way to read an internal service through the server,
// which is the hole the address checks close, one step later.
//
// The body is cut at maximumSize, so a hostile server cannot use a fetch
// to fill a pipe.
func Image(ctx context.Context, target *url.URL, maximumSize int64, shown func(contentType string) bool) (string, []byte, error) {
	timed, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	outgoing, err := http.NewRequestWithContext(timed, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", nil, err
	}
	outgoing.Header.Set("User-Agent", "teanode")
	outgoing.Header.Set("Accept", "image/*")
	fetched, err := Client().Do(outgoing)
	if err != nil {
		return "", nil, errors.Join(ErrNotFetched, err)
	}
	defer func() { _ = fetched.Body.Close() }()
	if fetched.StatusCode != http.StatusOK {
		return "", nil, ErrNotFetched
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(fetched.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(contentType, "image/") || !shown(contentType) {
		return "", nil, ErrNotImage
	}
	body, err := io.ReadAll(io.LimitReader(fetched.Body, maximumSize))
	if err != nil {
		return "", nil, errors.Join(ErrNotFetched, err)
	}
	return contentType, body, nil
}
