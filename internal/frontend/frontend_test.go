package frontend

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The dashboard is more than one file now: the libraries are a file of their
// own so that a release does not make a browser fetch them again, and a page
// nobody has opened -- or a catalog in a language nobody is reading -- is
// fetched when it is wanted. Every one of those names carries a content hash,
// which is what lets the server cache them forever, and the hash has to sit
// where the server looks for it.
func TestEveryNameTheBuildProducesSaysWhetherItCanBeCached(t *testing.T) {
	forever := []string{
		"teanode.45c7ec871d3012bd05ff.js",
		"teanode.51085f3732ce1c917b5a.css",
		"vendor.038556da1e3d6d187aa0.js",
		"runtime.bd77bd0d141665213095.js",
		"mailbox.c50ab694e154aeec730c.js",
		"catalog-zh.bff3da9d1e18597f6aeb.js",
	}
	for _, name := range forever {
		if !looksHashed(name) {
			t.Errorf("%q carries a content hash and can be cached forever", name)
		}
	}

	// What is linked by a fixed name, and so must not be: index.html names
	// the current build, and a page the model wrote links the helper and the
	// chart library by an address it was given once.
	never := []string{
		"index.html",
		"favicon.ico",
		"assets/artifact.js",
		"assets/artifact.css",
		"assets/echarts.min.js",
		"teanode.js",
		"a.b.js",
		// The note webpack leaves beside a bundle, which nothing links and
		// whose hash is not where the name ends.
		"vendor.038556da1e3d6d187aa0.js.LICENSE.txt",
	}
	for _, name := range never {
		if looksHashed(name) {
			t.Errorf("%q does not name one build and must not be cached forever", name)
		}
	}
}

// What the build actually left behind, served. A clean checkout has no build
// in it, so there is nothing to serve and nothing to check.
func TestTheBuiltDashboardIsServed(t *testing.T) {
	assets, err := fs.Sub(content, "static")
	if err != nil {
		t.Fatalf("the embedded dashboard: %s", err)
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		t.Skip("the dashboard has not been built into this binary")
	}
	handler := Handler()

	names, err := fs.Glob(assets, "*.js")
	if err != nil {
		t.Fatalf("listing the build: %s", err)
	}
	if len(names) < 2 {
		t.Fatalf("the dashboard is split into files that are fetched when they are wanted, and this build has %d", len(names))
	}
	for _, name := range names {
		request := httptest.NewRequest(http.MethodGet, "/"+name, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s: %d", name, recorder.Code)
		}
		if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
			t.Errorf("%s is named after its contents and should be cached forever, not %q", name, cache)
		}
	}

	// A message opened by its address is not a file, and the browser does its
	// own routing: everything that is not a file is the page itself.
	request := httptest.NewRequest(http.MethodGet, "/mailbox/01k/01m", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<title>TeaNode</title>") {
		t.Fatalf("a deep address is the dashboard: %d", recorder.Code)
	}
	if cache := recorder.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("the page names the current build and must not be kept: %q", cache)
	}
}

// An unclaimed well-known address is a 404, not the dashboard.
//
// Programs probe several of these in turn: a client looking for an
// authorization server tries more than one document and moves on when one is
// missing. Handed the dashboard's HTML with a 200 instead, it concludes the
// document exists and is broken. A deep link into the dashboard still gets the
// page, which is what the fallback is for.
func TestAnUnclaimedWellKnownAddressIsNotFound(t *testing.T) {
	handler := Handler()

	for _, address := range []string{"/.well-known/openid-configuration", "/.well-known/anything/at/all"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, address, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, not 404", address, response.Code)
		}
		if strings.Contains(response.Header().Get("Content-Type"), "text/html") && strings.Contains(response.Body.String(), "<html") {
			t.Errorf("%s answered with the dashboard", address)
		}
	}
}
