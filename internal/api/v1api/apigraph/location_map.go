package apigraph

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/place"
)

// The map: the style and the vector tiles the dashboard draws, fetched by this
// server from the host of the configured map style and handed on to the
// signed-in browser. The dashboard's content security policy allows nothing
// but this server, on purpose, because it shows mail written by strangers, so
// the browser asks here for everything the map needs, and a style that names
// its host is rewritten to name this server.
//
// This is the one place the server fetches an address because a browser asked
// for it, so it fetches from exactly one host, the style's, fixed by the
// operator's settings and never by the request, and answers only the kinds of
// file a map is made of.

const (
	// mapResponseMostBytes bounds what is read from the style host for one
	// request, and mapTimeout how long it may take.
	mapResponseMostBytes = 10 << 20
	mapTimeout           = 15 * time.Second

	// mapRedirectsMost is how many redirects within the style host are
	// followed.
	mapRedirectsMost = 3
)

// mapStyleAlias is the path that always means the style itself, wherever on
// its host the configured style is.
var mapStyleAlias = strings.TrimPrefix(api.PathMapStyle, api.PathMap)

// mapContentTypes are the kinds of file the style host may send. Anything else
// is not part of a map and is not passed on: a response served from this
// server's origin that a browser could render as a page would be a way to run
// script there.
var mapContentTypes = map[string]bool{
	"application/json":                   true,
	"application/x-protobuf":             true,
	"application/vnd.mapbox-vector-tile": true,
	"application/octet-stream":           true,
	"binary/octet-stream":                true,
	"image/png":                          true,
	"image/jpeg":                         true,
	"image/webp":                         true,
}

// mapClient fetches from the style host. It does not follow a redirect to
// another host.
var mapClient = &http.Client{
	Timeout: mapTimeout,
	CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) > mapRedirectsMost {
			return errors.New("too many redirects")
		}
		if next.URL.Scheme != via[0].URL.Scheme || next.URL.Host != via[0].URL.Host {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// mapPassedHeaders are the headers of the style host's answer that are passed
// on as they are.
var mapPassedHeaders = []string{"Cache-Control", "ETag", "Last-Modified"}

// mapView serves one file of the map. It is for a signed-in account only; the
// fetch is a cost to the style host that nobody anonymous should be able to
// cause from here.
func (self *graph) mapView(response http.ResponseWriter, request *http.Request) {
	username := api.UsernameFromRequest(request)
	if username == "" {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	if username != config.LocalUsername {
		found, err := self.database.GetUserByUsername(username)
		if err != nil {
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
		if found == nil || found.Disabled() {
			http.Error(response, "not signed in", http.StatusUnauthorized)
			return
		}
	}

	styleAddress, err := url.Parse(self.config.Current().Places.MapStyleURL)
	if err != nil || styleAddress.Host == "" || (styleAddress.Scheme != "https" && styleAddress.Scheme != "http") {
		http.Error(response, "this server has no map style set", http.StatusNotFound)
		return
	}
	upstream, isAllowed := mapUpstreamURL(styleAddress, request.URL)
	if !isAllowed {
		http.Error(response, "no such map file", http.StatusNotFound)
		return
	}

	upstreamRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, upstream.String(), nil)
	if err != nil {
		http.Error(response, "no such map file", http.StatusNotFound)
		return
	}
	upstreamRequest.Header.Set("User-Agent", place.UserAgent(self.config.Current()))
	// Only what a cache needs is forwarded: no cookie and no credential of the
	// person's goes to a third party. The encoding is asked for here and
	// undone here, since a style has to be read to be rewritten and since the
	// server's compression of its own answers (web.CompressionMiddleware) would
	// otherwise compress a tile that arrived compressed a second time.
	for _, name := range []string{"If-None-Match", "If-Modified-Since"} {
		if value := request.Header.Get(name); value != "" {
			upstreamRequest.Header.Set(name, value)
		}
	}
	upstreamRequest.Header.Set("Accept-Encoding", "gzip")

	upstreamResponse, err := mapClient.Do(upstreamRequest)
	if err != nil {
		log.Warningf("cannot fetch %s for the map: %s", upstream.Redacted(), err)
		http.Error(response, "the map server did not answer", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamResponse.Body.Close() }()

	switch upstreamResponse.StatusCode {
	case http.StatusOK, http.StatusNotModified:
	case http.StatusNotFound:
		http.Error(response, "no such map file", http.StatusNotFound)
		return
	default:
		http.Error(response, "the map server answered with an error", http.StatusBadGateway)
		return
	}

	mediaType, _, _ := mime.ParseMediaType(upstreamResponse.Header.Get("Content-Type"))
	if upstreamResponse.StatusCode == http.StatusOK && !mapContentTypes[mediaType] {
		http.Error(response, "the map server sent something that is not part of a map", http.StatusBadGateway)
		return
	}

	header := response.Header()
	for _, name := range mapPassedHeaders {
		if value := upstreamResponse.Header.Get(name); value != "" {
			header.Set(name, value)
		}
	}
	// What the style host sent is for the person who asked and for nobody
	// else's cache, since it was fetched for a signed-in account.
	if cacheControl := header.Get("Cache-Control"); cacheControl != "" {
		header.Set("Cache-Control", strings.ReplaceAll(cacheControl, "public", "private"))
	}
	// Whatever the style host sends is shown as a map file or not at all.
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")

	if upstreamResponse.StatusCode == http.StatusNotModified {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	header.Set("Content-Type", upstreamResponse.Header.Get("Content-Type"))

	var reader io.Reader = upstreamResponse.Body
	if strings.EqualFold(upstreamResponse.Header.Get("Content-Encoding"), "gzip") {
		unzipped, err := gzip.NewReader(upstreamResponse.Body)
		if err != nil {
			http.Error(response, "the map server's answer could not be read", http.StatusBadGateway)
			return
		}
		defer func() { _ = unzipped.Close() }()
		reader = unzipped
	} else if upstreamResponse.ContentLength > mapResponseMostBytes {
		http.Error(response, "the map file is too large", http.StatusBadGateway)
		return
	}

	if mediaType == "application/json" {
		self.writeRewrittenMapJSON(response, request, reader, styleAddress)
		return
	}

	// A tile, glyphs or an icon: streamed.
	if upstreamResponse.ContentLength >= 0 && upstreamResponse.Header.Get("Content-Encoding") == "" {
		header.Set("Content-Length", strconv.FormatInt(upstreamResponse.ContentLength, 10))
	}
	response.WriteHeader(http.StatusOK)
	copied, err := io.Copy(response, io.LimitReader(reader, mapResponseMostBytes))
	if err == nil && copied == mapResponseMostBytes {
		// Exactly the limit, or more. Cutting a file short and ending it as if
		// it were whole would hand the browser a broken tile, so the
		// connection is dropped instead.
		var probe [1]byte
		if count, _ := reader.Read(probe[:]); count > 0 {
			panic(http.ErrAbortHandler)
		}
	}
}

// writeRewrittenMapJSON answers a style or a TileJSON with every address on
// the style host changed to this server's map path, so that the browser asks
// here for the tiles, fonts and icons it names.
func (self *graph) writeRewrittenMapJSON(response http.ResponseWriter, request *http.Request, reader io.Reader, styleAddress *url.URL) {
	body, err := io.ReadAll(io.LimitReader(reader, mapResponseMostBytes+1))
	if err != nil || len(body) > mapResponseMostBytes {
		http.Error(response, "the map server's answer could not be read", http.StatusBadGateway)
		return
	}
	origin := styleAddress.Scheme + "://" + styleAddress.Host
	body = bytes.ReplaceAll(body, []byte(origin), []byte(self.originOf(request)+api.PathMap))
	response.Header().Set("Content-Length", strconv.FormatInt(int64(len(body)), 10))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(body)
}

// mapUpstreamURL is the address on the style host that a request is for, or
// false for a path no map file has. Built from the style's own scheme and host
// and the cleaned path: nothing of the request but its path and query decides
// anything, and the path cannot reach another host or climb out of its place.
func mapUpstreamURL(styleAddress *url.URL, requested *url.URL) (*url.URL, bool) {
	filePath := strings.TrimPrefix(requested.Path, api.PathMap)
	if filePath == mapStyleAlias {
		// The style itself, wherever on its host it is.
		upstream := *styleAddress
		upstream.User, upstream.Fragment = nil, ""
		return &upstream, true
	}
	if !strings.HasPrefix(filePath, "/") || strings.ContainsAny(filePath, "\\\x00") {
		return nil, false
	}
	// No empty, dot or dot-dot segment: the address is a plain path on the
	// style host, which a request cannot turn into another host or another
	// place on it.
	for _, segment := range strings.Split(filePath[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return nil, false
		}
	}
	return &url.URL{Scheme: styleAddress.Scheme, Host: styleAddress.Host, Path: filePath, RawQuery: requested.RawQuery}, true
}
