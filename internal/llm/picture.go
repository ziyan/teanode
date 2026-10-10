package llm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // decoded for shrinking
	"image/jpeg"
	_ "image/png" // decoded for shrinking
	"math"
	"sort"
	"sync"
)

// Shrinking the pictures of a request that would otherwise be too large to
// send. A phone's photo is several megabytes; a model sees no more than
// about two thousand pixels a side of it anyway, so a smaller copy of the
// same picture loses nothing it would have read.

const (
	// pictureLongestSidePixels is the longest side a shrunk picture keeps
	// while it still fits: about what a model reads of a picture at most.
	pictureLongestSidePixels = 2048
	// pictureShortestSidePixels is how small a shrunk picture may get
	// before shrinking it further is pointless.
	pictureShortestSidePixels = 256
	// pictureLeastBytes is the least a picture is given of a budget, however
	// many pictures share it.
	pictureLeastBytes = 48 << 10
	// pictureDecodePixels bounds what is decoded to shrink, about a 48
	// megapixel photo, which takes some 200 MB to shrink: a larger picture
	// is sent as it is, which is what happened before.
	pictureDecodePixels = 50_000_000
	// pictureBudgetStepBytes is the step a picture's share is rounded down
	// to. A turn's words grow every round and its pictures' share shrinks a
	// little with them; shrunk to the exact share, a picture would come out
	// different each round, and everything after it would miss the
	// provider's cache.
	pictureBudgetStepBytes = 64 << 10
	// pictureShrinksKept is how many shrunk pictures are kept for the next
	// round of a turn, which sends the same pictures again, and sends them
	// the same while they still fit.
	pictureShrinksKept = 32
)

var (
	pictureShrinksMutex sync.Mutex
	pictureShrinks      = map[[sha256.Size]byte]ContentPart{}
)

// pictureBytesOf adds up the bytes of every picture in messages.
func pictureBytesOf(messages []ChatMessage) int {
	total := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == "image" {
				total += len(part.Data)
			}
		}
	}
	return total
}

// fitPictures returns messages with their pictures shrunk to add up to no
// more than budgetBytes, or messages itself when they already do. The
// smaller pictures keep their size and the larger ones share what is left
// equally. The messages handed in are not changed.
func fitPictures(messages []ChatMessage, budgetBytes int) []ChatMessage {
	var sizes []int
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == "image" {
				sizes = append(sizes, len(part.Data))
			}
		}
	}
	if len(sizes) == 0 {
		return messages
	}
	total := 0
	for _, size := range sizes {
		total += size
	}
	if total <= budgetBytes {
		return messages
	}

	// What each picture larger than its share is given: the share left once
	// the pictures smaller than it keep their own size.
	sort.Ints(sizes)
	shareBytes := 0
	remainingBytes := budgetBytes
	for index, size := range sizes {
		shareBytes = remainingBytes / (len(sizes) - index)
		if size > shareBytes {
			break
		}
		remainingBytes -= size
	}
	shareBytes = max(shareBytes, pictureLeastBytes)

	fitted := make([]ChatMessage, len(messages))
	for index, message := range messages {
		fitted[index] = message
		if pictureBytesOf(messages[index:index+1]) == 0 {
			continue
		}
		parts := make([]ContentPart, len(message.Parts))
		for partIndex, part := range message.Parts {
			parts[partIndex] = part
			if part.Type == "image" && len(part.Data) > shareBytes {
				if shrunk, err := shrinkPicture(part, shareBytes); err == nil {
					parts[partIndex] = shrunk
				}
			}
		}
		fitted[index].Parts = parts
	}
	return fitted
}

// shrinkPicture returns a JPEG copy of picture of no more than
// maximumBytes, upright as a phone meant it; the smallest copy it could
// make when none is that small; or an error when the picture cannot be
// read or made any smaller.
func shrinkPicture(picture ContentPart, maximumBytes int) (ContentPart, error) {
	key := sha256.Sum256(picture.Data)
	pictureShrinksMutex.Lock()
	shrunk, isKept := pictureShrinks[key]
	pictureShrinksMutex.Unlock()
	if isKept && len(shrunk.Data) <= maximumBytes {
		return shrunk, nil
	}
	if maximumBytes > pictureBudgetStepBytes {
		maximumBytes -= maximumBytes % pictureBudgetStepBytes
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(picture.Data))
	if err != nil {
		return picture, fmt.Errorf("llm: a picture to shrink could not be read: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > pictureDecodePixels {
		return picture, fmt.Errorf("llm: a picture of %d by %d is not shrunk", config.Width, config.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(picture.Data))
	if err != nil {
		return picture, fmt.Errorf("llm: a picture to shrink could not be read: %w", err)
	}
	orientation := jpegOrientation(picture.Data)
	// On white: a JPEG keeps no transparency, and what was clear would
	// otherwise come out black, under text that is often black too.
	source := image.NewRGBA(image.Rect(0, 0, decoded.Bounds().Dx(), decoded.Bounds().Dy()))
	draw.Draw(source, source.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(source, source.Bounds(), decoded, decoded.Bounds().Min, draw.Over)

	longestSidePixels := min(max(config.Width, config.Height), pictureLongestSidePixels)
	quality := 85
	var encoded bytes.Buffer
	for attempt := 0; attempt < 6; attempt++ {
		scaled := scalePicture(source, longestSidePixels)
		encoded.Reset()
		if err := jpeg.Encode(&encoded, orientPicture(scaled, orientation), &jpeg.Options{Quality: quality}); err != nil {
			return picture, fmt.Errorf("llm: a shrunk picture could not be written: %w", err)
		}
		if encoded.Len() <= maximumBytes || longestSidePixels <= pictureShortestSidePixels {
			break
		}
		// Bytes go roughly with the area: the side by the square root of
		// how far over it is, and a little more so the next try fits.
		ratio := math.Sqrt(float64(maximumBytes)/float64(encoded.Len())) * 0.9
		longestSidePixels = max(int(float64(longestSidePixels)*ratio), pictureShortestSidePixels)
		quality = 75
	}
	// Over its share still, the smallest copy made is sent all the same:
	// nearer to fitting than the picture it came from.
	if encoded.Len() >= len(picture.Data) {
		return picture, fmt.Errorf("llm: a picture could not be made smaller than its %d bytes", len(picture.Data))
	}
	shrunk = ContentPart{Type: "image", MediaType: "image/jpeg", Data: bytes.Clone(encoded.Bytes())}
	pictureShrinksMutex.Lock()
	if len(pictureShrinks) >= pictureShrinksKept {
		clear(pictureShrinks)
	}
	pictureShrinks[key] = shrunk
	pictureShrinksMutex.Unlock()
	return shrunk, nil
}

// scalePicture returns source with its longest side no longer than
// longestSidePixels, each pixel the average of those it covers.
func scalePicture(source *image.RGBA, longestSidePixels int) *image.RGBA {
	sourceWidth, sourceHeight := source.Bounds().Dx(), source.Bounds().Dy()
	scale := float64(longestSidePixels) / float64(max(sourceWidth, sourceHeight))
	if scale >= 1 {
		return source
	}
	width := max(int(math.Round(float64(sourceWidth)*scale)), 1)
	height := max(int(math.Round(float64(sourceHeight)*scale)), 1)
	scaled := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		top, bottom := y*sourceHeight/height, max((y+1)*sourceHeight/height, y*sourceHeight/height+1)
		for x := 0; x < width; x++ {
			left, right := x*sourceWidth/width, max((x+1)*sourceWidth/width, x*sourceWidth/width+1)
			var red, green, blue, alpha, pixelCount int
			for sourceY := top; sourceY < bottom; sourceY++ {
				offset := sourceY*source.Stride + left*4
				for sourceX := left; sourceX < right; sourceX++ {
					red += int(source.Pix[offset])
					green += int(source.Pix[offset+1])
					blue += int(source.Pix[offset+2])
					alpha += int(source.Pix[offset+3])
					offset += 4
					pixelCount++
				}
			}
			offset := y*scaled.Stride + x*4
			scaled.Pix[offset] = uint8(red / pixelCount)
			scaled.Pix[offset+1] = uint8(green / pixelCount)
			scaled.Pix[offset+2] = uint8(blue / pixelCount)
			scaled.Pix[offset+3] = uint8(alpha / pixelCount)
		}
	}
	return scaled
}

// orientPicture turns picture the way an EXIF orientation of 1 to 8 says
// it was meant to be seen. A JPEG written again loses the tag that said
// so, and a phone's photo would then lie on its side.
func orientPicture(picture *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return picture
	}
	width, height := picture.Bounds().Dx(), picture.Bounds().Dy()
	isTransposed := orientation >= 5
	turnedWidth, turnedHeight := width, height
	if isTransposed {
		turnedWidth, turnedHeight = height, width
	}
	turned := image.NewRGBA(image.Rect(0, 0, turnedWidth, turnedHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var turnedX, turnedY int
			switch orientation {
			case 2:
				turnedX, turnedY = width-1-x, y
			case 3:
				turnedX, turnedY = width-1-x, height-1-y
			case 4:
				turnedX, turnedY = x, height-1-y
			case 5:
				turnedX, turnedY = y, x
			case 6:
				turnedX, turnedY = height-1-y, x
			case 7:
				turnedX, turnedY = height-1-y, width-1-x
			case 8:
				turnedX, turnedY = y, width-1-x
			}
			copy(turned.Pix[turnedY*turned.Stride+turnedX*4:][:4], picture.Pix[y*picture.Stride+x*4:][:4])
		}
	}
	return turned
}

// jpegOrientation reads the EXIF orientation of a JPEG: 1 to 8, or 0 when
// it has none or is not a JPEG.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	position := 2
	for position+4 <= len(data) {
		if data[position] != 0xFF {
			return 0
		}
		marker := data[position+1]
		segmentLength := int(binary.BigEndian.Uint16(data[position+2:]))
		// The pictures start at the start of scan; nothing after it says
		// how they lie.
		if marker == 0xDA || segmentLength < 2 || position+2+segmentLength > len(data) {
			return 0
		}
		segment := data[position+4 : position+2+segmentLength]
		if marker == 0xE1 && len(segment) > 14 && string(segment[:6]) == "Exif\x00\x00" {
			return exifOrientation(segment[6:])
		}
		position += 2 + segmentLength
	}
	return 0
}

// exifOrientation reads the orientation tag of the first directory of an
// EXIF block, which starts with its TIFF header.
func exifOrientation(tiff []byte) int {
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	directory := int(order.Uint32(tiff[4:]))
	if directory < 8 || directory+2 > len(tiff) {
		return 0
	}
	entryCount := int(order.Uint16(tiff[directory:]))
	for index := 0; index < entryCount; index++ {
		entry := directory + 2 + index*12
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			orientation := int(order.Uint16(tiff[entry+8:]))
			if orientation < 1 || orientation > 8 {
				return 0
			}
			return orientation
		}
	}
	return 0
}
