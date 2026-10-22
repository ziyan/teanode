package apigraph

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/models"
)

// A person is told when something is shared with them, or shared more widely,
// and an owner is told when somebody joins a circle their shares reach. Both
// are a note in the main conversation of the person's agent and a notification
// on their phones of kind circle, the way an invitation to a circle is told
// (agent/sharing_note.go). The sharer is never told about their own share.
//
// Passwords are not told here: a recipient waits for the owner's device to
// seal each item a key, and that waiting state is what the owner sees.

// shareNoticeInterval is how long a person is not told the same thing again:
// the same share at the same level by the same sharer. Moving a level back and
// forth would otherwise be a notification for every tap.
const shareNoticeInterval = 10 * time.Minute

// shareNoticeCache remembers when each person was last told each share. It is
// kept in memory: a restart forgets it, which costs one extra notice, and
// another instance of the server does not share it.
type shareNoticeCache struct {
	mutex  sync.Mutex
	sentAt map[string]time.Time
}

// take says whether the person may be told about the share now, and if so
// counts them as told.
func (self *shareNoticeCache) take(shareKey, toldUserId string, now time.Time) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sentAt == nil {
		self.sentAt = map[string]time.Time{}
	}
	key := shareKey + "\x00" + toldUserId
	if last, isKept := self.sentAt[key]; isKept && now.Sub(last) < shareNoticeInterval {
		return false
	}
	if len(self.sentAt) >= 4096 {
		for oldKey, last := range self.sentAt {
			if now.Sub(last) >= shareNoticeInterval {
				delete(self.sentAt, oldKey)
			}
		}
	}
	self.sentAt[key] = now
	return true
}

// permissionToUseShare is the permission a person needs to read a kind of
// share. Somebody who lacks it could not open what they are told about.
func permissionToUseShare(shareKind models.ShareKind) models.Permission {
	switch shareKind {
	case models.ShareKindCalendar:
		return models.PermissionCalendarUse
	case models.ShareKindLocation:
		return models.PermissionLocationUse
	case models.ShareKindHealth:
		return models.PermissionHealthUse
	case models.ShareKindPhotoAlbum:
		return models.PermissionPhotosUse
	}
	return models.PermissionCircleUse
}

// tellAboutShare tells everybody a share now reaches, except the sharer: the
// grantee, or the members of the grantee circle. The note is written in this
// transaction and the phones are told once it commits. A person who may not
// use that kind of share is not told. It returns the first error of writing a
// note, which fails the share with it, as an invitation's note does.
func (self *graph) tellAboutShare(ctx context.Context, sharer *models.User, note models.ShareReceivedNote, granteeUserId, granteeCircleId string) error {
	tx := self.transaction(ctx)
	toldUserIds := []string{}
	if granteeUserId != "" {
		toldUserIds = append(toldUserIds, granteeUserId)
	} else {
		circle, err := tx.GetCircle(granteeCircleId)
		if err != nil || circle == nil {
			return err
		}
		toldUserIds = circleMemberIds(circle)
	}
	detail := models.ShareReceivedDetail(note)
	shareKey := strings.Join(append([]string{sharer.ID, string(note.ShareKind), note.ShareSubject, note.ShareLevel}, note.ShareScopes...), "\x00")
	notifier := self.phoneNotifier()
	now := time.Now()
	for _, toldUserId := range toldUserIds {
		if toldUserId == sharer.ID {
			continue
		}
		permissions, err := tx.EffectivePermissions(toldUserId)
		if err != nil {
			return err
		}
		if permissions == nil || !permissions.Has(permissionToUseShare(note.ShareKind)) {
			continue
		}
		if !self.shareNotices.take(shareKey, toldUserId, now) {
			continue
		}
		agentId, err := agent.NoteSharing(tx, toldUserId, models.NoteShareReceived, detail)
		if err != nil {
			return err
		}
		if notifier != nil && agentId != "" {
			tx.AfterCommit(func() { notifier.NotifySharing(agentId, models.NoteShareReceived, detail) })
		}
	}
	return nil
}

// tellOwnersAboutJoin tells each member of a circle who has shared something
// with it that a person joined and can now see it: one note for each owner,
// listing the kinds. What the joiner could not use anyway is left out of the
// list, and an owner left with nothing to list is not told.
func (self *graph) tellOwnersAboutJoin(ctx context.Context, joined *models.User, circle *models.Circle) error {
	tx := self.transaction(ctx)
	owners, err := tx.ListCircleShareOwners(circle.ID)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		return nil
	}
	joinerPermissions, err := tx.EffectivePermissions(joined.ID)
	if err != nil {
		return err
	}
	if joinerPermissions == nil {
		return nil
	}
	notifier := self.phoneNotifier()
	for _, owner := range owners {
		if owner.OwnerUserID == joined.ID {
			continue
		}
		shareKinds := []models.ShareKind{}
		for _, shareKind := range owner.ShareKinds {
			if joinerPermissions.Has(permissionToUseShare(shareKind)) {
				shareKinds = append(shareKinds, shareKind)
			}
		}
		if len(shareKinds) == 0 {
			continue
		}
		detail := models.ShareReachedDetail(models.ShareReachedNote{MemberName: joined.DisplayName(), CircleName: circle.Name, ShareKinds: shareKinds})
		agentId, err := agent.NoteSharing(tx, owner.OwnerUserID, models.NoteShareReached, detail)
		if err != nil {
			return err
		}
		if notifier != nil && agentId != "" {
			tx.AfterCommit(func() { notifier.NotifySharing(agentId, models.NoteShareReached, detail) })
		}
	}
	return nil
}
