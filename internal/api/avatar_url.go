package api

import (
	"fmt"

	"github.com/ziyan/teanode/internal/models"
)

// UserAvatarURL is where a person's picture is served, or nil when they have
// none. The time it changed is in the address: a browser keeps the picture
// for a day without asking, and a new picture has to be a new address for
// that to be safe.
func UserAvatarURL(user *models.User) *string {
	if user == nil || user.AvatarFileID == "" || user.AvatarUpdatedAt == nil {
		return nil
	}
	address := fmt.Sprintf("%s/avatar/user/%s?v=%d", Prefix, user.ID, user.AvatarUpdatedAt.Unix())
	return &address
}
