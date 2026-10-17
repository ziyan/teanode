package picture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Metadata is what a picture file says about itself.
//
// A date without a time zone offset has no moment in it, only a wall clock
// reading: TakenAt then holds that reading as if it were UTC and
// TakenAtOffsetMinutes is nil, so a caller can tell the two cases apart and
// show the wall clock as written.
//
// WidthPixels and HeightPixels are the stored size of the picture. Turning it
// upright with Orientation swaps them for orientations 5 to 8. A HEIC file
// says in its own boxes how to turn it, and heif-dec has done that by the
// time a caller sees pixels, so Orientation does not apply to HEIC output.
type Metadata struct {
	TakenAt              *time.Time
	TakenAtOffsetMinutes *int
	Latitude             *float64
	Longitude            *float64
	CameraMake           string
	CameraModel          string
	// Orientation is the EXIF value 1 to 8, or 0 when the file does not say.
	Orientation  int
	WidthPixels  int
	HeightPixels int
}

// The bounds on what a file may make this reader allocate. An EXIF block in a
// JPEG cannot exceed one segment (64 KiB), and a HEIC's can be larger, but a
// phone's is a few dozen KiB.
const (
	maximumHeifMetaBytes = 32 << 20
	maximumExifItemBytes = 8 << 20
	maximumBoxesPerLevel = 100_000
	maximumTextBytes     = 128
)

// ReadMetadata reads the EXIF block of a JPEG or a HEIC/HEIF file. A file
// that is neither, or whose boxes, segments or EXIF directories do not fit
// inside it, is an error. A file that has no EXIF block is not: it gets the
// size it states for itself and nothing else.
func ReadMetadata(content io.ReaderAt, sizeBytes int64) (*Metadata, error) {
	if sizeBytes < 12 {
		return nil, errors.New("picture: the file is too short to be a picture")
	}
	header, err := readExactly(content, 0, 12, sizeBytes)
	if err != nil {
		return nil, err
	}
	switch {
	case header[0] == 0xFF && header[1] == 0xD8:
		return readJpegMetadata(content, sizeBytes)
	case string(header[4:8]) == "ftyp":
		return readHeifMetadata(content, sizeBytes)
	}
	return nil, errors.New("picture: the metadata of only JPEG and HEIC files can be read")
}

// readExactly reads length bytes at offset, refusing a range that is not
// inside the file before anything is allocated.
func readExactly(content io.ReaderAt, offset, length, sizeBytes int64) ([]byte, error) {
	if offset < 0 || length < 0 || offset > sizeBytes || length > sizeBytes-offset {
		return nil, errors.New("picture: the file ends before a part it points to")
	}
	data := make([]byte, length)
	if _, err := content.ReadAt(data, offset); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("picture: the file could not be read: %w", err)
	}
	return data, nil
}

// jpegScan is what a pass over a JPEG's segments finds.
type jpegScan struct {
	exif         []byte // the TIFF block of the first APP1 Exif segment, or nil
	widthPixels  int
	heightPixels int
}

func scanJpeg(content io.ReaderAt, sizeBytes int64) (*jpegScan, error) {
	scan := &jpegScan{}
	position := int64(2)
	for segmentCount := 0; segmentCount < maximumBoxesPerLevel; segmentCount++ {
		start, err := readExactly(content, position, 1, sizeBytes)
		if err != nil || start[0] != 0xFF {
			return nil, errors.New("picture: the JPEG does not have a marker where one belongs")
		}
		position++
		// Any number of 0xFF bytes may pad before the marker itself.
		marker := byte(0xFF)
		for marker == 0xFF {
			next, err := readExactly(content, position, 1, sizeBytes)
			if err != nil {
				return nil, errors.New("picture: the JPEG ends before its picture starts")
			}
			marker = next[0]
			position++
		}
		// Markers that stand alone, with no length after them.
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD8) {
			continue
		}
		// The picture data starts at the start of scan; nothing after it says
		// how the picture lies.
		if marker == 0xDA || marker == 0xD9 {
			return scan, nil
		}
		lengthBytes, err := readExactly(content, position, 2, sizeBytes)
		if err != nil {
			return nil, errors.New("picture: a JPEG segment is cut short")
		}
		segmentLength := int64(binary.BigEndian.Uint16(lengthBytes))
		if segmentLength < 2 || position+segmentLength > sizeBytes {
			return nil, errors.New("picture: a JPEG segment does not fit in the file")
		}
		payloadStart, payloadLength := position+2, segmentLength-2
		isFrameHeader := marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC
		switch {
		case marker == 0xE1 && scan.exif == nil && payloadLength > 6:
			prefix, err := readExactly(content, payloadStart, 6, sizeBytes)
			if err != nil {
				return nil, err
			}
			if string(prefix) == "Exif\x00\x00" {
				scan.exif, err = readExactly(content, payloadStart+6, payloadLength-6, sizeBytes)
				if err != nil {
					return nil, err
				}
			}
		case isFrameHeader && scan.widthPixels == 0 && payloadLength >= 6:
			frame, err := readExactly(content, payloadStart, 6, sizeBytes)
			if err != nil {
				return nil, err
			}
			scan.heightPixels = int(binary.BigEndian.Uint16(frame[1:3]))
			scan.widthPixels = int(binary.BigEndian.Uint16(frame[3:5]))
		}
		position += segmentLength
	}
	return nil, errors.New("picture: a JPEG has too many segments")
}

func readJpegMetadata(content io.ReaderAt, sizeBytes int64) (*Metadata, error) {
	scan, err := scanJpeg(content, sizeBytes)
	if err != nil {
		return nil, err
	}
	metadata := &Metadata{}
	if scan.exif != nil {
		if metadata, err = parseTiff(scan.exif); err != nil {
			return nil, err
		}
	}
	if metadata.WidthPixels == 0 || metadata.HeightPixels == 0 {
		metadata.WidthPixels, metadata.HeightPixels = scan.widthPixels, scan.heightPixels
	}
	return metadata, nil
}

// jpegOrientation reads the EXIF orientation of a JPEG: 1 to 8, or 0 when it
// has none or is not a JPEG. A damaged EXIF block must not stop a picture
// from being shown, so every failure reads as no orientation.
func jpegOrientation(content []byte) int {
	if len(content) < 4 || content[0] != 0xFF || content[1] != 0xD8 {
		return 0
	}
	scan, err := scanJpeg(bytes.NewReader(content), int64(len(content)))
	if err != nil || scan.exif == nil {
		return 0
	}
	metadata, err := parseTiff(scan.exif)
	if err != nil {
		return 0
	}
	return metadata.Orientation
}

// The EXIF tags this reader uses.
const (
	tagMake               = 0x010F
	tagModel              = 0x0110
	tagOrientation        = 0x0112
	tagDateTime           = 0x0132
	tagExifDirectory      = 0x8769
	tagGpsDirectory       = 0x8825
	tagOffsetTime         = 0x9010
	tagDateTimeOriginal   = 0x9003
	tagOffsetTimeOriginal = 0x9011
	tagPixelXDimension    = 0xA002
	tagPixelYDimension    = 0xA003
	tagGpsLatitudeRef     = 0x0001
	tagGpsLatitude        = 0x0002
	tagGpsLongitudeRef    = 0x0003
	tagGpsLongitude       = 0x0004
)

// TIFF value types, and the sizes of the structures around them.
const (
	tiffTypeAscii          = 2
	tiffTypeShort          = 3
	tiffTypeLong           = 4
	tiffTypeRational       = 5
	tiffEntryBytes         = 12
	tiffHeaderBytes        = 8
	tiffLittleEndianMarker = "II"
	tiffBigEndianMarker    = "MM"
)

// tiffTypeBytes is the size of one value of each TIFF type; a type not listed
// is one this reader skips.
var tiffTypeBytes = map[uint16]uint64{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8}

type tiffEntry struct {
	tag         uint16
	kind        uint16
	count       uint32
	valueOffset int // where the entry's 4 byte value or offset field starts
}

type tiffBlock struct {
	data  []byte
	order binary.ByteOrder
}

// parseTiff reads the directories of an EXIF block, which starts with its
// TIFF header. The structure has to fit inside the block, or the block is
// malformed; a value that does not (a date cut short, an offset past the end)
// is left out, because one bad value should not take the others with it.
func parseTiff(data []byte) (*Metadata, error) {
	if len(data) < tiffHeaderBytes {
		return nil, errors.New("picture: the EXIF block is too short")
	}
	block := &tiffBlock{data: data}
	switch string(data[:2]) {
	case tiffLittleEndianMarker:
		block.order = binary.LittleEndian
	case tiffBigEndianMarker:
		block.order = binary.BigEndian
	default:
		return nil, errors.New("picture: the EXIF block has no byte order mark")
	}
	if block.order.Uint16(data[2:]) != 42 {
		return nil, errors.New("picture: the EXIF block has no TIFF header")
	}
	primary, err := block.readDirectory(block.order.Uint32(data[4:]))
	if err != nil {
		return nil, err
	}

	metadata := &Metadata{}
	var exifDirectoryOffset, gpsDirectoryOffset uint32
	var dateTime string
	for _, entry := range primary {
		switch entry.tag {
		case tagMake:
			metadata.CameraMake = block.text(entry)
		case tagModel:
			metadata.CameraModel = block.text(entry)
		case tagOrientation:
			if value, isRead := block.integer(entry); isRead && value >= 1 && value <= 8 {
				metadata.Orientation = int(value)
			}
		case tagDateTime:
			dateTime = block.text(entry)
		case tagExifDirectory:
			exifDirectoryOffset, _ = block.integerAsOffset(entry)
		case tagGpsDirectory:
			gpsDirectoryOffset, _ = block.integerAsOffset(entry)
		}
	}

	offsetText := ""
	if exifDirectoryOffset != 0 {
		entries, err := block.readDirectory(exifDirectoryOffset)
		if err != nil {
			return nil, err
		}
		var originalDateTime, originalOffset, plainOffset string
		for _, entry := range entries {
			switch entry.tag {
			case tagDateTimeOriginal:
				originalDateTime = block.text(entry)
			case tagOffsetTimeOriginal:
				originalOffset = block.text(entry)
			case tagOffsetTime:
				plainOffset = block.text(entry)
			case tagPixelXDimension:
				if value, isRead := block.integer(entry); isRead && value <= math.MaxInt32 {
					metadata.WidthPixels = int(value)
				}
			case tagPixelYDimension:
				if value, isRead := block.integer(entry); isRead && value <= math.MaxInt32 {
					metadata.HeightPixels = int(value)
				}
			}
		}
		if originalDateTime != "" {
			dateTime, offsetText = originalDateTime, originalOffset
		} else {
			offsetText = plainOffset
		}
	}
	metadata.TakenAt, metadata.TakenAtOffsetMinutes = parseExifDate(dateTime, offsetText)

	if gpsDirectoryOffset != 0 {
		entries, err := block.readDirectory(gpsDirectoryOffset)
		if err != nil {
			return nil, err
		}
		metadata.Latitude, metadata.Longitude = block.coordinates(entries)
	}
	return metadata, nil
}

// readDirectory returns the entries of the directory at offset, or an error
// when the directory does not fit inside the block. A directory's own link to
// a next one is never followed, so a block cannot send the reader in circles.
func (self *tiffBlock) readDirectory(offset uint32) ([]tiffEntry, error) {
	if uint64(offset) < tiffHeaderBytes || uint64(offset)+2 > uint64(len(self.data)) {
		return nil, errors.New("picture: an EXIF directory lies outside the block")
	}
	count := uint64(self.order.Uint16(self.data[offset:]))
	if uint64(offset)+2+count*tiffEntryBytes > uint64(len(self.data)) {
		return nil, errors.New("picture: an EXIF directory is longer than the block")
	}
	entries := make([]tiffEntry, count)
	for index := range entries {
		position := int(offset) + 2 + index*tiffEntryBytes
		entries[index] = tiffEntry{
			tag:         self.order.Uint16(self.data[position:]),
			kind:        self.order.Uint16(self.data[position+2:]),
			count:       self.order.Uint32(self.data[position+4:]),
			valueOffset: position + 8,
		}
	}
	return entries, nil
}

// value returns the bytes an entry holds: in the entry itself when they fit in
// four, otherwise where the entry points.
func (self *tiffBlock) value(entry tiffEntry) ([]byte, bool) {
	unitBytes, isKnown := tiffTypeBytes[entry.kind]
	if !isKnown {
		return nil, false
	}
	totalBytes := unitBytes * uint64(entry.count)
	if totalBytes <= 4 {
		return self.data[entry.valueOffset : entry.valueOffset+int(totalBytes)], true
	}
	offset := uint64(self.order.Uint32(self.data[entry.valueOffset:]))
	if offset+totalBytes > uint64(len(self.data)) {
		return nil, false
	}
	return self.data[offset : offset+totalBytes], true
}

// text reads an ASCII entry up to its first NUL, bounded so a long one cannot
// fill a database column.
func (self *tiffBlock) text(entry tiffEntry) string {
	if entry.kind != tiffTypeAscii {
		return ""
	}
	value, isRead := self.value(entry)
	if !isRead {
		return ""
	}
	if end := bytes.IndexByte(value, 0); end >= 0 {
		value = value[:end]
	}
	text := strings.TrimSpace(strings.ToValidUTF8(string(value), ""))
	if len(text) > maximumTextBytes {
		text = text[:maximumTextBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}

// integer reads the first value of a SHORT or LONG entry.
func (self *tiffBlock) integer(entry tiffEntry) (uint32, bool) {
	if entry.count < 1 || (entry.kind != tiffTypeShort && entry.kind != tiffTypeLong) {
		return 0, false
	}
	value, isRead := self.value(entry)
	if !isRead {
		return 0, false
	}
	if entry.kind == tiffTypeShort {
		return uint32(self.order.Uint16(value)), true
	}
	return self.order.Uint32(value), true
}

func (self *tiffBlock) integerAsOffset(entry tiffEntry) (uint32, bool) {
	if entry.kind != tiffTypeLong {
		return 0, false
	}
	return self.integer(entry)
}

// rationals reads count RATIONAL values as fractions; a zero denominator makes
// the whole entry unreadable.
func (self *tiffBlock) rationals(entry tiffEntry, count uint32) ([]float64, bool) {
	if entry.kind != tiffTypeRational || entry.count != count {
		return nil, false
	}
	value, isRead := self.value(entry)
	if !isRead {
		return nil, false
	}
	numbers := make([]float64, count)
	for index := range numbers {
		numerator := self.order.Uint32(value[index*8:])
		denominator := self.order.Uint32(value[index*8+4:])
		if denominator == 0 {
			return nil, false
		}
		numbers[index] = float64(numerator) / float64(denominator)
	}
	return numbers, true
}

// coordinates turns the GPS directory's degrees, minutes and seconds into
// signed degrees. Both or neither come back, and only inside the range of the
// globe.
func (self *tiffBlock) coordinates(entries []tiffEntry) (*float64, *float64) {
	var latitudeEntry, longitudeEntry tiffEntry
	var latitudeReference, longitudeReference string
	var hasLatitude, hasLongitude bool
	for _, entry := range entries {
		switch entry.tag {
		case tagGpsLatitudeRef:
			latitudeReference = self.text(entry)
		case tagGpsLatitude:
			latitudeEntry, hasLatitude = entry, true
		case tagGpsLongitudeRef:
			longitudeReference = self.text(entry)
		case tagGpsLongitude:
			longitudeEntry, hasLongitude = entry, true
		}
	}
	if !hasLatitude || !hasLongitude {
		return nil, nil
	}
	latitude, isLatitudeRead := self.signedDegrees(latitudeEntry, latitudeReference, "N", "S", 90)
	longitude, isLongitudeRead := self.signedDegrees(longitudeEntry, longitudeReference, "E", "W", 180)
	if !isLatitudeRead || !isLongitudeRead {
		return nil, nil
	}
	return &latitude, &longitude
}

func (self *tiffBlock) signedDegrees(entry tiffEntry, reference, positive, negative string, limitDegrees float64) (float64, bool) {
	parts, isRead := self.rationals(entry, 3)
	if !isRead {
		return 0, false
	}
	degrees := parts[0] + parts[1]/60 + parts[2]/3600
	switch strings.ToUpper(reference) {
	case positive:
	case negative:
		degrees = -degrees
	default:
		return 0, false
	}
	if math.IsNaN(degrees) || math.IsInf(degrees, 0) || math.Abs(degrees) > limitDegrees {
		return 0, false
	}
	return degrees, true
}

// earliestPlausibleYear is the first year a date a file states for itself is
// believed. Cameras whose clock was never set write the year their clock
// starts at (1970, 1980, 2000 and the like), and a date from before there were
// digital cameras is a clock that was never set.
const earliestPlausibleYear = 1990

// isPlausibleDate says whether a date a file states for itself can be believed:
// it is not from before earliestPlausibleYear, and not more than a day ahead of
// now, which is as far as a time zone and a camera a little fast can put it.
func isPlausibleDate(moment time.Time) bool {
	return moment.Year() >= earliestPlausibleYear && !moment.After(time.Now().Add(24*time.Hour))
}

// parseExifDate reads "2006:01:02 15:04:05" and an optional "+02:00". With an
// offset the moment is exact. Without one all there is is a wall clock
// reading, returned as if it were UTC, with no offset. A date that
// isPlausibleDate refuses is no date.
func parseExifDate(dateText, offsetText string) (*time.Time, *int) {
	if dateText == "" {
		return nil, nil
	}
	wallClock, err := time.Parse("2006:01:02 15:04:05", dateText)
	if err != nil {
		// Some writers use dashes in the date.
		if wallClock, err = time.Parse("2006-01-02 15:04:05", dateText); err != nil {
			return nil, nil
		}
	}
	offsetMinutes, hasOffset := parseExifOffset(offsetText)
	if !hasOffset {
		if !isPlausibleDate(wallClock) {
			return nil, nil
		}
		return &wallClock, nil
	}
	moment := time.Date(wallClock.Year(), wallClock.Month(), wallClock.Day(), wallClock.Hour(), wallClock.Minute(), wallClock.Second(), 0, time.FixedZone("", offsetMinutes*60))
	if !isPlausibleDate(moment) {
		return nil, nil
	}
	return &moment, &offsetMinutes
}

// parseExifOffset reads "+02:00" or "-05:30" as minutes east of UTC, inside
// the range of time zones that exist.
func parseExifOffset(text string) (int, bool) {
	if len(text) != 6 || (text[0] != '+' && text[0] != '-') || text[3] != ':' {
		return 0, false
	}
	hours, minutes := twoDigits(text[1:3]), twoDigits(text[4:6])
	if hours < 0 || minutes < 0 || minutes > 59 {
		return 0, false
	}
	offsetMinutes := hours*60 + minutes
	if text[0] == '-' {
		offsetMinutes = -offsetMinutes
	}
	if offsetMinutes < -12*60 || offsetMinutes > 14*60 {
		return 0, false
	}
	return offsetMinutes, true
}

func twoDigits(text string) int {
	if text[0] < '0' || text[0] > '9' || text[1] < '0' || text[1] > '9' {
		return -1
	}
	return int(text[0]-'0')*10 + int(text[1]-'0')
}
