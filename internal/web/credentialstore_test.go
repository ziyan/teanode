package web_test

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
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
	users    map[string]*models.User
	roles    []*models.Role
	groups   []*models.Group
}

type storedCredential struct {
	keyHash string
	session *models.Session
	token   *models.Token
}

func newMemoryStore(users ...*models.User) *memoryStore {
	self := &memoryStore{
		sessions: map[string]*storedCredential{},
		tokens:   map[string]*storedCredential{},
		users:    map[string]*models.User{},
	}
	for _, user := range users {
		self.addUser(user)
	}
	return self
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

// The user table, in the same map. The authenticator looks accounts up on
// every request and writes two of them — claiming the server, and a password
// change — through a transaction; the fake honours exactly that much, and
// what seeding the roles and groups needs, and panics on anything else so
// that a new dependency is noticed rather than silently absent.

func (self *memoryStore) addUser(user *models.User) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.users == nil {
		self.users = map[string]*models.User{}
	}
	copied := *user
	if copied.ID == "" {
		copied.ID = security.NewULID()
	}
	self.users[copied.ID] = &copied
}

func (self *memoryStore) removeUser(username string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for id, user := range self.users {
		if strings.EqualFold(user.Username, username) {
			delete(self.users, id)
		}
	}
}

func (self *memoryStore) GetUser(userId string) (*models.User, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if user, ok := self.users[userId]; ok {
		copied := *user
		return &copied, nil
	}
	return nil, nil
}

func (self *memoryStore) GetUserByUsername(username string) (*models.User, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, user := range self.users {
		if strings.EqualFold(user.Username, username) {
			copied := *user
			return &copied, nil
		}
	}
	return nil, nil
}

func (self *memoryStore) CountUsers() (int64, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return int64(len(self.users)), nil
}

func (self *memoryStore) TransactionContext(_ context.Context, function func(db.Transaction) error) error {
	return function(&memoryTransaction{store: self})
}

// memoryTransaction is the handful of writes the authenticator makes, over
// the same map. The embedded interface is nil: any method not written here
// panics, which is the point.
type memoryTransaction struct {
	db.Transaction
	store *memoryStore
}

func (self *memoryTransaction) GetUser(userId string) (*models.User, error) {
	return self.store.GetUser(userId)
}

func (self *memoryTransaction) GetUserByUsername(username string) (*models.User, error) {
	return self.store.GetUserByUsername(username)
}

func (self *memoryTransaction) CountUsers() (int64, error) {
	return self.store.CountUsers()
}

func (self *memoryTransaction) ListUsers() ([]*models.User, error) {
	self.store.mutex.Lock()
	defer self.store.mutex.Unlock()
	users := make([]*models.User, 0, len(self.store.users))
	for _, user := range self.store.users {
		copied := *user
		users = append(users, &copied)
	}
	return users, nil
}

func (self *memoryTransaction) CreateUser(user *models.User) (*models.User, error) {
	if existing, _ := self.store.GetUserByUsername(user.Username); existing != nil {
		return nil, db.ErrAlreadyExists
	}
	self.store.addUser(user)
	return self.store.GetUserByUsername(user.Username)
}

func (self *memoryTransaction) UpdateUser(userId string, modify func(*models.User) error) (*models.User, error) {
	self.store.mutex.Lock()
	defer self.store.mutex.Unlock()
	user, ok := self.store.users[userId]
	if !ok {
		return nil, db.ErrNotFound
	}
	if err := modify(user); err != nil {
		return nil, err
	}
	copied := *user
	return &copied, nil
}

func (self *memoryTransaction) ListRoles() ([]*models.Role, error)   { return self.store.roles, nil }
func (self *memoryTransaction) ListGroups() ([]*models.Group, error) { return self.store.groups, nil }

func (self *memoryTransaction) CreateRole(role *models.Role) (*models.Role, error) {
	copied := *role
	copied.ID = security.NewULID()
	self.store.roles = append(self.store.roles, &copied)
	return &copied, nil
}

func (self *memoryTransaction) CreateGroup(group *models.Group) (*models.Group, error) {
	copied := *group
	copied.ID = security.NewULID()
	self.store.groups = append(self.store.groups, &copied)
	return &copied, nil
}

func (self *memoryTransaction) GetGroupByName(name string) (*models.Group, error) {
	for _, group := range self.store.groups {
		if strings.EqualFold(group.Name, name) {
			return group, nil
		}
	}
	return nil, nil
}

func (self *memoryTransaction) UpdateGroup(groupId string, modify func(*models.Group) error) (*models.Group, error) {
	for _, group := range self.store.groups {
		if group.ID == groupId {
			return group, modify(group)
		}
	}
	return nil, db.ErrNotFound
}
