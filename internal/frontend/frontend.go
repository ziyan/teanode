// Package frontend serves the dashboard, which is compiled into the binary.
//
// There is no separate web server, no CDN and no deploy step: a self-hosted
// mail server should not need anybody else's infrastructure to draw its own
// interface, and upgrading should be replacing one file.
package frontend

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("frontend")

// The build output. The directory is committed with a placeholder so that the
// embed below succeeds on a clean checkout, before anybody has run the
// frontend build.
//
//go:embed all:static
var content embed.FS

// Handler serves the dashboard.
//
// Hashed assets are cached hard, because their names change when they do.
// Everything else falls through to index.html so that the browser can handle
// its own routing: a reader who refreshes on /mail/01K... must not get a 404.
func Handler() http.Handler {
	assets, err := fs.Sub(content, "static")
	if err != nil {
		// This cannot happen unless the embed directive above is wrong, and
		// the server is more useful running without a dashboard than not
		// running at all.
		log.Errorf("the dashboard could not be loaded and will not be served: %s", err)
		return http.NotFoundHandler()
	}

	index, indexError := fs.ReadFile(assets, "index.html")
	if indexError != nil {
		log.Warningf("the dashboard was not built into this binary; run 'make web'")
	}
	fileServer := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")

		if name != "" && name != "index.html" {
			if file, err := assets.Open(name); err == nil {
				_ = file.Close()
				// A hashed name identifies exactly one build, so it can be
				// cached indefinitely.
				if looksHashed(name) {
					response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(response, request)
				return
			}
		}

		if indexError != nil {
			http.Error(response, "the dashboard was not built into this binary; run 'make web'", http.StatusNotFound)
			return
		}

		// index.html names the current assets, so it must never be cached.
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := response.Write(index); err != nil {
			log.Debugf("failed to write the dashboard: %s", err)
		}
	})
}

// looksHashed reports whether a filename carries a content hash, as
// "teanode.9f8e7d.js" does.
func looksHashed(name string) bool {
	parts := strings.Split(path.Base(name), ".")
	if len(parts) < 3 {
		return false
	}
	hash := parts[len(parts)-2]
	if len(hash) < 8 {
		return false
	}
	for _, character := range hash {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

// InlineScriptHashes returns a CSP source expression for every inline script
// in the dashboard's index.html, so the policy can allow exactly those and
// nothing else.
//
// Computed from what is actually embedded rather than written down somewhere:
// there is one inline script today, applying the stored theme before the
// bundle runs so the page does not paint dark and then correct itself, and a
// hash written by hand is a hash that goes stale the next time somebody edits
// it. Getting this wrong fails loudly — the script stops running — which is
// the right way round for a policy.
func InlineScriptHashes() []string {
	assets, err := fs.Sub(content, "static")
	if err != nil {
		return nil
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil
	}

	hashes := make([]string, 0, 1)
	for _, body := range inlineScripts(string(index)) {
		sum := sha256.Sum256([]byte(body))
		hashes = append(hashes, fmt.Sprintf("'sha256-%s'", base64.StdEncoding.EncodeToString(sum[:])))
	}
	return hashes
}

// inlineScripts returns the body of every <script> with no src attribute.
//
// A text scan rather than an HTML parse: the input is one file this project
// generates, the shape is known, and a parser here would be a dependency
// bought to read fifteen lines.
func inlineScripts(html string) []string {
	var bodies []string
	rest := html
	for {
		open := strings.Index(rest, "<script")
		if open < 0 {
			return bodies
		}
		rest = rest[open:]
		end := strings.Index(rest, ">")
		if end < 0 {
			return bodies
		}
		attributes := rest[:end]
		rest = rest[end+1:]

		close := strings.Index(rest, "</script>")
		if close < 0 {
			return bodies
		}
		body := rest[:close]
		rest = rest[close+len("</script>"):]

		// A script with a src has no body to hash; 'self' covers it.
		if !strings.Contains(attributes, "src=") {
			bodies = append(bodies, body)
		}
	}
}
