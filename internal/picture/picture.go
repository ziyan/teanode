// Package picture decodes, turns upright, shrinks and writes the pictures of a
// photo library, and reads what a picture or a video says about itself: when
// and where it was taken, and with what.
//
// Pictures are decoded in Go (JPEG, PNG and GIF). HEIC and HEIF go through
// libheif's heif-dec and video through ffmpeg and ffprobe, both child
// processes, so the server itself stays free of C code.
package picture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"runtime"
	"sync"

	// Registered for image.Decode; JPEG comes with the encoder above.
	_ "image/gif"
	_ "image/png"
)

// MaximumPixels is the largest picture Decode will decode. The check reads
// only the header, so a small file that claims a huge picture costs nothing.
const MaximumPixels = 100_000_000

// Decode decodes a JPEG, PNG or GIF and turns a JPEG the right way up by its
// EXIF orientation, because every picture written later loses the tag that
// said so and a phone's photo would lie on its side.
func Decode(content []byte) (image.Image, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("picture: not a JPEG, PNG or GIF: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaximumPixels {
		return nil, fmt.Errorf("picture: a picture of %d by %d pixels is too large", config.Width, config.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("picture: the %s could not be decoded: %w", format, err)
	}
	if format != "jpeg" {
		return decoded, nil
	}
	return orient(decoded, jpegOrientation(content)), nil
}

// orient turns source the way an EXIF orientation of 2 to 8 says it was meant
// to be seen. Anything else, including no orientation, returns source itself.
// The result of a turn is an *image.RGBA, copied once from whatever the
// decoder made, which is cheaper than asking a YCbCr picture for every pixel.
func orient(source image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	flat := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(flat, flat.Bounds(), source, bounds.Min, draw.Src)

	turnedWidth, turnedHeight := width, height
	if orientation >= 5 {
		turnedWidth, turnedHeight = height, width
	}
	turned := image.NewRGBA(image.Rect(0, 0, turnedWidth, turnedHeight))
	for row := 0; row < height; row++ {
		sourceRow := flat.Pix[row*flat.Stride : row*flat.Stride+width*4]
		for column := 0; column < width; column++ {
			var turnedColumn, turnedRow int
			switch orientation {
			case 2:
				turnedColumn, turnedRow = width-1-column, row
			case 3:
				turnedColumn, turnedRow = width-1-column, height-1-row
			case 4:
				turnedColumn, turnedRow = column, height-1-row
			case 5:
				turnedColumn, turnedRow = row, column
			case 6:
				turnedColumn, turnedRow = height-1-row, column
			case 7:
				turnedColumn, turnedRow = height-1-row, width-1-column
			case 8:
				turnedColumn, turnedRow = row, width-1-column
			}
			copy(turned.Pix[turnedRow*turned.Stride+turnedColumn*4:][:4], sourceRow[column*4:][:4])
		}
	}
	return turned
}

// Scale returns source with its longest side no longer than
// maximumLongSidePixels, each output pixel the mean of the source pixels it
// covers (the pixels at the edges of that area weighted by how much of them it
// covers). The standard library has no downscaler that averages, and picking
// every n-th pixel makes text and fine patterns shimmer. A picture that is
// already small enough comes back unchanged: nothing is ever enlarged.
func Scale(source image.Image, maximumLongSidePixels int) image.Image {
	bounds := source.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	longSidePixels := max(sourceWidth, sourceHeight)
	if maximumLongSidePixels < 1 || longSidePixels <= maximumLongSidePixels {
		return source
	}
	ratio := float64(maximumLongSidePixels) / float64(longSidePixels)
	outputWidth := max(int(math.Round(float64(sourceWidth)*ratio)), 1)
	outputHeight := max(int(math.Round(float64(sourceHeight)*ratio)), 1)

	output := image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	columnSpans := makeSpans(sourceWidth, outputWidth)
	rowSpans := makeSpans(sourceHeight, outputHeight)
	loadRow := rowLoader(source)

	// Rows are independent, so they are shared out in bands, one per core.
	workerCount := min(runtime.NumCPU(), outputHeight, 8)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		firstRow := outputHeight * worker / workerCount
		endRow := outputHeight * (worker + 1) / workerCount
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
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
				destination := output.Pix[outputRow*output.Stride : outputRow*output.Stride+outputWidth*4]
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
					base := outputColumn * 4
					destination[base] = roundByte(red)
					destination[base+1] = roundByte(green)
					destination[base+2] = roundByte(blue)
					destination[base+3] = roundByte(alpha)
				}
			}
		}()
	}
	waitGroup.Wait()
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
