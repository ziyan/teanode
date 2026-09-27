package browser

import (
	"context"
	"strings"
	"testing"
)

// A screenshot is handed to the model as a picture, not as its bytes in
// text; asked to be shown with no conversation to keep it in, it says it
// was not given to anybody rather than failing.
func TestAScreenshotIsAPicture(t *testing.T) {
	picture := []byte("\\x89PNG a picture")
	result, err := screenshotResult(context.Background(), picture, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Images) != 1 || result.Images[0].MediaType != "image/png" || string(result.Images[0].Data) != string(picture) {
		t.Errorf("the model was shown %+v", result.Images)
	}
	if strings.Contains(result.Content, "base64") || !strings.Contains(result.Content, `"given_to_the_person":false`) {
		t.Errorf("the answer read %s", result.Content)
	}
}

// The extension's screenshot, a data address in a JSON string, is read as
// the picture it is; anything else is not a picture.
func TestATabScreenshotIsReadAsAPicture(t *testing.T) {
	image, ok := pictureOf([]byte(`"data:image/png;base64,YSBwaWN0dXJl"`))
	if !ok || string(image) != "a picture" {
		t.Errorf("read %q, %v", image, ok)
	}
	if _, ok := pictureOf([]byte(`{"error":"no"}`)); ok {
		t.Error("an object was read as a picture")
	}
}
