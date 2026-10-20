package linkpreview_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/linkpreview"
)

func aPicture(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			canvas.Set(column, row, color.RGBA{R: uint8(column), G: uint8(row), B: 90, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		t.Fatalf("png.Encode: %s", err)
	}
	return encoded.Bytes()
}

func TestFetchPageReadsTheTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") != "" || request.Header.Get("Referer") != "" || request.Header.Get("Authorization") != "" {
			t.Errorf("the fetch carried something of the reader's: %v", request.Header)
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(`<head><meta property="og:title" content="Garden Chair"><meta property="og:image" content="/chair.png"></head>`))
	}))
	defer server.Close()

	page, err := linkpreview.NewFetcherWith(server.Client()).FetchPage(context.Background(), server.URL+"/shop")
	if err != nil {
		t.Fatalf("FetchPage: %s", err)
	}
	if page.Title != "Garden Chair" || page.ImageURL != server.URL+"/chair.png" {
		t.Errorf("FetchPage = %+v", page)
	}
}

func TestFetchPageReadsOnlyTheStartOfALongPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = response.Write([]byte(`<head><meta property="og:title" content="Early"></head><body>`))
		_, _ = response.Write([]byte(strings.Repeat("x", linkpreview.PageMaximumBytes*2)))
	}))
	defer server.Close()
	page, err := linkpreview.NewFetcherWith(server.Client()).FetchPage(context.Background(), server.URL)
	if err != nil || page.Title != "Early" {
		t.Errorf("FetchPage = %+v, %v", page, err)
	}
}

func TestFetchPageRefusesWhatIsNotAWebPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/pdf")
		_, _ = response.Write([]byte("%PDF-1.7"))
	}))
	defer server.Close()
	if _, err := linkpreview.NewFetcherWith(server.Client()).FetchPage(context.Background(), server.URL); !errors.Is(err, linkpreview.ErrNotHTML) {
		t.Errorf("FetchPage of a PDF = %v, want ErrNotHTML", err)
	}
}

func TestFetchPageRefusesAnAnswerOtherThanOK(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	if _, err := linkpreview.NewFetcherWith(server.Client()).FetchPage(context.Background(), server.URL); !errors.Is(err, linkpreview.ErrNotFetched) {
		t.Errorf("FetchPage of a missing page = %v, want ErrNotFetched", err)
	}
}

// The guarded fetcher is what refuses an address inside the network, and the
// test server is one: this is the whole of the protection against a link to
// an internal service.
func TestTheGuardedFetcherRefusesAnInternalAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		t.Error("the internal server was reached")
		response.Header().Set("Content-Type", "text/html")
	}))
	defer server.Close()
	if _, err := linkpreview.NewFetcher().FetchPage(context.Background(), server.URL); err == nil {
		t.Error("FetchPage reached an address on the loopback")
	}
	if _, err := linkpreview.NewFetcher().FetchImage(context.Background(), server.URL+"/a.png"); err == nil {
		t.Error("FetchImage reached an address on the loopback")
	}
}

func TestFetchImageScalesDownAndStripsTheOriginal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write(aPicture(t, 1200, 800))
	}))
	defer server.Close()

	stored, err := linkpreview.NewFetcherWith(server.Client()).FetchImage(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchImage: %s", err)
	}
	if stored.WidthPixels != linkpreview.ImageMaximumWidthPixels || stored.HeightPixels != 400 {
		t.Errorf("the picture is %d by %d, want 600 by 400", stored.WidthPixels, stored.HeightPixels)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(stored.Content))
	if err != nil {
		t.Fatalf("the stored picture is not a JPEG: %s", err)
	}
	if decoded.Bounds().Dx() != stored.WidthPixels {
		t.Errorf("the stored picture is %d wide, and says %d", decoded.Bounds().Dx(), stored.WidthPixels)
	}
}

func TestFetchImageKeepsASmallPictureTheSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write(aPicture(t, 120, 80))
	}))
	defer server.Close()
	stored, err := linkpreview.NewFetcherWith(server.Client()).FetchImage(context.Background(), server.URL)
	if err != nil || stored.WidthPixels != 120 || stored.HeightPixels != 80 {
		t.Errorf("FetchImage = %+v, %v", stored, err)
	}
}

func TestFetchImageRefusesWhatItWillNotKeep(t *testing.T) {
	cases := map[string]struct {
		contentType string
		body        func() []byte
	}{
		"a picture over the size limit": {"image/png", func() []byte { return append(aPicture(t, 10, 10), make([]byte, linkpreview.ImageMaximumBytes)...) }},
		"a scalable vector picture":     {"image/svg+xml", func() []byte { return []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>1</script></svg>") }},
		"a page":                        {"text/html", func() []byte { return []byte("<html></html>") }},
		"bytes that are not a picture":  {"image/png", func() []byte { return []byte("this is not a picture") }},
	}
	for name, found := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", found.contentType)
				_, _ = response.Write(found.body())
			}))
			defer server.Close()
			if _, err := linkpreview.NewFetcherWith(server.Client()).FetchImage(context.Background(), server.URL); !errors.Is(err, linkpreview.ErrNotPicture) {
				t.Errorf("FetchImage = %v, want ErrNotPicture", err)
			}
		})
	}
}
