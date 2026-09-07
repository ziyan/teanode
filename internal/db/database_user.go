package db

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// UserLookup is what authenticating a request needs of the user table, read
// outside any transaction because every request does it.
type UserLookup interface {
	// GetUser returns one by identifier, or nil.
	GetUser(userId string) (*models.User, error)

	// GetUserByUsername returns one by the name they sign in with, without
	// regard to case, or nil.
	GetUserByUsername(username string) (*models.User, error)

	// CountUsers says whether this server has been claimed: zero means the
	// next visitor is asked to create the first account.
	CountUsers() (int64, error)
}

// UserOperation changes the user table, in a transaction and with an audit
// row for every change.
type UserOperation interface {
	UserLookup

	ListUsers() ([]*models.User, error)
	CreateUser(user *models.User) (*models.User, error)

	// UpdateUser applies a change to the row as it stands, locked for the
	// duration, and records the before and after.
	UpdateUser(userId string, modify func(*models.User) error) (*models.User, error)

	DeleteUser(userId string) error
}

type userModel struct {
	ID           string     `gorm:"column:id;primaryKey"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	ModifiedAt   time.Time  `gorm:"column:modified_at"`
	Username     string     `gorm:"column:username"`
	Name         string     `gorm:"column:name"`
	PasswordHash *string    `gorm:"column:password_hash"`
	Email        string     `gorm:"column:email"`
	DisabledAt   *time.Time `gorm:"column:disabled_at"`
	Locale       string     `gorm:"column:locale"`
}

// "user" is a reserved word in PostgreSQL, so it is quoted. Every identifier
// in this project is quoted already.
func (userModel) TableName() string { return "user" }

type userGroupModel struct {
	UserID  string `gorm:"column:user_id;primaryKey"`
	GroupID string `gorm:"column:group_id;primaryKey"`
}

func (userGroupModel) TableName() string { return "user_group" }

func userFromModel(model *userModel, groupIds []string) *models.User {
	user := &models.User{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		Username:   model.Username,
		Name:       model.Name,
		Email:      model.Email,
		Locale:     model.Locale,
		GroupIDs:   groupIds,
	}
	if model.PasswordHash != nil {
		user.PasswordHash = *model.PasswordHash
	}
	if model.DisabledAt != nil {
		disabledAt := model.DisabledAt.In(time.Local)
		user.DisabledAt = &disabledAt
	}
	if user.GroupIDs == nil {
		user.GroupIDs = []string{}
	}
	return user
}

func userToModel(user *models.User) *userModel {
	model := &userModel{
		ID:         user.ID,
		CreatedAt:  user.CreatedAt,
		ModifiedAt: user.ModifiedAt,
		Username:   user.Username,
		Name:       user.Name,
		Email:      user.Email,
		Locale:     user.Locale,
		DisabledAt: user.DisabledAt,
	}
	if user.PasswordHash != "" {
		hash := user.PasswordHash
		model.PasswordHash = &hash
	}
	return model
}

// loadUserGroups reads the memberships of these users, by user.
func loadUserGroups(db *gorm.DB, userIds []string) (map[string][]string, error) {
	byUser := map[string][]string{}
	if len(userIds) == 0 {
		return byUser, nil
	}
	var rows []userGroupModel
	if err := db.Where("\"user_id\" IN ?", userIds).Order("\"group_id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		byUser[row.UserID] = append(byUser[row.UserID], row.GroupID)
	}
	return byUser, nil
}

func getUser(db *gorm.DB, condition string, value string) (*models.User, error) {
	var model userModel
	result := db.Where(condition, value).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	groups, err := loadUserGroups(db, []string{model.ID})
	if err != nil {
		return nil, err
	}
	return userFromModel(&model, groups[model.ID]), nil
}

func getUserByUsername(db *gorm.DB, username string) (*models.User, error) {
	if strings.TrimSpace(username) == "" {
		return nil, nil
	}
	return getUser(db, "lower(\"username\") = lower(?)", username)
}

func countUsers(db *gorm.DB) (int64, error) {
	var count int64
	err := db.Model(&userModel{}).Count(&count).Error
	return count, err
}

func (self *database) GetUser(userId string) (*models.User, error) {
	if userId == "" {
		return nil, nil
	}
	return getUser(self.db, "\"id\" = ?", userId)
}

func (self *database) GetUserByUsername(username string) (*models.User, error) {
	return getUserByUsername(self.db, username)
}

func (self *database) CountUsers() (int64, error) {
	return countUsers(self.db)
}

func (self *transaction) GetUser(userId string) (*models.User, error) {
	if userId == "" {
		return nil, nil
	}
	return getUser(self.tx, "\"id\" = ?", userId)
}

func (self *transaction) GetUserByUsername(username string) (*models.User, error) {
	return getUserByUsername(self.tx, username)
}

func (self *transaction) CountUsers() (int64, error) {
	return countUsers(self.tx)
}

func (self *transaction) ListUsers() ([]*models.User, error) {
	var rows []userModel
	if err := self.tx.Order("lower(\"username\") ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	userIds := make([]string, 0, len(rows))
	for _, row := range rows {
		userIds = append(userIds, row.ID)
	}
	groups, err := loadUserGroups(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	users := make([]*models.User, 0, len(rows))
	for index := range rows {
		users = append(users, userFromModel(&rows[index], groups[rows[index].ID]))
	}
	return users, nil
}

func (self *transaction) CreateUser(user *models.User) (*models.User, error) {
	if err := user.Validate(); err != nil {
		return nil, err
	}
	if existing, err := self.GetUserByUsername(user.Username); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrAlreadyExists
	}
	now := time.Now()
	created := *user
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	if err := self.applyMutation(models.AuditResourceUser, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		if err := tx.Create(userToModel(&created)).Error; err != nil {
			return err
		}
		return replaceUserGroups(tx, created.ID, created.GroupIDs)
	}); err != nil {
		return nil, err
	}
	return self.GetUser(created.ID)
}

func (self *transaction) UpdateUser(userId string, modify func(*models.User) error) (*models.User, error) {
	if err := lockRow(self.tx, &userModel{}, userId); err != nil {
		return nil, err
	}
	before, err := self.GetUser(userId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.GroupIDs = append([]string(nil), before.GroupIDs...)
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	if !strings.EqualFold(after.Username, before.Username) {
		if other, err := self.GetUserByUsername(after.Username); err != nil {
			return nil, err
		} else if other != nil && other.ID != userId {
			return nil, ErrAlreadyExists
		}
	}
	after.ID = before.ID
	after.CreatedAt = before.CreatedAt
	after.ModifiedAt = time.Now()
	if err := self.applyMutation(models.AuditResourceUser, userId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		model := userToModel(&after)
		if err := tx.Model(&userModel{}).Where("\"id\" = ?", userId).Select("*").Omit("id", "created_at").Updates(model).Error; err != nil {
			return err
		}
		return replaceUserGroups(tx, userId, after.GroupIDs)
	}); err != nil {
		return nil, err
	}
	return self.GetUser(userId)
}

func (self *transaction) DeleteUser(userId string) error {
	before, err := self.GetUser(userId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceUser, userId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", userId).Delete(&userModel{}).Error
	})
}

// replaceUserGroups makes the memberships of a user exactly this list.
func replaceUserGroups(tx *gorm.DB, userId string, groupIds []string) error {
	if err := tx.Where("\"user_id\" = ?", userId).Delete(&userGroupModel{}).Error; err != nil {
		return err
	}
	for _, groupId := range uniqueStrings(groupIds) {
		if err := tx.Create(&userGroupModel{UserID: userId, GroupID: groupId}).Error; err != nil {
			return err
		}
	}
	return nil
}
