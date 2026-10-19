package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		"ListPhotos":  {client.DocumentListPhotos, []string{"list"}, []string{"list"}},
		"CountPhotos": {client.DocumentCountPhotos, []string{"list"}, []string{"list"}},
		// How much of a date range a search by what is in the pictures cannot
		// see into: said beside a search of a range.
		"CountUndescribedPhotos": {client.DocumentCountUndescribedPhotos, []string{"list"}, []string{"list"}},
		"ReadPhoto":              {client.DocumentReadPhoto, []string{"read", "download"}, []string{"read", "look", "show"}},
		// Where a photo was taken, by the person's own place or the street:
		// said on the Place line of read, and beside the photo's details.
		"ReadPhotoPlace":   {client.DocumentReadPhotoPlace, []string{"read"}, []string{"read"}},
		"ReadPhotoLibrary": {client.DocumentReadPhotoLibrary, []string{"library"}, []string{"library"}},
		// For a player that cannot send the person's sign-in; neither the
		// command line nor the agent plays anything.
		"PhotoPlaybackAddress": {client.DocumentPhotoPlaybackAddress, nil, nil},
		// Marking a favorite and correcting a date are both an update of the photo.
		"UpdatePhoto": {client.DocumentUpdatePhoto, []string{"favorite", "set-date"}, []string{"favorite", "set-date"}},
		// Where a photo was taken: finding a city to pick, setting the place
		// from one or from a position, and taking the place away.
		"SearchPhotoPlaces":   {client.DocumentSearchPhotoPlaces, []string{"search-places", "set-location"}, []string{"search-places", "set-location"}},
		"SetPhotoLocation":    {client.DocumentSetPhotoLocation, []string{"set-location"}, []string{"set-location"}},
		"RemovePhotoLocation": {client.DocumentRemovePhotoLocation, []string{"remove-location"}, []string{"remove-location"}},
		"TrashPhotos":         {client.DocumentTrashPhotos, []string{"trash"}, []string{"trash"}},
		"RestorePhotos":       {client.DocumentRestorePhotos, []string{"restore"}, []string{"restore"}},
		// Making the previews of photos again: a person whose photo failed, or
		// looks wrong, and the agent they ask to do it.
		"RetryPhotoProcessing": {client.DocumentRetryPhotoProcessing, []string{"retry"}, []string{"retry"}},
		// What an uploader asks to decide what to skip; the agent keeps no
		// files of its own to upload.
		"FindPhotos": {client.DocumentFindPhotos, []string{"upload"}, nil},
		// Forever. The agent's call asks the person first.
		"DeletePhotos":    {client.DocumentDeletePhotos, []string{"delete"}, []string{"delete"}},
		"EmptyPhotoTrash": {client.DocumentEmptyPhotoTrash, []string{"empty-trash"}, []string{"empty-trash"}},
		// Asking for a description is a person looking at one photo, or the
		// agent looking at some.
		"DescribePhoto": {client.DocumentDescribePhoto, []string{"describe"}, []string{"describe", "look"}},
		// The command line sends a file itself, with upload, and keeps a file of
		// a conversation with save --attachment; the agent saves a file of a
		// conversation, a message or the computer.
		"SavePhotoFromAttachment": {client.DocumentSavePhotoFromAttachment, []string{"save"}, []string{"save"}},

		// Albums. The command line's are subcommands of album.
		"ListPhotoAlbums":       {client.DocumentListPhotoAlbums, []string{"album list"}, []string{"album-list"}},
		"ReadPhotoAlbum":        {client.DocumentReadPhotoAlbum, []string{"album read"}, []string{"album-read"}},
		"CreatePhotoAlbum":      {client.DocumentCreatePhotoAlbum, []string{"album create"}, []string{"album-create"}},
		"RenamePhotoAlbum":      {client.DocumentRenamePhotoAlbum, []string{"album rename"}, []string{"album-rename"}},
		"DeletePhotoAlbum":      {client.DocumentDeletePhotoAlbum, []string{"album delete"}, []string{"album-delete"}},
		"AddPhotosToAlbum":      {client.DocumentAddPhotosToAlbum, []string{"album add"}, []string{"album-add"}},
		"RemovePhotosFromAlbum": {client.DocumentRemovePhotosFromAlbum, []string{"album remove"}, []string{"album-remove"}},
		"SuggestPhotoAlbums":    {client.DocumentSuggestPhotoAlbums, []string{"album suggest"}, []string{"album-suggest"}},
		"SetPhotoAlbumCover":    {client.DocumentSetPhotoAlbumCover, []string{"album cover"}, []string{"album-cover"}},

		// Sharing an album with a person or a circle. Sharing again with the
		// same grantee changes what they have; changing one share by its
		// identifier, as the dashboard and the phone edit a row of the sheet,
		// is UpdatePhotoAlbumShare.
		"SharePhotoAlbum":       {client.DocumentSharePhotoAlbum, []string{"album share"}, []string{"album-share"}},
		"UpdatePhotoAlbumShare": {client.DocumentUpdatePhotoAlbumShare, []string{"album share-update"}, []string{"album-share-update"}},
		"RemovePhotoAlbumShare": {client.DocumentRemovePhotoAlbumShare, []string{"album unshare"}, []string{"album-unshare"}},
		"ListPhotoAlbumShares":  {client.DocumentListPhotoAlbumShares, []string{"album shares"}, []string{"album-shares"}},
		// Keeping a photo of a shared album in one's own library.
		"SavePhotoToLibrary": {client.DocumentSavePhotoToLibrary, []string{"save"}, []string{"save-shared"}},
	}

	// What the command line does without a GraphQL operation: the upload
	// endpoint and the file route are plain HTTP.
	commandLineOnly := map[string]bool{}
	// What the tool does that is not one operation of its own.
	toolOnly := map[string]bool{}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{
		reflect.TypeFor[apigraph.PhotoQuery](), reflect.TypeFor[apigraph.PhotoPlaceQuery](), reflect.TypeFor[apigraph.PhotoMutation](),
		reflect.TypeFor[apigraph.PhotoAlbumQuery](), reflect.TypeFor[apigraph.PhotoAlbumMutation](),
	} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}

	subcommands := map[string]bool{}
	for _, subcommand := range NewPhotoCommand().Commands {
		if len(subcommand.Commands) == 0 {
			subcommands[subcommand.Name] = true
		}
		for _, nested := range subcommand.Commands {
			subcommands[subcommand.Name+" "+nested.Name] = true
		}
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
		if !listedSubcommands[subcommand] && !commandLineOnly[subcommand] && !isPhotoFaceSurface(subcommand) {
			test.Errorf("teanode photo %s does nothing the table lists", subcommand)
		}
	}
	for toolOperation := range toolOperations {
		if !listedToolOperations[toolOperation] && !toolOnly[toolOperation] && !isPhotoFaceSurface(toolOperation) {
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

// fixtureAlbum is an album the fixture server answers with.
const fixtureAlbum = `{"id":"album-1","userId":"u","albumTitle":"Picnic","photoCount":3,"firstTakenAt":"2031-07-12T09:00:00Z","lastTakenAt":"2031-07-12T09:00:00Z","coverPreviewUrl":"/x"}`

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

	// photoPlaces is what ReadPhotoPlace answers for a photo, by id; a photo
	// not in it is answered null.
	photoPlaces map[string]string
	// albumsWithOthers makes the album listing hold an album shared with the
	// person and one that belongs to a circle.
	albumsWithOthers bool

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
			case strings.Contains(asked.Query, "ReadPhotoPlace("):
				answer, isKnown := self.photoPlaces[asked.Variables["photoId"].(string)]
				if !isKnown {
					answer = "null"
				}
				_, _ = response.Write([]byte(`{"data":{"ReadPhotoPlace":` + answer + `}}`))
			case strings.Contains(asked.Query, "TrashPhotos("):
				_, _ = response.Write([]byte(`{"data":{"TrashPhotos":2}}`))
			case strings.Contains(asked.Query, "RetryPhotoProcessing("):
				_, _ = response.Write([]byte(`{"data":{"RetryPhotoProcessing":1}}`))
			case strings.Contains(asked.Query, "ListCirclePeople"):
				_, _ = response.Write([]byte(`{"data":{"ListCirclePeople":[{"userId":"user-ben","username":"ben","name":"Ben Example"}]}}`))
			case strings.Contains(asked.Query, "ListCircles"):
				_, _ = response.Write([]byte(`{"data":{"ListCircles":[{"id":"circle-home","name":"Home","circleKind":"family","members":[]}]}}`))
			case strings.Contains(asked.Query, "SharePhotoAlbum("):
				granteeCircleId, _ := asked.Variables["granteeCircleId"].(string)
				if granteeCircleId != "" {
					_, _ = response.Write([]byte(`{"data":{"SharePhotoAlbum":{"id":"share-circle","photoAlbumId":"album-1","granteeCircleId":"` + granteeCircleId + `","granteeCircleName":"Home","shareLevel":"` + asked.Variables["shareLevel"].(string) + `","isLocationIncluded":` + fmt.Sprint(asked.Variables["isLocationIncluded"]) + `}}}`))
					return
				}
				_, _ = response.Write([]byte(`{"data":{"SharePhotoAlbum":{"id":"share-person","photoAlbumId":"album-1","granteeUserId":"` + asked.Variables["granteeUserId"].(string) + `","granteeUsername":"ben","shareLevel":"` + asked.Variables["shareLevel"].(string) + `","isLocationIncluded":` + fmt.Sprint(asked.Variables["isLocationIncluded"]) + `}}}`))
			case strings.Contains(asked.Query, "ListPhotoAlbumShares("):
				_, _ = response.Write([]byte(`{"data":{"ListPhotoAlbumShares":[{"id":"share-person","photoAlbumId":"album-1","granteeUserId":"user-ben","granteeUsername":"ben","shareLevel":"view","isLocationIncluded":false},{"id":"share-circle","photoAlbumId":"album-1","granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"contribute","isLocationIncluded":true}]}}`))
			case strings.Contains(asked.Query, "UpdatePhotoAlbumShare("):
				_, _ = response.Write([]byte(`{"data":{"UpdatePhotoAlbumShare":{"id":"` + asked.Variables["photoAlbumShareId"].(string) + `","photoAlbumId":"album-1","granteeUserId":"user-ben","granteeUsername":"ben","shareLevel":"` + asked.Variables["shareLevel"].(string) + `","isLocationIncluded":` + fmt.Sprint(asked.Variables["isLocationIncluded"]) + `}}}`))
			case strings.Contains(asked.Query, "RemovePhotoAlbumShare("):
				_, _ = response.Write([]byte(`{"data":{"RemovePhotoAlbumShare":true}}`))
			case strings.Contains(asked.Query, "SearchPhotoPlaces("):
				if strings.Contains(asked.Variables["query"].(string), "atlantis") {
					_, _ = response.Write([]byte(`{"data":{"SearchPhotoPlaces":[]}}`))
					return
				}
				distance := "null"
				if asked.Variables["nearLatitude"] != nil {
					distance = "3.5"
				}
				_, _ = response.Write([]byte(`{"data":{"SearchPhotoPlaces":[{"placeName":"Harborview, Exampleland","latitude":10.5,"longitude":20.25,"distanceKilometers":` + distance + `}]}}`))
			case strings.Contains(asked.Query, "SetPhotoLocation("):
				_, _ = response.Write([]byte(`{"data":{"SetPhotoLocation":2}}`))
			case strings.Contains(asked.Query, "RemovePhotoLocation("):
				_, _ = response.Write([]byte(`{"data":{"RemovePhotoLocation":1}}`))
			case strings.Contains(asked.Query, "UpdatePhoto("):
				_, _ = response.Write([]byte(`{"data":{"UpdatePhoto":{"id":"` + asked.Variables["photoId"].(string) + `","photoKind":"picture","takenAt":"2025-07-12T05:30:00Z","takenAtOffsetMinutes":540}}}`))
			case strings.Contains(asked.Query, "SavePhotoFromAttachment("):
				_, _ = response.Write([]byte(`{"data":{"SavePhotoFromAttachment":{"id":"photo-kept","photoKind":"picture","isOwnedByCaller":true}}}`))
			case strings.Contains(asked.Query, "SavePhotoToLibrary("):
				_, _ = response.Write([]byte(`{"data":{"SavePhotoToLibrary":{"id":"photo-mine","photoKind":"picture","isOwnedByCaller":true}}}`))
			case strings.Contains(asked.Query, "ListPhotoAlbums") && self.albumsWithOthers:
				_, _ = response.Write([]byte(`{"data":{"ListPhotoAlbums":[{"id":"album-1","ownerUserId":"user-me","albumTitle":"Picnic","photoCount":3,"isOwnedByCaller":true},{"id":"album-3","ownerUserId":"user-ben","albumTitle":"Hers","photoCount":4,"ownerName":"Ben Example","shareLevel":"view"},{"id":"album-4","circleId":"circle-home","albumTitle":"Ours","photoCount":9,"circleName":"Home","shareLevel":"contribute"}]}}`))
			case strings.Contains(asked.Query, "ListPhotoAlbums"):
				_, _ = response.Write([]byte(`{"data":{"ListPhotoAlbums":[` + fixtureAlbum + `,{"id":"album-2","userId":"u","albumTitle":"Empty one","photoCount":0,"coverPreviewUrl":""}]}}`))
			case strings.Contains(asked.Query, "ReadPhotoAlbum("):
				_, _ = response.Write([]byte(`{"data":{"ReadPhotoAlbum":` + fixtureAlbum + `}}`))
			case strings.Contains(asked.Query, "CreatePhotoAlbum("):
				_, _ = response.Write([]byte(`{"data":{"CreatePhotoAlbum":{"id":"album-new","userId":"u","albumTitle":"` + asked.Variables["albumTitle"].(string) + `","photoCount":3}}}`))
			case strings.Contains(asked.Query, "RenamePhotoAlbum("):
				_, _ = response.Write([]byte(`{"data":{"RenamePhotoAlbum":{"id":"album-1","userId":"u","albumTitle":"` + asked.Variables["albumTitle"].(string) + `","photoCount":3}}}`))
			case strings.Contains(asked.Query, "DeletePhotoAlbum("):
				_, _ = response.Write([]byte(`{"data":{"DeletePhotoAlbum":true}}`))
			case strings.Contains(asked.Query, "AddPhotosToAlbum("):
				_, _ = response.Write([]byte(`{"data":{"AddPhotosToAlbum":1}}`))
			case strings.Contains(asked.Query, "RemovePhotosFromAlbum("):
				_, _ = response.Write([]byte(`{"data":{"RemovePhotosFromAlbum":2}}`))
			case strings.Contains(asked.Query, "SetPhotoAlbumCover("):
				_, _ = response.Write([]byte(`{"data":{"SetPhotoAlbumCover":` + fixtureAlbum + `}}`))
			case strings.Contains(asked.Query, "SuggestPhotoAlbums("):
				_, _ = response.Write([]byte(`{"data":{"SuggestPhotoAlbums":[{"takenAfter":"2031-07-12T09:00:00Z","takenBefore":"2031-07-14T16:00:01Z","placeNames":["Exampleton, Exampleland","Sampleburg, Exampleland"],"albumTitle":"Exampleton and Sampleburg, Exampleland","photoCount":40,"samplePhotoIds":["photo-1"]}]}}`))
			case strings.Contains(asked.Query, "ReadPhotoLibrary"):
				_, _ = response.Write([]byte(`{"data":{"ReadPhotoLibrary":{"photoCount":5,"videoCount":2,"favoriteCount":1,"trashedCount":3,"usedBytes":1572864,"maximumFileBytes":8589934592,"maximumAccountBytes":0,"trashRetentionDays":30}}}`))
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
	if err != nil || !strings.Contains(output, "moved to the Trash 2 of 2 photos") {
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

func TestRetryNamesThePhotosAndSaysHowManyWereQueued(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "retry", "photo-1", "photo-2")
	if err != nil || !strings.Contains(output, "queued to make thumbnails again for 1 of 2 photos") {
		test.Fatalf("%v: %s", err, output)
	}
	if got := server.variables[len(server.variables)-1]["photoIds"]; !reflect.DeepEqual(got, []any{"photo-1", "photo-2"}) {
		test.Fatalf("%v", got)
	}
	if !strings.Contains(server.documents[len(server.documents)-1], "RetryPhotoProcessing(") {
		test.Fatalf("%s", server.documents[len(server.documents)-1])
	}
	_, output, err = server.run(test, "retry", "--json", "photo-1")
	if err != nil || !strings.Contains(output, `"retriedCount": 1`) {
		test.Fatalf("%v: %s", err, output)
	}
	if _, _, err := server.run(test, "retry"); err == nil {
		test.Fatal("retrying nothing was accepted")
	}
}

// Every place that writes a photo's date follows the photo's own offset, and
// the zone of the computer the command runs on has no say.
func TestAPhotoDateIsOnTheClockOfItsOffsetWhateverZoneTheComputerIsIn(test *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("here", 9*3600)
	defer func() { time.Local = original }()
	taken := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	offset := 60
	if got := photoTakenText(&models.Photo{TakenAt: &taken, TakenAtOffsetMinutes: &offset}); got != "2025-03-04 11:30" {
		test.Errorf("with an offset: %q", got)
	}
	// A photo not yet processed has no offset; it is on UTC, not on this clock.
	if got := photoTakenText(&models.Photo{TakenAt: &taken}); got != "2025-03-04 10:30" {
		test.Errorf("without an offset: %q", got)
	}
	if got := photoTakenText(&models.Photo{}); got != "" {
		test.Errorf("without a date: %q", got)
	}
	if got := photoDateText(&models.Photo{TakenAt: &taken, TakenAtOffsetMinutes: &offset}); got != "2025-03-04 11:30 (UTC+01:00)" {
		test.Errorf("in the details: %q", got)
	}
}

func TestTheLibraryCommandUsesTheDashboardsWords(test *testing.T) {
	server := newPhotoServer(test)
	_, output, err := server.run(test, "library")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"space used", "1.5 MB · No limit", "largest file", "8 GB", "in the Trash", "Trash kept for", "7 (2 videos)"} {
		if !strings.Contains(output, want) {
			test.Errorf("the library output lacks %q:\n%s", want, output)
		}
	}
	// A photo is any item, so the videos are among the photos and are said beside
	// them, not as a second count to add up.
	if strings.Contains(output, "videos  ") || strings.Contains(output, "\nvideos") {
		test.Errorf("the videos are listed apart from the photos:\n%s", output)
	}
	for _, unwanted := range []string{"room", "MiB", "GiB", "in the trash"} {
		if strings.Contains(output, unwanted) {
			test.Errorf("the library output says %q, which the dashboard does not:\n%s", unwanted, output)
		}
	}
	// A photo's date carries its offset in the details, and is the clock where it was taken.
	_, list, err := server.run(test, "list")
	if err != nil || !strings.Contains(list, "2025-03-04 11:30") || !strings.Contains(list, "date") {
		test.Fatalf("%v\n%s", err, list)
	}
}

// set-date corrects the date with an RFC 3339 time whose offset is the
// place's, which is sent as it was given: the offset is the one the photo is
// shown at.
func TestSetDateCorrectsWhenAPhotoWasTakenAndRefusesATimeWithoutAnOffset(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "set-date", "photo-1", "2025-07-12T14:30:00+09:00")
	if err != nil || !strings.Contains(output, "photo-1 was taken 2025-07-12 14:30 (UTC+09:00)") {
		test.Fatalf("%v %q", err, output)
	}
	sent := server.variables[len(server.variables)-1]
	if sent["photoId"] != "photo-1" || sent["takenAt"] != "2025-07-12T14:30:00+09:00" || sent["isFavorite"] != nil {
		test.Fatalf("sent %v", sent)
	}
	count := len(server.documents)
	for _, arguments := range [][]string{
		{"set-date", "photo-1", "2025-07-12T14:30:00"}, {"set-date", "photo-1", "2025-07-12"}, {"set-date", "photo-1", "yesterday"}, {"set-date", "photo-1"}, {"set-date", "photo-1", "2025-07-12T14:30:00Z", "extra"},
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
	if len(server.documents) != count {
		test.Fatal("a date that was refused reached the server")
	}
	if _, _, err := server.run(test, "set-date", "photo-1", "2025-07-12T05:30:00Z"); err != nil {
		test.Fatalf("a time on UTC: %v", err)
	}
}

func TestSearchPlacesListsCitiesAndSendsThePointItIsNear(test *testing.T) {
	server := newPhotoServer(test)
	_, output, err := server.run(test, "search-places", "harbor", "view", "--near", "10, 20", "--limit", "5")
	if err != nil || !strings.Contains(output, "Harborview, Exampleland") || !strings.Contains(output, "kilometers") || !strings.Contains(output, "3.5") {
		test.Fatalf("%v %q", err, output)
	}
	sent := server.variables[len(server.variables)-1]
	if sent["query"] != "harbor view" || sent["nearLatitude"] != 10.0 || sent["nearLongitude"] != 20.0 || sent["limitCount"] != 5.0 {
		test.Fatalf("sent %v", sent)
	}
	_, output, err = server.run(test, "search-places", "harbor")
	if err != nil || !strings.Contains(output, "Harborview") || strings.Contains(output, "kilometers") {
		test.Fatalf("%v %q", err, output)
	}
	if sent := server.variables[len(server.variables)-1]; sent["nearLatitude"] != nil || sent["nearLongitude"] != nil {
		test.Fatalf("a point nobody gave was sent: %v", sent)
	}
	if output, _, err = server.run(test, "search-places", "atlantis"); err != nil || !strings.Contains(output, "no city matches") {
		test.Fatalf("%v %q", err, output)
	}
	if _, output, err = server.run(test, "search-places", "--json", "harbor"); err != nil || !strings.Contains(output, `"placeName": "Harborview, Exampleland"`) {
		test.Fatalf("%v %q", err, output)
	}
	count := len(server.documents)
	for _, arguments := range [][]string{
		{"search-places"}, {"search-places", "harbor", "--near", "10"}, {"search-places", "harbor", "--near", "95,20"}, {"search-places", "harbor", "--near", "10,200"},
		{"search-places", "harbor", "--near", "ten,twenty"}, {"search-places", "harbor", "--limit", "0"}, {"search-places", "harbor", "--limit", "51"},
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Errorf("%v was accepted", arguments)
		}
	}
	if len(server.documents) != count {
		test.Fatal("a search that was refused reached the server")
	}
}

// set-location takes the best match for some words, or a position, and exactly
// one of them.
func TestSetLocationTakesAPlaceOrAPositionAndNeverBoth(test *testing.T) {
	server := newPhotoServer(test)
	// The server names the photo after the larger city near the one picked.
	server.photoViews["photo-1"] = `{"photo":{"id":"photo-1","latitude":10.5,"longitude":20.25,"placeName":"Harbor City, Exampleland"},"photoFiles":[],"photoPreviews":[]}`
	lastVariablesOf := func(operation string) map[string]any {
		for index := len(server.documents) - 1; index >= 0; index-- {
			if strings.Contains(server.documents[index], operation+"(") {
				return server.variables[index]
			}
		}
		return nil
	}
	output, _, err := server.run(test, "set-location", "photo-1", "photo-2", "--place", "harbor view")
	if err != nil || !strings.Contains(output, "set the location of 2 of 2 photos to Harbor City, Exampleland") {
		test.Fatalf("%v %q", err, output)
	}
	if lookup := lastVariablesOf("SearchPhotoPlaces"); lookup["query"] != "harbor view" || lookup["limitCount"] != 1.0 {
		test.Fatalf("looked up %v", lookup)
	}
	sent := lastVariablesOf("SetPhotoLocation")
	if sent["latitude"] != 10.5 || sent["longitude"] != 20.25 || !reflect.DeepEqual(sent["photoIds"], []any{"photo-1", "photo-2"}) {
		test.Fatalf("sent %v", sent)
	}

	// A position is looked up for its name, as the words are.
	_, output, err = server.run(test, "set-location", "photo-1", "--at", "10.5, 20.25", "--json")
	if err != nil || !strings.Contains(output, `"changedCount": 2`) || !strings.Contains(output, `"placeName": "Harbor City, Exampleland"`) {
		test.Fatalf("%v %q", err, output)
	}
	if lookup := lastVariablesOf("SearchPhotoPlaces"); lookup["query"] != "10.5, 20.25" {
		test.Fatalf("looked up %v", lookup)
	}

	// A position no city is near is said as the position.
	server.photoViews["photo-3"] = `{"photo":{"id":"photo-3","latitude":10.5,"longitude":20.25,"placeName":""},"photoFiles":[],"photoPreviews":[]}`
	if output, _, err := server.run(test, "set-location", "photo-3", "--place", "harbor"); err != nil || !strings.Contains(output, "to 10.5000, 20.2500") {
		test.Fatalf("%v %q", err, output)
	}

	count := len(server.documents)
	for name, arguments := range map[string][]string{
		"neither":          {"set-location", "photo-1"},
		"both":             {"set-location", "photo-1", "--place", "harbor", "--at", "10.5,20.25"},
		"no photos":        {"set-location", "--place", "harbor"},
		"latitude too big": {"set-location", "photo-1", "--at", "91,20"},
		"longitude small":  {"set-location", "photo-1", "--at", "10,-181"},
		"one number":       {"set-location", "photo-1", "--at", "10"},
		"not numbers":      {"set-location", "photo-1", "--at", "here"},
		"blank place":      {"set-location", "photo-1", "--place", "  "},
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	if len(server.documents) != count {
		test.Fatal("a location that was refused reached the server")
	}

	// Words no city matches set nothing, and say what to do instead.
	if _, _, err := server.run(test, "set-location", "photo-1", "--place", "atlantis"); err == nil || !strings.Contains(err.Error(), "no city matches") {
		test.Fatalf("%v", err)
	}
	if strings.Contains(server.documents[len(server.documents)-1], "SetPhotoLocation(") {
		test.Fatal("a place that matched nothing was sent")
	}
}

func TestRemoveLocationNamesThePhotosAndSaysHowManyChanged(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "remove-location", "--force", "photo-1", "photo-2")
	if err != nil || !strings.Contains(output, "removed the location from 1 of 2 photos") {
		test.Fatalf("%v: %s", err, output)
	}
	if got := server.variables[len(server.variables)-1]["photoIds"]; !reflect.DeepEqual(got, []any{"photo-1", "photo-2"}) {
		test.Fatalf("%v", got)
	}
	if _, output, err = server.run(test, "remove-location", "--force", "--json", "photo-1"); err != nil || !strings.Contains(output, `"changedCount": 1`) {
		test.Fatalf("%v: %s", err, output)
	}
	// Removing is for good, so with nobody at a terminal to ask it needs --force.
	count := len(server.documents)
	if _, _, err := server.run(test, "remove-location", "photo-1"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Fatalf("a removal without --force was not refused: %v", err)
	}
	if len(server.documents) != count {
		test.Fatal("a refused removal reached the server")
	}
	if _, _, err := server.run(test, "remove-location"); err == nil {
		test.Fatal("removing nothing was accepted")
	}
}

// save keeps a photo of a shared album by its id, or a file of a conversation by
// its attachment, and says which it needs when it is given neither or both.
func TestSaveKeepsAPhotoOfASharedAlbumOrAFileOfAConversation(test *testing.T) {
	server := newPhotoServer(test)
	output, _, err := server.run(test, "save", "photo-shared")
	if err != nil || !strings.Contains(output, "saved to your library as photo-mine") {
		test.Fatalf("%v %q", err, output)
	}
	if sent := server.variables[len(server.variables)-1]; sent["photoId"] != "photo-shared" || !strings.Contains(server.documents[len(server.documents)-1], "SavePhotoToLibrary(") {
		test.Fatalf("sent %v", sent)
	}
	output, _, err = server.run(test, "save", "--attachment", "attachment-one")
	if err != nil || !strings.Contains(output, "saved to your library as photo-kept") {
		test.Fatalf("%v %q", err, output)
	}
	if sent := server.variables[len(server.variables)-1]; sent["agentAttachmentId"] != "attachment-one" || !strings.Contains(server.documents[len(server.documents)-1], "SavePhotoFromAttachment(") {
		test.Fatalf("sent %v", sent)
	}
	count := len(server.documents)
	for _, arguments := range [][]string{{"save"}, {"save", "photo-shared", "--attachment", "attachment-one"}, {"save", "one", "two"}} {
		if _, _, err := server.run(test, arguments...); err == nil || !strings.Contains(err.Error(), "--attachment") {
			test.Errorf("%v: %v", arguments, err)
		}
	}
	if len(server.documents) != count {
		test.Fatal("a call that was refused reached the server")
	}
	// The help says which is which.
	for _, subcommand := range NewPhotoCommand().Commands {
		if subcommand.Name == "save" && (!strings.Contains(subcommand.Usage, "shared album") || !strings.Contains(subcommand.Usage, "--attachment")) {
			test.Errorf("the help of save: %q", subcommand.Usage)
		}
	}
}

func TestListNearAPointSendsTheThreeNumbers(test *testing.T) {
	server := newPhotoServer(test)
	if _, _, err := server.run(test, "list", "--near", "10.5,20.25,2"); err != nil {
		test.Fatal(err)
	}
	sent := server.variables[len(server.variables)-1]
	if sent["nearLatitude"] != 10.5 || sent["nearLongitude"] != 20.25 || sent["nearDistanceKilometers"] != float64(2) {
		test.Fatalf("sent %v", sent)
	}
	count := len(server.documents)
	for _, near := range []string{"10.5,20.25", "10.5,20.25,2,3", "north,20.25,2", "91,0,2", "0,181,2", "0,0,0", "0,0,-1", "nan,0,1", "0,0,inf", ","} {
		if _, _, err := server.run(test, "list", "--near", near); err == nil {
			test.Errorf("--near %q was accepted", near)
		}
	}
	if len(server.documents) != count {
		test.Fatal("a point that was refused reached the server")
	}
}

// read names where a photo was taken by the person's own place first, the
// street next and the city the photo carries last, and its JSON carries both
// answers.
func TestReadNamesThePlaceByOwnPlaceThenStreetThenCity(test *testing.T) {
	server := newPhotoServer(test)
	const photoRow = `{"photo":{"id":"%s","photoKind":"picture","latitude":10.5,"longitude":20.25,"placeName":"Harbor City, Exampleland","createdAt":"2030-03-05T09:50:00Z"},"photoFiles":[],"photoPreviews":[],"albums":[]}`
	for _, photoId := range []string{"photo-own", "photo-street", "photo-city"} {
		server.photoViews[photoId] = fmt.Sprintf(photoRow, photoId)
	}
	server.photoPlaces = map[string]string{
		"photo-own":    `{"locationPlace":{"id":"place-1","placeName":"Fixture Home","placeLabel":"home"},"addressText":""}`,
		"photo-street": `{"locationPlace":null,"addressText":"1 Fixture Road, Harbor City"}`,
	}
	for photoId, want := range map[string]string{"photo-own": "Fixture Home", "photo-street": "1 Fixture Road, Harbor City", "photo-city": "Harbor City, Exampleland"} {
		output, standardOutput, err := server.run(test, "read", photoId)
		if err != nil || !strings.Contains(output+standardOutput, want) {
			test.Fatalf("%s: %v %q %q", photoId, err, output, standardOutput)
		}
	}
	output, standardOutput, err := server.run(test, "read", "photo-own", "--json")
	if err != nil || !strings.Contains(output+standardOutput, `"locationPlace"`) || !strings.Contains(output+standardOutput, `"placeName": "Fixture Home"`) {
		test.Fatalf("%v %q %q", err, output, standardOutput)
	}
	output, standardOutput, err = server.run(test, "read", "photo-street", "--json")
	if err != nil || !strings.Contains(output+standardOutput, `"addressText": "1 Fixture Road, Harbor City"`) {
		test.Fatalf("%v %q %q", err, output, standardOutput)
	}
}
