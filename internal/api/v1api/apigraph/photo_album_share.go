package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	"github.com/ziyan/teanode/internal/push"
)

// Sharing an album: with one person or one circle, at a level, and the albums
// a circle keeps for itself. docs/subsystems/photos.md ("Sharing") says what
// each level shows, and docs/decisions/20261016-an-album-is-shared-at-a-level.md
// why.
//
// Who may read or change which photo and which album is decided in one place,
// the database's GetPhotoAccess and GetPhotoAlbumAccess, reached through
// photo_authorize.go. The operations here are about who an album is shared
// with, which is its manager's alone.

// PhotoAlbumShareView is one share of an album.
type PhotoAlbumShareView struct {
	ID           string `json:"id"`
	PhotoAlbumID string `json:"photoAlbumId"`

	// Who it is shared with: a person or a circle, never both.
	GranteeUserID           *string `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`
	GranteeCircleID         *string `json:"granteeCircleId"`
	GranteeCircleName       string  `json:"granteeCircleName"`

	// view or contribute
	ShareLevel string `json:"shareLevel"`

	// IsLocationIncluded says the people reached see where the album's photos
	// were taken and receive the original files.
	IsLocationIncluded bool `json:"isLocationIncluded"`

	CreatedAt time.Time `json:"createdAt"`
}

func describePhotoAlbumShare(share *models.PhotoAlbumShare) *PhotoAlbumShareView {
	view := &PhotoAlbumShareView{
		ID: share.ID, PhotoAlbumID: share.PhotoAlbumID, ShareLevel: string(share.ShareLevel),
		IsLocationIncluded: share.IsLocationIncluded, CreatedAt: share.CreatedAt,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
		view.GranteeAvatarURL, view.GranteeAvatarColorIndex = avatarUrl(share.GranteeUser), avatar.InitialColorIndex(share.GranteeUserID)
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

// SharePhotoAlbumArguments shares an album.
type SharePhotoAlbumArguments struct {
	// The album, which the caller owns; for a circle's album an admin of the
	// circle manages it, but a circle's album is not shared further, since
	// every member has it
	PhotoAlbumID string `json:"photoAlbumId"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// view (see the photos and save them) or contribute (also add photos of
	// their own and take out the ones they added)
	ShareLevel string `json:"shareLevel"`

	// Let them see where the photos were taken and receive the original
	// files; without it they see a picture made afresh, which carries no place
	IsLocationIncluded bool `json:"isLocationIncluded" graphapi:"nullable"`
}

// UpdatePhotoAlbumShareArguments changes one share.
type UpdatePhotoAlbumShareArguments struct {
	PhotoAlbumShareID string `json:"photoAlbumShareId"`

	// view or contribute
	ShareLevel string `json:"shareLevel"`

	IsLocationIncluded bool `json:"isLocationIncluded" graphapi:"nullable"`
}

// PhotoAlbumShareArguments names one share.
type PhotoAlbumShareArguments struct {
	PhotoAlbumShareID string `json:"photoAlbumShareId"`
}

func parsePhotoAlbumShareLevel(shareLevel string) (models.PhotoAlbumShareLevel, error) {
	level := models.PhotoAlbumShareLevel(strings.ToLower(strings.TrimSpace(shareLevel)))
	if !level.IsValid() {
		return "", fmt.Errorf("%w: an album is shared at view or contribute", api.ErrInvalidArguments)
	}
	return level, nil
}

func (self *graph) ListPhotoAlbumShares(ctx context.Context, arguments PhotoAlbumArguments) ([]*PhotoAlbumShareView, error) {
	principal, _, err := self.requirePhotoAlbumManager(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return nil, err
	}
	shares, err := self.transaction(ctx).ListPhotoAlbumShares(principal.User.ID, strings.TrimSpace(arguments.PhotoAlbumID))
	if err != nil {
		return nil, translateError(err)
	}
	views := make([]*PhotoAlbumShareView, 0, len(shares))
	for _, share := range shares {
		views = append(views, describePhotoAlbumShare(share))
	}
	return views, nil
}

func (self *graph) SharePhotoAlbum(ctx context.Context, arguments SharePhotoAlbumArguments) (*PhotoAlbumShareView, error) {
	principal, access, err := self.requirePhotoAlbumManager(ctx, arguments.PhotoAlbumID)
	if err != nil {
		return nil, err
	}
	shareLevel, err := parsePhotoAlbumShareLevel(arguments.ShareLevel)
	if err != nil {
		return nil, err
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	if access.CircleID != "" {
		return nil, fmt.Errorf("%w: a circle's album belongs to every member of the circle already", api.ErrInvalidArguments)
	}
	transaction := self.transaction(ctx)
	// What the grantee was given before, to tell whether this is more.
	earlier, err := transaction.ListPhotoAlbumShares(principal.User.ID, access.PhotoAlbumID)
	if err != nil {
		return nil, translateError(err)
	}
	saved, err := transaction.SharePhotoAlbum(principal.User.ID, &models.PhotoAlbumShare{
		PhotoAlbumID: access.PhotoAlbumID, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId,
		ShareLevel: shareLevel, IsLocationIncluded: arguments.IsLocationIncluded,
	})
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in: all of them answer as nothing found, because
		// which of them it was is not the caller's to learn.
		return nil, translateError(err)
	}
	shares, err := transaction.ListPhotoAlbumShares(principal.User.ID, access.PhotoAlbumID)
	if err != nil {
		return nil, translateError(err)
	}
	for _, share := range shares {
		if share.ID == saved.ID {
			saved = share
		}
	}
	if isPhotoAlbumShareWider(earlier, saved) {
		if err := self.tellAboutPhotoAlbumShare(ctx, principal.User, saved); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s shared a photo album at %s", principal.User.Username, shareLevel)
	return describePhotoAlbumShare(saved), nil
}

func (self *graph) UpdatePhotoAlbumShare(ctx context.Context, arguments UpdatePhotoAlbumShareArguments) (*PhotoAlbumShareView, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	shareLevel, err := parsePhotoAlbumShareLevel(arguments.ShareLevel)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	earlier, err := transaction.GetPhotoAlbumShare(principal.User.ID, strings.TrimSpace(arguments.PhotoAlbumShareID))
	if err != nil {
		return nil, translateError(err)
	}
	updated, err := transaction.UpdatePhotoAlbumShare(principal.User.ID, strings.TrimSpace(arguments.PhotoAlbumShareID), shareLevel, arguments.IsLocationIncluded)
	if err != nil {
		return nil, translateError(err)
	}
	shares, err := transaction.ListPhotoAlbumShares(principal.User.ID, updated.PhotoAlbumID)
	if err != nil {
		return nil, translateError(err)
	}
	for _, share := range shares {
		if share.ID == updated.ID {
			updated = share
		}
	}
	if isPhotoAlbumShareWider([]*models.PhotoAlbumShare{earlier}, updated) {
		if err := self.tellAboutPhotoAlbumShare(ctx, principal.User, updated); err != nil {
			return nil, err
		}
	}
	return describePhotoAlbumShare(updated), nil
}

// isPhotoAlbumShareWider says whether the share gives its grantee more than the
// album's shares did before: there was none for them, or a higher level, or it
// now includes where the photos were taken.
func isPhotoAlbumShareWider(earlier []*models.PhotoAlbumShare, saved *models.PhotoAlbumShare) bool {
	for _, share := range earlier {
		if share.GranteeUserID == saved.GranteeUserID && share.GranteeCircleID == saved.GranteeCircleID {
			isLevelHigher := saved.ShareLevel == models.PhotoAlbumShareLevelContribute && share.ShareLevel != models.PhotoAlbumShareLevelContribute
			return isLevelHigher || (saved.IsLocationIncluded && !share.IsLocationIncluded)
		}
	}
	return true
}

// tellAboutPhotoAlbumShare tells the people an album share reaches, naming the
// album.
func (self *graph) tellAboutPhotoAlbumShare(ctx context.Context, sharer *models.User, share *models.PhotoAlbumShare) error {
	album, err := self.transaction(ctx).GetPhotoAlbum(sharer.ID, share.PhotoAlbumID)
	if err != nil {
		return translateError(err)
	}
	return self.tellAboutShare(ctx, sharer, models.ShareReceivedNote{
		SharerName: sharer.DisplayName(), ShareKind: models.ShareKindPhotoAlbum, ShareLevel: string(share.ShareLevel),
		ShareSubject: album.AlbumTitle, ShareScopes: models.AlbumShareScopes(share.IsLocationIncluded),
	}, share.GranteeUserID, share.GranteeCircleID)
}

func (self *graph) RemovePhotoAlbumShare(ctx context.Context, arguments PhotoAlbumShareArguments) (bool, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).DeletePhotoAlbumShare(principal.User.ID, strings.TrimSpace(arguments.PhotoAlbumShareID)); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

// SavePhotoToLibrary keeps a photo of a shared album as a photo of the
// caller's own. Reading the preview and storing it takes as long as the file
// is big, so this mutation does not run inside the request's transaction (see
// isTransactionFreeMutation): it reads the rows in a short transaction of its
// own, leaves it, and then stores.
func (self *graph) SavePhotoToLibrary(ctx context.Context, arguments PhotoArguments) (*models.Photo, error) {
	var principal *api.Principal
	var shared *photo.SharedPhoto
	if err := self.inShortTransaction(ctx, func(ctx context.Context) error {
		var access *models.PhotoAccess
		var err error
		if principal, access, err = self.requirePhotoReader(ctx, arguments.PhotoID); err != nil {
			return err
		}
		if access.IsOwner {
			return fmt.Errorf("%w: the photo is in your library already", api.ErrInvalidArguments)
		}
		transaction := self.transaction(ctx)
		found, err := transaction.GetPhoto(access.OwnerUserID, access.PhotoID)
		if err != nil {
			return translateError(err)
		}
		files, err := transaction.ListPhotoFiles(access.OwnerUserID, []string{found.ID})
		if err != nil {
			return translateError(err)
		}
		previews, err := transaction.ListPhotoPreviews(access.OwnerUserID, []string{found.ID})
		if err != nil {
			return translateError(err)
		}
		shared = &photo.SharedPhoto{Photo: found, Files: files, Previews: previews, IsLocationIncluded: access.IsLocationIncluded}
		return nil
	}); err != nil {
		return nil, err
	}
	store, err := self.photoStore()
	if err != nil {
		return nil, err
	}
	saved, err := store.SaveSharedPhoto(ctx, principal.User.ID, shared)
	if err != nil {
		var overQuota *photo.QuotaError
		if errors.As(err, &overQuota) || errors.Is(err, photo.ErrNothingToSave) {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		return nil, translateError(err)
	}
	saved.IsOwnedByCaller = true
	return saved, nil
}

// photoAddNoticeInterval is how often one person is told that photos were
// added to one album: once, and then not again until it has been this long.
const photoAddNoticeInterval = 10 * time.Minute

// photoAddNoticeCache remembers when each reader was last told about each
// album, so that a person adding photos one after another is one notification.
// It is kept in memory: a restart forgets it, which costs one extra
// notification, and another instance of the server does not share it.
type photoAddNoticeCache struct {
	mutex  sync.Mutex
	sentAt map[string]time.Time
}

// take says whether the reader may be told about the album now, and if so
// counts them as told.
func (self *photoAddNoticeCache) take(photoAlbumId, readerUserId string, now time.Time) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sentAt == nil {
		self.sentAt = map[string]time.Time{}
	}
	key := photoAlbumId + "\x00" + readerUserId
	if last, isKept := self.sentAt[key]; isKept && now.Sub(last) < photoAddNoticeInterval {
		return false
	}
	if len(self.sentAt) >= 4096 {
		for oldKey, last := range self.sentAt {
			if now.Sub(last) >= photoAddNoticeInterval {
				delete(self.sentAt, oldKey)
			}
		}
	}
	self.sentAt[key] = now
	return true
}

// notifyPhotosAdded tells the phones of everybody else who reads the album
// that photos were added to it, once the change has committed: "Ana added 6
// photos to Family". A reader is told at most once per album every ten
// minutes. A phone belongs to a person's agent row, which a person has with
// the assistant off as well, so a reader with no phone registered is simply
// not told; so is a reader who may not keep a photo library, who could not
// open what the notification is about.
func (self *graph) notifyPhotosAdded(ctx context.Context, adder *models.User, album *models.PhotoAlbum, addedCount int) {
	notifier := self.phoneNotifier()
	transaction := self.transaction(ctx)
	if notifier == nil || transaction == nil {
		return
	}
	readerUserIds, err := transaction.ListPhotoAlbumReaderUserIDs(album.ID)
	if err != nil {
		log.Warningf("cannot list who reads a photo album to tell them: %s", err)
		return
	}
	agentIds := map[string]string{}
	for _, readerUserId := range readerUserIds {
		if readerUserId == adder.ID {
			continue
		}
		permissions, err := transaction.EffectivePermissions(readerUserId)
		if err != nil || permissions == nil || !permissions.Has(models.PermissionPhotosUse) {
			continue
		}
		reader, err := transaction.GetAgentByUser(readerUserId)
		if err != nil || reader == nil {
			continue
		}
		agentIds[readerUserId] = reader.ID
	}
	if len(agentIds) == 0 {
		return
	}
	body := fmt.Sprintf("%s added %d photos to %s", adder.DisplayName(), addedCount, album.AlbumTitle)
	if addedCount == 1 {
		body = fmt.Sprintf("%s added 1 photo to %s", adder.DisplayName(), album.AlbumTitle)
	}
	content := &push.Content{
		NotificationKind: models.NotificationPhoto,
		Title:            "Photos",
		Body:             agent.PhoneBody(body),
		ThreadID:         "photo-album-" + album.ID,
	}
	transaction.AfterCommit(func() {
		now := time.Now()
		for readerUserId, agentId := range agentIds {
			if self.photoAddNotices.take(album.ID, readerUserId, now) {
				notifier.NotifyPhones(agentId, models.NotificationPhoto, content)
			}
		}
	})
}
