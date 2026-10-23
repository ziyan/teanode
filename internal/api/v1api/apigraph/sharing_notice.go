package apigraph

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
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

// shareNoticeCacheLimit is how many shares the cache remembers at most.
const shareNoticeCacheLimit = 4096

// shareNoticeCache remembers when each person was last told each share. It is
// kept in memory: a restart forgets it, which costs one extra notice, and
// another instance of the server does not share it.
type shareNoticeCache struct {
	mutex  sync.Mutex
	sentAt map[string]time.Time
}

func shareNoticeKey(shareKey, toldUserId string) string {
	return shareKey + "\x00" + toldUserId
}

// isDue says whether the person may be told about the share now: they were not
// told it in the last shareNoticeInterval.
func (self *shareNoticeCache) isDue(shareKey, toldUserId string, now time.Time) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	last, isKept := self.sentAt[shareNoticeKey(shareKey, toldUserId)]
	return !isKept || now.Sub(last) >= shareNoticeInterval
}

// record counts the person as told about the share. It is called once the note
// is committed, so a note that failed to write, or a transaction that rolled
// back, does not stop the next attempt from telling them.
func (self *shareNoticeCache) record(shareKey, toldUserId string, now time.Time) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sentAt == nil {
		self.sentAt = map[string]time.Time{}
	}
	key := shareNoticeKey(shareKey, toldUserId)
	if _, isKept := self.sentAt[key]; !isKept && len(self.sentAt) >= shareNoticeCacheLimit {
		for oldKey, last := range self.sentAt {
			if now.Sub(last) >= shareNoticeInterval {
				delete(self.sentAt, oldKey)
			}
		}
		// Still full of fresh entries: the oldest goes.
		if len(self.sentAt) >= shareNoticeCacheLimit {
			oldestKey, oldestAt := "", now
			for oldKey, last := range self.sentAt {
				if oldestKey == "" || last.Before(oldestAt) {
					oldestKey, oldestAt = oldKey, last
				}
			}
			delete(self.sentAt, oldestKey)
		}
	}
	self.sentAt[key] = now
}

// permissionToUseShare is the permission a person needs to read a kind of
// share. Somebody who lacks it could not open what they are told about. A
// memory page and a skill reach only people who may use the agent.
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
	case models.ShareKindMemoryPage, models.ShareKindSkill, models.ShareKindRecording, models.ShareKindFinance:
		return models.PermissionAgentUse
	}
	return models.PermissionCircleUse
}

// isAccountActive says whether the account exists and may sign in. A disabled
// account is told nothing: nobody reads its notes, and its phones are silent.
func isAccountActive(tx db.Transaction, userId string) (bool, error) {
	found, err := tx.GetUser(userId)
	if err != nil {
		return false, err
	}
	return found != nil && !found.Disabled(), nil
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
	shareKey := strings.Join(append([]string{sharer.ID, string(note.ShareKind), note.ShareSubjectID, note.ShareSubject, note.ShareLevel}, note.ShareScopes...), "\x00")
	notifier := self.phoneNotifier()
	now := time.Now()
	for _, toldUserId := range toldUserIds {
		if toldUserId == sharer.ID {
			continue
		}
		isActive, err := isAccountActive(tx, toldUserId)
		if err != nil {
			return err
		}
		if !isActive {
			continue
		}
		permissions, err := tx.EffectivePermissions(toldUserId)
		if err != nil {
			return err
		}
		if permissions == nil || !permissions.Has(permissionToUseShare(note.ShareKind)) {
			continue
		}
		if !self.shareNotices.isDue(shareKey, toldUserId, now) {
			continue
		}
		agentId, err := agent.NoteSharing(tx, toldUserId, models.NoteShareReceived, detail)
		if err != nil {
			return err
		}
		tx.AfterCommit(func() { self.shareNotices.record(shareKey, toldUserId, now) })
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
		isActive, err := isAccountActive(tx, owner.OwnerUserID)
		if err != nil {
			return err
		}
		if !isActive {
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
