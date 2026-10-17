package apigraph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

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
	NamePhotoFaceGroup(context.Context, NamePhotoFaceGroupArguments) (*models.PhotoFaceGroup, error)
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
// from: the picture of its cover face.
func withGroupPictureAddresses(groups ...*models.PhotoFaceGroup) {
	for _, group := range groups {
		if group.CoverPhotoFaceID != "" {
			group.FacePictureURL = api.PhotoFacePicturePath(group.CoverPhotoFaceID)
		}
	}
}

// withFacePictureAddresses is the same for faces.
func withFacePictureAddresses(faces ...*models.PhotoFace) {
	for _, face := range faces {
		face.FacePictureURL = api.PhotoFacePicturePath(face.ID)
	}
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
	withGroupPictureAddresses(groups...)
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
	withGroupPictureAddresses(group)
	return group, nil
}

// NamePhotoFaceGroup names a group by one of the person's contacts. Naming is
// choosing from the address book, so a server without Contacts, or an account
// that may not use it, cannot name anybody, and is told what to turn on rather
// than that the contact was not found.
func (self *graph) NamePhotoFaceGroup(ctx context.Context, arguments NamePhotoFaceGroupArguments) (*models.PhotoFaceGroup, error) {
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
	group, err := self.transaction(ctx).NamePhotoFaceGroup(principal.User.ID, arguments.PhotoFaceGroupID, book.ID, contact.ID)
	if err != nil {
		return nil, translateError(err)
	}
	withGroupPictureAddresses(group)
	return group, nil
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
	withGroupPictureAddresses(group)
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
	withGroupPictureAddresses(group)
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
	withGroupPictureAddresses(group)
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
	withGroupPictureAddresses(group)
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

// photoFacePictureView serves the picture of one face to the owner of its
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
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if face, err = transaction.GetPhotoFace(user.ID, mux.Vars(request)["photoFaceId"]); err != nil {
			return err
		}
		preview, err = transaction.GetPhotoPreview(user.ID, face.PhotoID, models.PhotoPreviewLarge)
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
