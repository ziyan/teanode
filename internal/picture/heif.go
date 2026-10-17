package picture

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// heifBox is one ISO base media file format box: a four letter type and the
// bytes after its header.
type heifBox struct {
	kind    string
	payload []byte
}

// parseBoxes splits data into the boxes that follow one another in it. A box
// that claims more bytes than are left is an error, and so is a header cut
// short; a size of 0 means the box runs to the end.
func parseBoxes(data []byte) ([]heifBox, error) {
	var boxes []heifBox
	for len(data) > 0 {
		if len(boxes) >= maximumBoxesPerLevel {
			return nil, errors.New("picture: a HEIC box holds too many boxes")
		}
		if len(data) < 8 {
			return nil, errors.New("picture: a HEIC box header is cut short")
		}
		size := uint64(binary.BigEndian.Uint32(data))
		headerBytes := uint64(8)
		switch size {
		case 0:
			size = uint64(len(data))
		case 1:
			if len(data) < 16 {
				return nil, errors.New("picture: a HEIC box header is cut short")
			}
			size = binary.BigEndian.Uint64(data[8:])
			headerBytes = 16
		}
		if size < headerBytes || size > uint64(len(data)) {
			return nil, errors.New("picture: a HEIC box is larger than what holds it")
		}
		boxes = append(boxes, heifBox{kind: string(data[4:8]), payload: data[headerBytes:size]})
		data = data[size:]
	}
	return boxes, nil
}

// memoryReader reads big-endian numbers from a slice. The first read past the
// end sets err and every read after it returns 0, so a parser can read a whole
// structure and look at err once.
type memoryReader struct {
	data     []byte
	position int
	err      error
}

func (self *memoryReader) unsigned(widthBytes int) uint64 {
	if self.err != nil {
		return 0
	}
	if widthBytes < 0 || widthBytes > 8 || self.position+widthBytes > len(self.data) {
		self.err = errors.New("picture: a HEIC box is cut short")
		return 0
	}
	var value uint64
	for _, next := range self.data[self.position : self.position+widthBytes] {
		value = value<<8 | uint64(next)
	}
	self.position += widthBytes
	return value
}

// readHeifMetadata finds the Exif item of a HEIC or HEIF file, and the size of
// its primary picture. Only the meta box is read into memory; the EXIF bytes
// themselves are read from where iloc says they are.
func readHeifMetadata(content io.ReaderAt, sizeBytes int64) (*Metadata, error) {
	metaPayload, err := findMetaBox(content, sizeBytes)
	if err != nil {
		return nil, err
	}
	// A box with a version and flags in front of its contents.
	if len(metaPayload) < 4 {
		return nil, errors.New("picture: the HEIC meta box is cut short")
	}
	children, err := parseBoxes(metaPayload[4:])
	if err != nil {
		return nil, err
	}

	var primaryItemId uint64
	var hasPrimaryItem bool
	var exifItemId uint64
	var hasExifItem bool
	var locationBox, idataBox, propertiesBox []byte
	for _, child := range children {
		switch child.kind {
		case "pitm":
			reader := &memoryReader{data: child.payload}
			version := reader.unsigned(4) >> 24
			primaryItemId = reader.unsigned(2 + 2*int(min(version, 1)))
			if reader.err != nil {
				return nil, reader.err
			}
			hasPrimaryItem = true
		case "iinf":
			if exifItemId, hasExifItem, err = findExifItem(child.payload); err != nil {
				return nil, err
			}
		case "iloc":
			locationBox = child.payload
		case "idat":
			idataBox = child.payload
		case "iprp":
			propertiesBox = child.payload
		}
	}

	metadata := &Metadata{}
	if hasExifItem && locationBox != nil {
		exifItem, err := readItem(content, sizeBytes, locationBox, idataBox, exifItemId)
		if err != nil {
			return nil, err
		}
		if exifItem != nil {
			if metadata, err = parseHeifExif(exifItem); err != nil {
				return nil, err
			}
		}
	}
	if (metadata.WidthPixels == 0 || metadata.HeightPixels == 0) && hasPrimaryItem && propertiesBox != nil {
		width, height, err := findPrimarySize(propertiesBox, primaryItemId)
		if err != nil {
			return nil, err
		}
		metadata.WidthPixels, metadata.HeightPixels = width, height
	}
	return metadata, nil
}

// findMetaBox walks the top level boxes of the file, skipping the big ones by
// their size, and returns the contents of meta.
func findMetaBox(content io.ReaderAt, sizeBytes int64) ([]byte, error) {
	position := int64(0)
	for boxCount := 0; boxCount < maximumBoxesPerLevel && position < sizeBytes; boxCount++ {
		header, err := readExactly(content, position, min(16, sizeBytes-position), sizeBytes)
		if err != nil || len(header) < 8 {
			return nil, errors.New("picture: a HEIC box header is cut short")
		}
		size := int64(binary.BigEndian.Uint32(header))
		headerBytes := int64(8)
		switch size {
		case 0:
			size = sizeBytes - position
		case 1:
			if len(header) < 16 {
				return nil, errors.New("picture: a HEIC box header is cut short")
			}
			largeSize := binary.BigEndian.Uint64(header[8:])
			if largeSize > math.MaxInt64 {
				return nil, errors.New("picture: a HEIC box is larger than the file")
			}
			size, headerBytes = int64(largeSize), 16
		}
		if size < headerBytes || size > sizeBytes-position {
			return nil, errors.New("picture: a HEIC box is larger than the file")
		}
		if string(header[4:8]) == "meta" {
			if size-headerBytes > maximumHeifMetaBytes {
				return nil, errors.New("picture: the HEIC meta box is too large")
			}
			return readExactly(content, position+headerBytes, size-headerBytes, sizeBytes)
		}
		position += size
	}
	return nil, errors.New("picture: the HEIC file has no meta box")
}

// findExifItem reads the item information box for the item whose type is
// Exif. Entries older than version 2 carry no type and are skipped.
func findExifItem(payload []byte) (uint64, bool, error) {
	reader := &memoryReader{data: payload}
	version := reader.unsigned(4) >> 24
	reader.unsigned(2 + 2*int(min(version, 1)))
	if reader.err != nil {
		return 0, false, reader.err
	}
	entries, err := parseBoxes(payload[reader.position:])
	if err != nil {
		return 0, false, err
	}
	for _, entry := range entries {
		if entry.kind != "infe" {
			continue
		}
		entryReader := &memoryReader{data: entry.payload}
		entryVersion := entryReader.unsigned(4) >> 24
		if entryVersion < 2 {
			continue
		}
		itemId := entryReader.unsigned(2 + 2*int(min(entryVersion-2, 1)))
		entryReader.unsigned(2)
		itemType := entryReader.unsigned(4)
		if entryReader.err != nil {
			return 0, false, entryReader.err
		}
		if itemType == 0x45786966 { // "Exif"
			return itemId, true, nil
		}
	}
	return 0, false, nil
}

// readItem returns the bytes of one item, joined from its extents, or nil when
// iloc does not list it. The offsets in iloc come from the file, so each one
// is checked against the file's size before anything is read or allocated.
func readItem(content io.ReaderAt, sizeBytes int64, locationBox, idataBox []byte, itemId uint64) ([]byte, error) {
	reader := &memoryReader{data: locationBox}
	version := int(reader.unsigned(4) >> 24)
	sizes := reader.unsigned(2)
	offsetBytes, lengthBytes := int(sizes>>12&0xF), int(sizes>>8&0xF)
	baseOffsetBytes, indexBytes := int(sizes>>4&0xF), int(sizes&0xF)
	if version > 2 {
		return nil, errors.New("picture: the HEIC item locations are of a version this reader does not know")
	}
	if version == 0 {
		indexBytes = 0
	}
	for _, width := range []int{offsetBytes, lengthBytes, baseOffsetBytes, indexBytes} {
		if width != 0 && width != 4 && width != 8 {
			return nil, errors.New("picture: the HEIC item locations use a number size this reader does not know")
		}
	}
	itemCount := reader.unsigned(2 + 2*int(version/2))
	for index := uint64(0); index < itemCount && reader.err == nil; index++ {
		currentId := reader.unsigned(2 + 2*int(version/2))
		constructionMethod := uint64(0)
		if version >= 1 {
			constructionMethod = reader.unsigned(2) & 0xF
		}
		reader.unsigned(2) // data reference index
		baseOffset := reader.unsigned(baseOffsetBytes)
		extentCount := reader.unsigned(2)

		var item []byte
		isTooLong := false
		for extent := uint64(0); extent < extentCount && reader.err == nil; extent++ {
			reader.unsigned(indexBytes)
			extentOffset := reader.unsigned(offsetBytes)
			extentLength := reader.unsigned(lengthBytes)
			if currentId != itemId || reader.err != nil || isTooLong {
				continue
			}
			if extentLength == 0 || extentLength > maximumExifItemBytes-uint64(len(item)) || baseOffset > math.MaxInt64 || extentOffset > math.MaxInt64-baseOffset {
				isTooLong = true
				continue
			}
			start := int64(baseOffset + extentOffset)
			var part []byte
			var err error
			switch constructionMethod {
			case 0:
				part, err = readExactly(content, start, int64(extentLength), sizeBytes)
			case 1:
				if start > int64(len(idataBox)) || int64(extentLength) > int64(len(idataBox))-start {
					err = errors.New("picture: the HEIC file ends before a part it points to")
				} else {
					part = idataBox[start : start+int64(extentLength)]
				}
			default:
				// An offset into another item: not how a phone stores EXIF.
				isTooLong = true
				continue
			}
			if err != nil {
				return nil, err
			}
			item = append(item, part...)
		}
		if currentId == itemId && reader.err == nil {
			if isTooLong {
				return nil, errors.New("picture: the HEIC EXIF item is empty, too large or stored in a way this reader does not know")
			}
			return item, nil
		}
	}
	if reader.err != nil {
		return nil, reader.err
	}
	return nil, nil
}

// parseHeifExif reads an Exif item. It starts with a four byte offset to the
// TIFF header, counted from the end of that field, because the bytes between
// are usually the "Exif\0\0" a JPEG carries.
func parseHeifExif(item []byte) (*Metadata, error) {
	if len(item) < 4 {
		return nil, errors.New("picture: the HEIC EXIF item is too short")
	}
	offset := uint64(binary.BigEndian.Uint32(item))
	if offset > uint64(len(item)-4) {
		return nil, errors.New("picture: the HEIC EXIF item points past its end")
	}
	return parseTiff(item[4+offset:])
}

// findPrimarySize reads the size property (ispe) that iprp associates with the
// primary item.
func findPrimarySize(propertiesBox []byte, primaryItemId uint64) (int, int, error) {
	children, err := parseBoxes(propertiesBox)
	if err != nil {
		return 0, 0, err
	}
	var properties []heifBox
	var associations []byte
	for _, child := range children {
		switch child.kind {
		case "ipco":
			if properties, err = parseBoxes(child.payload); err != nil {
				return 0, 0, err
			}
		case "ipma":
			associations = child.payload
		}
	}
	if associations == nil {
		return 0, 0, nil
	}
	reader := &memoryReader{data: associations}
	header := reader.unsigned(4)
	version, flags := header>>24, header&0xFFFFFF
	entryCount := reader.unsigned(4)
	for entry := uint64(0); entry < entryCount && reader.err == nil; entry++ {
		itemId := reader.unsigned(2 + 2*int(min(version, 1)))
		associationCount := reader.unsigned(1)
		for association := uint64(0); association < associationCount && reader.err == nil; association++ {
			var propertyIndex uint64
			if flags&1 != 0 {
				propertyIndex = reader.unsigned(2) & 0x7FFF
			} else {
				propertyIndex = reader.unsigned(1) & 0x7F
			}
			if itemId != primaryItemId || propertyIndex < 1 || propertyIndex > uint64(len(properties)) {
				continue
			}
			property := properties[propertyIndex-1]
			if property.kind == "ispe" && len(property.payload) >= 12 {
				width := uint64(binary.BigEndian.Uint32(property.payload[4:]))
				height := uint64(binary.BigEndian.Uint32(property.payload[8:]))
				if width > math.MaxInt32 || height > math.MaxInt32 {
					return 0, 0, nil
				}
				return int(width), int(height), nil
			}
		}
	}
	if reader.err != nil {
		return 0, 0, reader.err
	}
	return 0, 0, nil
}
