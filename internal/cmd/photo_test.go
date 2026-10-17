package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Everything a person does with their photos is on every surface they use:
// the API, the command line and the agent's photo tool (and the dashboard and
// the phone, which are checked where they are written). One table names each,
// and nothing exists on a surface that the table does not list.
func TestPhotoParity(test *testing.T) {
	test.Parallel()

	// For each operation: the document the command line and the tool send
	// for it, the subcommands of teanode photo that use it, and the
	// operations of the photo tool that do. An empty list is a surface that
	// deliberately does not do it, and the comment says why.
	table := map[string]struct {
		document       string
		subcommands    []string
		toolOperations []string
	}{
		"ListPhotos":       {client.DocumentListPhotos, []string{"list"}, []string{"list"}},
		"CountPhotos":      {client.DocumentCountPhotos, []string{"list"}, []string{"list"}},
		"ReadPhoto":        {client.DocumentReadPhoto, []string{"show", "download"}, []string{"read", "look", "show"}},
		"ReadPhotoLibrary": {client.DocumentReadPhotoLibrary, []string{"library"}, []string{"library"}},
		"UpdatePhoto":      {client.DocumentUpdatePhoto, []string{"favorite"}, []string{"favorite"}},
		"TrashPhotos":      {client.DocumentTrashPhotos, []string{"trash"}, []string{"trash"}},
		"RestorePhotos":    {client.DocumentRestorePhotos, []string{"restore"}, []string{"restore"}},
		// What an uploader asks to decide what to skip; the agent keeps no
		// files of its own to upload.
		"FindPhotos": {client.DocumentFindPhotos, []string{"upload"}, nil},
		// Forever. The agent can move photos to the trash and back, and
		// only the person empties it.
		"DeletePhotos":    {client.DocumentDeletePhotos, []string{"delete"}, nil},
		"EmptyPhotoTrash": {client.DocumentEmptyPhotoTrash, []string{"empty-trash"}, nil},
		// The command line sends a file itself, with upload; the agent saves
		// a file of a conversation, a message or the computer.
		"SavePhotoFromAttachment": {client.DocumentSavePhotoFromAttachment, nil, []string{"save"}},
	}

	// What the command line does without a GraphQL operation: the upload
	// endpoint and the file route are plain HTTP.
	commandLineOnly := map[string]bool{}
	// What the tool does that is not one operation of its own.
	toolOnly := map[string]bool{}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.PhotoQuery](), reflect.TypeFor[apigraph.PhotoMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}

	subcommands := map[string]bool{}
	for _, subcommand := range NewPhotoCommand().Commands {
		subcommands[subcommand.Name] = true
	}
	toolOperations := map[string]bool{}
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("the agent has no photo tool")
	}
	properties, _ := photoTool.Parameters["properties"].(map[string]any)
	operation, _ := properties["operation"].(map[string]any)
	switch enum := operation["enum"].(type) {
	case []string:
		for _, name := range enum {
			toolOperations[name] = true
		}
	case []any:
		for _, name := range enum {
			toolOperations[name.(string)] = true
		}
	}

	listedSubcommands, listedToolOperations := map[string]bool{}, map[string]bool{}
	for name, entry := range table {
		if !operations[name] {
			test.Errorf("the API has no operation %s", name)
		}
		if !strings.Contains(entry.document, " "+name+"(") && !strings.Contains(entry.document, " "+name+" ") {
			test.Errorf("the document for %s does not call it:\n%s", name, entry.document)
		}
		for _, subcommand := range entry.subcommands {
			listedSubcommands[subcommand] = true
			if !subcommands[subcommand] {
				test.Errorf("teanode photo has no %s, which does %s", subcommand, name)
			}
		}
		for _, toolOperation := range entry.toolOperations {
			listedToolOperations[toolOperation] = true
			if !toolOperations[toolOperation] {
				test.Errorf("the photo tool has no %s, which does %s", toolOperation, name)
			}
		}
	}
	for name := range operations {
		if _, isListed := table[name]; !isListed {
			test.Errorf("the API's %s is on no other surface", name)
		}
	}
	for subcommand := range subcommands {
		if !listedSubcommands[subcommand] && !commandLineOnly[subcommand] {
			test.Errorf("teanode photo %s does nothing the table lists", subcommand)
		}
	}
	for toolOperation := range toolOperations {
		if !listedToolOperations[toolOperation] && !toolOnly[toolOperation] {
			test.Errorf("the photo tool's %s does nothing the table lists", toolOperation)
		}
	}
	// download and upload speak plain HTTP besides: they are in the table
	// through the operations they use, and say so here so that removing the
	// operation is noticed.
	for _, name := range []string{"upload", "download"} {
		if !subcommands[name] {
			test.Errorf("teanode photo has no %s", name)
		}
	}
}

func TestPhotoDatesAreDaysInTheShellsZoneOrFullTimes(test *testing.T) {
	test.Parallel()
	day, err := parsePhotoDate("after", "2025-07-12")
	if err != nil || day.Year() != 2025 || day.Month() != 7 || day.Day() != 12 || day.Hour() != 0 || day.Location() != time.Local {
		test.Fatalf("%v %v", day, err)
	}
	moment, err := parsePhotoDate("after", "2025-07-12T10:00:00+02:00")
	if err != nil || !moment.Equal(time.Date(2025, 7, 12, 8, 0, 0, 0, time.UTC)) {
		test.Fatalf("%v %v", moment, err)
	}
	if none, err := parsePhotoDate("after", ""); err != nil || none != nil {
		test.Fatalf("%v %v", none, err)
	}
	if _, err := parsePhotoDate("before", "last tuesday"); err == nil || !strings.Contains(err.Error(), "--before") {
		test.Fatalf("%v", err)
	}
}

func TestAFolderIsWalkedForPicturesAndVideosAndNotForHiddenOrOtherFiles(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	write := func(path string) string {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			test.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			test.Fatal(err)
		}
		return full
	}
	first, second, third := write("a.JPG"), write("trip/b.heic"), write("trip/clips/c.mov")
	write(".hidden.jpg")
	write(".thumbnails/d.jpg")
	write("trip/.e.jpg")
	write("notes.txt")
	write("trip/sidecar.xmp")
	explicit := write("album.txt")
	if err := os.Symlink(first, filepath.Join(root, "link.jpg")); err != nil {
		test.Skipf("no symbolic links here: %v", err)
	}
	paths, otherFileCount, err := collectPhotoPaths([]string{root, explicit, first})
	if err != nil {
		test.Fatal(err)
	}
	// The explicit file is taken whatever it is, a file named twice once.
	want := []string{first, filepath.Join(root, "link.jpg"), second, third, explicit}
	sort.Strings(want)
	got := append([]string{}, paths...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) || otherFileCount != 3 {
		test.Fatalf("paths %v, other %d; want %v and 3", got, otherFileCount, want)
	}
	if _, _, err := collectPhotoPaths([]string{filepath.Join(root, "nowhere")}); err == nil {
		test.Fatal("a file that is not there was collected")
	}
}

func digestHex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// photoServer is the least of a server the photo commands talk to: the GraphQL
// endpoint, the upload endpoint and the file route.
type photoServer struct {
	*httptest.Server
	mutex sync.Mutex
	// known are the client names FindPhotos says are there.
	known []string
	// uploads are the headers of each upload that started.
	uploads []http.Header
	// files are the bytes of each photo's original, by photo identifier.
	files map[string][]byte
	// photoView is what ReadPhoto answers, by photo identifier.
	photoViews map[string]string
	// documents are the GraphQL documents received.
	documents []string
	variables []map[string]any
}

func newPhotoServer(test *testing.T) *photoServer {
	test.Helper()
	self := &photoServer{files: map[string][]byte{}, photoViews: map[string]string{}}
	self.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		switch {
		case request.URL.Path == "/api/v1/graphql":
			var asked struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(request.Body).Decode(&asked); err != nil {
				http.Error(response, err.Error(), http.StatusBadRequest)
				return
			}
			self.documents = append(self.documents, asked.Query)
			self.variables = append(self.variables, asked.Variables)
			response.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(asked.Query, "FindPhotos("):
				var found []map[string]any
				for _, name := range asked.Variables["clientPhotoIds"].([]any) {
					if slices.Contains(self.known, name.(string)) {
						found = append(found, map[string]any{"id": "photo-known", "clientPhotoId": name})
					}
				}
				_ = json.NewEncoder(response).Encode(map[string]any{"data": map[string]any{"FindPhotos": found}})
			case strings.Contains(asked.Query, "ReadPhoto("):
				view, isKnown := self.photoViews[asked.Variables["photoId"].(string)]
				if !isKnown {
					_, _ = response.Write([]byte(`{"errors":[{"message":"api: not found"}]}`))
					return
				}
				_, _ = response.Write([]byte(`{"data":{"ReadPhoto":` + view + `}}`))
			case strings.Contains(asked.Query, "TrashPhotos("):
				_, _ = response.Write([]byte(`{"data":{"TrashPhotos":2}}`))
			case strings.Contains(asked.Query, "ListPhotos("):
				_, _ = response.Write([]byte(`{"data":{"ListPhotos":[{"id":"photo-1","photoKind":"picture","takenAt":"2025-03-04T10:30:00Z","takenAtOffsetMinutes":60,"placeName":"Example City","widthPixels":40,"heightPixels":30,"originalFilename":"IMG_1.JPG","photoProcessingState":"ready"}]}}`))
			default:
				_, _ = response.Write([]byte(`{"data":{}}`))
			}
		case request.URL.Path == "/api/v1/photos/uploads":
			content, _ := io.ReadAll(request.Body)
			self.uploads = append(self.uploads, request.Header.Clone())
			if strings.HasPrefix(request.Header.Get("Content-Type"), "text/plain") {
				http.Error(response, "files of type text/plain are not kept", http.StatusUnsupportedMediaType)
				return
			}
			response.Header().Set("Upload-Complete", "?1")
			response.Header().Set("Upload-Offset", "1")
			response.Header().Set("X-Photo-ID", "photo-"+digestHex(content)[:8])
			response.WriteHeader(http.StatusCreated)
		case strings.HasPrefix(request.URL.Path, "/api/v1/photos/") && strings.Contains(request.URL.Path, "/files/"):
			photoId := strings.Split(strings.TrimPrefix(request.URL.Path, "/api/v1/photos/"), "/")[0]
			content, isKnown := self.files[photoId]
			if !isKnown {
				http.NotFound(response, request)
				return
			}
			_, _ = response.Write(content)
		default:
			http.NotFound(response, request)
		}
	}))
	test.Cleanup(self.Close)
	return self
}

// run runs a photo command against the server and returns what it wrote to
// standard output, which is where the tables and JSON go.
func (self *photoServer) run(test *testing.T, arguments ...string) (string, string, error) {
	test.Helper()
	var writer bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &writer,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: self.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewPhotoCommand()},
	}
	standardOutput := captureStandardOutput(test, func() {
		err := command.Run(test.Context(), append([]string{"fixture", "photo"}, arguments...))
		if err != nil {
			writer.WriteString("ERROR: " + err.Error())
		}
	})
	text := writer.String()
	if index := strings.Index(text, "ERROR: "); index >= 0 {
		return text[:index], standardOutput, &commandFailure{message: text[index+len("ERROR: "):]}
	}
	return text, standardOutput, nil
}

type commandFailure struct{ message string }

func (self *commandFailure) Error() string { return self.message }

// captureStandardOutput is what a function writes to os.Stdout. The tests
// that use it do not run side by side.
func captureStandardOutput(test *testing.T, run func()) string {
	test.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		content, _ := io.ReadAll(reader)
		done <- string(content)
	}()
	run()
	os.Stdout = original
	_ = writer.Close()
	return <-done
}

func TestUploadSkipsWhatTheLibraryHasAndSaysWhatBecameOfEachFile(test *testing.T) {
	server := newPhotoServer(test)
	directory := test.TempDir()
	fresh, known, document := []byte("a new picture"), []byte("a picture already here"), []byte("not a photo at all")
	for name, content := range map[string][]byte{"fresh.jpg": fresh, "known.png": known, "notes.txt": document} {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
			test.Fatal(err)
		}
	}
	server.known = []string{digestHex(known)}

	// The folder is walked; the text file is named, so it is sent, and the
	// server refuses it.
	output, _, err := server.run(test, "upload", directory, filepath.Join(directory, "notes.txt"))
	if err == nil || !strings.Contains(err.Error(), "1 of the files could not be uploaded") {
		test.Fatalf("a failed file did not fail the command: %v", err)
	}
	if !strings.Contains(output, "1 uploaded, 1 already there, 1 failed") {
		test.Fatalf("no summary:\n%s", output)
	}
	if !strings.Contains(output, "uploaded         "+filepath.Join(directory, "fresh.jpg")) ||
		!strings.Contains(output, "already there    "+filepath.Join(directory, "known.png")) ||
		!strings.Contains(output, "failed           "+filepath.Join(directory, "notes.txt")+": ") ||
		!strings.Contains(output, "text/plain") {
		test.Fatalf("the lines:\n%s", output)
	}
	if len(server.uploads) != 2 {
		test.Fatalf("%d files were sent", len(server.uploads))
	}
	var freshHeaders http.Header
	for _, headers := range server.uploads {
		if headers.Get("X-Photo-Client-ID") == digestHex(fresh) {
			freshHeaders = headers
		}
	}
	if freshHeaders == nil || freshHeaders.Get("X-Photo-Declared-SHA256") != digestHex(fresh) || freshHeaders.Get("X-Photo-Source") != "commandLine" ||
		freshHeaders.Get("Content-Type") != "image/jpeg" || freshHeaders.Get("X-Photo-Filename") != "fresh.jpg" || freshHeaders.Get("Upload-Length") != "13" {
		test.Fatalf("the headers of the new picture: %v", freshHeaders)
	}
	// One question about all the files of the folder, not one for each.
	asked := 0
	for _, document := range server.documents {
		if strings.Contains(document, "FindPhotos(") {
			asked++
		}
	}
	if asked != 1 {
		test.Fatalf("FindPhotos was asked %d times", asked)
	}
}

func TestUploadInJSONSaysEachFilesOutcome(test *testing.T) {
	server := newPhotoServer(test)
	directory := test.TempDir()
	content := []byte("a picture")
	path := filepath.Join(directory, "one.jpg")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		test.Fatal(err)
	}
	server.known = []string{digestHex(content)}
	_, output, err := server.run(test, "upload", "--json", path)
	if err != nil {
		test.Fatal(err)
	}
	var answer struct {
		Results           []map[string]any `json:"results"`
		UploadedCount     int              `json:"uploadedCount"`
		AlreadyThereCount int              `json:"alreadyThereCount"`
		FailedCount       int              `json:"failedCount"`
	}
	if err := json.Unmarshal([]byte(output), &answer); err != nil {
		test.Fatalf("%v: %s", err, output)
	}
	if answer.AlreadyThereCount != 1 || answer.UploadedCount != 0 || answer.FailedCount != 0 || len(answer.Results) != 1 || answer.Results[0]["outcome"] != "alreadyThere" {
		test.Fatalf("%s", output)
	}
	if _, _, err := server.run(test, "upload"); err == nil {
		test.Fatal("an upload of nothing was accepted")
	}
}

func TestDownloadNeverReplacesAFileWithoutForceAndNeverLeavesAHalfFile(test *testing.T) {
	server := newPhotoServer(test)
	content := bytes.Repeat([]byte("pixels"), 50)
	server.files["photo-1"] = content
	server.photoViews["photo-1"] = `{"photo":{"id":"photo-1","originalFilename":"../../IMG_0001.JPG"},"photoFiles":[{"photoId":"photo-1","photoFileRole":"original","contentType":"image/jpeg","fileSizeBytes":300,"originalFilename":"../../IMG_0001.JPG"}],"photoPreviews":[]}`
	directory := test.TempDir()

	// Into a folder, under the name the photo has, which is a name and not a
	// path whatever the server says.
	output, _, err := server.run(test, "download", "photo-1", "--output", directory)
	if err != nil {
		test.Fatal(err)
	}
	saved := filepath.Join(directory, "IMG_0001.JPG")
	if got, err := os.ReadFile(saved); err != nil || !bytes.Equal(got, content) || !strings.Contains(output, "saved "+saved) {
		test.Fatalf("%v %q", err, output)
	}
	if entries, _ := os.ReadDir(directory); len(entries) != 1 {
		test.Fatalf("the folder holds %v", entries)
	}

	// A second download does not replace it.
	server.files["photo-1"] = bytes.Repeat([]byte("other!"), 50)
	if _, _, err := server.run(test, "download", "photo-1", "--output", saved); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Fatalf("%v", err)
	}
	if got, _ := os.ReadFile(saved); !bytes.Equal(got, content) {
		test.Fatal("the file was replaced without --force")
	}
	if _, _, err := server.run(test, "download", "photo-1", "--output", saved, "--force"); err != nil {
		test.Fatal(err)
	}
	if got, _ := os.ReadFile(saved); bytes.Equal(got, content) {
		test.Fatal("--force did not replace the file")
	}

	// A file that arrives short is not kept, and the name is not taken.
	server.files["photo-1"] = server.files["photo-1"][:100]
	short := filepath.Join(directory, "short.jpg")
	if _, _, err := server.run(test, "download", "photo-1", "--output", short); err == nil {
		test.Fatal("a short file was saved")
	}
	if entries, _ := os.ReadDir(directory); len(entries) != 1 {
		test.Fatalf("a failed download left %v", entries)
	}

	// A file the photo does not have, and a photo that is not there.
	if _, _, err := server.run(test, "download", "photo-1", "--file", "edited"); err == nil || !strings.Contains(err.Error(), "no edited file") {
		test.Fatalf("%v", err)
	}
	if _, _, err := server.run(test, "download", "photo-missing"); err == nil {
		test.Fatal("a photo that is not there was downloaded")
	}
	if _, _, err := server.run(test, "download", "photo-1", "--file", "sideways"); err == nil {
		test.Fatal("an unknown file role was accepted")
	}
}

func TestListSendsItsFiltersAndPrintsJSONOrATable(test *testing.T) {
	server := newPhotoServer(test)
	_, output, err := server.run(test, "list", "--json", "--favorites", "--trash", "--search", "harbor", "--after", "2025-01-02", "--kind", "video", "--limit", "7", "--continue-after", "photo-9")
	if err != nil {
		test.Fatal(err)
	}
	var photos []models.Photo
	if err := json.Unmarshal([]byte(output), &photos); err != nil || len(photos) != 1 || photos[0].ID != "photo-1" {
		test.Fatalf("%v: %s", err, output)
	}
	variables := server.variables[len(server.variables)-1]
	if variables["isFavorite"] != true || variables["isTrashed"] != true || variables["searchText"] != "harbor" || variables["photoKind"] != "video" ||
		variables["pageSize"] != float64(7) || variables["beforePhotoId"] != "photo-9" || variables["takenAfter"] == nil {
		test.Fatalf("%v", variables)
	}
	_, table, err := server.run(test, "list")
	if err != nil {
		test.Fatal(err)
	}
	// The time is the camera's, in its own zone.
	if !strings.Contains(table, "photo-1") || !strings.Contains(table, "2025-03-04 11:30") || !strings.Contains(table, "Example City") || !strings.Contains(table, "40x30") || !strings.Contains(table, "IMG_1.JPG") {
		test.Fatalf("the table:\n%s", table)
	}
	for _, arguments := range [][]string{{"list", "--limit", "501"}, {"list", "--kind", "gif"}, {"list", "--after", "yesterday"}} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
}

func TestTrashNamesThePhotosAndDeleteAsksFirst(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "trash", "photo-1", "photo-2")
	if err != nil || !strings.Contains(output, "moved to the trash 2 of 2 photos") {
		test.Fatalf("%v: %s", err, output)
	}
	if got := server.variables[len(server.variables)-1]["photoIds"]; !reflect.DeepEqual(got, []any{"photo-1", "photo-2"}) {
		test.Fatalf("%v", got)
	}
	if _, _, err := server.run(test, "trash"); err == nil {
		test.Fatal("trashing nothing was accepted")
	}
	// No terminal to ask on, so deleting forever without --force is refused
	// before anything is sent.
	before := len(server.documents)
	if _, _, err := server.run(test, "delete", "photo-1"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Fatalf("%v", err)
	}
	if _, _, err := server.run(test, "empty-trash"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Fatalf("%v", err)
	}
	if len(server.documents) != before {
		test.Fatal("something was sent without confirmation")
	}
}
