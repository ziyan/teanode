package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// uploadServer is a server that has taken a file whole and is slow to finish
// it: it answers the last part and then asks to be asked.
type uploadServer struct {
	*httptest.Server
	mutex sync.Mutex
	// requests are the methods received, in order.
	requests []string
	// headsUntilDone is how many questions of where the upload is it answers
	// "all the bytes, not done" before it says it is done.
	headsUntilDone int
	// doneAnswer is what the question is answered once it is done.
	doneWithPhotoID bool
	// lostAfterHeads makes the question answer that the upload is gone after
	// that many.
	lostAfterHeads int
	// foundPhotos is what FindPhotos answers.
	foundPhotoID string
	headCount    int
	graphqlCount int
}

func (self *uploadServer) count(method string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := 0
	for _, request := range self.requests {
		if request == method {
			count++
		}
	}
	return count
}

func newUploadServer(test *testing.T, sizeBytes int) *uploadServer {
	test.Helper()
	self := &uploadServer{doneWithPhotoID: true}
	self.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		self.requests = append(self.requests, request.Method)
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/photos/uploads":
			// The whole file is in this request, and the server is still
			// finishing it when the answer is lost: the client has the 104,
			// which says where the upload is, and nothing more.
			response.Header().Set("Location", "http://"+request.Host+"/api/v1/photos/uploads/upload-1")
			response.WriteHeader(104)
			response.Header().Set("Upload-Offset", strconv.Itoa(sizeBytes))
			response.Header().Set("Upload-Complete", "?0")
			response.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodHead:
			self.headCount++
			if self.lostAfterHeads > 0 && self.headCount > self.lostAfterHeads {
				http.NotFound(response, request)
				return
			}
			if self.headCount <= self.headsUntilDone {
				response.Header().Set("Upload-Offset", strconv.Itoa(sizeBytes))
				response.Header().Set("Upload-Complete", "?0")
				response.WriteHeader(http.StatusNoContent)
				return
			}
			response.Header().Set("Upload-Offset", strconv.Itoa(sizeBytes))
			response.Header().Set("Upload-Complete", "?1")
			if self.doneWithPhotoID {
				response.Header().Set("X-Photo-ID", "photo-made")
			}
			response.WriteHeader(http.StatusNoContent)
		case request.URL.Path == "/api/v1/graphql":
			self.graphqlCount++
			_, _ = response.Write([]byte(`{"data":{"FindPhotos":[{"id":"` + self.foundPhotoID + `","clientPhotoId":"waits"}]}}`))
		default:
			http.Error(response, "unexpected "+request.Method+" "+request.URL.Path, http.StatusInternalServerError)
		}
	}))
	test.Cleanup(self.Close)
	return self
}

// cutTheAnswer is a transport that sends the first request of an upload to the
// server and then reports the connection as lost, as a client sees an answer
// that took longer than it would wait.
type cutTheAnswer struct{ base http.RoundTripper }

func (self *cutTheAnswer) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost && request.URL.Path == "/api/v1/photos/uploads" {
		response, err := self.base.RoundTrip(request)
		if err == nil {
			_ = response.Body.Close()
		}
		return nil, errors.New("the answer did not come in time")
	}
	return self.base.RoundTrip(request)
}

func shortPollDelays(test *testing.T) {
	test.Helper()
	wait, first, last := photoCompletionWait, photoCompletionFirstDelay, photoCompletionLastDelay
	photoCompletionWait, photoCompletionFirstDelay, photoCompletionLastDelay = 5*time.Second, time.Millisecond, 5*time.Millisecond
	test.Cleanup(func() { photoCompletionWait, photoCompletionFirstDelay, photoCompletionLastDelay = wait, first, last })
}

func uploadOf(content string) *PhotoUpload {
	return &PhotoUpload{Source: strings.NewReader(content), SizeBytes: int64(len(content)), ClientPhotoID: "waits", Filename: "waits.jpg", ContentType: "image/jpeg"}
}

// The last part is sent, the server takes minutes to finish a large file, and
// the answer never comes. The client asks again until the server is done, and
// does not send the last part again or report a failure.
func TestTheClientWaitsForTheServerToFinishALargeUploadInsteadOfSendingItAgain(test *testing.T) {
	shortPollDelays(test)
	content := strings.Repeat("x", 100)
	server := newUploadServer(test, len(content))
	server.headsUntilDone = 4
	connection, err := New(Options{URL: server.URL, Token: "t", HTTPClient: &http.Client{Transport: &cutTheAnswer{base: http.DefaultTransport}}})
	if err != nil {
		test.Fatal(err)
	}
	photoId, err := connection.UploadPhoto(test.Context(), uploadOf(content))
	if err != nil || photoId != "photo-made" {
		test.Fatalf("%q %v", photoId, err)
	}
	if server.count(http.MethodPost) != 1 || server.count(http.MethodPatch) != 0 {
		test.Fatalf("the file was sent again: %v", server.requests)
	}
	if server.count(http.MethodHead) < 5 {
		test.Fatalf("the server was asked %d times", server.count(http.MethodHead))
	}
}

// A server that says the upload is complete and does not name the photo is
// asked which photo has the uploader's name.
func TestTheClientFindsThePhotoByItsNameWhenTheServerDoesNotSayWhich(test *testing.T) {
	shortPollDelays(test)
	content := strings.Repeat("y", 50)
	server := newUploadServer(test, len(content))
	server.doneWithPhotoID = false
	server.foundPhotoID = "photo-by-name"
	connection, err := New(Options{URL: server.URL, Token: "t", HTTPClient: &http.Client{Transport: &cutTheAnswer{base: http.DefaultTransport}}})
	if err != nil {
		test.Fatal(err)
	}
	photoId, err := connection.UploadPhoto(test.Context(), uploadOf(content))
	if err != nil || photoId != "photo-by-name" {
		test.Fatalf("%q %v", photoId, err)
	}
	if server.graphqlCount != 1 {
		test.Fatalf("the photo was looked for %d times", server.graphqlCount)
	}
}

// An upload the server lost while the client waited starts over; one the
// server never finishes is reported after the longest the server takes.
func TestTheClientStartsOverWhenTheUploadIsLostAndGivesUpWhenTheServerNeverFinishes(test *testing.T) {
	shortPollDelays(test)
	content := strings.Repeat("z", 50)
	server := newUploadServer(test, len(content))
	server.headsUntilDone = 1000
	server.lostAfterHeads = 2
	connection, err := New(Options{URL: server.URL, Token: "t", HTTPClient: &http.Client{Transport: &cutTheAnswer{base: http.DefaultTransport}}})
	if err != nil {
		test.Fatal(err)
	}
	if _, err := connection.UploadPhoto(test.Context(), uploadOf(content)); err == nil || !strings.Contains(err.Error(), "after 3 tries") {
		test.Fatalf("%v", err)
	}
	// Each try posted the file again, from the start.
	if server.count(http.MethodPost) != 3 {
		test.Fatalf("the upload was started %d times", server.count(http.MethodPost))
	}

	photoCompletionWait = 100 * time.Millisecond
	never := newUploadServer(test, len(content))
	never.headsUntilDone = 1 << 30
	connection, err = New(Options{URL: never.URL, Token: "t", HTTPClient: &http.Client{Transport: &cutTheAnswer{base: http.DefaultTransport}}})
	if err != nil {
		test.Fatal(err)
	}
	if _, err := connection.UploadPhoto(test.Context(), uploadOf(content)); err == nil || !strings.Contains(err.Error(), "did not finish storing") {
		test.Fatalf("%v", err)
	}
	if never.count(http.MethodPost) != 1 {
		test.Fatalf("the file was sent %d times while the client waited", never.count(http.MethodPost))
	}
}
