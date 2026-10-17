package picture

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"sort"
	"testing"
)

// testByteOrder is a byte order that can also write numbers.
type testByteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// testEntry is one entry of a TIFF directory a test builds by hand.
type testEntry struct {
	tag   uint16
	kind  uint16
	count uint32
	value []byte // already in the byte order of the block
}

func asciiEntry(tag uint16, text string) testEntry {
	value := append([]byte(text), 0)
	return testEntry{tag: tag, kind: 2, count: uint32(len(value)), value: value}
}

func shortEntry(order testByteOrder, tag uint16, number uint16) testEntry {
	value := order.AppendUint16(nil, number)
	return testEntry{tag: tag, kind: 3, count: 1, value: value}
}

func longEntry(order testByteOrder, tag uint16, number uint32) testEntry {
	return testEntry{tag: tag, kind: 4, count: 1, value: order.AppendUint32(nil, number)}
}

// rationalEntry holds numerator and denominator pairs.
func rationalEntry(order testByteOrder, tag uint16, pairs ...[2]uint32) testEntry {
	var value []byte
	for _, pair := range pairs {
		value = order.AppendUint32(value, pair[0])
		value = order.AppendUint32(value, pair[1])
	}
	return testEntry{tag: tag, kind: 5, count: uint32(len(pairs)), value: value}
}

// buildTiff assembles an EXIF block: a header, the three directories the
// reader follows, and the values that do not fit in an entry.
func buildTiff(order testByteOrder, primary, exif, gps []testEntry) []byte {
	if exif != nil {
		primary = append(primary, testEntry{tag: tagExifDirectory, kind: 4, count: 1})
	}
	if gps != nil {
		primary = append(primary, testEntry{tag: tagGpsDirectory, kind: 4, count: 1})
	}
	directories := [][]testEntry{primary, exif, gps}
	directoryBytes := func(entries []testEntry) int { return 2 + len(entries)*12 + 4 }
	offsets := make([]int, 3)
	position := 8
	for index, entries := range directories {
		if index > 0 && entries == nil {
			continue
		}
		offsets[index] = position
		position += directoryBytes(entries)
	}
	// The directory pointers can be filled now that the directories have places.
	for index := range primary {
		switch primary[index].tag {
		case tagExifDirectory:
			primary[index].value = order.AppendUint32(nil, uint32(offsets[1]))
		case tagGpsDirectory:
			primary[index].value = order.AppendUint32(nil, uint32(offsets[2]))
		}
	}
	var dataArea []byte
	dataStart := position
	block := make([]byte, 0, 256)
	block = append(block, 'I', 'I')
	if order == binary.BigEndian {
		block[0], block[1] = 'M', 'M'
	}
	block = order.AppendUint16(block, 42)
	block = order.AppendUint32(block, 8)
	for index, entries := range directories {
		if index > 0 && entries == nil {
			continue
		}
		sorted := append([]testEntry(nil), entries...)
		sort.Slice(sorted, func(first, second int) bool { return sorted[first].tag < sorted[second].tag })
		block = order.AppendUint16(block, uint16(len(sorted)))
		for _, entry := range sorted {
			block = order.AppendUint16(block, entry.tag)
			block = order.AppendUint16(block, entry.kind)
			block = order.AppendUint32(block, entry.count)
			if len(entry.value) <= 4 {
				padded := append(append([]byte(nil), entry.value...), 0, 0, 0, 0)[:4]
				block = append(block, padded...)
				continue
			}
			block = order.AppendUint32(block, uint32(dataStart+len(dataArea)))
			dataArea = append(dataArea, entry.value...)
			if len(dataArea)%2 == 1 {
				dataArea = append(dataArea, 0)
			}
		}
		block = order.AppendUint32(block, 0)
	}
	return append(block, dataArea...)
}

// exampleTiff is a block with every field the reader looks for: 10.5 degrees
// north, 20.25 degrees east, in the given byte order.
func exampleTiff(order testByteOrder, orientation uint16) []byte {
	return buildTiff(order,
		[]testEntry{
			asciiEntry(tagMake, "Example Camera Co"),
			asciiEntry(tagModel, "Model 1"),
			shortEntry(order, tagOrientation, orientation),
			asciiEntry(tagDateTime, "2020:01:01 00:00:00"),
		},
		[]testEntry{
			asciiEntry(tagDateTimeOriginal, "2024:05:06 07:08:09"),
			asciiEntry(tagOffsetTimeOriginal, "+02:00"),
			longEntry(order, tagPixelXDimension, 4000),
			longEntry(order, tagPixelYDimension, 3000),
		},
		[]testEntry{
			asciiEntry(tagGpsLatitudeRef, "N"),
			rationalEntry(order, tagGpsLatitude, [2]uint32{10, 1}, [2]uint32{30, 1}, [2]uint32{0, 1}),
			asciiEntry(tagGpsLongitudeRef, "E"),
			rationalEntry(order, tagGpsLongitude, [2]uint32{20, 1}, [2]uint32{1500, 100}, [2]uint32{0, 1}),
		})
}

// quadrantPicture is a picture whose four quarters are different colors, so
// that where each ends up says how it was turned: A top left, B top right,
// C bottom left, D bottom right.
func quadrantPicture(width, height int) *image.RGBA {
	quarters := map[string]color.RGBA{
		"A": {R: 230, G: 30, B: 30, A: 255},
		"B": {R: 30, G: 200, B: 30, A: 255},
		"C": {R: 30, G: 30, B: 230, A: 255},
		"D": {R: 230, G: 220, B: 30, A: 255},
	}
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			name := "A"
			switch {
			case column >= width/2 && row < height/2:
				name = "B"
			case column < width/2 && row >= height/2:
				name = "C"
			case column >= width/2 && row >= height/2:
				name = "D"
			}
			picture.SetRGBA(column, row, quarters[name])
		}
	}
	return picture
}

// quadrantName names the quarter a pixel is nearest to in color.
func quadrantName(t *testing.T, pixel color.Color) string {
	t.Helper()
	red, green, blue, _ := pixel.RGBA()
	candidates := map[string][3]uint32{"A": {230, 30, 30}, "B": {30, 200, 30}, "C": {30, 30, 230}, "D": {230, 220, 30}}
	best, bestDistance := "", uint32(1<<31)
	for name, want := range candidates {
		distance := absoluteDifference(red>>8, want[0]) + absoluteDifference(green>>8, want[1]) + absoluteDifference(blue>>8, want[2])
		if distance < bestDistance {
			best, bestDistance = name, distance
		}
	}
	if bestDistance > 150 {
		t.Fatalf("pixel %v is not near any quarter", pixel)
	}
	return best
}

func absoluteDifference(first, second uint32) uint32 {
	if first > second {
		return first - second
	}
	return second - first
}

// jpegWith writes picture as a JPEG and splices exif, an EXIF block starting
// at its TIFF header, in behind the start of image marker.
func jpegWith(t *testing.T, picture image.Image, exif []byte) []byte {
	t.Helper()
	return mustJpeg(picture, exif)
}

func mustJpeg(picture image.Image, exif []byte) []byte {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 95}); err != nil {
		panic(err)
	}
	if exif == nil {
		return encoded.Bytes()
	}
	segment := append([]byte("Exif\x00\x00"), exif...)
	spliced := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	spliced = binary.BigEndian.AppendUint16(spliced, uint16(len(segment)+2))
	spliced = append(spliced, segment...)
	return append(spliced, encoded.Bytes()[2:]...)
}

// heifOptions are the ways a test HEIC differs.
type heifOptions struct {
	locationVersion int  // 0, 1 or 2
	isInHeaderData  bool // the EXIF item lives in idat, not in mdat (version 1 or 2)
	isLargeMdat     bool // mdat's size is in the 64 bit field
	hasExif         bool
	infeVersion     int // 2 or 3
}

func box(kind string, payload ...[]byte) []byte {
	joined := bytes.Join(payload, nil)
	result := binary.BigEndian.AppendUint32(nil, uint32(8+len(joined)))
	result = append(result, kind...)
	return append(result, joined...)
}

func fullBox(kind string, version byte, payload ...[]byte) []byte {
	return box(kind, append([][]byte{{version, 0, 0, 0}}, payload...)...)
}

func bigEndian32(values ...uint32) []byte {
	var result []byte
	for _, value := range values {
		result = binary.BigEndian.AppendUint32(result, value)
	}
	return result
}

// buildHeif assembles the boxes of a HEIC file with the primary picture's
// size (item 1, 4032 by 3024) and, when asked, an Exif item (item 2) holding
// exif, a TIFF block.
func buildHeif(options heifOptions, exif []byte) []byte {
	exifItem := append(bigEndian32(6), []byte("Exif\x00\x00")...)
	exifItem = append(exifItem, exif...)

	build := func(exifOffset uint32) []byte {
		infe := func(itemId uint32, kind string) []byte {
			if options.infeVersion == 3 {
				return fullBox("infe", 3, bigEndian32(itemId), []byte{0, 0}, []byte(kind), []byte{0})
			}
			return fullBox("infe", 2, binary.BigEndian.AppendUint16(nil, uint16(itemId)), []byte{0, 0}, []byte(kind), []byte{0})
		}
		itemInfo := fullBox("iinf", 0, []byte{0, 1}, infe(1, "hvc1"))
		if options.hasExif {
			itemInfo = fullBox("iinf", 0, []byte{0, 2}, infe(1, "hvc1"), infe(2, "Exif"))
		}

		var locations []byte
		method := uint16(0)
		if options.isInHeaderData {
			method = 1
		}
		// All widths are 4 bytes, except that the base offset has none.
		sizes := []byte{0x44, 0x00}
		itemId := func(value uint32) []byte {
			if options.locationVersion == 2 {
				return bigEndian32(value)
			}
			return binary.BigEndian.AppendUint16(nil, uint16(value))
		}
		count := func(value uint32) []byte {
			if options.locationVersion == 2 {
				return bigEndian32(value)
			}
			return binary.BigEndian.AppendUint16(nil, uint16(value))
		}
		item := func(id uint32, offset, length uint32, usesMethod bool) []byte {
			result := itemId(id)
			if options.locationVersion >= 1 {
				result = binary.BigEndian.AppendUint16(result, 0)
				if usesMethod {
					result[len(result)-1] = byte(method)
				}
			}
			result = append(result, 0, 0) // data reference index
			result = binary.BigEndian.AppendUint16(result, 1)
			return append(result, bigEndian32(offset, length)...)
		}
		items := item(1, 1000, 50, false)
		itemTotal := uint32(1)
		if options.hasExif {
			items = append(items, item(2, exifOffset, uint32(len(exifItem)), true)...)
			itemTotal = 2
		}
		locations = fullBox("iloc", byte(options.locationVersion), sizes, count(itemTotal), items)

		properties := box("iprp",
			box("ipco", fullBox("ispe", 0, bigEndian32(4032, 3024))),
			fullBox("ipma", 0, bigEndian32(1), binary.BigEndian.AppendUint16(nil, 1), []byte{1, 1}))
		parts := [][]byte{fullBox("hdlr", 0, bigEndian32(0), []byte("pict"), make([]byte, 13)), fullBox("pitm", 0, []byte{0, 1}), itemInfo, locations, properties}
		if options.isInHeaderData {
			parts = append(parts, box("idat", exifItem))
		}
		return fullBox("meta", 0, parts...)
	}

	fileType := box("ftyp", []byte("heic"), bigEndian32(0), []byte("mif1heic"))
	meta := build(0)
	var mdatHeaderBytes int
	var mdatHeader []byte
	if options.isLargeMdat {
		mdatHeader, mdatHeaderBytes = append(append(bigEndian32(1), "mdat"...), make([]byte, 8)...), 16
		binary.BigEndian.PutUint64(mdatHeader[8:], uint64(16+len(exifItem)))
	} else {
		mdatHeader, mdatHeaderBytes = append(bigEndian32(uint32(8+len(exifItem))), "mdat"...), 8
	}
	exifOffset := uint32(len(fileType) + len(meta) + mdatHeaderBytes)
	if options.isInHeaderData {
		// Offsets into idat start at its first byte.
		exifOffset = 0
	}
	meta = build(exifOffset)
	file := append(append([]byte(nil), fileType...), meta...)
	file = append(file, mdatHeader...)
	return append(file, exifItem...)
}
