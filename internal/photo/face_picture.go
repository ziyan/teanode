package photo

import (
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/picture"
)

const (
	// facePictureMarginFraction is the room left around a face's box on each
	// side, as a fraction of the box: enough for the hair and the chin, which
	// the detector's box leaves out, without showing the neighbors.
	facePictureMarginFraction = 0.3

	// FacePictureSidePixels is the longest side of a face picture. A picture
	// of a face is shown small, as a round avatar or a tile in a grid.
	FacePictureSidePixels = 256

	// ContactPictureMarginFraction is the room left around a face for a
	// contact's picture: more than a face tile has, so that the hair and the
	// shoulders show, as they do in a portrait.
	ContactPictureMarginFraction = 0.4

	// ContactPictureSidePixels is the side of a contact's picture, the size a
	// person's own picture is kept at (avatar.SidePixels).
	ContactPictureSidePixels = avatar.SidePixels

	// facePictureJpegQuality is the quality a face picture is written at.
	facePictureJpegQuality = 85
)

// FacePicture is the picture of one face: a square cut from the photo's
// preview around the face's box, with a margin, no more than
// FacePictureSidePixels on a side, written as a JPEG. A face at the edge of
// the picture gets a square moved inwards instead of one that is cut short,
// and a square larger than the picture is the picture's shorter side.
func FacePicture(preview image.Image, face *models.PhotoFace) ([]byte, error) {
	return cutFacePicture(preview, face, facePictureMarginFraction, FacePictureSidePixels)
}

// ContactPicture is the picture of a contact cut from one of their faces: the
// same square as FacePicture, with the room around the face a portrait has, no
// more than ContactPictureSidePixels on a side. It is never made larger than
// the part of the preview it is cut from.
func ContactPicture(preview image.Image, face *models.PhotoFace) ([]byte, error) {
	return cutFacePicture(preview, face, ContactPictureMarginFraction, ContactPictureSidePixels)
}

func cutFacePicture(preview image.Image, face *models.PhotoFace, marginFraction float64, sidePixels int) ([]byte, error) {
	bounds := preview.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return nil, fmt.Errorf("the preview is empty")
	}
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	centerColumn := (face.BoxLeftFraction + face.BoxWidthFraction/2) * width
	centerRow := (face.BoxTopFraction + face.BoxHeightFraction/2) * height
	side := math.Max(face.BoxWidthFraction*width, face.BoxHeightFraction*height) * (1 + 2*marginFraction)
	side = math.Max(math.Min(side, math.Min(width, height)), 1)
	left := math.Min(math.Max(centerColumn-side/2, 0), width-side)
	top := math.Min(math.Max(centerRow-side/2, 0), height-side)
	square := image.Rect(
		bounds.Min.X+int(math.Round(left)), bounds.Min.Y+int(math.Round(top)),
		bounds.Min.X+int(math.Round(left+side)), bounds.Min.Y+int(math.Round(top+side)),
	).Intersect(bounds)
	if square.Empty() {
		return nil, fmt.Errorf("the face is outside the picture")
	}
	var cut image.Image
	if subImager, isCuttable := preview.(interface {
		SubImage(image.Rectangle) image.Image
	}); isCuttable {
		cut = subImager.SubImage(square)
	} else {
		copied := image.NewRGBA(image.Rect(0, 0, square.Dx(), square.Dy()))
		draw.Draw(copied, copied.Bounds(), preview, square.Min, draw.Src)
		cut = copied
	}
	return picture.EncodeJPEG(picture.Scale(cut, sidePixels), facePictureJpegQuality)
}
