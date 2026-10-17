package apigraph

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// avatarUrl is where a person's picture is served, or nil when they have
// none. The time it changed is in the address: a browser keeps the picture
// for a day without asking, and a new picture has to be a new address for
// that to be safe.
func avatarUrl(user *models.User) *string {
	if user == nil || user.AvatarFileID == "" || user.AvatarUpdatedAt == nil {
		return nil
	}
	address := fmt.Sprintf("%s/avatar/user/%s?v=%d", api.Prefix, user.ID, user.AvatarUpdatedAt.Unix())
	return &address
}

// canSeeAvatar says whether the caller may be shown this person's picture:
// the person themselves, anyone who manages accounts, anyone who shares a
// circle with them, and anyone who is in a chat with them now, since a chat
// shows each member's name to the others and the picture goes with the name.
// The one place that decides.
func canSeeAvatar(tx db.Transaction, principal *api.Principal, userId string) (bool, error) {
	if principal == nil || userId == "" {
		return false, nil
	}
	if principal.User != nil && principal.User.ID == userId {
		return true, nil
	}
	if principal.Permissions.Has(models.PermissionUserManage) {
		return true, nil
	}
	if principal.User == nil {
		return false, nil
	}
	isInCircle, err := tx.SharesCircle(principal.User.ID, userId)
	if err != nil || isInCircle {
		return isInCircle, err
	}
	return tx.SharesChat(principal.User.ID, userId)
}

// requireAvatarOwner is the account whose picture an operation changes: the
// caller's own when no account is named, otherwise the one named. Changing
// somebody else's needs user:manage, the same rule UpdateUser applies to
// somebody else's name, and a refusal answers not found.
func (self *graph) requireAvatarOwner(ctx context.Context, userId *string) (string, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return "", err
	}
	ownId := ""
	if principal.User != nil {
		ownId = principal.User.ID
	}
	target := ownId
	if userId != nil && strings.TrimSpace(*userId) != "" {
		target = strings.TrimSpace(*userId)
	}
	if target == "" {
		// The console is not an account and has no picture of its own.
		return "", api.ErrInvalidArguments
	}
	if target != ownId {
		if _, err := self.requirePermission(ctx, models.PermissionUserManage); err != nil {
			return "", err
		}
	}
	return target, nil
}

type SetUserAvatarArguments struct {
	// ID of the User whose picture to set; the caller's own when omitted
	UserID *string `json:"userId"`

	// The picture: a PNG or JPEG of at most 8 MiB and 20 megapixels, base64 encoded
	Content string `json:"content"`
}

func (self *graph) SetUserAvatar(ctx context.Context, arguments SetUserAvatarArguments) (*User, error) {
	userId, err := self.requireAvatarOwner(ctx, arguments.UserID)
	if err != nil {
		return nil, err
	}

	// Refused by size before it is decoded, so a request cannot make the
	// server allocate more than the picture is allowed to be.
	text := strings.TrimSpace(arguments.Content)
	if len(text) > base64.StdEncoding.EncodedLen(avatar.MaxInputBytes) {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, avatar.ErrUnusable)
	}
	content, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("%w: the picture must be base64 text", api.ErrInvalidArguments)
	}
	normalized, err := avatar.Normalize(ctx, content)
	if err != nil {
		if errors.Is(err, avatar.ErrUnusable) {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		// Others' pictures are being processed: the same picture sent again
		// in a moment is accepted.
		if errors.Is(err, avatar.ErrBusy) {
			return nil, api.ErrTooManyRequests
		}
		return nil, err
	}

	tx := self.transaction(ctx)
	existing, err := tx.GetUser(userId)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, api.ErrNotFound
	}

	// A new file under a new name each time, so a viewer holding the old
	// address is never handed bytes that do not match its version.
	now := time.Now()
	fileId := fmt.Sprintf("avatar-user-%s-%d", userId, now.UnixNano())
	if err := self.storage.PutFile(ctx, fileId, normalized); err != nil {
		return nil, err
	}
	replacedFileId := ""
	updated, err := tx.UpdateUser(userId, func(user *models.User) error {
		replacedFileId = user.AvatarFileID
		user.AvatarFileID = fileId
		user.AvatarUpdatedAt = &now
		return nil
	})
	if err != nil {
		self.deleteAvatarFile(ctx, fileId)
		return nil, translateError(err)
	}
	// The old bytes go once the row naming the new ones is committed: a
	// commit that failed would otherwise leave the account naming a file
	// that is gone.
	tx.AfterCommit(func() { self.deleteAvatarFile(context.WithoutCancel(ctx), replacedFileId) })
	log.Noticef("%s set the picture of the account %q", operatorName(ctx), updated.Username)
	return describeUser(updated), nil
}

type RemoveUserAvatarArguments struct {
	// ID of the User whose picture to remove; the caller's own when omitted
	UserID *string `json:"userId"`
}

func (self *graph) RemoveUserAvatar(ctx context.Context, arguments RemoveUserAvatarArguments) (*User, error) {
	userId, err := self.requireAvatarOwner(ctx, arguments.UserID)
	if err != nil {
		return nil, err
	}
	removedFileId := ""
	tx := self.transaction(ctx)
	updated, err := tx.UpdateUser(userId, func(user *models.User) error {
		removedFileId = user.AvatarFileID
		user.AvatarFileID = ""
		user.AvatarUpdatedAt = nil
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	tx.AfterCommit(func() { self.deleteAvatarFile(context.WithoutCancel(ctx), removedFileId) })
	log.Noticef("%s removed the picture of the account %q", operatorName(ctx), updated.Username)
	return describeUser(updated), nil
}

// deleteAvatarFile removes bytes no row names any more. A failure is logged
// and not returned: the row is already right, and what is left behind costs
// only the space.
func (self *graph) deleteAvatarFile(ctx context.Context, fileId string) {
	if fileId == "" {
		return
	}
	if err := self.storage.DeleteFile(ctx, fileId); err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Warningf("failed to remove the picture file %s: %s", fileId, err)
	}
}

// avatarSidePixelsServed are the sides, in pixels, a picture is served at: the
// stored one, and the small one a list row draws. They are the values of the
// size query parameter of the picture address, which both apps send.
var avatarSidePixelsServed = map[int]bool{64: true, avatar.SidePixels: true}

// A person's picture, served as a picture.
//
// Beside the contact photo's handler and written the same way, because both
// answer a plain image request from a page that cannot add headers of its own.
// Whoever may not see it, and whoever has no picture, is told the same thing,
// so the address does not say which accounts exist.
func (self *graph) avatarView(response http.ResponseWriter, request *http.Request) {
	username := api.UsernameFromRequest(request)
	if username == "" {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return
	}
	var user *models.User
	if username != config.LocalUsername {
		found, err := self.database.GetUserByUsername(username)
		if err != nil {
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
		if found == nil || found.Disabled() {
			http.Error(response, "not signed in", http.StatusUnauthorized)
			return
		}
		user = found
	}

	userId := strings.TrimSpace(mux.Vars(request)["userId"])
	sidePixels := avatar.SidePixels
	if asked := request.URL.Query().Get("size"); asked != "" {
		parsed, err := strconv.Atoi(asked)
		if err != nil || !avatarSidePixelsServed[parsed] {
			http.Error(response, "size is 64 or 512", http.StatusBadRequest)
			return
		}
		sidePixels = parsed
	}

	var principal *api.Principal
	var owner *models.User
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) (err error) {
		if principal, err = self.resolvePrincipal(tx, username, user); err != nil {
			return err
		}
		isAllowed, err := canSeeAvatar(tx, principal, userId)
		if err != nil || !isAllowed {
			return err
		}
		owner, err = tx.GetUser(userId)
		return err
	}); err != nil {
		log.Errorf("cannot read a person's picture: %s", err)
		http.Error(response, "cannot read that just now", http.StatusInternalServerError)
		return
	}
	if owner == nil || owner.AvatarFileID == "" || owner.AvatarUpdatedAt == nil {
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}

	tag := `"` + strconv.FormatInt(owner.AvatarUpdatedAt.UnixNano(), 10) + `-` + strconv.Itoa(sidePixels) + `"`
	response.Header().Set("ETag", tag)
	// Private, because who may see a picture is decided per caller. A day,
	// since a changed picture is a new address (?v=) and never needs this
	// copy to expire.
	response.Header().Set("Cache-Control", "private, max-age=86400")
	if matchesTag(request.Header.Get("If-None-Match"), tag) {
		response.WriteHeader(http.StatusNotModified)
		return
	}

	content, err := self.storage.GetFile(request.Context(), owner.AvatarFileID)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			log.Errorf("cannot read the picture file %s: %s", owner.AvatarFileID, err)
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
		http.Error(response, "no such picture", http.StatusNotFound)
		return
	}
	if sidePixels != avatar.SidePixels {
		if content, err = avatar.Resize(content, sidePixels); err != nil {
			log.Errorf("cannot resize the picture file %s: %s", owner.AvatarFileID, err)
			http.Error(response, "cannot read that just now", http.StatusInternalServerError)
			return
		}
	}

	response.Header().Set("Content-Type", "image/png")
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	// It is a picture and nothing else, whatever the file turns out to hold.
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("Content-Disposition", "inline")
	if request.Method == http.MethodHead {
		return
	}
	if _, err := response.Write(content); err != nil {
		log.Debugf("a person's picture could not be written: %s", err)
	}
}
