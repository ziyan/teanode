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
