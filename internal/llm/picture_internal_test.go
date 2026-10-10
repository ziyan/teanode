package llm

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"strings"
	"testing"
)

// noisyPhoto is a JPEG of random pixels: as hard to compress as a picture
// gets, so a few hundred pixels a side already weigh what a photo does.
func noisyPhoto(test *testing.T, width, height int) []byte {
	test.Helper()
	random := rand.New(rand.NewPCG(7, 11))
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for index := range picture.Pix {
		picture.Pix[index] = uint8(random.IntN(256))
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 95}); err != nil {
		test.Fatalf("encode: %s", err)
	}
	return encoded.Bytes()
}

// withOrientation puts an EXIF block saying orientation right after the
// start of a JPEG, the way a phone writes it.
func withOrientation(photo []byte, orientation uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM")
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))
	_ = binary.Write(&tiff, binary.BigEndian, []uint16{0x0112, 3})
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, []uint16{orientation, 0})
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))
	segment := append([]byte("Exif\x00\x00"), tiff.Bytes()...)

	var marked bytes.Buffer
	marked.Write(photo[:2])
	marked.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&marked, binary.BigEndian, uint16(len(segment)+2))
	marked.Write(segment)
	marked.Write(photo[2:])
	return marked.Bytes()
}

func TestPicturesThatFitAreSentAsTheyAre(test *testing.T) {
	test.Parallel()

	messages := []ChatMessage{{Role: RoleUser, Parts: []ContentPart{
		{Type: "text", Text: "What does this say?"},
		{Type: "image", MediaType: "image/png", Data: []byte("a small picture")},
	}}}
	fitted := fitPictures(messages, 1<<20)
	if &fitted[0] != &messages[0] {
		test.Errorf("pictures within the budget were copied")
	}
}

func TestPicturesOverTheBudgetAreShrunkToFitIt(test *testing.T) {
	test.Parallel()

	large := noisyPhoto(test, 900, 700)
	small := []byte("a picture already small")
	messages := []ChatMessage{
		{Role: RoleUser, Parts: []ContentPart{{Type: "image", MediaType: "image/jpeg", Data: large}}},
		{Role: RoleUser, Parts: []ContentPart{
			{Type: "text", Text: "And this one"},
			{Type: "image", MediaType: "image/png", Data: small},
			{Type: "image", MediaType: "image/jpeg", Data: large},
		}},
	}
	budgetBytes := len(large) / 2
	fitted := fitPictures(messages, budgetBytes)

	if total := pictureBytesOf(fitted); total > budgetBytes {
		test.Errorf("the pictures add up to %d bytes, more than the %d budgeted", total, budgetBytes)
	}
	if !bytes.Equal(fitted[1].Parts[1].Data, small) {
		test.Errorf("a picture already within its share was changed")
	}
	if fitted[1].Parts[0].Text != "And this one" {
		test.Errorf("the words beside the pictures were lost")
	}
	shrunk := fitted[0].Parts[0]
	if shrunk.MediaType != "image/jpeg" {
		test.Errorf("a shrunk picture is %s", shrunk.MediaType)
	}
	if _, _, err := image.Decode(bytes.NewReader(shrunk.Data)); err != nil {
		test.Errorf("a shrunk picture cannot be read: %s", err)
	}
	if !bytes.Equal(messages[0].Parts[0].Data, large) {
		test.Errorf("the messages handed in were changed")
	}
}

func TestAPhoneTurnedPhotoIsShrunkUpright(test *testing.T) {
	test.Parallel()

	// Stored 300 wide and 200 high, the left half dark, with orientation 6:
	// to be seen turned a quarter clockwise, 200 wide and 300 high, dark on
	// top.
	picture := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			shade := uint8(240)
			if x < 150 {
				shade = 15
			}
			picture.Set(x, y, color.RGBA{shade, shade, shade, 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 90}); err != nil {
		test.Fatalf("encode: %s", err)
	}
	photo := withOrientation(encoded.Bytes(), 6)
	if orientation := jpegOrientation(photo); orientation != 6 {
		test.Fatalf("the orientation read as %d", orientation)
	}

	shrunk, err := shrinkPicture(ContentPart{Type: "image", MediaType: "image/jpeg", Data: photo}, len(photo)-1)
	if err != nil {
		test.Fatalf("shrink: %s", err)
	}
	upright, _, err := image.Decode(bytes.NewReader(shrunk.Data))
	if err != nil {
		test.Fatalf("decode: %s", err)
	}
	if bounds := upright.Bounds(); bounds.Dx() != 200 || bounds.Dy() != 300 {
		test.Fatalf("the shrunk photo is %d by %d, not turned upright", bounds.Dx(), bounds.Dy())
	}
	top, _, _, _ := upright.At(100, 20).RGBA()
	bottom, _, _, _ := upright.At(100, 280).RGBA()
	if top > bottom {
		test.Errorf("the dark half came out at the bottom: turned the wrong way")
	}
}

func TestARequestTooLargeForThePlanHasItsPicturesShrunk(test *testing.T) {
	test.Parallel()

	made, server := signedIn(test, nil)
	defer server.Close()

	photo := noisyPhoto(test, 1100, 900)
	if len(photo) <= codexBodyBytes {
		test.Fatalf("the photo is %d bytes, too small to test with", len(photo))
	}
	encoded, err := made.encode(&ChatRequest{
		Model: "gpt-5.5",
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: strings.Repeat("Be brief. ", 5000)},
			{Role: RoleUser, Parts: []ContentPart{
				{Type: "text", Text: "Got a receipt for you"},
				{Type: "image", MediaType: "image/jpeg", Data: photo},
			}},
		},
	})
	if err != nil {
		test.Fatalf("encode: %s", err)
	}
	if len(encoded) > codexBodyBytes {
		test.Errorf("the body is %d bytes, more than the %d the plan takes", len(encoded), codexBodyBytes)
	}
	var body codexRequest
	if err := json.Unmarshal(encoded, &body); err != nil {
		test.Fatalf("what it sent was not readable: %s", err)
	}
	content := body.Input[0].Content
	if len(content) != 2 || content[1].Type != "input_image" || !strings.HasPrefix(content[1].ImageURL, "data:image/jpeg;base64,") {
		test.Errorf("the picture was not sent: %+v", content)
	}
}
