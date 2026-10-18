package apigraph

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

// styleHost is a map style host: a style naming itself, a gzipped tile, and
// things a map does not have.
func styleHost(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var lock sync.Mutex
	var seen []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		lock.Lock()
		seen = append(seen, request.URL.RequestURI()+" "+request.Header.Get("Cookie")+request.Header.Get("Authorization"))
		lock.Unlock()
		switch request.URL.Path {
		case "/styles/liberty":
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Cache-Control", "public, max-age=3600")
			response.Header().Set("ETag", `"style-1"`)
			if request.Header.Get("If-None-Match") == `"style-1"` {
				response.WriteHeader(http.StatusNotModified)
				return
			}
			_, _ = response.Write([]byte(`{"sources":{"planet":{"url":"` + server.URL + `/planet"}},"glyphs":"` + server.URL + `/fonts/{fontstack}/{range}.pbf"}`))
		case "/planet/1/2/3.pbf":
			var compressed bytes.Buffer
			writer := gzip.NewWriter(&compressed)
			_, _ = writer.Write([]byte("tile bytes"))
			_ = writer.Close()
			response.Header().Set("Content-Type", "application/x-protobuf")
			response.Header().Set("Content-Encoding", "gzip")
			_, _ = response.Write(compressed.Bytes())
		case "/page.html":
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte("<script>alert(1)</script>"))
		case "/big.pbf":
			response.Header().Set("Content-Type", "application/x-protobuf")
			_, _ = response.Write(bytes.Repeat([]byte("x"), mapResponseMostBytes+10))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		lock.Lock()
		defer lock.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestTheMapIsFetchedFromTheStyleHostOnly(t *testing.T) {
	world := newLocationWorld(t)
	upstream, seen := styleHost(t)
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.MapStyleURL = upstream.URL + "/styles/liberty"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	if err := world.graph.AddRoutes(router); err != nil {
		t.Fatal(err)
	}
	get := func(user string, address string, headers ...string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, address, nil)
		request.Host = "teanode.example"
		if user != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, user)
		}
		request.Header.Set("Cookie", "session=secret")
		for index := 0; index+1 < len(headers); index += 2 {
			request.Header.Set(headers[index], headers[index+1])
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	// Nobody anonymous.
	if response := get("", api.PathMap+"/styles/liberty"); response.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", response.Code)
	}

	// The style names this server, wherever it was written to name the host,
	// and the stable path is the configured style.
	for _, address := range []string{api.PathMapStyle, api.PathMap + "/styles/liberty"} {
		response := get(world.ada.Username, address)
		body := response.Body.String()
		if response.Code != http.StatusOK || strings.Contains(body, upstream.URL) ||
			!strings.Contains(body, "http://teanode.example/api/v1/map/planet") || !strings.Contains(body, "http://teanode.example/api/v1/map/fonts/{fontstack}") {
			t.Errorf("%s: %d %s", address, response.Code, body)
		}
		if got := response.Header().Get("Cache-Control"); got != "private, max-age=3600" || response.Header().Get("ETag") != `"style-1"` {
			t.Errorf("%s: caching headers pass through, made private: %q %q", address, got, response.Header().Get("ETag"))
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no sniffing", address)
		}
	}
	// A cache that has it is told so.
	if response := get(world.ada.Username, api.PathMapStyle, "If-None-Match", `"style-1"`); response.Code != http.StatusNotModified || response.Body.Len() != 0 {
		t.Errorf("not modified: %d %q", response.Code, response.Body.String())
	}

	// A tile comes through as plain bytes; the server's own compression is
	// the only one on the way out.
	tile := get(world.ada.Username, api.PathMap+"/planet/1/2/3.pbf")
	if tile.Code != http.StatusOK || tile.Body.String() != "tile bytes" || tile.Header().Get("Content-Encoding") != "" ||
		tile.Header().Get("Content-Type") != "application/x-protobuf" {
		t.Errorf("a tile: %d %q %v", tile.Code, tile.Body.String(), tile.Header())
	}

	// What is not part of a map is not passed on, and paths that climb or name
	// another host are not asked for.
	if response := get(world.ada.Username, api.PathMap+"/page.html"); response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "script") {
		t.Errorf("a page: %d %s", response.Code, response.Body.String())
	}
	if response := get(world.ada.Username, api.PathMap+"/missing.pbf"); response.Code != http.StatusNotFound {
		t.Errorf("missing: %d", response.Code)
	}
	for _, address := range []string{api.PathMap + "/../secret", api.PathMap + "//evil.example/x", api.PathMap + "/a/../b", api.PathMap + "/%2e%2e/x"} {
		before := len(seen())
		if response := get(world.ada.Username, address); response.Code != http.StatusNotFound && response.Code != http.StatusMovedPermanently {
			t.Errorf("%s: %d", address, response.Code)
		}
		if len(seen()) != before {
			t.Errorf("%s: the style host was asked", address)
		}
	}
	// Nothing of the person's goes to the host.
	for _, request := range seen() {
		if strings.Contains(request, "secret") {
			t.Errorf("the cookie went to the style host: %s", request)
		}
	}

	// Only GET.
	post := httptest.NewRequest(http.MethodPost, api.PathMap+"/styles/liberty", nil)
	post.Header.Set(api.AuthenticatedUsernameHeader, world.ada.Username)
	recorded := httptest.NewRecorder()
	router.ServeHTTP(recorded, post)
	if recorded.Code == http.StatusOK {
		t.Error("a map file is read, never posted")
	}
}

func TestTheMapRefusesAFileThatIsTooLarge(t *testing.T) {
	world := newLocationWorld(t)
	upstream, _ := styleHost(t)
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.MapStyleURL = upstream.URL + "/styles/liberty"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	if err := world.graph.AddRoutes(router); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, api.PathMap+"/big.pbf", nil)
	request.Header.Set(api.AuthenticatedUsernameHeader, world.ada.Username)
	response := httptest.NewRecorder()
	defer func() {
		// A body over the limit drops the connection instead of ending as if
		// it were whole.
		if recovered := recover(); recovered != http.ErrAbortHandler && response.Code != http.StatusBadGateway {
			t.Errorf("a file over the limit: %v %d", recovered, response.Code)
		}
	}()
	router.ServeHTTP(response, request)
}
