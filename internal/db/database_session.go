package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// TouchInterval is how stale "last used" is allowed to get before a request
// writes it back.
//
// Writing it on every request would put a row update in front of every page
// the dashboard loads, for a column nobody reads more precisely than "this
// morning". A minute is far finer than the list shows and turns a busy
// session from one write per request into one per minute.
//
// The comparison is against the stored value rather than anything held in
// memory, so several instances do not each keep their own idea of when it was
// last written.
const TouchInterval = time.Minute

// SessionOperation is what the authenticator needs of the session table.
//
// Deliberately narrow. There is no general Modify here: a session is created,
// looked at, touched and ended, and offering more would invite a caller to
// change who a session belongs to.
type SessionOperation interface {
	// CreateSession stores a new session. The hash of the secret half is
	// passed separately and is never part of the model: the model is what
	// the API returns, and a hash that reaches the dashboard is one somebody
	// can work on offline.
	CreateSession(session *models.Session, keyHash string) (*models.Session, error)

	// GetSession returns one by identifier together with that hash, or nil
	// when there is no such row. Revoked and expired sessions are returned;
	// deciding what to do about them is the caller's.
	GetSession(sessionId string) (*models.Session, string, error)

	// ListSessions returns an account's sessions, newest first.
	ListSessions(userId string, options *SessionOptions) ([]*models.Session, error)

	// TouchSession records that a session was used, from an address and a
	// user agent, unless it was already recorded within TouchInterval.
	TouchSession(sessionId string, at time.Time, ip, userAgent string) error

	// RevokeSession ends one session. Ending one that is already ended is not
	// an error.
	RevokeSession(sessionId string, at time.Time) error

	// RevokeSessionsByUser ends every session an account has, and returns how
	// many it ended. Excluding one, when the caller wants to keep the session
	// they are using.
	RevokeSessionsByUser(userId string, at time.Time, except string) (int64, error)

	// ScavengeSessions removes rows that are no longer worth keeping.
	ScavengeSessions(now time.Time) (int64, error)
}

// SessionOptions narrows a listing.
type SessionOptions struct {
	// IncludeRevoked returns ended sessions as well, which is what the list
	// shows so that "revoked an hour ago" is visible rather than the row
	// disappearing.
	IncludeRevoked bool

	// Limit bounds the rows returned. Zero means the default.
	Limit int
}

const defaultSessionLimit = 200

func (self *SessionOptions) limit() int {
	if self == nil || self.Limit <= 0 {
		return defaultSessionLimit
	}
	return self.Limit
}

func (self *SessionOptions) includeRevoked() bool {
	return self != nil && self.IncludeRevoked
}

// revokedRetention is how long an ended session or token is kept so that the
// list can show it was ended, before the sweep removes it.
const revokedRetention = 30 * 24 * time.Hour

// expiredRetention is how long a session is kept after it stops working. A
// short grace, so that somebody who was logged out by an expiry can see that
// is what happened.
const expiredRetention = 24 * time.Hour

type sessionModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	UserID  string `gorm:"column:user_id;size:32;index"`
	KeyHash string `gorm:"size:64"`

	ExpiresAt *time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time

	IP        string `gorm:"column:ip;size:64"`
	UserAgent string `gorm:"type:text"`
}

func (self *sessionModel) TableName() string {
	return "session"
}

func timeOrNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	stored := value.UTC()
	return &stored
}

func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.In(time.Local)
}

func sessionFromModel(model *sessionModel) *models.Session {
	return &models.Session{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		UserID:     model.UserID,
		ExpiresAt:  timeOrZero(model.ExpiresAt),
		UsedAt:     timeOrZero(model.UsedAt),
		RevokedAt:  timeOrZero(model.RevokedAt),
		IP:         model.IP,
		UserAgent:  model.UserAgent,
	}
}

func (self *database) CreateSession(session *models.Session, keyHash string) (*models.Session, error) {
	now := time.Now()
	model := &sessionModel{
		ID:         session.ID,
		CreatedAt:  now,
		ModifiedAt: now,
		UserID:     session.UserID,
		KeyHash:    keyHash,
		ExpiresAt:  timeOrNil(session.ExpiresAt),
		UsedAt:     timeOrNil(session.UsedAt),
		IP:         session.IP,
		UserAgent:  session.UserAgent,
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return sessionFromModel(model), nil
}

func (self *database) GetSession(sessionId string) (*models.Session, string, error) {
	var model sessionModel
	if err := self.db.First(&model, "\"id\" = ?", sessionId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", nil
		}
		return nil, "", err
	}
	return sessionFromModel(&model), model.KeyHash, nil
}

func (self *database) ListSessions(userId string, options *SessionOptions) ([]*models.Session, error) {
	query := self.db.Where("\"user_id\" = ?", userId)
	if !options.includeRevoked() {
		query = query.Where("\"revoked_at\" IS NULL")
	}

	var found []sessionModel
	if err := query.Order("\"created_at\" DESC").Limit(options.limit()).Find(&found).Error; err != nil {
		return nil, err
	}

	sessions := make([]*models.Session, 0, len(found))
	for index := range found {
		sessions = append(sessions, sessionFromModel(&found[index]))
	}
	return sessions, nil
}

func (self *database) TouchSession(sessionId string, at time.Time, ip, userAgent string) error {
	// Guarded on used_at as well as the identifier, so that two instances
	// touching the same session in the same second do not both write, and a
	// flush that lost a race cannot move the column backwards.
	return self.db.Model(&sessionModel{}).
		Where("\"id\" = ? AND (\"used_at\" IS NULL OR \"used_at\" <= ?)", sessionId, at.Add(-TouchInterval)).
		Updates(map[string]any{
			"used_at":     at.UTC(),
			"ip":          ip,
			"user_agent":  userAgent,
			"modified_at": at.UTC(),
		}).Error
}

func (self *database) RevokeSession(sessionId string, at time.Time) error {
	return self.db.Model(&sessionModel{}).
		Where("\"id\" = ? AND \"revoked_at\" IS NULL", sessionId).
		Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()}).Error
}

func (self *database) RevokeSessionsByUser(userId string, at time.Time, except string) (int64, error) {
	query := self.db.Model(&sessionModel{}).Where("\"user_id\" = ? AND \"revoked_at\" IS NULL", userId)
	if except != "" {
		query = query.Where("\"id\" <> ?", except)
	}
	result := query.Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()})
	return result.RowsAffected, result.Error
}

func (self *database) ScavengeSessions(now time.Time) (int64, error) {
	result := self.db.Where(
		"(\"expires_at\" IS NOT NULL AND \"expires_at\" < ?) OR (\"revoked_at\" IS NOT NULL AND \"revoked_at\" < ?)",
		now.Add(-expiredRetention), now.Add(-revokedRetention),
	).Delete(&sessionModel{})
	return result.RowsAffected, result.Error
}
