package photo_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// exifEntry is one entry of a TIFF directory built by hand.
type exifEntry struct {
	tag   uint16
	kind  uint16
	count uint32
	value []byte
}

func exifText(tag uint16, text string) exifEntry {
	value := append([]byte(text), 0)
	return exifEntry{tag: tag, kind: 2, count: uint32(len(value)), value: value}
}

func exifShort(tag, number uint16) exifEntry {
	return exifEntry{tag: tag, kind: 3, count: 1, value: binary.LittleEndian.AppendUint16(nil, number)}
}

func exifRationals(tag uint16, pairs ...[2]uint32) exifEntry {
	var value []byte
	for _, pair := range pairs {
		value = binary.LittleEndian.AppendUint32(value, pair[0])
		value = binary.LittleEndian.AppendUint32(value, pair[1])
	}
	return exifEntry{tag: tag, kind: 5, count: uint32(len(pairs)), value: value}
}

// buildExif is a little endian EXIF block with the three directories a
// camera writes: the main one, the EXIF one and the GPS one.
func buildExif(main, exif, gps []exifEntry) []byte {
	const (
		tagExifDirectory = 0x8769
		tagGpsDirectory  = 0x8825
	)
	main = append(main, exifEntry{tag: tagExifDirectory, kind: 4, count: 1}, exifEntry{tag: tagGpsDirectory, kind: 4, count: 1})
	directories := [][]exifEntry{main, exif, gps}
	offsets := make([]uint32, len(directories))
	position := uint32(8)
	for index, entries := range directories {
		offsets[index] = position
		position += uint32(2 + len(entries)*12 + 4)
	}
	main[len(main)-2].value = binary.LittleEndian.AppendUint32(nil, offsets[1])
	main[len(main)-1].value = binary.LittleEndian.AppendUint32(nil, offsets[2])
	block := []byte{'I', 'I'}
	block = binary.LittleEndian.AppendUint16(block, 42)
	block = binary.LittleEndian.AppendUint32(block, 8)
	var data []byte
	for _, entries := range directories {
		sort.Slice(entries, func(first, second int) bool { return entries[first].tag < entries[second].tag })
		block = binary.LittleEndian.AppendUint16(block, uint16(len(entries)))
		for _, entry := range entries {
			block = binary.LittleEndian.AppendUint16(block, entry.tag)
			block = binary.LittleEndian.AppendUint16(block, entry.kind)
			block = binary.LittleEndian.AppendUint32(block, entry.count)
			if len(entry.value) <= 4 {
				block = append(block, append(append([]byte(nil), entry.value...), 0, 0, 0, 0)[:4]...)
				continue
			}
			block = binary.LittleEndian.AppendUint32(block, position+uint32(len(data)))
			data = append(data, entry.value...)
			if len(data)%2 == 1 {
				data = append(data, 0)
			}
		}
		block = binary.LittleEndian.AppendUint32(block, 0)
	}
	return append(block, data...)
}

// quadrants is a picture whose four quarters are different colors, so that
// where each one ends up says how the picture was turned: red top left,
// green top right, blue bottom left, yellow bottom right.
func quadrants(widthPixels, heightPixels int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, widthPixels, heightPixels))
	for row := range heightPixels {
		for column := range widthPixels {
			shade := color.RGBA{R: 230, G: 30, B: 30, A: 255}
			switch {
			case column >= widthPixels/2 && row < heightPixels/2:
				shade = color.RGBA{R: 30, G: 200, B: 30, A: 255}
			case column < widthPixels/2 && row >= heightPixels/2:
				shade = color.RGBA{R: 30, G: 30, B: 230, A: 255}
			case column >= widthPixels/2 && row >= heightPixels/2:
				shade = color.RGBA{R: 230, G: 220, B: 30, A: 255}
			}
			picture.SetRGBA(column, row, shade)
		}
	}
	return picture
}

// quadrantAt names the quarter a pixel is nearest to in color.
func quadrantAt(test *testing.T, picture image.Image, column, row int) string {
	test.Helper()
	red, green, blue, _ := picture.At(column, row).RGBA()
	candidates := map[string][3]int{"red": {230, 30, 30}, "green": {30, 200, 30}, "blue": {30, 30, 230}, "yellow": {230, 220, 30}}
	best, bestDistance := "", 1<<30
	for name, want := range candidates {
		distance := 0
		for index, got := range []uint32{red >> 8, green >> 8, blue >> 8} {
			difference := int(got) - want[index]
			distance += difference * difference
		}
		if distance < bestDistance {
			best, bestDistance = name, distance
		}
	}
	return best
}

// exifJpeg is a JPEG of quadrants with an EXIF block that says it was taken
// at an invented date, offset and place, by an invented camera, and turned
// the way orientation says.
func exifJpeg(test *testing.T, widthPixels, heightPixels int, orientation uint16) []byte {
	test.Helper()
	return exifJpegDated(test, widthPixels, heightPixels, orientation, "2024:05:06 07:08:09", "+02:00")
}

// exifJpegDated is exifJpeg with the date and offset the camera wrote. An
// empty offset is a camera that wrote none.
func exifJpegDated(test *testing.T, widthPixels, heightPixels int, orientation uint16, date, offset string) []byte {
	test.Helper()
	dateEntries := []exifEntry{exifText(0x9003, date)}
	if offset != "" {
		dateEntries = append(dateEntries, exifText(0x9011, offset))
	}
	exif := buildExif(
		[]exifEntry{exifText(0x010F, "Example Camera Co"), exifText(0x0110, "Model 1"), exifShort(0x0112, orientation)},
		dateEntries,
		[]exifEntry{
			exifText(0x0001, "N"), exifRationals(0x0002, [2]uint32{10, 1}, [2]uint32{0, 1}, [2]uint32{0, 1}),
			exifText(0x0003, "E"), exifRationals(0x0004, [2]uint32{20, 1}, [2]uint32{0, 1}, [2]uint32{0, 1}),
		})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, quadrants(widthPixels, heightPixels), &jpeg.Options{Quality: 95}); err != nil {
		test.Fatal(err)
	}
	segment := append([]byte("Exif\x00\x00"), exif...)
	spliced := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	spliced = binary.BigEndian.AppendUint16(spliced, uint16(len(segment)+2))
	spliced = append(spliced, segment...)
	return append(spliced, encoded.Bytes()[2:]...)
}

// uploadAndProcess sends a file and runs the processing pass over it,
// returning the photo as it is afterwards.
func (self *fixture) uploadAndProcess(clientPhotoId string, headers map[string]string, content []byte) *models.Photo {
	self.test.Helper()
	got := self.send(http.MethodPost, self.collectionUrl(), headers, content)
	self.expect(got, http.StatusCreated)
	if err := self.store.Process(self.test.Context(), 4); err != nil {
		self.test.Fatal(err)
	}
	return self.photoByClientId(clientPhotoId)
}

func (self *fixture) previewOf(photoId string, photoPreviewSize models.PhotoPreviewSize) (*models.PhotoPreview, []byte) {
	self.test.Helper()
	var found *models.PhotoPreview
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		previews, err := transaction.ListPhotoPreviews(self.userId, []string{photoId})
		if err != nil {
			self.test.Fatal(err)
		}
		for _, preview := range previews {
			if preview.PhotoPreviewSize == photoPreviewSize {
				found = preview
			}
		}
	})
	if found == nil {
		return nil, nil
	}
	content := self.files.get(models.PhotoPreviewFileID(photoId, photoPreviewSize))
	if int64(len(content)) != found.PreviewSizeBytes {
		self.test.Fatalf("the %s preview is recorded as %d bytes and stored as %d", photoPreviewSize, found.PreviewSizeBytes, len(content))
	}
	return found, content
}

func TestAJpegWithExifBecomesReadyWithItsDateAndPlaceAndUprightPreviews(test *testing.T) {
	fixture := newFixture(test)
	// Stored 800 wide and 400 high and marked as lying on its side (6: the
	// top of the picture is on the right), so it is shown 400 wide and 800 high.
	found := fixture.uploadAndProcess("exif", jpegHeaders("exif", "X-Photo-Filename", "IMG_0001.JPG"), exifJpeg(test, 800, 400, 6))
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingClaimedAt != nil || found.ProcessingErrorMessage != "" {
		test.Fatalf("%+v", found)
	}
	wantTakenAt := time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC)
	if found.TakenAt == nil || !found.TakenAt.Equal(wantTakenAt) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != 120 {
		test.Fatalf("taken at %v, offset %v", found.TakenAt, found.TakenAtOffsetMinutes)
	}
	if found.Latitude == nil || *found.Latitude != 10.0 || found.Longitude == nil || *found.Longitude != 20.0 {
		test.Fatalf("place %v %v", found.Latitude, found.Longitude)
	}
	if wantPlace, _ := place.Nearest(10.0, 20.0); found.PlaceName != wantPlace {
		test.Fatalf("the place is %q, the table says %q", found.PlaceName, wantPlace)
	}
	if found.CameraMake != "Example Camera Co" || found.CameraModel != "Model 1" {
		test.Fatalf("camera %q %q", found.CameraMake, found.CameraModel)
	}
	if found.PhotoKind != models.PhotoKindPicture || found.WidthPixels != 400 || found.HeightPixels != 800 {
		test.Fatalf("kind %s, size %d by %d", found.PhotoKind, found.WidthPixels, found.HeightPixels)
	}

	large, largeBytes := fixture.previewOf(found.ID, models.PhotoPreviewLarge)
	small, smallBytes := fixture.previewOf(found.ID, models.PhotoPreviewSmall)
	if large == nil || small == nil {
		test.Fatalf("previews: large %v, small %v", large, small)
	}
	// Nothing is made larger than it is: the large one is the whole picture,
	// and the small one has 400 pixels on its long side.
	if large.WidthPixels != 400 || large.HeightPixels != 800 || small.WidthPixels != 200 || small.HeightPixels != 400 {
		test.Fatalf("large %d by %d, small %d by %d", large.WidthPixels, large.HeightPixels, small.WidthPixels, small.HeightPixels)
	}
	for name, content := range map[string][]byte{"large": largeBytes, "small": smallBytes} {
		decoded, err := jpeg.Decode(bytes.NewReader(content))
		if err != nil {
			test.Fatalf("the %s preview is not a JPEG: %v", name, err)
		}
		// Turned a quarter turn clockwise, the left half of the stored
		// picture's top (red, green) is on the right, top first.
		bounds := decoded.Bounds()
		width, height := bounds.Dx(), bounds.Dy()
		for corner, want := range map[string]string{"top left": "blue", "top right": "red", "bottom left": "yellow", "bottom right": "green"} {
			column, row := width/4, height/4
			if strings.Contains(corner, "right") {
				column = width * 3 / 4
			}
			if strings.Contains(corner, "bottom") {
				row = height * 3 / 4
			}
			if got := quadrantAt(test, decoded, column, row); got != want {
				test.Errorf("the %s preview's %s is %s, want %s", name, corner, got, want)
			}
		}
		if bytes.Contains(content, []byte("Exif\x00\x00")) {
			test.Errorf("the %s preview carries the picture's EXIF block, with its place", name)
		}
	}
}

func TestWhatTheUploaderSaidWinsOverWhatTheFileSays(test *testing.T) {
	fixture := newFixture(test)
	found := fixture.uploadAndProcess("told", jpegHeaders("told",
		"X-Photo-Taken-At", "2020-01-02T03:04:05-05:00", "X-Photo-Latitude", "1.5", "X-Photo-Longitude", "2.5"),
		exifJpeg(test, 80, 40, 1))
	if found.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("%+v", found)
	}
	if found.TakenAt == nil || !found.TakenAt.Equal(time.Date(2020, 1, 2, 8, 4, 5, 0, time.UTC)) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != -300 {
		test.Fatalf("the date was replaced by the file's: %v %v", found.TakenAt, found.TakenAtOffsetMinutes)
	}
	if found.Latitude == nil || *found.Latitude != 1.5 || found.Longitude == nil || *found.Longitude != 2.5 {
		test.Fatalf("the place was replaced by the file's: %v %v", found.Latitude, found.Longitude)
	}
	// What the headers did not say still comes from the file.
	if found.CameraMake != "Example Camera Co" || found.WidthPixels != 80 || found.HeightPixels != 40 {
		test.Fatalf("%+v", found)
	}
}

// setOwnerZone sets the time zone of the account the fixture signs in as.
func (self *fixture) setOwnerZone(zone string) {
	self.test.Helper()
	self.age(`UPDATE "user" SET timezone = '` + zone + `' WHERE id = '` + self.userId + `'`)
}

// The picture exifJpeg makes says 2024-05-06 07:08:09 at +02:00, which is
// 05:08:09 UTC.
func TestADateEndingInZTakesItsOffsetFromTheFileOrFromTheOwnersZone(test *testing.T) {
	fixture := newFixture(test)
	// The owner's zone is five hours east of UTC all year.
	fixture.setOwnerZone("Etc/GMT-5")
	instant := time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC)

	// A phone knows the instant and not the zone.
	phone := fixture.uploadAndProcess("phone", jpegHeaders("phone", "X-Photo-Taken-At", "2024-05-06T05:08:09Z"), exifJpeg(test, 80, 40, 1))
	if phone.TakenAt == nil || !phone.TakenAt.Equal(instant) || phone.TakenAtOffsetMinutes == nil || *phone.TakenAtOffsetMinutes != 120 {
		test.Fatalf("the instant %v with offset %v, want the header's instant and the file's +120", phone.TakenAt, phone.TakenAtOffsetMinutes)
	}

	// The same date written with an offset of zero is an uploader that knows
	// the photo was taken in UTC, and the file does not overrule it.
	utc := fixture.uploadAndProcess("utc", jpegHeaders("utc", "X-Photo-Taken-At", "2024-05-06T05:08:09+00:00"), exifJpeg(test, 80, 41, 1))
	if utc.TakenAt == nil || !utc.TakenAt.Equal(instant) || utc.TakenAtOffsetMinutes == nil || *utc.TakenAtOffsetMinutes != 0 {
		test.Fatalf("the instant %v with offset %v, want an offset of 0", utc.TakenAt, utc.TakenAtOffsetMinutes)
	}

	// An instant that is not the file's own moment has not the file's offset:
	// the date was changed after the file was written. It has the owner's.
	changed := fixture.uploadAndProcess("changed", jpegHeaders("changed", "X-Photo-Taken-At", "2024-05-07T05:08:09Z"), exifJpeg(test, 80, 42, 1))
	if changed.TakenAt == nil || !changed.TakenAt.Equal(instant.Add(24*time.Hour)) || changed.TakenAtOffsetMinutes == nil || *changed.TakenAtOffsetMinutes != 300 {
		test.Fatalf("the instant %v with offset %v, want the header's instant and the owner's +300", changed.TakenAt, changed.TakenAtOffsetMinutes)
	}

	// A file that says no offset gives the photo the owner's.
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, quadrants(30, 20)); err != nil {
		test.Fatal(err)
	}
	plain := fixture.uploadAndProcess("plain-z", map[string]string{"Content-Type": "image/png", "X-Photo-Client-ID": "plain-z", "X-Photo-Taken-At": "2024-05-06T05:08:09Z"}, encoded.Bytes())
	if plain.TakenAt == nil || !plain.TakenAt.Equal(instant) || plain.TakenAtOffsetMinutes == nil || *plain.TakenAtOffsetMinutes != 300 {
		test.Fatalf("the instant %v with offset %v, want the owner's +300", plain.TakenAt, plain.TakenAtOffsetMinutes)
	}
}

// A camera that wrote a date and no offset wrote a clock reading, not a moment.
// It is read on the clock of the owner's zone, which gives it an instant and an
// offset, so that a photo with a date always has both.
func TestAPictureDateWithNoOffsetIsAClockReadingInTheOwnersZone(test *testing.T) {
	fixture := newFixture(test)
	// Eight hours behind UTC all year.
	fixture.setOwnerZone("Etc/GMT+8")
	west := fixture.uploadAndProcess("west", jpegHeaders("west"), exifJpegDated(test, 80, 40, 1, "2024:05:06 07:08:09", ""))
	wantInstant := time.Date(2024, 5, 6, 15, 8, 9, 0, time.UTC)
	if west.TakenAt == nil || !west.TakenAt.Equal(wantInstant) || west.TakenAtOffsetMinutes == nil || *west.TakenAtOffsetMinutes != -480 {
		test.Fatalf("the instant %v with offset %v, want 15:08:09 UTC and -480", west.TakenAt, west.TakenAtOffsetMinutes)
	}

	// An account with no zone is on UTC.
	fixture.setOwnerZone("")
	bare := fixture.uploadAndProcess("bare", jpegHeaders("bare"), exifJpegDated(test, 80, 41, 1, "2024:05:06 07:08:09", ""))
	if bare.TakenAt == nil || !bare.TakenAt.Equal(time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)) || bare.TakenAtOffsetMinutes == nil || *bare.TakenAtOffsetMinutes != 0 {
		test.Fatalf("the instant %v with offset %v, want the clock reading on UTC", bare.TakenAt, bare.TakenAtOffsetMinutes)
	}

	// A zone this server does not know is the same as none.
	fixture.setOwnerZone("Nowhere/Invented")
	odd := fixture.uploadAndProcess("odd", jpegHeaders("odd"), exifJpegDated(test, 80, 42, 1, "2024:05:06 07:08:09", ""))
	if odd.TakenAtOffsetMinutes == nil || *odd.TakenAtOffsetMinutes != 0 {
		test.Fatalf("offset %v", odd.TakenAtOffsetMinutes)
	}
}

func TestAPngWithoutMetadataGetsASizeAndPreviewsAndNoDate(test *testing.T) {
	fixture := newFixture(test)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, quadrants(30, 20)); err != nil {
		test.Fatal(err)
	}
	found := fixture.uploadAndProcess("plain", map[string]string{"Content-Type": "image/png", "X-Photo-Client-ID": "plain"}, encoded.Bytes())
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.TakenAt != nil || found.TakenAtOffsetMinutes != nil || found.Latitude != nil || found.PlaceName != "" {
		test.Fatalf("%+v", found)
	}
	if found.WidthPixels != 30 || found.HeightPixels != 20 || found.PhotoKind != models.PhotoKindPicture {
		test.Fatalf("%+v", found)
	}
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		if preview, content := fixture.previewOf(found.ID, photoPreviewSize); preview == nil || preview.ContentType != "image/jpeg" || len(content) == 0 {
			test.Fatalf("the %s preview: %v", photoPreviewSize, preview)
		}
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewVideo); preview != nil {
		test.Fatal("a picture has a video preview")
	}
}

func TestAVideoGetsItsLengthAPosterAndAPlayableCopy(test *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		test.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		test.Skip("ffprobe is not installed")
	}
	path := filepath.Join(test.TempDir(), "generated.mp4")
	command := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=15",
		"-pix_fmt", "yuv420p", "-c:v", "libx264", "-metadata", "creation_time=2024-05-06T05:08:09.000000Z", path)
	if output, err := command.CombinedOutput(); err != nil {
		test.Skipf("ffmpeg cannot make a test video: %v: %s", err, output)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	// Work is done in a directory of its own inside the work directory, which
	// must be empty afterwards, and nothing is made in the system's temporary
	// directory, which may be memory. Both are the test's, because /tmp is
	// shared with whatever else is running.
	temporaryDirectory := test.TempDir()
	test.Setenv("TMPDIR", temporaryDirectory)
	fixture := newFixture(test)
	fixture.setOwnerZone("Etc/GMT-5")
	found := fixture.uploadAndProcess("clip", map[string]string{"Content-Type": "video/mp4", "X-Photo-Client-ID": "clip"}, content)
	if found.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("%+v", found)
	}
	// A video's creation time is an instant, and the photo has the owner's
	// offset for it.
	if found.TakenAt == nil || !found.TakenAt.Equal(time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC)) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != 300 {
		test.Fatalf("taken at %v, offset %v", found.TakenAt, found.TakenAtOffsetMinutes)
	}
	if found.PhotoKind != models.PhotoKindVideo || found.WidthPixels != 320 || found.HeightPixels != 240 || found.DurationMS < 1900 || found.DurationMS > 2200 {
		test.Fatalf("kind %s, size %d by %d, %d ms", found.PhotoKind, found.WidthPixels, found.HeightPixels, found.DurationMS)
	}
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		preview, previewBytes := fixture.previewOf(found.ID, photoPreviewSize)
		if preview == nil || preview.ContentType != "image/jpeg" || preview.WidthPixels != 320 || preview.HeightPixels != 240 {
			test.Fatalf("the %s poster: %+v", photoPreviewSize, preview)
		}
		if _, err := jpeg.Decode(bytes.NewReader(previewBytes)); err != nil {
			test.Fatalf("the %s poster is not a JPEG: %v", photoPreviewSize, err)
		}
	}
	playable, playableBytes := fixture.previewOf(found.ID, models.PhotoPreviewVideo)
	if playable == nil || playable.ContentType != "video/mp4" || len(playableBytes) < 100 || string(playableBytes[4:8]) != "ftyp" {
		test.Fatalf("the video preview: %+v", playable)
	}
	if entries, err := os.ReadDir(temporaryDirectory); err != nil || len(entries) != 0 {
		test.Fatalf("something was made in the system's temporary directory: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(fixture.workDirectory); err != nil || len(entries) != 0 {
		test.Fatalf("the work directory was left behind: %v %v", entries, err)
	}
}

func TestACorruptFileWaitsLongerAfterEachFailureAndFailsAfterItsAttemptsWithAPlainReason(test *testing.T) {
	fixture := newFixture(test)
	fixture.uploadAndProcess("corrupt", jpegHeaders("corrupt"), append([]byte{0xFF, 0xD8, 0xFF}, randomBytes(200, 41)...))
	// One, five, fifteen and then sixty minutes.
	waits := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	for attempt := 1; attempt < 5; attempt++ {
		found := fixture.photoByClientId("corrupt")
		if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingAttemptCount != attempt || found.ProcessingClaimedAt != nil {
			test.Fatalf("after %d attempts: %+v", attempt, found)
		}
		// What the person and the model read is a fixed sentence: nothing of
		// what the decoder said, which names the format and where it failed.
		if found.ProcessingErrorMessage != "The picture could not be read." {
			test.Fatalf("the reason is %q", found.ProcessingErrorMessage)
		}
		if found.ProcessingNotBefore == nil {
			test.Fatalf("no wait after %d attempts: %+v", attempt, found)
		}
		if wait := time.Until(*found.ProcessingNotBefore); wait < waits[attempt-1]-10*time.Second || wait > waits[attempt-1]+10*time.Second {
			test.Fatalf("after %d attempts the photo waits %s, want about %s", attempt, wait, waits[attempt-1])
		}
		// While it waits, a pass leaves it alone.
		if err := fixture.store.Process(test.Context(), 4); err != nil {
			test.Fatal(err)
		}
		if again := fixture.photoByClientId("corrupt"); again.ProcessingAttemptCount != attempt {
			test.Fatalf("a photo that waits was tried: %+v", again)
		}
		fixture.age("UPDATE photo SET processing_not_before = now() - interval '1 second' WHERE client_photo_id = 'corrupt'")
		if err := fixture.store.Process(test.Context(), 4); err != nil {
			test.Fatal(err)
		}
	}
	found := fixture.photoByClientId("corrupt")
	if found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingAttemptCount != 5 || found.ProcessingErrorMessage != "The picture could not be read." || found.ProcessingNotBefore != nil {
		test.Fatalf("after the attempts: %+v", found)
	}
	// A failed photo is not tried again.
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if again := fixture.photoByClientId("corrupt"); again.ProcessingAttemptCount != 5 {
		test.Fatalf("a failed photo was tried again: %+v", again)
	}
	if found.PhotoKind != models.PhotoKindPicture {
		test.Fatalf("a failed photo lost its kind: %s", found.PhotoKind)
	}

	// Asking again puts it back with a fresh count; it fails the same way, and
	// is counted again from one.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if retried, err := transaction.RetryPhotoProcessing(fixture.userId, []string{found.ID}); err != nil || retried != 1 {
			test.Fatalf("retried %d: %v", retried, err)
		}
	})
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if again := fixture.photoByClientId("corrupt"); again.PhotoProcessingState != models.PhotoProcessingWaiting || again.ProcessingAttemptCount != 1 {
		test.Fatalf("after a retry: %+v", again)
	}
}

func TestAKindOfPictureThatCannotBeReadFailsAtOnce(test *testing.T) {
	fixture := newFixture(test)
	found := fixture.uploadAndProcess("webp", map[string]string{"Content-Type": "image/webp", "X-Photo-Client-ID": "webp"}, append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), randomBytes(100, 42)...))
	if found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingAttemptCount != 1 || found.ProcessingErrorMessage != "This kind of file is not supported." {
		test.Fatalf("%+v", found)
	}
	if found.PhotoKind != models.PhotoKindPicture {
		test.Fatalf("kind %s", found.PhotoKind)
	}
}

func TestAStaleClaimIsTakenAgainAfterTenMinutes(test *testing.T) {
	fixture := newFixture(test)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("claimed"), smallJpeg(test, 80))
	fixture.expect(got, http.StatusCreated)
	// Another instance took it a moment ago and is working on it.
	fixture.age("UPDATE photo SET processing_claimed_at = now() WHERE client_photo_id = 'claimed'")
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if fixture.photoByClientId("claimed").PhotoProcessingState != models.PhotoProcessingWaiting {
		test.Fatal("a photo another instance is working on was taken")
	}
	fixture.store.SetNow(func() time.Time { return time.Now().Add(9 * time.Minute) })
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if fixture.photoByClientId("claimed").PhotoProcessingState != models.PhotoProcessingWaiting {
		test.Fatal("a claim of nine minutes was taken")
	}
	fixture.store.SetNow(func() time.Time { return time.Now().Add(11 * time.Minute) })
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if found := fixture.photoByClientId("claimed"); found.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("a stale claim was not taken again: %+v", found)
	}
}

func TestAReadyPhotoWithoutPreviewsIsProcessedAgain(test *testing.T) {
	fixture := newFixture(test)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("stub"), exifJpeg(test, 40, 20, 1))
	fixture.expect(got, http.StatusCreated)
	// Marked ready by the version that made no previews.
	fixture.age("UPDATE photo SET photo_processing_state = 'ready' WHERE client_photo_id = 'stub'")
	found := fixture.photoByClientId("stub")
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewSmall); preview != nil {
		test.Fatal("the photo already has a preview")
	}
	// The first pass finds nothing waiting and puts it back; the second
	// does the work.
	for range 2 {
		if err := fixture.store.Process(test.Context(), 4); err != nil {
			test.Fatal(err)
		}
	}
	found = fixture.photoByClientId("stub")
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.TakenAt == nil {
		test.Fatalf("%+v", found)
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewSmall); preview == nil {
		test.Fatal("the photo still has no small preview")
	}
}

func TestAPhotoWhoseEditedFileArrivesIsShownFromTheEditedFile(test *testing.T) {
	fixture := newFixture(test)
	original := fixture.uploadAndProcess("edited", jpegHeaders("edited"), exifJpeg(test, 80, 40, 1))
	if original.WidthPixels != 80 {
		test.Fatalf("%+v", original)
	}
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("edited", "X-Photo-File-Role", "edited"), exifJpeg(test, 60, 60, 1))
	fixture.expect(got, http.StatusCreated)
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	edited := fixture.photoByClientId("edited")
	if edited.WidthPixels != 60 || edited.HeightPixels != 60 || !edited.UpdatedAt.After(original.UpdatedAt) {
		test.Fatalf("%+v", edited)
	}
}

// heifBox is a box of an ISO base media file: its size, its type, its bytes.
func heifBox(kind string, payload ...[]byte) []byte {
	joined := bytes.Join(payload, nil)
	box := binary.BigEndian.AppendUint32(nil, uint32(8+len(joined)))
	box = append(box, kind...)
	return append(box, joined...)
}

// heicOfSize is the boxes of a HEIC that say how large its picture is and hold
// no picture: all processing reads of it before a program decodes it.
func heicOfSize(widthPixels, heightPixels uint32) []byte {
	versioned := func(kind string, payload ...[]byte) []byte {
		return heifBox(kind, append([][]byte{{0, 0, 0, 0}}, payload...)...)
	}
	size := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, widthPixels), heightPixels)
	properties := heifBox("iprp",
		heifBox("ipco", versioned("ispe", size)),
		versioned("ipma", binary.BigEndian.AppendUint32(nil, 1), []byte{0, 1, 1, 1}))
	meta := versioned("meta", versioned("pitm", []byte{0, 1}), properties)
	return append(heifBox("ftyp", []byte("heic"), []byte{0, 0, 0, 0}, []byte("mif1heic")), meta...)
}

// installFakeHeifDec puts a stand-in for heif-dec first on the path. heif-dec
// is a program the image carries and a developer machine may not. It notes that
// it ran, and writes the picture in decoded.jpg as the decoded one.
func installFakeHeifDec(test *testing.T, decoded []byte) (ranMarker string) {
	test.Helper()
	directory := test.TempDir()
	ranMarker = filepath.Join(directory, "ran")
	if err := os.WriteFile(filepath.Join(directory, "decoded.jpg"), decoded, 0o600); err != nil {
		test.Fatal(err)
	}
	script := "#!/bin/sh\ntouch " + ranMarker + "\ncp " + filepath.Join(directory, "decoded.jpg") + " \"$3\"\n"
	if err := os.WriteFile(filepath.Join(directory, "heif-dec"), []byte(script), 0o755); err != nil {
		test.Fatal(err)
	}
	test.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return ranMarker
}

func TestAHeicIsDecodedByHeifDecAndItsPreviewsComeFromThatPicture(test *testing.T) {
	// What is checked is that processing asks for the program and uses what it
	// wrote.
	installFakeHeifDec(test, exifJpeg(test, 64, 32, 1))
	fixture := newFixture(test)
	found := fixture.uploadAndProcess("heic", map[string]string{"Content-Type": "image/heic", "X-Photo-Client-ID": "heic"}, heicOfSize(64, 32))
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.WidthPixels != 64 || found.HeightPixels != 32 {
		test.Fatalf("%+v", found)
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewSmall); preview == nil || preview.WidthPixels != 64 {
		test.Fatalf("the small preview: %+v", preview)
	}

	// Without heif-dec the photo is not lost: it waits, and its size and kind
	// are known from the file. A program the server lacks says nothing of the
	// file, so the attempt is not counted, and the reason is a plain sentence
	// that does not name the program.
	test.Setenv("PATH", test.TempDir())
	missing := fixture.uploadAndProcess("heic-missing", map[string]string{"Content-Type": "image/heic", "X-Photo-Client-ID": "heic-missing"}, heicOfSize(48, 24))
	if missing.PhotoProcessingState != models.PhotoProcessingWaiting || missing.ProcessingAttemptCount != 0 || missing.WidthPixels != 48 || missing.ProcessingNotBefore == nil {
		test.Fatalf("%+v", missing)
	}
	if strings.Contains(missing.ProcessingErrorMessage, "heif-dec") || missing.ProcessingErrorMessage != "The previews could not be made yet; they will be tried again." {
		test.Fatalf("the reason is %q", missing.ProcessingErrorMessage)
	}
}

// heif-dec holds the whole picture it decodes, and a small file can claim a
// huge one: the size the boxes give is bounded before the program is run, as a
// JPEG's is before it is decoded, and the photo is failed at once.
func TestAHeicClaimingAHugePictureIsFailedAtOnceAndHeifDecIsNeverRun(test *testing.T) {
	ranMarker := installFakeHeifDec(test, smallJpeg(test, 10))
	fixture := newFixture(test)
	for name, size := range map[string][2]uint32{"many-pixels": {16000, 4001}, "a-long-row": {70000, 10}} {
		found := fixture.uploadAndProcess(name, map[string]string{"Content-Type": "image/heic", "X-Photo-Client-ID": name}, heicOfSize(size[0], size[1]))
		if found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingAttemptCount != 1 || found.ProcessingErrorMessage != "This picture is too large to make previews of." {
			test.Fatalf("%s: %+v", name, found)
		}
	}
	if _, err := os.Stat(ranMarker); err == nil {
		test.Fatal("heif-dec was run on a picture that claims to be too large")
	}
}

// A phone sends a picture saved from a message as image.jpeg whatever its
// format: it is kept as what its bytes are, and read that way.
// A WebP, which an iPhone keeps for pictures saved from the web and from
// some apps, is made into previews like a JPEG. This one is 16 by 12 pixels
// of one color, written by an encoder for this test.
func TestAWebPIsMadeIntoPreviews(test *testing.T) {
	webP := []byte{
		0x52, 0x49, 0x46, 0x46, 0x1e, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50,
		0x56, 0x50, 0x38, 0x4c, 0x11, 0x00, 0x00, 0x00, 0x2f, 0x0f, 0xc0, 0x02,
		0x00, 0x07, 0x50, 0xb2, 0x2a, 0x14, 0xb9, 0xff, 0x81, 0x88, 0xe8, 0x7f,
		0x00, 0x00,
	}
	fixture := newFixture(test)
	found := fixture.uploadAndProcess("webp", map[string]string{"Content-Type": "image/webp", "X-Photo-Client-ID": "webp", "X-Photo-Filename": "IMG_0001.WEBP"}, webP)
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.WidthPixels != 16 || found.HeightPixels != 12 {
		test.Fatalf("%+v", found)
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewSmall); preview == nil {
		test.Fatal("no small preview")
	}
}

func TestAPictureSentAsTheWrongTypeIsKeptAndReadAsWhatItsBytesAre(test *testing.T) {
	ranMarker := installFakeHeifDec(test, exifJpeg(test, 64, 32, 1))
	fixture := newFixture(test)
	jpegAsHeic := fixture.uploadAndProcess("jpeg-as-heic", map[string]string{"Content-Type": "image/heic", "X-Photo-Client-ID": "jpeg-as-heic", "X-Photo-Filename": "image.heic"}, smallJpeg(test, 10))
	if jpegAsHeic.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("%+v", jpegAsHeic)
	}
	if files := fixture.photoFiles(jpegAsHeic.ID); len(files) != 1 || files[0].ContentType != "image/jpeg" {
		test.Fatalf("%+v", files)
	}
	if _, err := os.Stat(ranMarker); err == nil {
		test.Fatal("heif-dec was run on a JPEG")
	}
	heicAsJpeg := fixture.uploadAndProcess("heic-as-jpeg", map[string]string{"Content-Type": "image/jpeg", "X-Photo-Client-ID": "heic-as-jpeg", "X-Photo-Filename": "image.jpeg"}, heicOfSize(64, 32))
	if heicAsJpeg.PhotoProcessingState != models.PhotoProcessingReady || heicAsJpeg.WidthPixels != 64 {
		test.Fatalf("%+v", heicAsJpeg)
	}
	if files := fixture.photoFiles(heicAsJpeg.ID); len(files) != 1 || files[0].ContentType != "image/heic" {
		test.Fatalf("%+v", files)
	}
}

// A file kept as the wrong type before its bytes were looked at is read as
// what they are.
func TestAPictureKeptAsTheWrongTypeIsReadAsWhatItsBytesAre(test *testing.T) {
	ranMarker := installFakeHeifDec(test, exifJpeg(test, 64, 32, 1))
	fixture := newFixture(test)
	keptAs := func(clientPhotoId, sentContentType, keptContentType string, content []byte) *models.Photo {
		test.Helper()
		got := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": sentContentType, "X-Photo-Client-ID": clientPhotoId}, content)
		fixture.expect(got, http.StatusCreated)
		dbtest.Exec(test, fixture.database, "UPDATE photo_file SET content_type = '"+keptContentType+"' WHERE photo_id = '"+got.header.Get("X-Photo-ID")+"'")
		if err := fixture.store.Process(test.Context(), 4); err != nil {
			test.Fatal(err)
		}
		return fixture.photoByClientId(clientPhotoId)
	}
	if found := keptAs("jpeg-kept-as-heic", "image/jpeg", "image/heic", smallJpeg(test, 10)); found.PhotoProcessingState != models.PhotoProcessingReady || found.WidthPixels != 16 {
		test.Fatalf("%+v", found)
	}
	if _, err := os.Stat(ranMarker); err == nil {
		test.Fatal("heif-dec was run on a JPEG")
	}
	if found := keptAs("heic-kept-as-jpeg", "image/heic", "image/jpeg", heicOfSize(64, 32)); found.PhotoProcessingState != models.PhotoProcessingReady || found.WidthPixels != 64 {
		test.Fatalf("%+v", found)
	}
}

// A HEIC is turned by its own boxes, which heif-dec applies, and not by the
// orientation of its EXIF block, which describes the pixels the camera stored:
// the size read before decoding is swapped for a JPEG on its side and for no
// other kind of file.
func TestOnlyAJpegsSizeIsSwappedByItsExifOrientation(test *testing.T) {
	fixture := newFixture(test)
	content := exifJpeg(test, 64, 32, 6)
	for contentType, want := range map[string][2]int{"image/jpeg": {32, 64}, "image/heic": {64, 32}, "image/heif": {64, 32}} {
		if widthPixels, heightPixels := fixture.store.ReadPictureMetadataSize(content, contentType); widthPixels != want[0] || heightPixels != want[1] {
			test.Errorf("%s: %dx%d, want %dx%d", contentType, widthPixels, heightPixels, want[0], want[1])
		}
	}
}

// liveVideoContent is a short QuickTime movie like the one an iPhone records
// beside a Live Photo, in HEVC when ffmpeg can write it, which is the kind
// that browsers cannot play. It is generated, never a real recording.
func liveVideoContent(test *testing.T) []byte {
	test.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		test.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		test.Skip("ffprobe is not installed")
	}
	for _, encoder := range []string{"libx265", "libx264"} {
		path := filepath.Join(test.TempDir(), "live.mov")
		command := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=15",
			"-pix_fmt", "yuv420p", "-c:v", encoder, "-f", "mov", path)
		if output, err := command.CombinedOutput(); err != nil {
			test.Logf("%s cannot make a test video: %v: %s", encoder, err, output)
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			test.Fatal(err)
		}
		return content
	}
	test.Skip("ffmpeg cannot make a test video")
	return nil
}

// uploadLivePhoto sends a picture and the video beside it, and runs the
// processing pass over them.
func (self *fixture) uploadLivePhoto(clientPhotoId string, pictureContent, videoContent []byte) *models.Photo {
	self.test.Helper()
	self.expect(self.send(http.MethodPost, self.collectionUrl(), map[string]string{"Content-Type": "video/quicktime", "X-Photo-Client-ID": clientPhotoId, "X-Photo-File-Role": "liveVideo"}, videoContent), http.StatusCreated)
	return self.uploadAndProcess(clientPhotoId, jpegHeaders(clientPhotoId, "X-Photo-Kind", "livePhoto"), pictureContent)
}

func TestALivePhotoGetsAPlayableVideoPreviewFromItsVideo(test *testing.T) {
	video := liveVideoContent(test)
	temporaryDirectory := test.TempDir()
	test.Setenv("TMPDIR", temporaryDirectory)
	fixture := newFixture(test)
	found := fixture.uploadLivePhoto("live", smallJpeg(test, 90), video)
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.PhotoKind != models.PhotoKindLivePhoto {
		test.Fatalf("%+v", found)
	}
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		if preview, _ := fixture.previewOf(found.ID, photoPreviewSize); preview == nil || preview.ContentType != "image/jpeg" {
			test.Fatalf("the %s preview: %+v", photoPreviewSize, preview)
		}
	}
	playable, playableBytes := fixture.previewOf(found.ID, models.PhotoPreviewVideo)
	if playable == nil || playable.ContentType != "video/mp4" || len(playableBytes) < 100 || string(playableBytes[4:8]) != "ftyp" || playable.WidthPixels != 320 || playable.HeightPixels != 240 {
		test.Fatalf("the video preview: %+v", playable)
	}
	// The photo's size and length are those of the picture, not the video.
	if found.DurationMS != 0 {
		test.Fatalf("the picture has a length of %d ms", found.DurationMS)
	}
	if entries, err := os.ReadDir(temporaryDirectory); err != nil || len(entries) != 0 {
		test.Fatalf("something was made in the system's temporary directory: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(fixture.workDirectory); err != nil || len(entries) != 0 {
		test.Fatalf("the work directory was left behind: %v %v", entries, err)
	}
	if promisedBytes := fixture.store.WorkSpacePromisedBytes(); promisedBytes != 0 {
		test.Fatalf("%d bytes of room are still promised", promisedBytes)
	}
}

// The phone sends a Live Photo's picture first, and it is ready before the
// video arrives. The video then puts the photo back to waiting for its video
// preview, and its faces, which come from the picture, are not found again.
func TestALivePhotoWhoseVideoArrivesAfterItsPictureIsReadyGetsItsVideoPreviewWithoutFindingFacesAgain(test *testing.T) {
	video := liveVideoContent(test)
	fixture := newFixture(test)
	reader := &fakeFaceReader{scripts: map[int][]*faces.FoundFace{0: {scriptedFace(0, 0.1, 0.95, 0.2)}}}
	fixture.store.SetFaceReader(reader, nil)
	found := fixture.uploadAndProcess("late-video", jpegHeaders("late-video", "X-Photo-Kind", "livePhoto"), smallJpeg(test, reddish[0]))
	if found.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("%+v", found)
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewVideo); preview != nil {
		test.Fatalf("a Live Photo with no video has a video preview: %+v", preview)
	}
	facesBefore := fixture.facesOf(found.ID)
	foundAtBefore := fixture.facesFoundAt("late-video")
	if len(facesBefore) != 1 || foundAtBefore == "never" || reader.callCount() != 1 {
		test.Fatalf("%d faces, found at %s, %d reads", len(facesBefore), foundAtBefore, reader.callCount())
	}

	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "video/quicktime", "X-Photo-Client-ID": "late-video", "X-Photo-File-Role": "liveVideo"}, video), http.StatusCreated)
	if waiting := fixture.photoByClientId("late-video"); waiting.PhotoProcessingState != models.PhotoProcessingWaiting || waiting.ProcessingClaimedAt != nil {
		test.Fatalf("the video left the photo %s", waiting.PhotoProcessingState)
	}
	fixture.process()
	again := fixture.photoByClientId("late-video")
	if again.PhotoProcessingState != models.PhotoProcessingReady || again.PhotoKind != models.PhotoKindLivePhoto {
		test.Fatalf("%+v", again)
	}
	playable, _ := fixture.previewOf(found.ID, models.PhotoPreviewVideo)
	if playable == nil || playable.ContentType != "video/mp4" {
		test.Fatalf("the video preview: %+v", playable)
	}
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		if preview, _ := fixture.previewOf(found.ID, photoPreviewSize); preview == nil {
			test.Fatalf("no %s preview", photoPreviewSize)
		}
	}
	if count := fixture.count("photo_preview"); count != "3" {
		test.Fatalf("%s previews", count)
	}
	facesAfter := fixture.facesOf(found.ID)
	if len(facesAfter) != 1 || facesAfter[0].ID != facesBefore[0].ID || fixture.facesFoundAt("late-video") != foundAtBefore || reader.callCount() != 1 {
		test.Fatalf("the faces were found again: %+v, %d reads", facesAfter, reader.callCount())
	}
}

func TestALivePhotoWithABrokenVideoIsStillReadyWithItsPictures(test *testing.T) {
	// The programs must be there for the video to be tried at all.
	liveVideoContent(test)
	fixture := newFixture(test)
	found := fixture.uploadLivePhoto("broken", smallJpeg(test, 91), randomBytes(3000, 77))
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.PhotoKind != models.PhotoKindLivePhoto || found.ProcessingAttemptCount != 0 {
		test.Fatalf("%+v", found)
	}
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		if preview, _ := fixture.previewOf(found.ID, photoPreviewSize); preview == nil {
			test.Fatalf("no %s preview", photoPreviewSize)
		}
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewVideo); preview != nil {
		test.Fatalf("a broken video has a preview: %+v", preview)
	}
	if entries, err := os.ReadDir(fixture.workDirectory); err != nil || len(entries) != 0 {
		test.Fatalf("the work directory was left behind: %v %v", entries, err)
	}
}

func TestAReadyLivePhotoWithoutAVideoPreviewIsMadeAgainOnce(test *testing.T) {
	video := liveVideoContent(test)
	fixture := newFixture(test)
	found := fixture.uploadLivePhoto("older", smallJpeg(test, 92), video)
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewVideo); preview == nil {
		test.Fatal("the video preview was not made")
	}
	// As a Live Photo processed before video previews were made: ready, with
	// no video preview, and a video that arrived after its other previews.
	fixture.age("DELETE FROM photo_preview WHERE photo_preview_size = 'video'")
	fixture.age("UPDATE photo_preview SET created_at = now() - interval '3 days'")
	// An idle pass takes it back, and the next one makes the preview.
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if again := fixture.photoByClientId("older"); again.PhotoProcessingState != models.PhotoProcessingWaiting {
		test.Fatalf("the photo is %s", again.PhotoProcessingState)
	}
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	again := fixture.photoByClientId("older")
	if again.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("the photo is %s", again.PhotoProcessingState)
	}
	if playable, _ := fixture.previewOf(again.ID, models.PhotoPreviewVideo); playable == nil || playable.ContentType != "video/mp4" {
		test.Fatalf("the video preview: %+v", playable)
	}
}

// A video that cannot be made into a preview leaves its photo ready without
// one, and the photo is not put back for it again and again.
func TestAReadyLivePhotoWithAVideoThatCannotBeReadIsNotMadeAgainEveryDay(test *testing.T) {
	liveVideoContent(test)
	fixture := newFixture(test)
	found := fixture.uploadLivePhoto("unreadable", smallJpeg(test, 93), randomBytes(3000, 78))
	fixture.age("UPDATE photo_preview SET created_at = now() - interval '3 days'")
	// The video arrived after the previews were made, so one more attempt is
	// made, which fails the same way.
	for attempt := 0; attempt < 2; attempt++ {
		if err := fixture.store.Process(test.Context(), 4); err != nil {
			test.Fatal(err)
		}
		fixture.age("UPDATE photo SET updated_at = now() - interval '3 days'")
	}
	// Every later pass leaves the photo as it is.
	for attempt := 0; attempt < 2; attempt++ {
		if err := fixture.store.Process(test.Context(), 4); err != nil {
			test.Fatal(err)
		}
		if again := fixture.photoByClientId("unreadable"); again.PhotoProcessingState != models.PhotoProcessingReady {
			test.Fatalf("pass %d: the photo is %s", attempt, again.PhotoProcessingState)
		}
		fixture.age("UPDATE photo SET updated_at = now() - interval '3 days'")
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewVideo); preview != nil {
		test.Fatalf("an unreadable video has a preview: %+v", preview)
	}
}
