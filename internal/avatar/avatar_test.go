package avatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
	"time"
)

// The same three ids are in the iPhone app's tests, with the same indexes:
// that agreement is what makes a person one color on every screen.
func TestInitialColorIndex(t *testing.T) {
	t.Parallel()

	for userId, want := range map[string]int{
		"01JABCDEFGHJKMNPQRSTVWXYZ0": 10,
		"01HZZZZZZZZZZZZZZZZZZZZZZZ": 4,
		"user-a":                     0,
	} {
		if got := InitialColorIndex(userId); got != want {
			t.Errorf("InitialColorIndex(%q) = %d, want %d", userId, got, want)
		}
	}
}

// halves is a square picture that is red above and blue below, so that a
// quarter turn is visible in where the colors end up.
func halves(side int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(picture, image.Rect(0, 0, side, side/2), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(picture, image.Rect(0, side/2, side, side), image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
	return picture
}

// exifSegment is an APP1 block carrying an orientation tag, a location
// marker string to look for afterwards, and nothing a decoder needs.
func exifSegment(orientation uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM")
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, orientation)
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))
	tiff.WriteString("GPSLatitude=00.0000")

	body := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(body)+2))
	return append(segment, body...)
}

func jpegWithExif(t *testing.T, picture image.Image, orientation uint16) []byte {
	t.Helper()
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, picture, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("cannot encode the test picture: %s", err)
	}
	content := plain.Bytes()
	// Right after the start-of-image marker, where a camera puts it.
	return append(append(append([]byte{}, content[:2]...), exifSegment(orientation)...), content[2:]...)
}

func dominant(picture image.Image, x, y int) string {
	red, _, blue, _ := picture.At(x, y).RGBA()
	if red > blue {
		return "red"
	}
	return "blue"
}

func TestNormalizeDropsMetadataAndTurnsThePictureUpright(t *testing.T) {
	t.Parallel()

	original := jpegWithExif(t, halves(100), 6)
	if !bytes.Contains(original, []byte("GPSLatitude")) {
		t.Fatalf("the test picture does not carry the block it is about")
	}
	normalized, err := Normalize(t.Context(), original)
	if err != nil {
		t.Fatalf("Normalize: %s", err)
	}
	if bytes.Contains(normalized, []byte("GPSLatitude")) || bytes.Contains(normalized, []byte("Exif")) {
		t.Errorf("the result still carries the metadata block")
	}
	decoded, format, err := image.Decode(bytes.NewReader(normalized))
	if err != nil || format != "png" {
		t.Fatalf("the result is %q, err %v; want a PNG", format, err)
	}
	if decoded.Bounds().Dx() != SidePixels || decoded.Bounds().Dy() != SidePixels {
		t.Errorf("the result is %v, want %dx%d", decoded.Bounds(), SidePixels, SidePixels)
	}
	// Orientation 6 is a quarter turn clockwise: red, which was on top,
	// ends up on the right.
	if dominant(decoded, SidePixels-20, SidePixels/2) != "red" || dominant(decoded, 20, SidePixels/2) != "blue" {
		t.Errorf("the picture was not turned upright: right is %s, left is %s",
			dominant(decoded, SidePixels-20, SidePixels/2), dominant(decoded, 20, SidePixels/2))
	}
}

func TestNormalizeCropsTheCenterSquare(t *testing.T) {
	t.Parallel()

	// Wide: green in the middle third, red on both sides. The square is the
	// middle, so no red should survive.
	wide := image.NewRGBA(image.Rect(0, 0, 300, 100))
	draw.Draw(wide, wide.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(wide, image.Rect(100, 0, 200, 100), image.NewUniform(color.RGBA{G: 255, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, wide); err != nil {
		t.Fatal(err)
	}
	normalized, err := Normalize(t.Context(), encoded.Bytes())
	if err != nil {
		t.Fatalf("Normalize: %s", err)
	}
	decoded, err := png.Decode(bytes.NewReader(normalized))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != SidePixels || decoded.Bounds().Dy() != SidePixels {
		t.Fatalf("the result is %v, want a square of %d", decoded.Bounds(), SidePixels)
	}
	for _, point := range []image.Point{{0, 0}, {SidePixels - 1, 0}, {0, SidePixels - 1}, {SidePixels - 1, SidePixels - 1}, {SidePixels / 2, SidePixels / 2}} {
		red, green, _, _ := decoded.At(point.X, point.Y).RGBA()
		if red != 0 || green == 0 {
			t.Errorf("pixel %v is not the middle third of the original", point)
		}
	}
}

func TestNormalizeRefusesWhatItCannotUse(t *testing.T) {
	t.Parallel()

	// A header that declares 30000 by 30000 pixels and nothing else: a few
	// dozen bytes that would cost gigabytes to decode.
	var header bytes.Buffer
	header.WriteString("\x89PNG\r\n\x1a\n")
	chunk := make([]byte, 13)
	binary.BigEndian.PutUint32(chunk[0:], 30000)
	binary.BigEndian.PutUint32(chunk[4:], 30000)
	chunk[8], chunk[9] = 8, 2
	_ = binary.Write(&header, binary.BigEndian, uint32(len(chunk)))
	header.WriteString("IHDR")
	header.Write(chunk)
	_ = binary.Write(&header, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("IHDR"), chunk...)))

	for name, content := range map[string][]byte{
		"a huge declared size": header.Bytes(),
		"nothing":              nil,
		"text":                 []byte("not a picture"),
		"too many bytes":       bytes.Repeat([]byte{0}, MaxInputBytes+1),
	} {
		if _, err := Normalize(t.Context(), content); err != ErrUnusable {
			t.Errorf("%s: err = %v, want ErrUnusable", name, err)
		}
	}
}

func TestResizeShrinksAndLeavesSmallerRequestsAlone(t *testing.T) {
	t.Parallel()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, halves(SidePixels)); err != nil {
		t.Fatal(err)
	}
	small, err := Resize(encoded.Bytes(), 64)
	if err != nil {
		t.Fatalf("Resize: %s", err)
	}
	decoded, err := png.Decode(bytes.NewReader(small))
	if err != nil || decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
		t.Fatalf("the result is %v, err %v; want 64x64", decoded.Bounds(), err)
	}
	if dominant(decoded, 32, 5) != "red" || dominant(decoded, 32, 58) != "blue" {
		t.Errorf("the shrunk picture lost its halves")
	}
	same, err := Resize(encoded.Bytes(), 1024)
	if err != nil || !bytes.Equal(same, encoded.Bytes()) {
		t.Errorf("a request above the stored size should return the picture as it is, err %v", err)
	}
}

func TestEnlargingKeepsASolidColorSolid(t *testing.T) {
	t.Parallel()

	solid := image.NewRGBA(image.Rect(0, 0, 10, 10))
	draw.Draw(solid, solid.Bounds(), image.NewUniform(color.RGBA{R: 10, G: 200, B: 30, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, solid); err != nil {
		t.Fatal(err)
	}
	normalized, err := Normalize(t.Context(), encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := png.Decode(bytes.NewReader(normalized))
	if got := color.RGBAModel.Convert(decoded.At(0, 0)).(color.RGBA); got != (color.RGBA{R: 10, G: 200, B: 30, A: 255}) {
		t.Errorf("a corner of the enlarged picture is %v", got)
	}
}

// Only a few pictures are decoded at once: with every turn taken, another
// waits its time and is then turned away as busy, and takes its turn as soon as
// one is free. A file that is not a picture is refused without waiting.
func TestOnlyAFewPicturesAreDecodedAtOnce(t *testing.T) {
	// Not parallel: the turns are the package's.
	original := image.NewRGBA(image.Rect(0, 0, 64, 32))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, original); err != nil {
		t.Fatalf("Encode: %s", err)
	}
	previousWait := normalizeWait
	normalizeWait = 100 * time.Millisecond
	defer func() { normalizeWait = previousWait }()

	for range ConcurrentNormalizations {
		normalizeSlots <- struct{}{}
	}
	if _, err := Normalize(t.Context(), encoded.Bytes()); !errors.Is(err, ErrBusy) {
		t.Errorf("with every turn taken a picture got %v; want busy", err)
	}
	if _, err := Normalize(t.Context(), []byte("not a picture")); err != ErrUnusable {
		t.Errorf("a file that is not a picture waited for a turn: %v", err)
	}
	// A turn freed while one waits is the waiter's.
	normalizeWait = 5 * time.Second
	go func() {
		time.Sleep(50 * time.Millisecond)
		<-normalizeSlots
	}()
	if _, err := Normalize(t.Context(), encoded.Bytes()); err != nil {
		t.Errorf("a picture that waited for a turn got %v", err)
	}
	for len(normalizeSlots) > 0 {
		<-normalizeSlots
	}
}
