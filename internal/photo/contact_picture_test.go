package photo_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// A contact's picture is a square around the face with room for the hair and
// the shoulders, as large as a person's own picture and no larger than what it
// was cut from.
func TestAContactPictureIsASquareAroundTheFaceWithRoomForTheShoulders(test *testing.T) {
	test.Parallel()
	// A large grey preview with a red face at its middle and a blue band
	// past the room a portrait leaves.
	preview := image.NewRGBA(image.Rect(0, 0, 2000, 1500))
	for row := range 1500 {
		for column := range 2000 {
			shade := color.RGBA{R: 120, G: 120, B: 120, A: 255}
			switch {
			case column >= 900 && column < 1100 && row >= 600 && row < 900:
				shade = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			case column >= 1500:
				shade = color.RGBA{R: 30, G: 30, B: 220, A: 255}
			}
			preview.SetRGBA(column, row, shade)
		}
	}
	// The box is 200 by 300 pixels, so the square is 300 * 1.8 = 540 pixels
	// before it is shrunk to the side a person's own picture has.
	face := &models.PhotoFace{BoxLeftFraction: 0.45, BoxTopFraction: 0.4, BoxWidthFraction: 0.1, BoxHeightFraction: 0.2}
	encoded, err := photo.ContactPicture(preview, face)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		test.Fatal(err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != photo.ContactPictureSidePixels || bounds.Dy() != photo.ContactPictureSidePixels {
		test.Fatalf("%v", bounds)
	}
	redAt := func(column, row int) uint32 { red, _, _, _ := decoded.At(column, row).RGBA(); return red >> 8 }
	if redAt(256, 256) < 190 {
		test.Fatalf("the center is not the face: %d", redAt(256, 256))
	}
	if redAt(10, 10) < 100 || redAt(10, 10) > 140 {
		test.Fatalf("the corner is the room around the face, grey: %d", redAt(10, 10))
	}
	// 40 percent of room on each side of the longer side of the box: the face
	// is 300 of 540 pixels high, so it starts at the 114th row of the 512
	// pixel picture.
	if redAt(256, 120) < 190 || redAt(256, 108) > 150 {
		test.Fatalf("the face does not fill 55 percent of the picture: %d %d", redAt(256, 120), redAt(256, 108))
	}
}

// A face at the edge of a photo has a square moved inwards, wholly inside the
// photo, with the face in it, not a strip cut short.
func TestAContactPictureOfAFaceAtTheEdgeStaysInsideThePhoto(test *testing.T) {
	test.Parallel()
	preview := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	for row := range 800 {
		for column := range 1000 {
			shade := color.RGBA{R: 120, G: 120, B: 120, A: 255}
			if column < 150 && row < 200 {
				shade = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			}
			preview.SetRGBA(column, row, shade)
		}
	}
	// A face in the top left corner of the photo.
	face := &models.PhotoFace{BoxLeftFraction: 0, BoxTopFraction: 0, BoxWidthFraction: 0.15, BoxHeightFraction: 0.25}
	encoded, err := photo.ContactPicture(preview, face)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		test.Fatal(err)
	}
	// The square is 200 * 1.8 = 360 pixels: 360 of the photo's pixels, scaled
	// up by nothing, all of them inside the photo.
	if bounds := decoded.Bounds(); bounds.Dx() != 360 || bounds.Dy() != 360 {
		test.Fatalf("a picture is not made larger than what it was cut from: %v", bounds)
	}
	redAt := func(column, row int) uint32 { red, _, _, _ := decoded.At(column, row).RGBA(); return red >> 8 }
	if redAt(10, 10) < 190 || redAt(140, 190) < 190 {
		test.Fatalf("the face is in the corner of the picture: %d %d", redAt(10, 10), redAt(140, 190))
	}
	if redAt(250, 300) < 100 || redAt(250, 300) > 140 {
		test.Fatalf("past the face is the room around it: %d", redAt(250, 300))
	}

	// A face at the far corner is moved inwards from there as well, and a
	// face larger than the photo is the photo's shorter side.
	far := &models.PhotoFace{BoxLeftFraction: 0.85, BoxTopFraction: 0.75, BoxWidthFraction: 0.15, BoxHeightFraction: 0.25}
	if encoded, err = photo.ContactPicture(preview, far); err != nil {
		test.Fatal(err)
	}
	if decoded, err = jpeg.Decode(bytes.NewReader(encoded)); err != nil || decoded.Bounds().Dx() != 360 || decoded.Bounds().Dy() != 360 {
		test.Fatalf("%v %v", decoded.Bounds(), err)
	}
	huge := &models.PhotoFace{BoxLeftFraction: 0, BoxTopFraction: 0, BoxWidthFraction: 1, BoxHeightFraction: 1}
	if encoded, err = photo.ContactPicture(preview, huge); err != nil {
		test.Fatal(err)
	}
	if decoded, err = jpeg.Decode(bytes.NewReader(encoded)); err != nil || decoded.Bounds().Dx() != 512 || decoded.Bounds().Dy() != 512 {
		// 800 pixels, scaled down to 512.
		test.Fatalf("%v %v", decoded.Bounds(), err)
	}
}
