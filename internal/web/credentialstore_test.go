package web_test

import (
	"sort"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// memoryStore is a CredentialStore in a map.
//
// The authenticator's own behaviour — what a wrong secret does, when an
// expired session stops working, what logging out ends — is worth testing
// without a PostgreSQL in the way. That the same operations behave against a
// real one is covered in internal/db.
type memoryStore struct {
	mutex    sync.Mutex
	sessions map[string]*storedCredential
	tokens   map[string]*storedCredential
}

type storedCredential struct {
	keyHash string
	session *models.Session
	token   *models.Token
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		sessions: map[string]*storedCredential{},
		tokens:   map[string]*storedCredential{},
	}
}

func (self *memoryStore) CreateSession(session *models.Session, keyHash string) (*models.Session, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	stored := *session
	stored.CreatedAt = time.Now()
	stored.ModifiedAt = stored.CreatedAt
	self.sessions[session.ID] = &storedCredential{keyHash: keyHash, session: &stored}
	return &stored, nil
}

func (self *memoryStore) GetSession(sessionId string) (*models.Session, string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, ok := self.sessions[sessionId]
	if !ok {
		return nil, "", nil
	}
	copied := *found.session
	return &copied, found.keyHash, nil
}

func (self *memoryStore) ListSessions(userId string, options *db.SessionOptions) ([]*models.Session, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var sessions []*models.Session
	for _, found := range self.sessions {
		if found.session.UserID != userId {
			continue
		}
		if !found.session.RevokedAt.IsZero() && (options == nil || !options.IncludeRevoked) {
			continue
		}
		copied := *found.session
		sessions = append(sessions, &copied)
	}
	sort.Slice(sessions, func(one, two int) bool {
		return sessions[one].CreatedAt.After(sessions[two].CreatedAt)
	})
	return sessions, nil
}

func (self *memoryStore) TouchSession(sessionId string, at time.Time, ip, userAgent string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if found, ok := self.sessions[sessionId]; ok {
		found.session.UsedAt = at
		found.session.IP = ip
		found.session.UserAgent = userAgent
	}
	return nil
}

func (self *memoryStore) RevokeSession(sessionId string, at time.Time) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if found, ok := self.sessions[sessionId]; ok && found.session.RevokedAt.IsZero() {
		found.session.RevokedAt = at
	}
	return nil
}

func (self *memoryStore) RevokeSessionsByUser(userId string, at time.Time, except string) (int64, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var ended int64
	for id, found := range self.sessions {
		if found.session.UserID != userId || id == except || !found.session.RevokedAt.IsZero() {
			continue
		}
		found.session.RevokedAt = at
		ended++
	}
	return ended, nil
}

func (self *memoryStore) ScavengeSessions(now time.Time) (int64, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var removed int64
	for id, found := range self.sessions {
		if !found.session.ExpiresAt.IsZero() && found.session.ExpiresAt.Before(now) {
			delete(self.sessions, id)
			removed++
		}
	}
	return removed, nil
}

func (self *memoryStore) CreateToken(token *models.Token, keyHash string) (*models.Token, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	stored := *token
	stored.CreatedAt = time.Now()
	stored.ModifiedAt = stored.CreatedAt
	self.tokens[token.ID] = &storedCredential{keyHash: keyHash, token: &stored}
	return &stored, nil
}

func (self *memoryStore) GetToken(tokenId string) (*models.Token, string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, ok := self.tokens[tokenId]
	if !ok {
		return nil, "", nil
	}
	copied := *found.token
	return &copied, found.keyHash, nil
}

func (self *memoryStore) ListTokens(userId string, options *db.SessionOptions) ([]*models.Token, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var tokens []*models.Token
	for _, found := range self.tokens {
		if found.token.UserID != userId {
			continue
		}
		if !found.token.RevokedAt.IsZero() && (options == nil || !options.IncludeRevoked) {
			continue
		}
		copied := *found.token
		tokens = append(tokens, &copied)
	}
	sort.Slice(tokens, func(one, two int) bool {
		return tokens[one].CreatedAt.After(tokens[two].CreatedAt)
	})
	return tokens, nil
}

func (self *memoryStore) TouchToken(tokenId string, at time.Time, ip, userAgent string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if found, ok := self.tokens[tokenId]; ok {
		found.token.UsedAt = at
		found.token.IP = ip
		found.token.UserAgent = userAgent
	}
	return nil
}

func (self *memoryStore) RevokeToken(tokenId string, at time.Time) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if found, ok := self.tokens[tokenId]; ok && found.token.RevokedAt.IsZero() {
		found.token.RevokedAt = at
	}
	return nil
}

func (self *memoryStore) RevokeTokensByUser(userId string, at time.Time) (int64, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var ended int64
	for _, found := range self.tokens {
		if found.token.UserID == userId && found.token.RevokedAt.IsZero() {
			found.token.RevokedAt = at
			ended++
		}
	}
	return ended, nil
}

func (self *memoryStore) ScavengeTokens(now time.Time) (int64, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var removed int64
	for id, found := range self.tokens {
		if !found.token.ExpiresAt.IsZero() && found.token.ExpiresAt.Before(now) {
			delete(self.tokens, id)
			removed++
		}
	}
	return removed, nil
}
