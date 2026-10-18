// Package picture decodes, turns upright, shrinks and writes the pictures of a
// photo library, and reads what a picture or a video says about itself: when
// and where it was taken, and with what.
//
// Pictures are decoded in Go (JPEG, PNG, GIF and WebP). HEIC and HEIF go through
// libheif's heif-dec and video through ffmpeg and ffprobe, both child
// processes, so the server itself stays free of C code.
package picture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"runtime"
	"sync"

	// Registered for image.Decode; JPEG comes with the encoder above.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// MaximumPixels is the largest picture Decode will decode: 64 megapixels. The
// check reads only the header, so a small file that claims a huge picture
// costs nothing.
const MaximumPixels = 64_000_000

// MaximumSidePixels is the longest side Decode will decode. A JPEG cannot be
// longer than this, and a PNG or a GIF can claim billions of pixels along one
// side: a strip one pixel high and a billion wide is under MaximumPixels, and
// scaling it would still allocate a row of gigabytes.
const MaximumSidePixels = 65535

// ErrTooLarge is a picture with more than MaximumPixels pixels, or with a side
// longer than MaximumSidePixels.
var ErrTooLarge = errors.New("picture: the picture is too large")

// ReadSize reads the size of a JPEG, PNG, GIF or WebP from its header, and refuses
// one that is empty, has more than MaximumPixels pixels or has a side longer
// than MaximumSidePixels. It is what a caller asks before it decodes, to learn
// what the decoded picture will take.
func ReadSize(content []byte) (widthPixels, heightPixels int, err error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, 0, fmt.Errorf("picture: not a JPEG, PNG, GIF or WebP: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaximumSidePixels || config.Height > MaximumSidePixels ||
		int64(config.Width)*int64(config.Height) > MaximumPixels {
		return 0, 0, fmt.Errorf("%w: %d by %d pixels", ErrTooLarge, config.Width, config.Height)
	}
	return config.Width, config.Height, nil
}

// Decode decodes a JPEG, PNG, GIF or WebP. A JPEG that its EXIF orientation says is
// turned comes back as a view of the pixels as they are stored, seen the right
// way up, because every picture written later loses the tag that said so and a
// phone's photo would lie on its side. The view turns nothing: it costs no copy
// of the picture, and Scale reads through it while it samples. Anything else
// that asks a pixel of it is told the upright one.
func Decode(content []byte) (image.Image, error) {
	if _, _, err := ReadSize(content); err != nil {
		return nil, err
	}
	decoded, format, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("picture: the decoding failed: %w", err)
	}
	if format != "jpeg" {
		return decoded, nil
	}
	if orientation := jpegOrientation(content); orientation >= 2 && orientation <= 8 {
		return &uprightView{stored: decoded, orientation: orientation}, nil
	}
	return decoded, nil
}

// uprightView is a picture seen the way an EXIF orientation of 2 to 8 says it
// was meant to be seen, without a copy of its pixels.
type uprightView struct {
	stored      image.Image
	orientation int
}

func (self *uprightView) ColorModel() color.Model { return self.stored.ColorModel() }

func (self *uprightView) Bounds() image.Rectangle {
	bounds := self.stored.Bounds()
	if self.orientation >= 5 {
		return image.Rect(0, 0, bounds.Dy(), bounds.Dx())
	}
	return image.Rect(0, 0, bounds.Dx(), bounds.Dy())
}

func (self *uprightView) At(column, row int) color.Color {
	bounds := self.stored.Bounds()
	storedColumn, storedRow := uprightToStored(self.orientation, column, row, bounds.Dx(), bounds.Dy())
	return self.stored.At(bounds.Min.X+storedColumn, bounds.Min.Y+storedRow)
}

// uprightToStored is the pixel of a stored picture of the given size that is
// at (column, row) once the orientation has turned it.
func uprightToStored(orientation, column, row, storedWidth, storedHeight int) (int, int) {
	switch orientation {
	case 2:
		return storedWidth - 1 - column, row
	case 3:
		return storedWidth - 1 - column, storedHeight - 1 - row
	case 4:
		return column, storedHeight - 1 - row
	case 5:
		return row, column
	case 6:
		return row, storedHeight - 1 - column
	case 7:
		return storedWidth - 1 - row, storedHeight - 1 - column
	case 8:
		return storedWidth - 1 - row, column
	}
	return column, row
}

// storedToUpright is the place in the turned picture of a stored pixel, for a
// stored picture of the given size.
func storedToUpright(orientation, column, row, storedWidth, storedHeight int) (int, int) {
	switch orientation {
	case 2:
		return storedWidth - 1 - column, row
	case 3:
		return storedWidth - 1 - column, storedHeight - 1 - row
	case 4:
		return column, storedHeight - 1 - row
	case 5:
		return row, column
	case 6:
		return storedHeight - 1 - row, column
	case 7:
		return storedHeight - 1 - row, storedWidth - 1 - column
	case 8:
		return row, storedWidth - 1 - column
	}
	return column, row
}

// Scale returns source with its longest side no longer than
// maximumLongSidePixels, each output pixel the mean of the source pixels it
// covers (the pixels at the edges of that area weighted by how much of them it
// covers). The standard library has no downscaler that averages, and picking
// every n-th pixel makes text and fine patterns shimmer. A picture that is
// already small enough comes back unchanged: nothing is ever enlarged.
//
// A picture from Decode that is turned by its EXIF orientation is turned while
// it is sampled: each output pixel is written where the turn puts it, so the
// full-size picture is never copied, flipped or transposed. Such a picture is
// always scaled, to the size it has if it is small enough, because the result
// has to be a picture with the turn applied.
func Scale(source image.Image, maximumLongSidePixels int) image.Image {
	orientation := 0
	view, isTurned := source.(*uprightView)
	if isTurned {
		source, orientation = view.stored, view.orientation
	}
	bounds := source.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	longSidePixels := max(sourceWidth, sourceHeight)
	if maximumLongSidePixels < 1 || (longSidePixels <= maximumLongSidePixels && !isTurned) {
		if isTurned {
			return view
		}
		return source
	}
	ratio := math.Min(float64(maximumLongSidePixels)/float64(longSidePixels), 1)
	outputWidth := max(int(math.Round(float64(sourceWidth)*ratio)), 1)
	outputHeight := max(int(math.Round(float64(sourceHeight)*ratio)), 1)

	// The picture is made in the stored orientation's shape and each pixel is
	// put where the turn sends it; a turn on its side swaps the two sides.
	turnedWidth, turnedHeight := outputWidth, outputHeight
	if orientation >= 5 {
		turnedWidth, turnedHeight = outputHeight, outputWidth
	}
	output := image.NewRGBA(image.Rect(0, 0, turnedWidth, turnedHeight))
	columnSpans := makeSpans(sourceWidth, outputWidth)
	rowSpans := makeSpans(sourceHeight, outputHeight)
	loadRow := rowLoader(source)

	// Rows are independent, so they are shared out in bands, one per core.
	workerCount := min(runtime.NumCPU(), outputHeight, 8)
	var waitGroup sync.WaitGroup
	var panicMutex sync.Mutex
	var panicked any
	for worker := 0; worker < workerCount; worker++ {
		firstRow := outputHeight * worker / workerCount
		endRow := outputHeight * (worker + 1) / workerCount
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			// A panic here would take the whole process down, since nothing
			// above this goroutine can catch it. It is handed to the caller,
			// which can.
			defer func() {
				if recovered := recover(); recovered != nil {
					panicMutex.Lock()
					panicked = recovered
					panicMutex.Unlock()
				}
			}()
			// Each output row is first the weighted sum of the source rows it
			// covers, then that row is shrunk across.
			sourceRowValues := make([]float32, sourceWidth*4)
			accumulated := make([]float32, sourceWidth*4)
			for outputRow := firstRow; outputRow < endRow; outputRow++ {
				clear(accumulated)
				span := rowSpans[outputRow]
				for offset, weight := range span.weights {
					loadRow(span.first+offset, sourceRowValues)
					for index, value := range sourceRowValues {
						accumulated[index] += value * weight
					}
				}
				for outputColumn := 0; outputColumn < outputWidth; outputColumn++ {
					columnSpan := columnSpans[outputColumn]
					var red, green, blue, alpha float32
					for offset, weight := range columnSpan.weights {
						base := (columnSpan.first + offset) * 4
						red += accumulated[base] * weight
						green += accumulated[base+1] * weight
						blue += accumulated[base+2] * weight
						alpha += accumulated[base+3] * weight
					}
					turnedColumn, turnedRow := storedToUpright(orientation, outputColumn, outputRow, outputWidth, outputHeight)
					destination := output.Pix[turnedRow*output.Stride+turnedColumn*4:][:4]
					destination[0] = roundByte(red)
					destination[1] = roundByte(green)
					destination[2] = roundByte(blue)
					destination[3] = roundByte(alpha)
				}
			}
		}()
	}
	waitGroup.Wait()
	if panicked != nil {
		panic(panicked)
	}
	return output
}

// span is the source pixels one output pixel covers along one axis: from first,
// one weight each, the weights adding up to 1.
type span struct {
	first   int
	weights []float32
}

// makeSpans works out, for each of outputLength pixels, which of the
// sourceLength pixels it covers. Output pixel n covers the stretch from
// n*ratio to (n+1)*ratio of the source, and a source pixel counts for as much
// of that stretch as it lies inside it.
func makeSpans(sourceLength, outputLength int) []span {
	ratio := float64(sourceLength) / float64(outputLength)
	spans := make([]span, outputLength)
	for index := range spans {
		start := float64(index) * ratio
		end := math.Min(float64(index+1)*ratio, float64(sourceLength))
		first := int(math.Floor(start))
		last := min(int(math.Ceil(end))-1, sourceLength-1)
		last = max(last, first)
		weights := make([]float32, last-first+1)
		var total float64
		for offset := range weights {
			covered := math.Min(float64(first+offset+1), end) - math.Max(float64(first+offset), start)
			covered = math.Max(covered, 0)
			weights[offset] = float32(covered)
			total += covered
		}
		if total <= 0 {
			weights[0], total = 1, 1
		}
		for offset := range weights {
			weights[offset] = float32(float64(weights[offset]) / total)
		}
		spans[index] = span{first: first, weights: weights}
	}
	return spans
}

func roundByte(value float32) uint8 {
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return uint8(value + 0.5)
}

// rowLoader returns a function that writes one row of source into destination
// as premultiplied red, green, blue and alpha, each 0 to 255. The types a
// decoder makes have their own fast paths; anything else goes through At.
func rowLoader(source image.Image) func(row int, destination []float32) {
	bounds := source.Bounds()
	width := bounds.Dx()
	switch typed := source.(type) {
	case *image.RGBA:
		return func(row int, destination []float32) {
			pixels := typed.Pix[typed.PixOffset(bounds.Min.X, bounds.Min.Y+row):][:width*4]
			for index, value := range pixels {
				destination[index] = float32(value)
			}
		}
	case *image.NRGBA:
		return func(row int, destination []float32) {
			pixels := typed.Pix[typed.PixOffset(bounds.Min.X, bounds.Min.Y+row):][:width*4]
			for column := 0; column < width; column++ {
				alpha := float32(pixels[column*4+3])
				scale := alpha / 255
				destination[column*4] = float32(pixels[column*4]) * scale
				destination[column*4+1] = float32(pixels[column*4+1]) * scale
				destination[column*4+2] = float32(pixels[column*4+2]) * scale
				destination[column*4+3] = alpha
			}
		}
	case *image.Gray:
		return func(row int, destination []float32) {
			pixels := typed.Pix[typed.PixOffset(bounds.Min.X, bounds.Min.Y+row):][:width]
			for column, value := range pixels {
				destination[column*4] = float32(value)
				destination[column*4+1] = float32(value)
				destination[column*4+2] = float32(value)
				destination[column*4+3] = 255
			}
		}
	case *image.YCbCr:
		return func(row int, destination []float32) {
			for column := 0; column < width; column++ {
				pixelColumn, pixelRow := bounds.Min.X+column, bounds.Min.Y+row
				luma := int32(typed.Y[typed.YOffset(pixelColumn, pixelRow)]) * 0x10101
				chromaOffset := typed.COffset(pixelColumn, pixelRow)
				blueDifference := int32(typed.Cb[chromaOffset]) - 128
				redDifference := int32(typed.Cr[chromaOffset]) - 128
				// The same fixed point arithmetic as color.YCbCrToRGB.
				redValue := (luma + 91881*redDifference) >> 16
				greenValue := (luma - 22554*blueDifference - 46802*redDifference) >> 16
				blueValue := (luma + 116130*blueDifference) >> 16
				destination[column*4] = clampChannel(redValue)
				destination[column*4+1] = clampChannel(greenValue)
				destination[column*4+2] = clampChannel(blueValue)
				destination[column*4+3] = 255
			}
		}
	}
	return func(row int, destination []float32) {
		for column := 0; column < width; column++ {
			red, green, blue, alpha := source.At(bounds.Min.X+column, bounds.Min.Y+row).RGBA()
			destination[column*4] = float32(red >> 8)
			destination[column*4+1] = float32(green >> 8)
			destination[column*4+2] = float32(blue >> 8)
			destination[column*4+3] = float32(alpha >> 8)
		}
	}
}

func clampChannel(value int32) float32 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return float32(value)
}

// EncodeJPEG writes picture as a JPEG. A JPEG has no transparency, and what
// was clear would come out black, under text that is often black too, so a
// picture that is not opaque is laid on white first.
func EncodeJPEG(picture image.Image, quality int) ([]byte, error) {
	if quality < 1 || quality > 100 {
		return nil, fmt.Errorf("picture: JPEG quality %d is not between 1 and 100", quality)
	}
	if bounds := picture.Bounds(); bounds.Dx() < 1 || bounds.Dy() < 1 {
		return nil, errors.New("picture: an empty picture cannot be written")
	}
	if !isOpaque(picture) {
		flat := image.NewRGBA(picture.Bounds())
		draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(flat, flat.Bounds(), picture, picture.Bounds().Min, draw.Over)
		picture = flat
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("picture: the JPEG could not be written: %w", err)
	}
	return encoded.Bytes(), nil
}

// isOpaque reports whether every pixel of picture is opaque, where the type
// can say so cheaply and for the rest by looking.
func isOpaque(picture image.Image) bool {
	if opaque, isKnown := picture.(interface{ Opaque() bool }); isKnown {
		return opaque.Opaque()
	}
	bounds := picture.Bounds()
	for row := bounds.Min.Y; row < bounds.Max.Y; row++ {
		for column := bounds.Min.X; column < bounds.Max.X; column++ {
			if _, _, _, alpha := picture.At(column, row).RGBA(); alpha != 0xffff {
				return false
			}
		}
	}
	return true
}

// EncodePPM writes picture as a binary PPM (P6, maximum value 255): a short
// text header, then red, green and blue bytes for every pixel. The face
// reader takes this instead of an encoded picture so that it needs none of
// OpenCV's image decoders. Transparency is laid on white, as EncodeJPEG does.
func EncodePPM(picture image.Image) []byte {
	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	header := fmt.Sprintf("P6\n%d %d\n255\n", width, height)
	encoded := make([]byte, len(header), len(header)+width*height*3)
	copy(encoded, header)
	loadRow := rowLoader(picture)
	values := make([]float32, width*4)
	for row := 0; row < height; row++ {
		loadRow(row, values)
		for column := 0; column < width; column++ {
			// Premultiplied, so white behind it is whatever the pixel lets through.
			uncovered := 255 - values[column*4+3]
			encoded = append(encoded,
				roundByte(values[column*4]+uncovered),
				roundByte(values[column*4+1]+uncovered),
				roundByte(values[column*4+2]+uncovered))
		}
	}
	return encoded
}
