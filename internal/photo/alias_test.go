package photo_test

import (
	"net/http"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// filesByRole is the blobs a photo's files name, by role.
func (self *fixture) filesByRole(photoId string) map[models.PhotoFileRole]string {
	self.test.Helper()
	byRole := map[models.PhotoFileRole]string{}
	for _, file := range self.photoFiles(photoId) {
		byRole[file.PhotoFileRole] = file.BlobSHA256
	}
	return byRole
}

// Two assets on a phone can have the same original (an edited copy saved as a
// new asset, say) and different edits. The second is an alias of the first
// photo only because its original matched; its edit is its own, and does not
// replace the first's.
func TestTwoClientIdsWithTheSameOriginalAndDifferentEditsAreTwoPhotos(test *testing.T) {
	fixture := newFixture(test)
	original := smallJpeg(test, 90)
	firstEdit, secondEdit := smallJpeg(test, 91), smallJpeg(test, 92)
	editedHeaders := func(clientPhotoId string) map[string]string {
		return jpegHeaders(clientPhotoId, "X-Photo-File-Role", "edited")
	}
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-a"), original), http.StatusCreated)
	aliased := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b"), original)
	fixture.expect(aliased, http.StatusCreated)
	if aliased.header.Get("X-Photo-ID") != fixture.photoByClientId("asset-a").ID || fixture.count("photo") != "1" || fixture.count("photo_client_alias") != "1" {
		test.Fatal("the same original under a second name is not one photo")
	}
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), editedHeaders("asset-a"), firstEdit), http.StatusCreated)
	second := fixture.send(http.MethodPost, fixture.collectionUrl(), editedHeaders("asset-b"), secondEdit)
	fixture.expect(second, http.StatusCreated)

	if fixture.count("photo") != "2" || fixture.count("photo_client_alias") != "0" {
		test.Fatalf("photos %s, aliases %s", fixture.count("photo"), fixture.count("photo_client_alias"))
	}
	first, other := fixture.photoByClientId("asset-a"), fixture.photoByClientId("asset-b")
	if first == nil || other == nil || first.ID == other.ID || second.header.Get("X-Photo-ID") != other.ID {
		test.Fatalf("%+v %+v", first, other)
	}
	for _, check := range []struct {
		photoId string
		edit    []byte
	}{{first.ID, firstEdit}, {other.ID, secondEdit}} {
		byRole := fixture.filesByRole(check.photoId)
		if len(byRole) != 2 || byRole[models.PhotoFileRoleOriginal] != digestOf(original) || byRole[models.PhotoFileRoleEdited] != digestOf(check.edit) {
			test.Fatalf("files of %s: %v", check.photoId, byRole)
		}
	}
	// One blob for the original, one for each edit; every file counts.
	if fixture.count("photo_blob") != "3" || fixture.count("photo_file") != "4" {
		test.Fatalf("blobs %s, files %s", fixture.count("photo_blob"), fixture.count("photo_file"))
	}
}

// A picture saved from TeaNode to the phone and backed up again is the photo it
// came from, whatever the phone calls it, and stays one photo.
func TestASavedCopyBackedUpWithNoEditStaysOnePhoto(test *testing.T) {
	fixture := newFixture(test)
	original := smallJpeg(test, 93)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-a"), original)
	fixture.expect(first, http.StatusCreated)
	saved := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b"), original)
	fixture.expect(saved, http.StatusCreated)
	// The phone sends it again, as a retry does.
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b"), original), http.StatusCreated)

	if saved.header.Get("X-Photo-ID") != first.header.Get("X-Photo-ID") || fixture.count("photo") != "1" || fixture.count("photo_file") != "1" {
		test.Fatalf("photos %s, files %s", fixture.count("photo"), fixture.count("photo_file"))
	}
	if found := fixture.photoByClientId("asset-b"); found == nil || found.ID != first.header.Get("X-Photo-ID") {
		test.Fatalf("the second name does not find the photo: %+v", found)
	}
}

// A Live Photo's video arriving under an alias is the video of a Live Photo of
// that name, not a second video of the photo the alias points at.
func TestALiveVideoUnderAnAliasMakesItsOwnPhoto(test *testing.T) {
	fixture := newFixture(test)
	original := smallJpeg(test, 94)
	video := randomBytes(300, 95)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-a"), original), http.StatusCreated)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b"), original), http.StatusCreated)
	live := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{
		"Content-Type": "video/quicktime", "X-Photo-Client-ID": "asset-b", "X-Photo-File-Role": "liveVideo",
	}, video)
	fixture.expect(live, http.StatusCreated)

	first, other := fixture.photoByClientId("asset-a"), fixture.photoByClientId("asset-b")
	if first == nil || other == nil || first.ID == other.ID || live.header.Get("X-Photo-ID") != other.ID {
		test.Fatalf("%+v %+v", first, other)
	}
	if other.PhotoKind != models.PhotoKindLivePhoto || first.PhotoKind != models.PhotoKindPicture {
		test.Fatalf("kinds %s and %s", first.PhotoKind, other.PhotoKind)
	}
	if byRole := fixture.filesByRole(first.ID); len(byRole) != 1 {
		test.Fatalf("the first photo gained a file: %v", byRole)
	}
	byRole := fixture.filesByRole(other.ID)
	if len(byRole) != 2 || byRole[models.PhotoFileRoleOriginal] != digestOf(original) || byRole[models.PhotoFileRoleLiveVideo] != digestOf(video) {
		test.Fatalf("%v", byRole)
	}
}

// The split photo has an original file of its own, which the quota counts like
// any file: a split that does not fit is refused.
func TestSplittingAnAliasedPhotoCountsItsOriginalAgainstTheQuota(test *testing.T) {
	fixture := newFixture(test)
	original, edit := smallJpeg(test, 96), smallJpeg(test, 97)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(len(original) + len(edit))
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-a"), original), http.StatusCreated)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b"), original), http.StatusCreated)
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b", "X-Photo-File-Role", "edited"), edit)
	fixture.expect(refused, http.StatusRequestEntityTooLarge)
	if fixture.count("photo") != "1" || fixture.count("photo_client_alias") != "1" {
		test.Fatalf("a refused split left photos %s, aliases %s", fixture.count("photo"), fixture.count("photo_client_alias"))
	}
}

// The same picture sent again under a name the photo does not have yet costs no
// room, so a full account is not turned away for it. The request says the digest
// it will have, and that is believed only as far as the bytes the person has
// under it.
func TestADuplicateOfAnOriginalIsNotRefusedForRoomItWouldNotUse(test *testing.T) {
	fixture := newFixture(test)
	original := smallJpeg(test, 98)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-a"), original), http.StatusCreated)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	fixture.fillAccountWith("filler", 1500)
	declared := func(clientPhotoId string, more ...string) map[string]string {
		return jpegHeaders(clientPhotoId, append([]string{"X-Photo-Declared-SHA256", digestOf(original)}, more...)...)
	}

	// Without the digest the account is full for anything new.
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-b"), original), http.StatusRequestEntityTooLarge)
	// With it, the duplicate is let in, whole or in parts.
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), declared("asset-b"), original)
	fixture.expect(first, http.StatusCreated)
	if first.header.Get("X-Photo-ID") != fixture.photoByClientId("asset-a").ID || fixture.count("photo_file") != "2" {
		test.Fatalf("the duplicate was not recorded against the photo: files %s", fixture.count("photo_file"))
	}
	unsized := fixture.send(http.MethodPost, fixture.collectionUrl(), declared("asset-c", "Upload-Complete", "?0"), original[:len(original)/2])
	fixture.expect(unsized, http.StatusCreated)
	finished := fixture.send(http.MethodPatch, unsized.header.Get("Location"), appendHeaders(itoa(len(original)/2), "?1"), original[len(original)/2:])
	fixture.expect(finished, http.StatusCreated)
	if finished.header.Get("X-Photo-ID") != first.header.Get("X-Photo-ID") {
		test.Fatalf("the duplicate sent in parts made another photo")
	}

	// A request that names the digest and sends more than those bytes, or a
	// file that is not an original, gets no such pass.
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), declared("asset-d"), append(append([]byte(nil), original...), randomBytes(500, 99)...)), http.StatusRequestEntityTooLarge)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), declared("asset-e", "X-Photo-File-Role", "edited"), original), http.StatusRequestEntityTooLarge)
	// And a digest of bytes the person does not have is not believed.
	other := smallJpeg(test, 20)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("asset-f", "X-Photo-Declared-SHA256", digestOf(other)), other), http.StatusRequestEntityTooLarge)
}
