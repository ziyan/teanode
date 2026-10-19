package apigraph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/sync/semaphore"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	// Aliased: the package has a test helper of its own named picture.
	photopicture "github.com/ziyan/teanode/internal/picture"
	"github.com/ziyan/teanode/internal/storage"
)

// PhotoFaceQuery reads the people found in the person's photos: groups of
// faces, each a person the server believes it has seen more than once. The
// photos of a group are ListPhotos with its faceGroupId, and the photos of a
// contact are ListPhotos with contactId.
type PhotoFaceQuery interface {
	// ListPhotoFaceGroups lists the groups worth showing: those named by a
	// contact first, by the contact's name, and then the groups of at least
	// three faces nobody has named, the largest first. Hidden groups are left
	// out unless isHiddenIncluded is true.
	ListPhotoFaceGroups(context.Context, ListPhotoFaceGroupsArguments) ([]*models.PhotoFaceGroup, error)
	ReadPhotoFaceGroup(context.Context, PhotoFaceGroupArguments) (*models.PhotoFaceGroup, error)
}

// PhotoFaceMutation changes who the faces in the person's photos are. None of
// it changes a photo.
type PhotoFaceMutation interface {
	// NamePhotoFaceGroup names a group by a contact from the person's own
	// address books. It needs the Contacts service and contacts:use, because
	// the address book is the one list of people; a client that lets the
	// person type a new name makes the contact first with SaveContact.
	//
	// A contact names one person. When it already names another of the
	// person's groups the two are merged: the answer is the group that was
	// already named, with the faces of both, and mergedPhotoFaceGroupId is
	// the group that went into it. When the contact has no picture, it is
	// given one cut from the group's best face, and isContactPictureSet says
	// so; a picture the contact has is never replaced.
	NamePhotoFaceGroup(context.Context, NamePhotoFaceGroupArguments) (*NamedPhotoFaceGroup, error)
	UnnamePhotoFaceGroup(context.Context, PhotoFaceGroupArguments) (*models.PhotoFaceGroup, error)

	// MergePhotoFaceGroups moves the faces of the other groups into the first
	// and deletes the others.
	MergePhotoFaceGroups(context.Context, MergePhotoFaceGroupsArguments) (*models.PhotoFaceGroup, error)

	// RemovePhotoFaceFromGroup takes a face out of its group, for "this is
	// not them": the face is made a group of its own, which the person has
	// confirmed, so it is not grouped again.
	RemovePhotoFaceFromGroup(context.Context, PhotoFaceArguments) (*models.PhotoFaceGroup, error)
	HidePhotoFaceGroup(context.Context, HidePhotoFaceGroupArguments) (*models.PhotoFaceGroup, error)

	// SetPhotoFaceRecognition switches the finding of faces in the person's
	// photos on or off and answers the new state. Off deletes every face and
	// group they have; on has their photos read again.
	SetPhotoFaceRecognition(context.Context, SetPhotoFaceRecognitionArguments) (bool, error)
}

// ListPhotoFaceGroupsArguments asks for the groups of faces.
type ListPhotoFaceGroupsArguments struct {
	// IsHiddenIncluded lists the groups the person hid as well.
	IsHiddenIncluded *bool `json:"isHiddenIncluded"`
}

// PhotoFaceGroupArguments names one group of faces.
type PhotoFaceGroupArguments struct {
	PhotoFaceGroupID string `json:"photoFaceGroupId"`
}

// PhotoFaceArguments names one face.
type PhotoFaceArguments struct {
	PhotoFaceID string `json:"photoFaceId"`
}

// NamePhotoFaceGroupArguments names a group by a contact.
type NamePhotoFaceGroupArguments struct {
	PhotoFaceGroupID string `json:"photoFaceGroupId"`

	// ContactID is a contact in one of the person's own address books.
	ContactID string `json:"contactId"`
}

// NamedPhotoFaceGroup is what naming a group answers: the group as it is now,
// and what else the naming did.
type NamedPhotoFaceGroup struct {
	// PhotoFaceGroup is the group that is named. When the contact named
	// another group already it is that one, with the faces of both.
	PhotoFaceGroup *models.PhotoFaceGroup `json:"photoFaceGroup"`

	// MergedPhotoFaceGroupID is the group that was put into PhotoFaceGroup
	// and no longer exists; empty when nothing was merged.
	MergedPhotoFaceGroupID string `json:"mergedPhotoFaceGroupId"`

	// IsContactPictureSet says the contact had no picture and was given one,
	// cut from a face of the group.
	IsContactPictureSet bool `json:"isContactPictureSet"`
}

// MergePhotoFaceGroupsArguments names the groups to put into one, at least two
// and at most 50. The first is the one that stays.
type MergePhotoFaceGroupsArguments struct {
	PhotoFaceGroupIDs []string `json:"photoFaceGroupIds"`
}

// HidePhotoFaceGroupArguments hides a group from the people listing, or shows
// it again. A hidden group goes on taking the faces of its person, so they
// stay hidden.
type HidePhotoFaceGroupArguments struct {
	PhotoFaceGroupID string `json:"photoFaceGroupId"`
	IsHidden         bool   `json:"isHidden"`
}

// SetPhotoFaceRecognitionArguments switches the person's faces on or off.
type SetPhotoFaceRecognitionArguments struct {
	IsEnabled bool `json:"isEnabled"`
}

// withGroupPictureAddresses fills in where each group's picture is fetched
// from: the picture of its cover face. It also says which group is the caller
// themselves: the one named by ownContactId, the contact that is the caller's
// own card (User.ContactID). Nothing is ever the caller's own when ownContactId
// is empty.
func withGroupPictureAddresses(ownContactId string, groups ...*models.PhotoFaceGroup) {
	for _, group := range groups {
		if group.CoverPhotoFaceID != "" {
			group.FacePictureURL = api.PhotoFacePicturePath(group.CoverPhotoFaceID)
		}
		group.IsOwnContact = isOwnContact(ownContactId, group.ContactID)
	}
}

// withGroupAccounts says, for each group named by a contact the caller linked
// to somebody in their circles, who that is. The link is the caller's own note:
// it is read through the caller's contacts only, and only while they still share
// a circle with the person, so a group never shows an account its contact does
// not say.
func (self *graph) withGroupAccounts(ctx context.Context, ownerUserId string, groups ...*models.PhotoFaceGroup) error {
	isNamed := false
	for _, group := range groups {
		if group.ContactID != "" {
			isNamed = true
		}
	}
	if !isNamed {
		return nil
	}
	links, err := self.transaction(ctx).ListContactAccountLinks(ownerUserId)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	accountUserIds := make(map[string]string, len(links))
	for _, link := range links {
		accountUserIds[link.AddressBookID+"\n"+link.ID] = link.AccountUserID
	}
	var people map[string]*CircleMember
	for _, group := range groups {
		accountUserId := accountUserIds[group.ContactAddressBookID+"\n"+group.ContactID]
		if group.ContactID == "" || accountUserId == "" {
			continue
		}
		if people == nil {
			if people, err = self.circlePeopleOf(ctx, ownerUserId); err != nil {
				return err
			}
		}
		person := people[accountUserId]
		if person == nil {
			continue
		}
		group.AccountUserID = &accountUserId
		group.Account = &models.PhotoFaceGroupAccount{
			UserID: person.UserID, Username: person.Username, Name: person.Name,
			AvatarURL: person.AvatarURL, AvatarColorIndex: person.AvatarColorIndex,
		}
	}
	return nil
}

// withFacePictureAddresses is the same for faces.
func withFacePictureAddresses(ownContactId string, faces ...*models.PhotoFace) {
	for _, face := range faces {
		face.FacePictureURL = api.PhotoFacePicturePath(face.ID)
		face.IsOwnContact = isOwnContact(ownContactId, face.ContactID)
	}
}

// isOwnContact is whether the contact that names a group, empty for a group
// nobody named, is the caller's own.
func isOwnContact(ownContactId, groupContactId string) bool {
	return ownContactId != "" && groupContactId == ownContactId
}

func (self *graph) ListPhotoFaceGroups(ctx context.Context, arguments ListPhotoFaceGroupsArguments) ([]*models.PhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := self.transaction(ctx).ListPhotoFaceGroups(principal.User.ID, arguments.IsHiddenIncluded != nil && *arguments.IsHiddenIncluded)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(principal.User.ContactID, groups...)
	if err := self.withGroupAccounts(ctx, principal.User.ID, groups...); err != nil {
		return nil, err
	}
	return groups, nil
}

func (self *graph) ReadPhotoFaceGroup(ctx context.Context, arguments PhotoFaceGroupArguments) (*models.PhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	group, err := self.transaction(ctx).GetPhotoFaceGroup(principal.User.ID, arguments.PhotoFaceGroupID)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(principal.User.ContactID, group)
	if err := self.withGroupAccounts(ctx, principal.User.ID, group); err != nil {
		return nil, err
	}
	return group, nil
}

// NamePhotoFaceGroup names a group by one of the person's contacts. Naming is
// choosing from the address book, so a server without Contacts, or an account
// that may not use it, cannot name anybody, and is told what to turn on rather
// than that the contact was not found.
func (self *graph) NamePhotoFaceGroup(ctx context.Context, arguments NamePhotoFaceGroupArguments) (*NamedPhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	if !self.isServiceEnabled(models.ServiceContacts) {
		return nil, fmt.Errorf("%w: naming a person means choosing a contact from your address book, and the Contacts service is switched off on this server; turn on Contacts to name people", api.ErrInvalidArguments)
	}
	if !principal.Permissions.Has(models.PermissionContactsUse) {
		return nil, fmt.Errorf("%w: naming a person means choosing a contact from your address book, and your account may not use Contacts", api.ErrInvalidArguments)
	}
	if strings.TrimSpace(arguments.ContactID) == "" {
		return nil, fmt.Errorf("%w: say which contact names the group", api.ErrInvalidArguments)
	}
	contact, book, err := self.ownContact(ctx, arguments.ContactID)
	if err != nil {
		return nil, err
	}
	group, mergedPhotoFaceGroupId, err := self.transaction(ctx).NamePhotoFaceGroup(principal.User.ID, arguments.PhotoFaceGroupID, book.ID, contact.ID)
	if err != nil {
		return nil, translateError(err)
	}
	isContactPictureSet, err := self.giveContactPictureFromGroup(ctx, principal, contact, group.ID)
	if err != nil {
		return nil, err
	}
	withGroupPictureAddresses(principal.User.ContactID, group)
	if err := self.withGroupAccounts(ctx, principal.User.ID, group); err != nil {
		return nil, err
	}
	return &NamedPhotoFaceGroup{PhotoFaceGroup: group, MergedPhotoFaceGroupID: mergedPhotoFaceGroupId, IsContactPictureSet: isContactPictureSet}, nil
}

func (self *graph) UnnamePhotoFaceGroup(ctx context.Context, arguments PhotoFaceGroupArguments) (*models.PhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	group, err := self.transaction(ctx).UnnamePhotoFaceGroup(principal.User.ID, arguments.PhotoFaceGroupID)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(principal.User.ContactID, group)
	if err := self.withGroupAccounts(ctx, principal.User.ID, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (self *graph) MergePhotoFaceGroups(ctx context.Context, arguments MergePhotoFaceGroupsArguments) (*models.PhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	group, err := self.transaction(ctx).MergePhotoFaceGroups(principal.User.ID, arguments.PhotoFaceGroupIDs)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(principal.User.ContactID, group)
	if err := self.withGroupAccounts(ctx, principal.User.ID, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (self *graph) RemovePhotoFaceFromGroup(ctx context.Context, arguments PhotoFaceArguments) (*models.PhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	group, err := self.transaction(ctx).RemovePhotoFaceFromGroup(principal.User.ID, arguments.PhotoFaceID)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(principal.User.ContactID, group)
	if err := self.withGroupAccounts(ctx, principal.User.ID, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (self *graph) HidePhotoFaceGroup(ctx context.Context, arguments HidePhotoFaceGroupArguments) (*models.PhotoFaceGroup, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	group, err := self.transaction(ctx).HidePhotoFaceGroup(principal.User.ID, arguments.PhotoFaceGroupID, arguments.IsHidden)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(principal.User.ContactID, group)
	if err := self.withGroupAccounts(ctx, principal.User.ID, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (self *graph) SetPhotoFaceRecognition(ctx context.Context, arguments SetPhotoFaceRecognitionArguments) (bool, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).SetPhotoFaceRecognition(principal.User.ID, arguments.IsEnabled); err != nil {
		return false, translateError(err)
	}
	return arguments.IsEnabled, nil
}

// maximumFacePreviewBytes bounds the large preview read to cut a face from.
const maximumFacePreviewBytes = 64 << 20

// facePictureConcurrency is how many face pictures are cut at once. Each cut
// decodes a preview of 2048 pixels on its long side, some 16 MB of pixels, and
// a page of faces asks for dozens at the same moment; the rest wait their turn,
// or are given up on when the browser goes away.
const facePictureConcurrency = 4

// facePictureSlots is the turns for cutting a face picture, shared by every
// request of the process.
var facePictureSlots = semaphore.NewWeighted(facePictureConcurrency)

// photoFacePictureView serves the picture of one face to whoever may read its
// photo: a square cut from the photo's large preview. It is cut when asked
// for, and cached by the browser; the ETag changes when the preview is made
// again.
func (self *graph) photoFacePictureView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.photoPerson(response, request, false)
	if !isAuthorized {
		return
	}
	var face *models.PhotoFace
	var preview *models.PhotoPreview
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		// The face is read through the owner of its photo once the person is
		// let in to the photo: a face in a photo of a shared album is shown
		// to whoever reads the album.
		ownerUserId, err := transaction.GetPhotoFaceOwnerUserID(mux.Vars(request)["photoFaceId"])
		if err != nil {
			return err
		}
		if face, err = transaction.GetPhotoFace(ownerUserId, mux.Vars(request)["photoFaceId"]); err != nil {
			return err
		}
		if _, err = transaction.GetPhotoAccess(user.ID, face.PhotoID); err != nil {
			return err
		}
		preview, err = transaction.GetPhotoPreview(ownerUserId, face.PhotoID, models.PhotoPreviewLarge)
		return err
	}); err != nil {
		photoHttpError(response, err)
		return
	}
	header := response.Header()
	etag := fmt.Sprintf(`"face-%s-%d"`, face.ID, preview.CreatedAt.UnixMicro())
	header.Set("ETag", etag)
	// The picture of a face does not change while its preview does not, but
	// a person may be shown it on a screen that has long been open.
	header.Set("Cache-Control", "private, max-age=86400")
	if matched := request.Header.Get("If-None-Match"); matched != "" && etagMatches(matched, etag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		http.Error(response, "photo storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	if preview.PreviewSizeBytes <= 0 || preview.PreviewSizeBytes > maximumFacePreviewBytes {
		http.Error(response, "not found", http.StatusNotFound)
		return
	}
	header.Set("Content-Type", "image/jpeg")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("X-Content-Type-Options", "nosniff")
	if request.Method == http.MethodHead {
		response.WriteHeader(http.StatusOK)
		return
	}
	if err := facePictureSlots.Acquire(ctx, 1); err != nil {
		header.Del("ETag")
		header.Del("Content-Type")
		http.Error(response, "the request was given up on", http.StatusServiceUnavailable)
		return
	}
	defer facePictureSlots.Release(1)
	stream, err := files.OpenFileRange(ctx, models.PhotoPreviewFileID(face.PhotoID, models.PhotoPreviewLarge), 0, preview.PreviewSizeBytes)
	if err != nil {
		header.Del("ETag")
		header.Del("Content-Type")
		photoHttpError(response, err)
		return
	}
	content, err := io.ReadAll(io.LimitReader(stream, preview.PreviewSizeBytes+1))
	_ = stream.Close()
	var cut []byte
	if err == nil {
		cut, err = cutFacePicture(content, face)
	}
	if err != nil {
		header.Del("ETag")
		header.Del("Content-Type")
		photoHttpError(response, err)
		return
	}
	header.Set("Content-Length", fmt.Sprint(len(cut)))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(cut)
}

// cutFacePicture decodes a large preview and cuts the face out of it.
func cutFacePicture(previewContent []byte, face *models.PhotoFace) ([]byte, error) {
	decoded, err := photopicture.Decode(previewContent)
	if err != nil {
		return nil, err
	}
	return photo.FacePicture(decoded, face)
}
