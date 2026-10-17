package picture

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"
)

func encodePng(t *testing.T, picture image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestDecodeFormats(t *testing.T) {
	source := quadrantPicture(32, 16)
	var gifBytes bytes.Buffer
	if err := gif.Encode(&gifBytes, source, nil); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"jpeg": jpegWith(t, source, nil),
		"png":  encodePng(t, source),
		"gif":  gifBytes.Bytes(),
	} {
		decoded, err := Decode(content)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 16 {
			t.Errorf("%s: size = %v", name, decoded.Bounds())
		}
		if got := quadrantName(t, decoded.At(24, 12)); got != "D" {
			t.Errorf("%s: bottom right is %s", name, got)
		}
	}
}

func TestDecodeRefusesWhatIsNotAPicture(t *testing.T) {
	if _, err := Decode([]byte("plain text")); err == nil {
		t.Error("text: want an error")
	}
	if _, err := Decode(nil); err == nil {
		t.Error("nothing: want an error")
	}
	// Cut short in the middle of the data: the header is fine, the pixels are not.
	valid := encodePng(t, quadrantPicture(32, 16))
	if _, err := Decode(valid[:len(valid)/2]); err == nil {
		t.Error("truncated: want an error")
	}
}

// pngClaiming writes just the signature and header of a PNG of the given
// size. The pixels are missing, which is the point: they are never read.
func pngClaiming(width, height uint32) []byte {
	header := binary.BigEndian.AppendUint32(nil, width)
	header = binary.BigEndian.AppendUint32(header, height)
	header = append(header, 8, 2, 0, 0, 0)
	content := []byte("\x89PNG\r\n\x1a\n")
	content = binary.BigEndian.AppendUint32(content, uint32(len(header)))
	chunk := append([]byte("IHDR"), header...)
	content = append(content, chunk...)
	return binary.BigEndian.AppendUint32(content, crc32.ChecksumIEEE(chunk))
}

func TestDecodeRefusesTooManyPixelsBeforeDecoding(t *testing.T) {
	// 100 megapixels is allowed by the header check; one more row is not.
	_, err := Decode(pngClaiming(20000, 5001))
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want a refusal for size", err)
	}
	// Within the limit the header passes and the missing pixels are what fail.
	_, err = Decode(pngClaiming(1000, 1000))
	if err == nil || strings.Contains(err.Error(), "too large") {
		t.Errorf("err = %v, want a decoding failure", err)
	}
}

func TestDecodeTurnsEveryOrientationUpright(t *testing.T) {
	// The four quarters of the source, A B over C D, as they should read after
	// each orientation of the EXIF standard is applied.
	expected := map[uint16]struct {
		width, height int
		rows          [2][2]string
	}{
		1: {32, 16, [2][2]string{{"A", "B"}, {"C", "D"}}},
		2: {32, 16, [2][2]string{{"B", "A"}, {"D", "C"}}},
		3: {32, 16, [2][2]string{{"D", "C"}, {"B", "A"}}},
		4: {32, 16, [2][2]string{{"C", "D"}, {"A", "B"}}},
		5: {16, 32, [2][2]string{{"A", "C"}, {"B", "D"}}},
		6: {16, 32, [2][2]string{{"C", "A"}, {"D", "B"}}},
		7: {16, 32, [2][2]string{{"D", "B"}, {"C", "A"}}},
		8: {16, 32, [2][2]string{{"B", "D"}, {"A", "C"}}},
	}
	for _, order := range []testByteOrder{binary.LittleEndian, binary.BigEndian} {
		for orientation, want := range expected {
			tiff := buildTiff(order, []testEntry{shortEntry(order, tagOrientation, orientation)}, nil, nil)
			decoded, err := Decode(jpegWith(t, quadrantPicture(32, 16), tiff))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() != want.width || decoded.Bounds().Dy() != want.height {
				t.Errorf("orientation %d: size = %v", orientation, decoded.Bounds())
				continue
			}
			for row := 0; row < 2; row++ {
				for column := 0; column < 2; column++ {
					pixel := decoded.At(want.width/4+column*want.width/2, want.height/4+row*want.height/2)
					if got := quadrantName(t, pixel); got != want.rows[row][column] {
						t.Errorf("orientation %d: quarter at row %d column %d is %s, want %s", orientation, row, column, got, want.rows[row][column])
					}
				}
			}
		}
	}
}

func TestDecodeLeavesAPNGAloneWhateverItsBytesSay(t *testing.T) {
	// Only a JPEG's EXIF is believed.
	decoded, err := Decode(encodePng(t, quadrantPicture(32, 16)))
	if err != nil || decoded.Bounds().Dx() != 32 {
		t.Errorf("decoded = %v, err = %v", decoded.Bounds(), err)
	}
}

func TestScaleAveragesTheCoveredPixels(t *testing.T) {
	// Three pixels shrunk to two: each output pixel covers a pixel and a half.
	source := image.NewRGBA(image.Rect(0, 0, 3, 1))
	source.SetRGBA(0, 0, color.RGBA{A: 255})
	source.SetRGBA(1, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	source.SetRGBA(2, 0, color.RGBA{A: 255})
	scaled := Scale(source, 2)
	if scaled.Bounds().Dx() != 2 || scaled.Bounds().Dy() != 1 {
		t.Fatalf("size = %v", scaled.Bounds())
	}
	for column := 0; column < 2; column++ {
		red, _, _, alpha := scaled.At(column, 0).RGBA()
		if red>>8 != 85 || alpha>>8 != 255 {
			t.Errorf("pixel %d: red = %d alpha = %d, want 85 and 255", column, red>>8, alpha>>8)
		}
	}

	// A checkerboard shrunk to a quarter of its side is gray, not one of its colors.
	board := image.NewGray(image.Rect(0, 0, 8, 8))
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			if (row+column)%2 == 0 {
				board.SetGray(column, row, color.Gray{Y: 255})
			}
		}
	}
	scaled = Scale(board, 2)
	for row := 0; row < 2; row++ {
		for column := 0; column < 2; column++ {
			red, _, _, _ := scaled.At(column, row).RGBA()
			if red>>8 < 127 || red>>8 > 128 {
				t.Errorf("checkerboard pixel (%d, %d) = %d, want 127 or 128", column, row, red>>8)
			}
		}
	}
}

func TestScaleKeepsTheShapeAndNeverEnlarges(t *testing.T) {
	source := quadrantPicture(300, 200)
	scaled := Scale(source, 150)
	if scaled.Bounds().Dx() != 150 || scaled.Bounds().Dy() != 100 {
		t.Errorf("landscape = %v", scaled.Bounds())
	}
	tall := Scale(quadrantPicture(200, 301), 100)
	if tall.Bounds().Dx() != 66 || tall.Bounds().Dy() != 100 {
		t.Errorf("portrait = %v", tall.Bounds())
	}
	if same := Scale(source, 300); same != image.Image(source) {
		t.Error("a picture already within the limit should come back as it was")
	}
	if same := Scale(source, 1000); same != image.Image(source) {
		t.Error("a picture must not be enlarged")
	}
	if same := Scale(source, 0); same != image.Image(source) {
		t.Error("no limit should leave the picture alone")
	}
	// A very thin picture keeps at least one pixel across.
	thin := Scale(image.NewRGBA(image.Rect(0, 0, 1000, 2)), 10)
	if thin.Bounds().Dx() != 10 || thin.Bounds().Dy() != 1 {
		t.Errorf("thin = %v", thin.Bounds())
	}
	// The quarters stay in place.
	for _, quarter := range []struct {
		column, row int
		name        string
	}{{37, 25, "A"}, {112, 25, "B"}, {37, 75, "C"}, {112, 75, "D"}} {
		if got := quadrantName(t, scaled.At(quarter.column, quarter.row)); got != quarter.name {
			t.Errorf("quarter %s reads as %s", quarter.name, got)
		}
	}
}

// genericPicture hides a picture's type, so Scale has to read it pixel by pixel.
type genericPicture struct{ image.Image }

func TestScaleTypesAgree(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, gradientPicture(97, 61), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, isYCbCr := decoded.(*image.YCbCr); !isYCbCr {
		t.Fatalf("the decoder made a %T", decoded)
	}
	reference := Scale(genericPicture{decoded}, 40).(*image.RGBA)
	for name, source := range map[string]image.Image{
		"YCbCr": decoded,
		"RGBA":  toRGBA(decoded),
		"NRGBA": toNRGBA(decoded),
	} {
		scaled := Scale(source, 40).(*image.RGBA)
		if scaled.Bounds() != reference.Bounds() {
			t.Fatalf("%s: size = %v, want %v", name, scaled.Bounds(), reference.Bounds())
		}
		for index := range scaled.Pix {
			// The YCbCr path rounds the same way color.YCbCrToRGB does, the
			// generic path through the color model; they may differ by one.
			if difference := int(scaled.Pix[index]) - int(reference.Pix[index]); difference < -1 || difference > 1 {
				t.Fatalf("%s: byte %d = %d, want %d", name, index, scaled.Pix[index], reference.Pix[index])
			}
		}
	}

	gray := image.NewGray(image.Rect(0, 0, 10, 10))
	for index := range gray.Pix {
		gray.Pix[index] = 200
	}
	if red, _, _, alpha := Scale(gray, 5).At(2, 2).RGBA(); red>>8 != 200 || alpha>>8 != 255 {
		t.Errorf("gray = %d %d", red>>8, alpha>>8)
	}
	palette := image.NewPaletted(image.Rect(0, 0, 10, 10), color.Palette{color.RGBA{R: 10, G: 20, B: 30, A: 255}})
	if red, green, blue, _ := Scale(palette, 5).At(2, 2).RGBA(); red>>8 != 10 || green>>8 != 20 || blue>>8 != 30 {
		t.Errorf("palette = %d %d %d", red>>8, green>>8, blue>>8)
	}
}

func TestScaleKeepsPremultipliedTransparencyHonest(t *testing.T) {
	// Half of the pixels fully clear, half opaque red: the average is a
	// half-clear red, whatever color the clear ones carried.
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 255, B: 255, A: 0})
	scaled := Scale(source, 1)
	red, green, blue, alpha := scaled.At(0, 0).RGBA()
	if red>>8 != 128 || green>>8 != 0 || blue>>8 != 0 || alpha>>8 != 128 {
		t.Errorf("pixel = %d %d %d %d, want red and alpha at half", red>>8, green>>8, blue>>8, alpha>>8)
	}
}

func TestScaleOfAPhoneSizedPictureIsFast(t *testing.T) {
	if isRaceDetectorOn {
		t.Skip("the race detector makes timing meaningless")
	}
	// The same shape a phone's decoder gives: YCbCr, 4:2:0.
	source := image.NewYCbCr(image.Rect(0, 0, 4032, 3024), image.YCbCrSubsampleRatio420)
	for index := range source.Y {
		source.Y[index] = uint8(index)
	}
	started := time.Now()
	scaled := Scale(source, 2048)
	elapsed := time.Since(started)
	if scaled.Bounds().Dx() != 2048 || scaled.Bounds().Dy() != 1536 {
		t.Errorf("size = %v", scaled.Bounds())
	}
	if elapsed > time.Second {
		t.Errorf("scaling took %v", elapsed)
	}
	t.Logf("4032 by 3024 to 2048 took %v", elapsed)
}

func TestEncodeJPEG(t *testing.T) {
	encoded, err := EncodeJPEG(quadrantPicture(32, 16), 90)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil || decoded.Bounds().Dx() != 32 {
		t.Fatalf("decoded = %v, err = %v", decoded, err)
	}
	if got := quadrantName(t, decoded.At(24, 12)); got != "D" {
		t.Errorf("bottom right is %s", got)
	}
	// Written pictures are upright already and say nothing to turn.
	if jpegOrientation(encoded) != 0 {
		t.Error("the JPEG carries an orientation")
	}

	// What was clear comes out white, not black.
	clear := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	encoded, err = EncodeJPEG(clear, 90)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if red, _, _, _ := decoded.At(8, 8).RGBA(); red>>8 < 250 {
		t.Errorf("clear pixel came out %d, want white", red>>8)
	}

	for _, quality := range []int{0, 101, -5} {
		if _, err := EncodeJPEG(quadrantPicture(4, 4), quality); err == nil {
			t.Errorf("quality %d: want an error", quality)
		}
	}
	if _, err := EncodeJPEG(image.NewRGBA(image.Rect(0, 0, 0, 0)), 80); err == nil {
		t.Error("empty picture: want an error")
	}
}

func TestEncodePPM(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	source.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	source.SetRGBA(2, 1, color.RGBA{R: 250, G: 251, B: 252, A: 255})
	// A clear pixel is laid on white.
	source.SetRGBA(1, 0, color.RGBA{})
	encoded := EncodePPM(source)
	header := "P6\n3 2\n255\n"
	if !bytes.HasPrefix(encoded, []byte(header)) {
		t.Fatalf("header = %q", encoded[:len(header)])
	}
	body := encoded[len(header):]
	if len(body) != 3*2*3 {
		t.Fatalf("body = %d bytes", len(body))
	}
	if !bytes.Equal(body[0:3], []byte{1, 2, 3}) || !bytes.Equal(body[3:6], []byte{255, 255, 255}) || !bytes.Equal(body[15:18], []byte{250, 251, 252}) {
		t.Errorf("body = %v", body)
	}

	// A YCbCr picture, as a JPEG decodes to, gives what the color model says.
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, gradientPicture(20, 10), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(&jpegBytes)
	if err != nil {
		t.Fatal(err)
	}
	body = EncodePPM(decoded)[len("P6\n20 10\n255\n"):]
	for row := 0; row < 10; row++ {
		for column := 0; column < 20; column++ {
			red, green, blue, _ := decoded.At(column, row).RGBA()
			offset := (row*20 + column) * 3
			for channel, want := range []uint32{red >> 8, green >> 8, blue >> 8} {
				if difference := int(body[offset+channel]) - int(want); difference < -1 || difference > 1 {
					t.Fatalf("pixel (%d, %d) channel %d = %d, want %d", column, row, channel, body[offset+channel], want)
				}
			}
		}
	}
}

// gradientPicture changes color in both directions, so a wrong row or column
// would show.
func gradientPicture(width, height int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			picture.SetRGBA(column, row, color.RGBA{R: uint8(column * 255 / width), G: uint8(row * 255 / height), B: uint8((column + row) * 255 / (width + height)), A: 255})
		}
	}
	return picture
}

func toRGBA(source image.Image) *image.RGBA {
	converted := image.NewRGBA(source.Bounds())
	for row := 0; row < source.Bounds().Dy(); row++ {
		for column := 0; column < source.Bounds().Dx(); column++ {
			converted.Set(column, row, source.At(column, row))
		}
	}
	return converted
}

func toNRGBA(source image.Image) *image.NRGBA {
	converted := image.NewNRGBA(source.Bounds())
	for row := 0; row < source.Bounds().Dy(); row++ {
		for column := 0; column < source.Bounds().Dx(); column++ {
			converted.Set(column, row, source.At(column, row))
		}
	}
	return converted
}
