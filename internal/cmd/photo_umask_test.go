//go:build unix

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A downloaded photo is a new file like any other: the person's umask decides
// who can read it, and it is not made readable by everyone when they said it
// should not be.
func TestDownloadRespectsTheUmask(test *testing.T) {
	server := newPhotoServer(test)
	server.files["photo-1"] = bytes.Repeat([]byte("pixels"), 50)
	server.photoViews["photo-1"] = `{"photo":{"id":"photo-1","originalFilename":"IMG_0001.JPG"},"photoFiles":[{"photoId":"photo-1","photoFileRole":"original","contentType":"image/jpeg","fileSizeBytes":300,"originalFilename":"IMG_0001.JPG"}],"photoPreviews":[]}`
	for _, umask := range []int{0o077, 0o027, 0o022, 0o002} {
		previous := syscall.Umask(umask)
		directory := test.TempDir()
		_, _, err := server.run(test, "download", "photo-1", "--output", directory)
		_, _, forcedErr := server.run(test, "download", "photo-1", "--output", filepath.Join(directory, "IMG_0001.JPG"), "--force")
		syscall.Umask(previous)
		if err != nil || forcedErr != nil {
			test.Fatalf("umask %04o: %v %v", umask, err, forcedErr)
		}
		info, err := os.Stat(filepath.Join(directory, "IMG_0001.JPG"))
		if err != nil {
			test.Fatal(err)
		}
		if want := os.FileMode(0o666 &^ umask); info.Mode().Perm() != want {
			test.Errorf("umask %04o: the file has mode %04o, want %04o", umask, info.Mode().Perm(), want)
		}
	}
}
