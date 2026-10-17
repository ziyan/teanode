// Package avatar turns whatever picture a person uploads into the one picture
// the server keeps: a square PNG, with nothing in it but pixels.
//
// Normalizing on the way in is what makes the rest simple. A phone writes the
// place a photo was taken into the file, and the dashboard shows other
// people's pictures to them; re-encoding from the decoded pixels is the one
// step that is certain to leave every metadata block behind, where an
// allow-list of blocks to strip would be a list somebody forgets to extend.
package avatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
)

const (
	// Size is the side of the stored picture, in pixels.
	Size = 512

	// MaxInputBytes is the largest upload accepted, the same bound an
	// attachment upload has.
	MaxInputBytes = 8 << 20

	// MaxPixels is the largest picture decoded, 20 megapixels. A small file
	// can declare a huge image, and decoding it would cost memory the file's
	// size does not hint at, so the declared size is checked before a pixel
	// is read.
	MaxPixels = 20_000_000

	// ColorCount is how many colors a person without a picture can have, the
	// avatar colors of the dashboard's stylesheet.
	ColorCount = 12
)

// ErrUnusable is every reason a picture is refused. One message for all of
// them: a person who sent a WebP, a 40 megapixel photograph or a file that is
// not a picture needs the same instruction.
var ErrUnusable = errors.New("the picture must be a PNG or JPEG of at most 20 megapixels")

// Normalize decodes a PNG or JPEG, crops the largest centered square, scales
// it to Size by Size and encodes it as a PNG. A JPEG's orientation tag is
// applied first, because the tag is dropped with the rest of the metadata and
// a sideways portrait is worse than a location left behind.
func Normalize(content []byte) ([]byte, error) {
	if len(content) == 0 || len(content) > MaxInputBytes {
		return nil, ErrUnusable
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, ErrUnusable
	}
	if format != "png" && format != "jpeg" {
		return nil, ErrUnusable
	}
	if configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width)*int64(configuration.Height) > MaxPixels {
		return nil, ErrUnusable
	}

	var decoded image.Image
	if format == "png" {
		decoded, err = png.Decode(bytes.NewReader(content))
	} else {
		decoded, err = jpeg.Decode(bytes.NewReader(content))
	}
	if err != nil {
		return nil, ErrUnusable
	}

	bounds := decoded.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	left := bounds.Min.X + (bounds.Dx()-side)/2
	top := bounds.Min.Y + (bounds.Dy()-side)/2
	square := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(square, square.Bounds(), decoded, image.Pt(left, top), draw.Src)

	scaled := scale(square, Size)
	if format == "jpeg" {
		scaled = orient(scaled, jpegOrientation(content))
	}
	return encode(scaled)
}

// Resize scales a stored picture to a smaller square, for the places that
// draw a person at the size of a list row. A request for the stored size or
// more returns the picture as it is.
func Resize(content []byte, size int) ([]byte, error) {
	if size <= 0 {
		return nil, ErrUnusable
	}
	decoded, err := png.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != bounds.Dy() || size >= bounds.Dx() {
		return content, nil
	}
	square := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(square, square.Bounds(), decoded, bounds.Min, draw.Src)
	return encode(scale(square, size))
}

// InitialColorIndex says which of the ColorCount colors a person without a
// picture is drawn on: the 32-bit FNV-1a hash of their id, modulo the number
// of colors. The dashboard and the iPhone app compute the same thing from the
// same id, so a person is the same color on every screen; the hash is spelled
// out here, with its constants, because that agreement is the whole point.
func InitialColorIndex(userId string) int {
	hash := uint32(0x811c9dc5)
	for index := 0; index < len(userId); index++ {
		hash ^= uint32(userId[index])
		hash *= 0x01000193
	}
	return int(hash % ColorCount)
}

func encode(picture *image.RGBA) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buffer, picture); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// contribution is how much one source pixel counts toward one target pixel.
type contribution struct {
	first   int
	weights []float32
}

// contributions works out, for each target pixel along one axis, which source
// pixels it is made of. Shrinking averages every source pixel the target
// covers by the area it covers, which is what keeps a 4000 pixel photograph
// from turning to noise at 512; enlarging interpolates between the two
// nearest, because there is nothing to average.
func contributions(sourceSize, targetSize int) []contribution {
	result := make([]contribution, targetSize)
	ratio := float64(sourceSize) / float64(targetSize)
	for target := range result {
		if ratio >= 1 {
			start, end := float64(target)*ratio, float64(target+1)*ratio
			first, last := int(math.Floor(start)), int(math.Ceil(end))-1
			last = min(last, sourceSize-1)
			weights := make([]float32, 0, last-first+1)
			var total float32
			for source := first; source <= last; source++ {
				weight := float32(math.Min(end, float64(source+1)) - math.Max(start, float64(source)))
				weights = append(weights, weight)
				total += weight
			}
			for index := range weights {
				weights[index] /= total
			}
			result[target] = contribution{first: first, weights: weights}
			continue
		}
		center := (float64(target)+0.5)*ratio - 0.5
		first := int(math.Floor(center))
		fraction := float32(center - float64(first))
		lowIndex, highIndex := max(first, 0), min(first+1, sourceSize-1)
		if lowIndex == highIndex {
			result[target] = contribution{first: lowIndex, weights: []float32{1}}
			continue
		}
		result[target] = contribution{first: lowIndex, weights: []float32{1 - fraction, fraction}}
	}
	return result
}

// scale resamples a square to another square, one axis and then the other,
// on premultiplied color so that a transparent edge does not bleed its
// invisible color into its neighbors.
func scale(square *image.RGBA, size int) *image.RGBA {
	side := square.Bounds().Dx()
	// Both axes of a square are the same, so one list serves for each.
	axis := contributions(side, size)

	// Horizontal pass: side rows of size pixels, four channels each.
	across := make([]float32, side*size*4)
	for row := 0; row < side; row++ {
		line := square.Pix[row*square.Stride:]
		for column, parts := range axis {
			var red, green, blue, alpha float32
			for offset, weight := range parts.weights {
				base := (parts.first + offset) * 4
				red += weight * float32(line[base])
				green += weight * float32(line[base+1])
				blue += weight * float32(line[base+2])
				alpha += weight * float32(line[base+3])
			}
			target := (row*size + column) * 4
			across[target], across[target+1], across[target+2], across[target+3] = red, green, blue, alpha
		}
	}

	scaled := image.NewRGBA(image.Rect(0, 0, size, size))
	for row, parts := range axis {
		for column := 0; column < size; column++ {
			var red, green, blue, alpha float32
			for offset, weight := range parts.weights {
				base := ((parts.first+offset)*size + column) * 4
				red += weight * across[base]
				green += weight * across[base+1]
				blue += weight * across[base+2]
				alpha += weight * across[base+3]
			}
			target := row*scaled.Stride + column*4
			scaled.Pix[target] = roundChannel(red)
			scaled.Pix[target+1] = roundChannel(green)
			scaled.Pix[target+2] = roundChannel(blue)
			scaled.Pix[target+3] = roundChannel(alpha)
		}
	}
	return scaled
}

func roundChannel(channel float32) uint8 {
	switch {
	case channel <= 0:
		return 0
	case channel >= 255:
		return 255
	}
	return uint8(channel + 0.5)
}

// orient turns a square the way its orientation tag says is upright. The tag
// numbers are the EXIF ones: 2 and 4 mirror, 3 is upside down, 5 and 7
// transpose, 6 and 8 are quarter turns.
func orient(picture *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return picture
	}
	side := picture.Bounds().Dx()
	last := side - 1
	oriented := image.NewRGBA(picture.Bounds())
	for row := 0; row < side; row++ {
		for column := 0; column < side; column++ {
			var sourceColumn, sourceRow int
			switch orientation {
			case 2:
				sourceColumn, sourceRow = last-column, row
			case 3:
				sourceColumn, sourceRow = last-column, last-row
			case 4:
				sourceColumn, sourceRow = column, last-row
			case 5:
				sourceColumn, sourceRow = row, column
			case 6:
				sourceColumn, sourceRow = row, last-column
			case 7:
				sourceColumn, sourceRow = last-row, last-column
			case 8:
				sourceColumn, sourceRow = last-row, column
			}
			copy(oriented.Pix[row*oriented.Stride+column*4:row*oriented.Stride+column*4+4],
				picture.Pix[sourceRow*picture.Stride+sourceColumn*4:])
		}
	}
	return oriented
}

// jpegOrientation reads the orientation tag from a JPEG's EXIF block, 1 when
// there is none or it cannot be read. Only this one tag is looked for; the
// block is otherwise never interpreted.
func jpegOrientation(content []byte) int {
	position := 2
	for position+4 <= len(content) && content[position] == 0xFF {
		marker := content[position+1]
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		length := int(binary.BigEndian.Uint16(content[position+2:]))
		segmentEnd := position + 2 + length
		if length < 2 || segmentEnd > len(content) {
			return 1
		}
		segment := content[position+4 : segmentEnd]
		if marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
		position = segmentEnd
	}
	return 1
}

func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	directory := int(order.Uint32(tiff[4:]))
	if directory < 8 || directory+2 > len(tiff) {
		return 1
	}
	entryCount := int(order.Uint16(tiff[directory:]))
	for index := 0; index < entryCount; index++ {
		entry := directory + 2 + index*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			orientation := int(order.Uint16(tiff[entry+8:]))
			if orientation < 1 || orientation > 8 {
				return 1
			}
			return orientation
		}
	}
	return 1
}
