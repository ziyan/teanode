package linkpreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html/charset"

	"github.com/ziyan/teanode/internal/picture"
	"github.com/ziyan/teanode/internal/util/safefetch"
)

const (
	// PageTimeout is how long a page may take. A link that does not answer by
	// then has no preview, and is not asked again for an hour.
	PageTimeout = 5 * time.Second

	// PageMaximumBytes is how much of a page is read. The tags that matter are
	// in the head, which is near the start, so the rest is not worth waiting
	// for.
	PageMaximumBytes = 512 << 10

	// ImageMaximumBytes is the largest picture fetched.
	ImageMaximumBytes = 2 << 20

	// ImageMaximumWidthPixels is the width a stored picture is scaled down
	// to. A card is shown at a fraction of this, even on a phone.
	ImageMaximumWidthPixels = 600

	imageJPEGQuality = 82

	// concurrentImageDecodes is how many pictures are decoded at once. A
	// picture of a few megabytes is many times that once decoded, and the
	// pictures are strangers' to begin with.
	concurrentImageDecodes = 2
)

var (
	// ErrNotFetched is a page or picture that could not be had: no answer, a
	// refusal, or an answer other than 200.
	ErrNotFetched = errors.New("linkpreview: could not fetch it")

	// ErrNotHTML is a link to something other than a web page.
	ErrNotHTML = errors.New("linkpreview: not a web page")

	// ErrNotPicture is an address that is not a picture this server keeps.
	ErrNotPicture = errors.New("linkpreview: not a picture")
)

// imageTypes are the kinds of picture that are decoded, the ones the picture
// package reads. Never SVG, which can carry script.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

var pageTypes = map[string]bool{"text/html": true, "application/xhtml+xml": true}

var imageDecodes = make(chan struct{}, concurrentImageDecodes)

// Fetcher fetches pages and their pictures.
type Fetcher struct {
	client *http.Client
}

// NewFetcher is a Fetcher that goes through safefetch.
func NewFetcher() *Fetcher {
	return &Fetcher{client: safefetch.Client()}
}

// NewFetcherWith is a Fetcher that fetches with client, for a test that has a
// server of its own to ask.
func NewFetcherWith(client *http.Client) *Fetcher {
	return &Fetcher{client: client}
}

// FetchPage reads what the page at address says about itself.
func (self *Fetcher) FetchPage(ctx context.Context, address string) (*Page, error) {
	target, err := safefetch.ParseTarget(address)
	if err != nil {
		return nil, err
	}
	timed, cancel := context.WithTimeout(ctx, PageTimeout)
	defer cancel()
	body, contentType, finalAddress, err := self.get(timed, target, fetchRules{
		accept: "text/html, application/xhtml+xml;q=0.9", mediaTypes: pageTypes, refusal: ErrNotHTML, maximumBytes: PageMaximumBytes,
	})
	if err != nil {
		return nil, err
	}
	// Decoded from whatever the page is written in, which its header or its
	// own meta tag says.
	decoded, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, err
	}
	return Parse(decoded, finalAddress)
}

// Image is a picture as this server keeps it: a JPEG with none of the
// original's metadata, scaled down.
type Image struct {
	Content      []byte
	WidthPixels  int
	HeightPixels int
}

// FetchImage fetches the picture at address and makes the picture this server
// keeps from it. A picture of another kind, one over ImageMaximumBytes, or one
// that cannot be decoded is ErrNotPicture.
func (self *Fetcher) FetchImage(ctx context.Context, address string) (*Image, error) {
	target, err := safefetch.ParseTarget(address)
	if err != nil {
		return nil, err
	}
	timed, cancel := context.WithTimeout(ctx, safefetch.Timeout)
	defer cancel()
	body, _, _, err := self.get(timed, target, fetchRules{
		accept: "image/png, image/jpeg, image/gif, image/webp", mediaTypes: imageTypes, refusal: ErrNotPicture,
		maximumBytes: ImageMaximumBytes, isWholeRequired: true,
	})
	if err != nil {
		return nil, err
	}
	select {
	case imageDecodes <- struct{}{}:
		defer func() { <-imageDecodes }()
	case <-timed.Done():
		return nil, timed.Err()
	}
	decoded, err := picture.Decode(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotPicture, err)
	}
	scaled := picture.Scale(decoded, ImageMaximumWidthPixels)
	content, err := picture.EncodeJPEG(scaled, imageJPEGQuality)
	if err != nil {
		return nil, err
	}
	bounds := scaled.Bounds()
	return &Image{Content: content, WidthPixels: bounds.Dx(), HeightPixels: bounds.Dy()}, nil
}

// fetchRules say what a fetch accepts.
type fetchRules struct {
	// accept is the Accept header sent.
	accept string

	// mediaTypes are the kinds of answer read; any other is refusal.
	mediaTypes map[string]bool
	refusal    error

	// maximumBytes is how much of the body is read. A longer body is cut, or
	// refused where isWholeRequired says a part of it is no use (a picture).
	maximumBytes    int64
	isWholeRequired bool
}

// get fetches target with no credentials, no cookies and no referer, and
// returns the body, its Content-Type and the address the redirects ended at.
func (self *Fetcher) get(ctx context.Context, target *url.URL, rules fetchRules) ([]byte, string, *url.URL, error) {
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", nil, err
	}
	outgoing.Header.Set("User-Agent", "TeaNode link preview")
	outgoing.Header.Set("Accept", rules.accept)
	fetched, err := self.client.Do(outgoing)
	if err != nil {
		return nil, "", nil, errors.Join(ErrNotFetched, err)
	}
	defer func() { _ = fetched.Body.Close() }()
	if fetched.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("%w: status %d", ErrNotFetched, fetched.StatusCode)
	}
	contentType := fetched.Header.Get("Content-Type")
	if !rules.mediaTypes[strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))] {
		return nil, "", nil, rules.refusal
	}
	body, err := io.ReadAll(io.LimitReader(fetched.Body, rules.maximumBytes+1))
	if err != nil {
		return nil, "", nil, errors.Join(ErrNotFetched, err)
	}
	if int64(len(body)) > rules.maximumBytes {
		if rules.isWholeRequired {
			return nil, "", nil, fmt.Errorf("%w: more than %d bytes", rules.refusal, rules.maximumBytes)
		}
		body = body[:rules.maximumBytes]
	}
	return body, contentType, fetched.Request.URL, nil
}
