package photo_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// answer is what a request got back, with the interim responses it saw first.
type answer struct {
	status  int
	header  http.Header
	body    string
	interim []int
	// interimLocation is the Location the 104 carried.
	interimLocation string
}

func (self *fixture) send(method, address string, headers map[string]string, body []byte) *answer {
	self.test.Helper()
	got := &answer{}
	trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
		got.interim = append(got.interim, code)
		got.interimLocation = header.Get("Location")
		return nil
	}}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(self.test.Context(), trace), method, address, bytes.NewReader(body))
	if err != nil {
		self.test.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		self.test.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	content, _ := io.ReadAll(response.Body)
	got.status, got.header, got.body = response.StatusCode, response.Header, string(content)
	return got
}

func (self *fixture) expect(got *answer, status int) {
	self.test.Helper()
	if got.status != status {
		self.test.Fatalf("status %d, want %d: %s", got.status, status, got.body)
	}
}

func jpegHeaders(clientPhotoId string, more ...string) map[string]string {
	headers := map[string]string{"Content-Type": "image/jpeg", "X-Photo-Client-ID": clientPhotoId}
	for index := 0; index+1 < len(more); index += 2 {
		headers[more[index]] = more[index+1]
	}
	return headers
}

func TestOptionsAnswersTheLimitInTheFormOfTheClientsDraft(test *testing.T) {
	fixture := newFixture(test)
	older := fixture.send(http.MethodOptions, fixture.collectionUrl(), nil, nil)
	fixture.expect(older, http.StatusOK)
	if older.header.Get("Upload-Limit") != "8589934592" {
		test.Fatalf("Upload-Limit %q", older.header.Get("Upload-Limit"))
	}
	for _, version := range []string{"3", "4"} {
		got := fixture.send(http.MethodOptions, fixture.collectionUrl(), map[string]string{"Upload-Draft-Interop-Version": version}, nil)
		if got.header.Get("Upload-Limit") != "8589934592" || got.header.Get("Upload-Draft-Interop-Version") != version {
			test.Fatalf("version %s: %v", version, got.header)
		}
	}
	for _, version := range []string{"5", "6"} {
		got := fixture.send(http.MethodOptions, fixture.collectionUrl(), map[string]string{"Upload-Draft-Interop-Version": version}, nil)
		if got.header.Get("Upload-Limit") != "max-size=8589934592" || got.header.Get("Upload-Draft-Interop-Version") != version {
			test.Fatalf("version %s: %v", version, got.header)
		}
	}
	fixture.configuration.Photos.MaxFileBytes = 1 << 20
	got := fixture.send(http.MethodOptions, fixture.collectionUrl(), map[string]string{"Upload-Draft-Interop-Version": "6"}, nil)
	if got.header.Get("Upload-Limit") != "max-size=1048576" {
		test.Fatalf("the limit did not follow the configuration: %q", got.header.Get("Upload-Limit"))
	}
	bad := fixture.send(http.MethodOptions, fixture.collectionUrl(), map[string]string{"Upload-Draft-Interop-Version": "latest"}, nil)
	fixture.expect(bad, http.StatusBadRequest)
}

func TestAPlainUploadMakesAPhotoAndAnnouncesTheUploadFirst(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 40)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("plain-one",
		"X-Photo-Filename", "IMG_0001.JPG", "X-Photo-Taken-At", "2024-05-17T09:30:00+02:00",
		"X-Photo-Latitude", "10.0", "X-Photo-Longitude", "20.0", "X-Photo-Is-Favorite", "true",
		"X-Photo-Source", "iphone", "X-Photo-Declared-SHA256", digestOf(picture), "Upload-Draft-Interop-Version", "6"), picture)
	fixture.expect(got, http.StatusCreated)
	if len(got.interim) != 1 || got.interim[0] != 104 {
		test.Fatalf("interim responses %v, want one 104", got.interim)
	}
	if got.interimLocation == "" || got.interimLocation != got.header.Get("Location") || !strings.Contains(got.interimLocation, "/photos/uploads/") {
		test.Fatalf("the 104 said %q and the answer %q", got.interimLocation, got.header.Get("Location"))
	}
	if got.header.Get("Upload-Offset") != strconv.Itoa(len(picture)) || got.header.Get("Upload-Complete") != "?1" || got.header.Get("Upload-Draft-Interop-Version") != "6" {
		test.Fatalf("headers %v", got.header)
	}
	photoId := got.header.Get("X-Photo-ID")
	found := fixture.photoByClientId("plain-one")
	if found == nil || found.ID != photoId {
		test.Fatalf("the photo %v is not the one announced, %s", found, photoId)
	}
	if found.PhotoKind != models.PhotoKindPicture || found.PhotoSource != models.PhotoSourceIPhone || !found.IsFavorite || found.OriginalFilename != "IMG_0001.JPG" ||
		found.Latitude == nil || *found.Latitude != 10 || found.Longitude == nil || *found.Longitude != 20 ||
		found.TakenAt == nil || !found.TakenAt.Equal(time.Date(2024, 5, 17, 7, 30, 0, 0, time.UTC)) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != 120 ||
		found.PhotoProcessingState != models.PhotoProcessingWaiting {
		test.Fatalf("the photo does not say what the headers said: %+v", found)
	}
	files := fixture.photoFiles(found.ID)
	if len(files) != 1 || files[0].PhotoFileRole != models.PhotoFileRoleOriginal || files[0].BlobSHA256 != digestOf(picture) || files[0].FileSizeBytes != int64(len(picture)) || files[0].ContentType != "image/jpeg" {
		test.Fatalf("files %+v", files)
	}
	if !bytes.Equal(fixture.files.get(models.PhotoBlobFileID(digestOf(picture))), picture) {
		test.Fatal("the blob is not the picture")
	}
	if names := fixture.files.names(); len(names) != 1 {
		test.Fatalf("parts were left in the store: %v", names)
	}
}

func TestAnUploadResumesFromWhereItGotToWithEitherGenerationOfHeaders(test *testing.T) {
	for _, generation := range []struct {
		name                     string
		incompleteHeader         [2]string
		completeHeader           [2]string
		isLastRequestStillMarked bool
	}{
		{name: "complete", incompleteHeader: [2]string{"Upload-Complete", "?0"}, completeHeader: [2]string{"Upload-Complete", "?1"}},
		{name: "incomplete", incompleteHeader: [2]string{"Upload-Incomplete", "?1"}, completeHeader: [2]string{"Upload-Incomplete", "?0"}},
	} {
		test.Run(generation.name, func(test *testing.T) {
			fixture := newFixture(test)
			whole := randomBytes(8<<20+12345, 1)
			first := whole[:8<<20+1000]
			got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("resumable", generation.incompleteHeader[0], generation.incompleteHeader[1], "Upload-Length", strconv.Itoa(len(whole))), first)
			fixture.expect(got, http.StatusCreated)
			if got.header.Get("X-Photo-ID") != "" || got.header.Get("Upload-Complete") != "?0" || got.header.Get("Upload-Offset") != strconv.Itoa(len(first)) {
				test.Fatalf("an upload that is not finished said %v", got.header)
			}
			location := got.header.Get("Location")
			if fixture.photoByClientId("resumable") != nil {
				test.Fatal("a photo exists before its file is whole")
			}

			status := fixture.send(http.MethodHead, location, nil, nil)
			fixture.expect(status, http.StatusNoContent)
			if status.header.Get("Upload-Offset") != strconv.Itoa(len(first)) || status.header.Get("Upload-Complete") != "?0" || status.header.Get("Upload-Length") != strconv.Itoa(len(whole)) {
				test.Fatalf("HEAD said %v", status.header)
			}

			rest := whole[len(first):]
			patched := fixture.send(http.MethodPatch, location, map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": strconv.Itoa(len(first)), generation.completeHeader[0]: generation.completeHeader[1]}, rest)
			fixture.expect(patched, http.StatusCreated)
			found := fixture.photoByClientId("resumable")
			if found == nil || patched.header.Get("X-Photo-ID") != found.ID || patched.header.Get("Upload-Offset") != strconv.Itoa(len(whole)) || patched.header.Get("Upload-Complete") != "?1" {
				test.Fatalf("the finished upload said %v", patched.header)
			}
			if !bytes.Equal(fixture.files.get(models.PhotoBlobFileID(digestOf(whole))), whole) {
				test.Fatal("the blob is not what was sent in two requests")
			}
			done := fixture.send(http.MethodHead, location, nil, nil)
			if done.header.Get("Upload-Complete") != "?1" || done.header.Get("X-Photo-ID") != found.ID {
				test.Fatalf("a finished upload says %v", done.header)
			}
		})
	}
}

func TestAnIncompletePatchAnswersNoContentWithTheNewOffset(test *testing.T) {
	fixture := newFixture(test)
	first := randomBytes(1000, 2)
	second := randomBytes(500, 3)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("pieces", "Upload-Complete", "?0"), first)
	fixture.expect(created, http.StatusCreated)
	patched := fixture.send(http.MethodPatch, created.header.Get("Location"), map[string]string{"Upload-Offset": "1000", "Upload-Complete": "?0"}, second)
	fixture.expect(patched, http.StatusNoContent)
	if patched.header.Get("Upload-Offset") != "1500" {
		test.Fatalf("offset %q", patched.header.Get("Upload-Offset"))
	}
	last := fixture.send(http.MethodPatch, created.header.Get("Location"), map[string]string{"Upload-Offset": "1500", "Upload-Complete": "?1"}, nil)
	fixture.expect(last, http.StatusCreated)
	if !bytes.Equal(fixture.files.get(models.PhotoBlobFileID(digestOf(append(append([]byte{}, first...), second...)))), append(append([]byte{}, first...), second...)) {
		test.Fatal("three small parts did not make the file")
	}
}

func TestABodyCutInTheMiddleOfAPartLeavesTheOffsetAtTheLastWholePart(test *testing.T) {
	fixture := newFixture(test)
	whole := randomBytes(20<<20, 4)
	connection, err := net.Dial("tcp", fixture.server.Listener.Addr().String())
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	request := fmt.Sprintf("POST /api/v1/photos/uploads HTTP/1.1\r\nHost: %s\r\nContent-Type: image/jpeg\r\nX-Photo-Client-ID: cut\r\nUpload-Complete: ?1\r\nContent-Length: %d\r\n\r\n", fixture.server.Listener.Addr(), len(whole))
	written := make(chan error, 1)
	go func() {
		// Ten of twenty mebibytes, so one whole part and a piece of the next.
		_, err := connection.Write(append([]byte(request), whole[:10<<20]...))
		written <- err
	}()
	reader := textproto.NewReader(bufio.NewReader(connection))
	statusLine, err := reader.ReadLine()
	if err != nil || !strings.Contains(statusLine, " 104") {
		test.Fatalf("no 104 before the body was read: %q %v", statusLine, err)
	}
	header, err := reader.ReadMIMEHeader()
	if err != nil || header.Get("Location") == "" {
		test.Fatalf("the 104 has no location: %v %v", header, err)
	}
	location := header.Get("Location")
	if err := <-written; err != nil {
		test.Fatal(err)
	}
	_ = connection.Close()

	deadline := time.Now().Add(10 * time.Second)
	var status *answer
	for time.Now().Before(deadline) {
		status = fixture.send(http.MethodHead, location, nil, nil)
		if status.header.Get("Upload-Offset") == strconv.Itoa(8<<20) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status.header.Get("Upload-Offset") != strconv.Itoa(8<<20) {
		test.Fatalf("the offset is %q, want the one whole part of 8 MiB", status.header.Get("Upload-Offset"))
	}
	// And it can be finished from there.
	var finished *answer
	for time.Now().Before(deadline) {
		finished = fixture.send(http.MethodPatch, location, map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": strconv.Itoa(8 << 20), "Upload-Complete": "?1"}, whole[8<<20:])
		if finished.status != http.StatusConflict {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	fixture.expect(finished, http.StatusCreated)
	if !bytes.Equal(fixture.files.get(models.PhotoBlobFileID(digestOf(whole))), whole) {
		test.Fatal("the resumed upload is not the file")
	}
}

func TestAWrongOffsetIsAnsweredWithConflictAndTheRightOffset(test *testing.T) {
	fixture := newFixture(test)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("offsets", "Upload-Complete", "?0"), randomBytes(700, 5))
	fixture.expect(created, http.StatusCreated)
	for _, offset := range []string{"0", "699", "701"} {
		got := fixture.send(http.MethodPatch, created.header.Get("Location"), map[string]string{"Upload-Offset": offset, "Upload-Complete": "?0"}, []byte("more"))
		fixture.expect(got, http.StatusConflict)
		if got.header.Get("Upload-Offset") != "700" {
			test.Fatalf("offset %s was answered with %q", offset, got.header.Get("Upload-Offset"))
		}
	}
	missing := fixture.send(http.MethodPatch, created.header.Get("Location"), map[string]string{"Upload-Complete": "?0"}, []byte("more"))
	fixture.expect(missing, http.StatusBadRequest)
	if !strings.Contains(missing.body, "Upload-Offset") {
		test.Fatalf("the refusal does not name the header: %s", missing.body)
	}
	wrongType := fixture.send(http.MethodPatch, created.header.Get("Location"), map[string]string{"Content-Type": "text/plain", "Upload-Offset": "700"}, []byte("more"))
	fixture.expect(wrongType, http.StatusUnsupportedMediaType)
}

func TestADeclaredDigestThatDoesNotMatchIsRefused(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 90)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("wrong-digest", "X-Photo-Declared-SHA256", digestOf([]byte("something else"))), picture)
	fixture.expect(got, http.StatusBadRequest)
	if !strings.Contains(got.body, "X-Photo-Declared-SHA256") {
		test.Fatalf("the refusal does not name the header: %s", got.body)
	}
	if fixture.photoByClientId("wrong-digest") != nil || fixture.count("photo_blob") != "0" {
		test.Fatal("a file that failed its digest was kept")
	}
	status := fixture.send(http.MethodHead, got.header.Get("Location"), nil, nil)
	fixture.expect(status, http.StatusNotFound)
}

func TestTheSameBytesUnderTwoNamesMakeTwoPhotosAndOneBlob(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 120)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first-name"), picture)
	second := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("second-name"), picture)
	fixture.expect(first, http.StatusCreated)
	fixture.expect(second, http.StatusCreated)
	if first.header.Get("X-Photo-ID") == second.header.Get("X-Photo-ID") || fixture.count("photo") != "2" || fixture.count("photo_blob") != "1" || fixture.count("photo_file") != "2" {
		test.Fatalf("photos %s blobs %s files %s", fixture.count("photo"), fixture.count("photo_blob"), fixture.count("photo_file"))
	}
	// The same name again is the same photo.
	again := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first-name"), picture)
	fixture.expect(again, http.StatusCreated)
	if again.header.Get("X-Photo-ID") != first.header.Get("X-Photo-ID") || fixture.count("photo") != "2" {
		test.Fatal("the same name made another photo")
	}
}

func TestAnAccountOverItsLimitIsRefusedWithTheLimitInTheMessage(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("fits"), randomBytes(1000, 6))
	fixture.expect(first, http.StatusCreated)

	// A request that says how big the file is, before it is sent.
	early := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("early"), randomBytes(1000, 7))
	fixture.expect(early, http.StatusRequestEntityTooLarge)
	if !strings.Contains(early.body, "1500") {
		test.Fatalf("the refusal does not name the limit: %s", early.body)
	}
	if fixture.photoByClientId("early") != nil {
		test.Fatal("a refused file made a photo")
	}

	// One that does not, until it has all arrived.
	started := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("late", "Upload-Complete", "?0"), randomBytes(1000, 8))
	fixture.expect(started, http.StatusCreated)
	late := fixture.send(http.MethodPatch, started.header.Get("Location"), map[string]string{"Upload-Offset": "1000", "Upload-Complete": "?1"}, nil)
	fixture.expect(late, http.StatusRequestEntityTooLarge)
	if !strings.Contains(late.body, "1500") || fixture.photoByClientId("late") != nil {
		test.Fatalf("the refusal at the end: %s", late.body)
	}

	// A file the photo already has is let in again, though the account is full.
	retry := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("fits"), randomBytes(1000, 6))
	fixture.expect(retry, http.StatusCreated)
}

func TestAFileOverTheFileLimitIsRefusedBeforeItIsRead(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxFileBytes = 100
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("too-big"), randomBytes(200, 9))
	fixture.expect(got, http.StatusRequestEntityTooLarge)
	if !strings.Contains(got.body, "100") {
		test.Fatalf("the refusal does not name the limit: %s", got.body)
	}
	declared := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("declared-big", "Upload-Complete", "?0", "Upload-Length", "5000"), []byte("x"))
	fixture.expect(declared, http.StatusRequestEntityTooLarge)

	// And one that outgrows the limit while it is sent without a length.
	fixture.configuration.Photos.MaxFileBytes = 1000
	started := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("grows", "Upload-Complete", "?0"), randomBytes(800, 10))
	fixture.expect(started, http.StatusCreated)
	grown := fixture.send(http.MethodPatch, started.header.Get("Location"), map[string]string{"Upload-Offset": "800", "Upload-Complete": "?0"}, randomBytes(400, 11))
	fixture.expect(grown, http.StatusRequestEntityTooLarge)
}

func TestHeadersThatAreWrongAreRefusedByName(test *testing.T) {
	fixture := newFixture(test)
	for _, bad := range []struct{ header, value string }{
		{"X-Photo-File-Role", "thumbnail"}, {"X-Photo-Kind", "gif"}, {"X-Photo-Source", "conversation"}, {"X-Photo-Source", "somewhere"},
		{"X-Photo-Taken-At", "yesterday"}, {"X-Photo-Taken-At", "2024-05-17 09:30:00"},
		{"X-Photo-Latitude", "91"}, {"X-Photo-Latitude", "north"}, {"X-Photo-Longitude", "-181"},
		{"X-Photo-Is-Favorite", "maybe"}, {"X-Photo-Is-Screenshot", "2"},
		{"X-Photo-Declared-SHA256", "abc"}, {"X-Photo-Filename", "../escape.jpg"}, {"X-Photo-Filename", strings.Repeat("a", 256)},
		{"X-Photo-Client-ID", strings.Repeat("a", 256)}, {"Upload-Complete", "yes"}, {"Upload-Length", "-1"}, {"Upload-Incomplete", "1"},
	} {
		headers := jpegHeaders("bad-headers")
		headers[bad.header] = bad.value
		got := fixture.send(http.MethodPost, fixture.collectionUrl(), headers, []byte("bytes"))
		fixture.expect(got, http.StatusBadRequest)
		if !strings.Contains(got.body, bad.header) {
			test.Fatalf("%s: %s was refused as %q", bad.value, bad.header, got.body)
		}
	}
	missing := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "image/jpeg"}, []byte("bytes"))
	fixture.expect(missing, http.StatusBadRequest)
	if !strings.Contains(missing.body, "X-Photo-Client-ID") {
		test.Fatalf("the missing name was refused as %q", missing.body)
	}
	oneCoordinate := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("one-coordinate", "X-Photo-Latitude", "10"), []byte("bytes"))
	fixture.expect(oneCoordinate, http.StatusBadRequest)
	if fixture.count("photo_upload") != "0" {
		test.Fatal("a refused request left an upload behind")
	}
}

func TestATrashedPhotoStaysTrashedWhenItsFileArrivesAgain(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 150)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("trashed"), picture)
	fixture.expect(first, http.StatusCreated)
	photoId := first.header.Get("X-Photo-ID")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(fixture.userId, []string{photoId}); err != nil {
			test.Fatal(err)
		}
	})
	for _, content := range [][]byte{picture, smallJpeg(test, 160)} {
		again := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("trashed"), content)
		fixture.expect(again, http.StatusCreated)
		if again.header.Get("X-Photo-ID") != photoId {
			test.Fatal("the file made another photo")
		}
		found := fixture.photoByClientId("trashed")
		if found.TrashedAt == nil {
			test.Fatal("the photo came out of the trash")
		}
	}
	files := fixture.photoFiles(photoId)
	if len(files) != 1 || files[0].BlobSHA256 != digestOf(smallJpeg(test, 160)) {
		test.Fatalf("the replaced file is not the newest: %+v", files)
	}
}

func TestALiveVideoJoinsThePhotoItBelongsTo(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 170)
	video := randomBytes(3000, 12)
	// The video arrives first, as parallel uploads allow.
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "video/quicktime", "X-Photo-Client-ID": "live", "X-Photo-File-Role": "liveVideo"}, video)
	fixture.expect(first, http.StatusCreated)
	second := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("live", "X-Photo-Kind", "livePhoto"), picture)
	fixture.expect(second, http.StatusCreated)
	if first.header.Get("X-Photo-ID") != second.header.Get("X-Photo-ID") {
		test.Fatal("the two files made two photos")
	}
	found := fixture.photoByClientId("live")
	files := fixture.photoFiles(found.ID)
	if found.PhotoKind != models.PhotoKindLivePhoto || len(files) != 2 || files[0].PhotoFileRole != models.PhotoFileRoleLiveVideo || files[1].PhotoFileRole != models.PhotoFileRoleOriginal {
		test.Fatalf("%+v %+v", found, files)
	}
}

func TestAFileThatIsNeitherAPictureNorAVideoIsRefused(test *testing.T) {
	fixture := newFixture(test)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "application/pdf", "X-Photo-Client-ID": "document"}, []byte("%PDF-1.4 not a photo"))
	fixture.expect(got, http.StatusUnsupportedMediaType)
	// A picture sent as a form sends it is told by what it is.
	form := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "X-Photo-Client-ID": "as-a-form"}, smallJpeg(test, 10))
	fixture.expect(form, http.StatusCreated)
	files := fixture.photoFiles(form.header.Get("X-Photo-ID"))
	if len(files) != 1 || files[0].ContentType != "image/jpeg" {
		test.Fatalf("%+v", files)
	}
	named := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "application/octet-stream", "X-Photo-Client-ID": "by-name", "X-Photo-Filename": "IMG_0002.HEIC"}, randomBytes(100, 13))
	fixture.expect(named, http.StatusCreated)
	if files := fixture.photoFiles(named.header.Get("X-Photo-ID")); len(files) != 1 || files[0].ContentType != "image/heic" {
		test.Fatalf("%+v", files)
	}
}

func TestDeletingAnUploadEndsItAndNothingMoreIsAcceptedForIt(test *testing.T) {
	fixture := newFixture(test)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("cancelled", "Upload-Complete", "?0"), randomBytes(500, 14))
	fixture.expect(created, http.StatusCreated)
	location := created.header.Get("Location")
	fixture.expect(fixture.send(http.MethodDelete, location, nil, nil), http.StatusNoContent)
	fixture.expect(fixture.send(http.MethodHead, location, nil, nil), http.StatusNotFound)
	fixture.expect(fixture.send(http.MethodPatch, location, map[string]string{"Upload-Offset": "500", "Upload-Complete": "?1"}, nil), http.StatusNotFound)
	fixture.expect(fixture.send(http.MethodDelete, location, nil, nil), http.StatusNoContent)
	fixture.expect(fixture.send(http.MethodHead, fixture.collectionUrl()+"/no-such-upload", nil, nil), http.StatusNotFound)
}

func TestAnUploadBelongsToItsOwner(test *testing.T) {
	fixture := newFixture(test)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("mine", "Upload-Complete", "?0"), randomBytes(500, 15))
	fixture.expect(created, http.StatusCreated)
	// The same server for somebody else's account.
	other := dbtest.CreateUser(test, fixture.database, "somebody-else")
	var found *models.PhotoUpload
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		uploadId := created.header.Get("Location")[strings.LastIndex(created.header.Get("Location"), "/")+1:]
		var err error
		found, err = transaction.GetPhotoUpload(other, uploadId)
		return err
	})
	if err == nil || found != nil {
		test.Fatalf("somebody else read the upload: %v", err)
	}
}
