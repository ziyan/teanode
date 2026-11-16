package models_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// Each phone app names itself as a photo's source when it backs the photo up:
// a value the server does not know is refused with the whole upload.
func TestPhonePhotoSourcesAreKnown(test *testing.T) {
	for _, source := range []models.PhotoSource{models.PhotoSourceIPhone, models.PhotoSourceAndroid} {
		if !source.IsValid() {
			test.Errorf("%q is not a photo source the server knows", source)
		}
	}
	if models.PhotoSource("somewhere").IsValid() {
		test.Error("an unknown source is taken")
	}
}
