package photo_test

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// These tests run the command line's uploader against the real upload
// endpoint, so that what the two sides say to each other is checked once, in
// one place.

// flakyTransport sends requests to the real server and then misbehaves as
// its rules say: it can lose a request before the server sees it, or lose
// the answer after the server has acted on it.
type flakyTransport struct {
	base http.RoundTripper

	mutex    sync.Mutex
	requests []string
	// fail is asked about each request, by method and how many of that
	// method came before, and says what happens to it.
	fail func(method string, index int) failure
	seen map[string]int
}

type failure int

const (
	deliver failure = iota
	// loseRequest fails before the server sees anything.
	loseRequest
	// loseAnswer sends the request and fails as the answer comes back.
	loseAnswer
)

func (self *flakyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	self.mutex.Lock()
	if self.seen == nil {
		self.seen = map[string]int{}
	}
	index := self.seen[request.Method]
	self.seen[request.Method]++
	self.requests = append(self.requests, request.Method)
	what := deliver
	if self.fail != nil {
		what = self.fail(request.Method, index)
	}
	self.mutex.Unlock()
	switch what {
	case loseRequest:
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, errors.New("the connection was reset")
	case loseAnswer:
		response, err := self.base.RoundTrip(request)
		if err == nil {
			_ = response.Body.Close()
		}
		return nil, errors.New("the connection was reset as the answer came")
	}
	return self.base.RoundTrip(request)
}

func (self *flakyTransport) count(method string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.seen[method]
}

func (self *fixture) clientWith(transport http.RoundTripper) *client.Client {
	self.test.Helper()
	connection, err := client.New(client.Options{URL: self.server.URL, Token: "fixture-token", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		self.test.Fatal(err)
	}
	return connection
}

func uploadOf(content []byte, clientPhotoId string) *client.PhotoUpload {
	return &client.PhotoUpload{
		Source: bytes.NewReader(content), SizeBytes: int64(len(content)), ClientPhotoID: clientPhotoId, Filename: "IMG_0001.JPG",
		ContentType: "image/jpeg", DeclaredSHA256: digestOf(content),
	}
}

func TestTheClientSendsASmallFileInOneRequestAndABigOneInParts(test *testing.T) {
	fixture := newFixture(test)
	transport := &flakyTransport{base: http.DefaultTransport}
	connection := fixture.clientWith(transport)

	small := smallJpeg(test, 5)
	photoId, err := connection.UploadPhoto(test.Context(), uploadOf(small, digestOf(small)))
	if err != nil {
		test.Fatal(err)
	}
	if transport.count(http.MethodPost) != 1 || transport.count(http.MethodPatch) != 0 {
		test.Fatalf("a small file took %v", transport.requests)
	}
	if found := fixture.photoByClientId(digestOf(small)); found == nil || found.ID != photoId || found.PhotoSource != models.PhotoSourceCommandLine || found.OriginalFilename != "IMG_0001.JPG" {
		test.Fatalf("%+v", found)
	}

	// 20 MiB is two whole parts and a short one.
	big := randomBytes(20<<20, 71)
	var progress []int64
	upload := uploadOf(big, digestOf(big))
	upload.OnProgress = func(storedBytes int64) { progress = append(progress, storedBytes) }
	transport.requests = nil
	bigId, err := connection.UploadPhoto(test.Context(), upload)
	if err != nil {
		test.Fatal(err)
	}
	if got := strings.Join(transport.requests, " "); got != "POST PATCH PATCH" {
		test.Fatalf("a file of three parts took %s", got)
	}
	want := []int64{8 << 20, 16 << 20, 20 << 20}
	if len(progress) != 3 || progress[0] != want[0] || progress[1] != want[1] || progress[2] != want[2] {
		test.Fatalf("progress %v, want %v", progress, want)
	}
	if !bytes.Equal(fixture.files.get(models.PhotoBlobFileID(digestOf(big))), big) {
		test.Fatal("the stored bytes are not the file")
	}
	if found := fixture.photoByClientId(digestOf(big)); found == nil || found.ID != bigId {
		test.Fatalf("%+v", found)
	}
}

func TestTheClientAsksWhereTheServerGotToAfterALostRequestOrAnswer(test *testing.T) {
	for name, rule := range map[string]func(method string, index int) failure{
		"the request of the second part is lost": func(method string, index int) failure {
			if method == http.MethodPatch && index == 0 {
				return loseRequest
			}
			return deliver
		},
		"the answer to the second part is lost after the server stored it": func(method string, index int) failure {
			if method == http.MethodPatch && index == 0 {
				return loseAnswer
			}
			return deliver
		},
		"the answer to the first request is lost, the 104 having said where the upload is": func(method string, index int) failure {
			if method == http.MethodPost {
				return loseAnswer
			}
			return deliver
		},
		"the answer to the last part is lost, and the file is already a photo": func(method string, index int) failure {
			if method == http.MethodPatch && index == 1 {
				return loseAnswer
			}
			return deliver
		},
	} {
		test.Run(name, func(test *testing.T) {
			fixture := newFixture(test)
			transport := &flakyTransport{base: http.DefaultTransport, fail: rule}
			connection := fixture.clientWith(transport)
			content := randomBytes(20<<20, 72)
			photoId, err := connection.UploadPhoto(test.Context(), uploadOf(content, digestOf(content)))
			if err != nil {
				test.Fatalf("%v (requests: %v)", err, transport.requests)
			}
			if transport.count(http.MethodHead) == 0 {
				test.Fatalf("the server was not asked where it got to: %v", transport.requests)
			}
			found := fixture.photoByClientId(digestOf(content))
			if found == nil || found.ID != photoId || fixture.count("photo") != "1" {
				test.Fatalf("%+v, %s photos", found, fixture.count("photo"))
			}
			if !bytes.Equal(fixture.files.get(models.PhotoBlobFileID(digestOf(content))), content) {
				test.Fatal("the stored bytes are not the file")
			}
		})
	}
}

func TestTheClientGivesUpAfterThreeTriesAtAPart(test *testing.T) {
	fixture := newFixture(test)
	transport := &flakyTransport{base: http.DefaultTransport, fail: func(method string, index int) failure {
		if method == http.MethodPatch {
			return loseRequest
		}
		return deliver
	}}
	connection := fixture.clientWith(transport)
	content := randomBytes(10<<20, 73)
	_, err := connection.UploadPhoto(test.Context(), uploadOf(content, digestOf(content)))
	if err == nil || !strings.Contains(err.Error(), "after 3 tries") {
		test.Fatalf("%v", err)
	}
	if transport.count(http.MethodPatch) != 3 {
		test.Fatalf("the part was tried %d times", transport.count(http.MethodPatch))
	}
}

func TestTheClientDoesNotRetryAFileTheServerRefuses(test *testing.T) {
	fixture := newFixture(test)
	transport := &flakyTransport{base: http.DefaultTransport}
	connection := fixture.clientWith(transport)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	upload := uploadOf(svg, digestOf(svg))
	upload.ContentType = "image/svg+xml"
	_, err := connection.UploadPhoto(test.Context(), upload)
	if err == nil || !strings.Contains(err.Error(), "415") || !strings.Contains(err.Error(), "image/svg+xml") {
		test.Fatalf("%v", err)
	}
	if transport.count(http.MethodPost) != 1 {
		test.Fatalf("a refused file was sent %d times", transport.count(http.MethodPost))
	}

	// A digest that is not the file's is refused too, once.
	jpeg := smallJpeg(test, 6)
	wrong := uploadOf(jpeg, "wrong-digest")
	wrong.DeclaredSHA256 = digestOf([]byte("something else"))
	if _, err := connection.UploadPhoto(test.Context(), wrong); err == nil || !strings.Contains(err.Error(), "400") {
		test.Fatalf("%v", err)
	}
	if transport.count(http.MethodPost) != 2 {
		test.Fatalf("%d posts", transport.count(http.MethodPost))
	}
}

func TestTheClientSendingTheSameFileAgainFindsThePhotoThatIsThere(test *testing.T) {
	fixture := newFixture(test)
	connection := fixture.clientWith(http.DefaultTransport)
	content := smallJpeg(test, 7)
	first, err := connection.UploadPhoto(test.Context(), uploadOf(content, digestOf(content)))
	if err != nil {
		test.Fatal(err)
	}
	second, err := connection.UploadPhoto(test.Context(), uploadOf(content, digestOf(content)))
	if err != nil || second != first || fixture.count("photo") != "1" {
		test.Fatalf("%q %q %v, %s photos", first, second, err, fixture.count("photo"))
	}
}
