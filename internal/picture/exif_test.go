package picture

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
	"time"
)

func readMetadataFrom(t *testing.T, content []byte) (*Metadata, error) {
	t.Helper()
	return ReadMetadata(bytes.NewReader(content), int64(len(content)))
}

func TestReadMetadataFromJPEGInBothByteOrders(t *testing.T) {
	for name, order := range map[string]testByteOrder{"little endian": binary.LittleEndian, "big endian": binary.BigEndian} {
		t.Run(name, func(t *testing.T) {
			content := jpegWith(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), exampleTiff(order, 6))
			metadata, err := readMetadataFrom(t, content)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.CameraMake != "Example Camera Co" || metadata.CameraModel != "Model 1" {
				t.Errorf("camera = %q %q", metadata.CameraMake, metadata.CameraModel)
			}
			if metadata.Orientation != 6 {
				t.Errorf("orientation = %d", metadata.Orientation)
			}
			if metadata.WidthPixels != 4000 || metadata.HeightPixels != 3000 {
				t.Errorf("size = %d by %d", metadata.WidthPixels, metadata.HeightPixels)
			}
			if metadata.TakenAt == nil || metadata.TakenAtOffsetMinutes == nil {
				t.Fatalf("date = %v offset = %v", metadata.TakenAt, metadata.TakenAtOffsetMinutes)
			}
			if want := time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC); !metadata.TakenAt.Equal(want) {
				t.Errorf("TakenAt = %v, want the moment %v", metadata.TakenAt, want)
			}
			if *metadata.TakenAtOffsetMinutes != 120 {
				t.Errorf("offset = %d", *metadata.TakenAtOffsetMinutes)
			}
			if metadata.Latitude == nil || metadata.Longitude == nil || *metadata.Latitude != 10.5 || *metadata.Longitude != 20.25 {
				t.Errorf("place = %v %v", metadata.Latitude, metadata.Longitude)
			}
		})
	}
}

func TestReadMetadataDateWithoutOffsetIsAWallClockInUTC(t *testing.T) {
	order := binary.BigEndian
	tiff := buildTiff(order,
		[]testEntry{asciiEntry(tagDateTime, "2021:12:31 23:59:58")},
		nil,
		[]testEntry{
			asciiEntry(tagGpsLatitudeRef, "S"),
			rationalEntry(order, tagGpsLatitude, [2]uint32{10, 1}, [2]uint32{30, 1}, [2]uint32{0, 1}),
			asciiEntry(tagGpsLongitudeRef, "W"),
			rationalEntry(order, tagGpsLongitude, [2]uint32{20, 1}, [2]uint32{15, 1}, [2]uint32{0, 1}),
		})
	metadata, err := readMetadataFrom(t, jpegWith(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), tiff))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TakenAtOffsetMinutes != nil {
		t.Errorf("offset = %d, want none", *metadata.TakenAtOffsetMinutes)
	}
	if want := time.Date(2021, 12, 31, 23, 59, 58, 0, time.UTC); metadata.TakenAt == nil || !metadata.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", metadata.TakenAt, want)
	}
	if metadata.Latitude == nil || *metadata.Latitude != -10.5 || *metadata.Longitude != -20.25 {
		t.Errorf("place = %v %v", metadata.Latitude, metadata.Longitude)
	}
	// The block says nothing about size, so the JPEG's own is used.
	if metadata.WidthPixels != 8 || metadata.HeightPixels != 8 {
		t.Errorf("size = %d by %d", metadata.WidthPixels, metadata.HeightPixels)
	}
}

func TestReadMetadataJPEGWithoutExif(t *testing.T) {
	metadata, err := readMetadataFrom(t, jpegWith(t, image.NewRGBA(image.Rect(0, 0, 12, 7)), nil))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.WidthPixels != 12 || metadata.HeightPixels != 7 || metadata.TakenAt != nil || metadata.Latitude != nil || metadata.Orientation != 0 {
		t.Errorf("metadata = %+v", metadata)
	}
}

func TestReadMetadataLeavesOutValuesItCannotUse(t *testing.T) {
	order := binary.LittleEndian
	tiff := buildTiff(order,
		[]testEntry{asciiEntry(tagDateTime, "0000:00:00 00:00:00")},
		[]testEntry{asciiEntry(tagDateTimeOriginal, "not a date"), asciiEntry(tagOffsetTimeOriginal, "+99:99")},
		[]testEntry{
			asciiEntry(tagGpsLatitudeRef, "N"),
			// A zero denominator is not a number.
			rationalEntry(order, tagGpsLatitude, [2]uint32{10, 0}, [2]uint32{0, 1}, [2]uint32{0, 1}),
			asciiEntry(tagGpsLongitudeRef, "E"),
			rationalEntry(order, tagGpsLongitude, [2]uint32{20, 1}, [2]uint32{0, 1}, [2]uint32{0, 1}),
		})
	metadata, err := readMetadataFrom(t, jpegWith(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), tiff))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TakenAt != nil || metadata.Latitude != nil || metadata.Longitude != nil {
		t.Errorf("metadata = %+v", metadata)
	}
}

// A camera whose clock was never set writes the year its clock starts at, and
// a date far ahead is a clock set wrong; neither is when the photo was taken.
func TestParseExifDateRefusesDatesNobodyTookAPhotoOn(t *testing.T) {
	now := time.Now().UTC()
	for name, text := range map[string]string{
		"the epoch":         "1970:01:01 00:00:00",
		"before cameras":    "1989:12:31 23:59:59",
		"a year ahead":      now.AddDate(1, 0, 0).Format("2006:01:02 15:04:05"),
		"two days ahead":    now.AddDate(0, 0, 2).Format("2006:01:02 15:04:05"),
		"a far future year": "2099:01:01 00:00:00",
		"with dashes":       "1980-01-01 00:00:00",
	} {
		if moment, offset := parseExifDate(text, ""); moment != nil || offset != nil {
			t.Errorf("%s: %q gave %v %v, want no date", name, text, moment, offset)
		}
		if moment, offset := parseExifDate(text, "+02:00"); moment != nil || offset != nil {
			t.Errorf("%s with an offset: %q gave %v %v, want no date", name, text, moment, offset)
		}
	}
	// The earliest year, and a time a few hours ahead (a camera a little fast,
	// or a zone east of the server), are believed.
	if moment, _ := parseExifDate("1990:01:01 00:00:00", ""); moment == nil {
		t.Error("1990 is believed")
	}
	soon := now.Add(3 * time.Hour).Format("2006:01:02 15:04:05")
	if moment, _ := parseExifDate(soon, ""); moment == nil {
		t.Errorf("%s is believed", soon)
	}
}

func TestReadMetadataOffsets(t *testing.T) {
	for text, want := range map[string]int{"+00:00": 0, "-05:30": -330, "+14:00": 840, "-12:00": -720} {
		got, isValid := parseExifOffset(text)
		if !isValid || got != want {
			t.Errorf("parseExifOffset(%q) = %d, %v; want %d", text, got, isValid, want)
		}
	}
	for _, text := range []string{"", "Z", "+2:00", "+02:60", "+15:00", "-13:00", "+0200", "+ab:cd"} {
		if got, isValid := parseExifOffset(text); isValid {
			t.Errorf("parseExifOffset(%q) = %d, want invalid", text, got)
		}
	}
}

func TestReadMetadataFromHEIC(t *testing.T) {
	for name, options := range map[string]heifOptions{
		"iloc version 0":                    {locationVersion: 0, hasExif: true, infeVersion: 2},
		"iloc version 1":                    {locationVersion: 1, hasExif: true, infeVersion: 2},
		"iloc version 2 and infe version 3": {locationVersion: 2, hasExif: true, infeVersion: 3},
		"item in idat":                      {locationVersion: 1, hasExif: true, infeVersion: 2, isInHeaderData: true},
		"large mdat":                        {locationVersion: 1, hasExif: true, infeVersion: 2, isLargeMdat: true},
	} {
		t.Run(name, func(t *testing.T) {
			content := buildHeif(options, exampleTiff(binary.BigEndian, 6))
			metadata, err := readMetadataFrom(t, content)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.CameraMake != "Example Camera Co" || metadata.Orientation != 6 {
				t.Errorf("metadata = %+v", metadata)
			}
			if metadata.Latitude == nil || *metadata.Latitude != 10.5 || *metadata.Longitude != 20.25 {
				t.Errorf("place = %v %v", metadata.Latitude, metadata.Longitude)
			}
			if metadata.TakenAt == nil || metadata.TakenAtOffsetMinutes == nil || *metadata.TakenAtOffsetMinutes != 120 {
				t.Errorf("date = %v %v", metadata.TakenAt, metadata.TakenAtOffsetMinutes)
			}
			// EXIF says 4000 by 3000, which wins over the ispe property.
			if metadata.WidthPixels != 4000 || metadata.HeightPixels != 3000 {
				t.Errorf("size = %d by %d", metadata.WidthPixels, metadata.HeightPixels)
			}
		})
	}
}

func TestReadMetadataFromHEICWithoutExifUsesTheSizeProperty(t *testing.T) {
	metadata, err := readMetadataFrom(t, buildHeif(heifOptions{locationVersion: 1, infeVersion: 2}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.WidthPixels != 4032 || metadata.HeightPixels != 3024 || metadata.TakenAt != nil {
		t.Errorf("metadata = %+v", metadata)
	}
}

func TestReadMetadataFromHEICExifWithoutSizeUsesTheSizeProperty(t *testing.T) {
	tiff := buildTiff(binary.LittleEndian, []testEntry{asciiEntry(tagMake, "Example Camera Co")}, nil, nil)
	metadata, err := readMetadataFrom(t, buildHeif(heifOptions{locationVersion: 1, hasExif: true, infeVersion: 2}, tiff))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CameraMake != "Example Camera Co" || metadata.WidthPixels != 4032 || metadata.HeightPixels != 3024 {
		t.Errorf("metadata = %+v", metadata)
	}
}

func TestReadMetadataRefusesWhatIsNotAPicture(t *testing.T) {
	for name, content := range map[string][]byte{
		"empty":   nil,
		"short":   {0xFF, 0xD8, 0xFF},
		"text":    []byte("this is not a picture at all"),
		"unknown": append([]byte{0, 0, 0, 20}, []byte("ftypxxxx")...),
	} {
		if _, err := readMetadataFrom(t, content); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// Every way of cutting a valid file short is an error or a smaller answer,
// never a panic.
func TestReadMetadataSurvivesTruncation(t *testing.T) {
	files := map[string][]byte{
		"jpeg": jpegWith(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), exampleTiff(binary.LittleEndian, 3)),
		"heic": buildHeif(heifOptions{locationVersion: 1, hasExif: true, infeVersion: 2}, exampleTiff(binary.BigEndian, 3)),
	}
	for name, content := range files {
		for length := 0; length < len(content); length++ {
			// The file reports its true size, as a file on disk that was
			// cut short would.
			_, _ = ReadMetadata(bytes.NewReader(content[:length]), int64(length))
			// And lies about it, as a reader whose size came from elsewhere might.
			_, _ = ReadMetadata(bytes.NewReader(content[:length]), int64(len(content)))
		}
		t.Log(name, "survived", len(content), "truncations")
	}
}

func TestReadMetadataRefusesBadStructure(t *testing.T) {
	validJpeg := func(exif []byte) []byte { return jpegWith(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), exif) }
	goodTiff := exampleTiff(binary.LittleEndian, 1)

	// A directory with 65535 entries in a block of a few hundred bytes.
	hugeCount := append([]byte(nil), goodTiff...)
	binary.LittleEndian.PutUint16(hugeCount[8:], 0xFFFF)
	// The first directory is somewhere past the end.
	farDirectory := append([]byte(nil), goodTiff...)
	binary.LittleEndian.PutUint32(farDirectory[4:], 0x7FFFFFFF)
	// A directory that points at itself is read once, not forever.
	loop := append([]byte(nil), goodTiff...)
	binary.LittleEndian.PutUint32(loop[4:], 0)
	// A byte order mark that is neither.
	noOrder := append([]byte(nil), goodTiff...)
	noOrder[0], noOrder[1] = 'X', 'X'

	for name, tiff := range map[string][]byte{"count": hugeCount, "directory": farDirectory, "loop": loop, "order": noOrder} {
		if _, err := readMetadataFrom(t, validJpeg(tiff)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	// A text value that claims four billion bytes is left out, with the rest kept.
	hugeText := buildTiff(binary.LittleEndian, []testEntry{{tag: tagMake, kind: 2, count: 0xFFFFFFFF, value: []byte{1, 0, 0, 0}}, shortEntry(binary.LittleEndian, tagOrientation, 3)}, nil, nil)
	metadata, err := readMetadataFrom(t, validJpeg(hugeText))
	if err != nil || metadata.CameraMake != "" || metadata.Orientation != 3 {
		t.Errorf("metadata = %+v, err = %v", metadata, err)
	}

	// A segment longer than the file.
	segment := validJpeg(goodTiff)
	binary.BigEndian.PutUint16(segment[4:], 0xFFFF)
	if _, err := readMetadataFrom(t, segment); err == nil {
		t.Error("a JPEG segment past the end: want an error")
	}
}

func TestReadMetadataRefusesBadBoxes(t *testing.T) {
	good := buildHeif(heifOptions{locationVersion: 1, hasExif: true, infeVersion: 2}, exampleTiff(binary.BigEndian, 1))
	metaStart := bytes.Index(good, []byte("meta")) - 4
	patch := func(offset int, value []byte) []byte {
		changed := append([]byte(nil), good...)
		copy(changed[offset:], value)
		return changed
	}

	tests := map[string][]byte{
		// meta claims to be longer than the file.
		"meta too long": patch(metaStart, bigEndian32(0x7FFFFFFF)),
		// A box smaller than its own header.
		"box too short": patch(metaStart, bigEndian32(4)),
		// 64 bit sizes that overflow.
		"huge large size": append(append([]byte(nil), good[:metaStart]...), append(append(bigEndian32(1), "meta"...), 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)...),
		// No meta at all.
		"no meta": good[:metaStart],
	}
	// iloc's item count and an extent length that claim billions.
	locationStart := bytes.Index(good, []byte("iloc")) + 4
	// The Exif item is the second one listed, after sixteen bytes of the first.
	exifItemStart := locationStart + 4 + 2 + 2 + 16
	tests["huge extent"] = patch(exifItemStart+12, bigEndian32(0xFFFFFFFF))
	tests["far extent"] = patch(exifItemStart+8, bigEndian32(0xFFFFFFF0))
	// An item count of 65535 with the Exif item not among them runs off the end.
	notListed := patch(locationStart+4+2, []byte{0xFF, 0xFF})
	copy(notListed[exifItemStart:], []byte{0, 7})
	tests["many items"] = notListed
	for name, content := range tests {
		if _, err := readMetadataFrom(t, content); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func FuzzReadMetadata(f *testing.F) {
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	f.Add(mustJpeg(picture, exampleTiff(binary.LittleEndian, 6)))
	f.Add(mustJpeg(picture, exampleTiff(binary.BigEndian, 2)))
	f.Add(buildHeif(heifOptions{locationVersion: 1, hasExif: true, infeVersion: 2}, exampleTiff(binary.BigEndian, 6)))
	f.Add(buildHeif(heifOptions{locationVersion: 2, hasExif: true, infeVersion: 3, isInHeaderData: true}, exampleTiff(binary.LittleEndian, 8)))
	f.Fuzz(func(t *testing.T, content []byte) {
		_, _ = ReadMetadata(bytes.NewReader(content), int64(len(content)))
	})
}
