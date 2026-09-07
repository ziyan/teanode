package db

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// RoleOperation changes the role table, in a transaction and with an audit row
// for every change.
type RoleOperation interface {
	ListRoles() ([]*models.Role, error)
	GetRole(roleId string) (*models.Role, error)
	GetRoleByName(name string) (*models.Role, error)
	CreateRole(role *models.Role) (*models.Role, error)
	UpdateRole(roleId string, modify func(*models.Role) error) (*models.Role, error)
	DeleteRole(roleId string) error
}

type roleModel struct {
	ID          string    `gorm:"column:id;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
}

func (roleModel) TableName() string { return "role" }

type rolePermissionModel struct {
	RoleID        string `gorm:"column:role_id;primaryKey"`
	PermissionKey string `gorm:"column:permission_key;primaryKey"`
}

func (rolePermissionModel) TableName() string { return "role_permission" }

func roleFromModel(model *roleModel, permissions []models.Permission) *models.Role {
	if permissions == nil {
		permissions = []models.Permission{}
	}
	return &models.Role{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt.In(time.Local),
		ModifiedAt:  model.ModifiedAt.In(time.Local),
		Name:        model.Name,
		Description: model.Description,
		Permissions: permissions,
	}
}

// loadRolePermissions reads the permissions of these roles, by role. A key
// the code does not know is dropped here, so nothing downstream sees it.
func loadRolePermissions(db *gorm.DB, roleIds []string) (map[string][]models.Permission, error) {
	byRole := map[string][]models.Permission{}
	if len(roleIds) == 0 {
		return byRole, nil
	}
	var rows []rolePermissionModel
	if err := db.Where("\"role_id\" IN ?", roleIds).Order("\"permission_key\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		permission := models.Permission(row.PermissionKey)
		if !permission.IsValid() {
			continue
		}
		byRole[row.RoleID] = append(byRole[row.RoleID], permission)
	}
	return byRole, nil
}

func (self *transaction) getRole(condition string, value string) (*models.Role, error) {
	var model roleModel
	result := self.tx.Where(condition, value).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	permissions, err := loadRolePermissions(self.tx, []string{model.ID})
	if err != nil {
		return nil, err
	}
	return roleFromModel(&model, permissions[model.ID]), nil
}

func (self *transaction) GetRole(roleId string) (*models.Role, error) {
	if roleId == "" {
		return nil, nil
	}
	return self.getRole("\"id\" = ?", roleId)
}

func (self *transaction) GetRoleByName(name string) (*models.Role, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	return self.getRole("lower(\"name\") = lower(?)", name)
}

func (self *transaction) ListRoles() ([]*models.Role, error) {
	var rows []roleModel
	if err := self.tx.Order("lower(\"name\") ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	roleIds := make([]string, 0, len(rows))
	for _, row := range rows {
		roleIds = append(roleIds, row.ID)
	}
	permissions, err := loadRolePermissions(self.tx, roleIds)
	if err != nil {
		return nil, err
	}
	roles := make([]*models.Role, 0, len(rows))
	for index := range rows {
		roles = append(roles, roleFromModel(&rows[index], permissions[rows[index].ID]))
	}
	return roles, nil
}

func (self *transaction) CreateRole(role *models.Role) (*models.Role, error) {
	if err := role.Validate(); err != nil {
		return nil, err
	}
	if existing, err := self.GetRoleByName(role.Name); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrAlreadyExists
	}
	now := time.Now()
	created := *role
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	if err := self.applyMutation(models.AuditResourceRole, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		if err := tx.Create(&roleModel{ID: created.ID, CreatedAt: now, ModifiedAt: now, Name: created.Name, Description: created.Description}).Error; err != nil {
			return err
		}
		return replaceRolePermissions(tx, created.ID, created.Permissions)
	}); err != nil {
		return nil, err
	}
	return self.GetRole(created.ID)
}

func (self *transaction) UpdateRole(roleId string, modify func(*models.Role) error) (*models.Role, error) {
	if err := lockRow(self.tx, &roleModel{}, roleId); err != nil {
		return nil, err
	}
	before, err := self.GetRole(roleId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.Permissions = append([]models.Permission(nil), before.Permissions...)
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	if !strings.EqualFold(after.Name, before.Name) {
		if other, err := self.GetRoleByName(after.Name); err != nil {
			return nil, err
		} else if other != nil && other.ID != roleId {
			return nil, ErrAlreadyExists
		}
	}
	after.ID = before.ID
	after.CreatedAt = before.CreatedAt
	after.ModifiedAt = time.Now()
	if err := self.applyMutation(models.AuditResourceRole, roleId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		if err := tx.Model(&roleModel{}).Where("\"id\" = ?", roleId).Updates(map[string]any{
			"modified_at": after.ModifiedAt, "name": after.Name, "description": after.Description,
		}).Error; err != nil {
			return err
		}
		return replaceRolePermissions(tx, roleId, after.Permissions)
	}); err != nil {
		return nil, err
	}
	return self.GetRole(roleId)
}

func (self *transaction) DeleteRole(roleId string) error {
	before, err := self.GetRole(roleId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceRole, roleId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", roleId).Delete(&roleModel{}).Error
	})
}

func replaceRolePermissions(tx *gorm.DB, roleId string, permissions []models.Permission) error {
	if err := tx.Where("\"role_id\" = ?", roleId).Delete(&rolePermissionModel{}).Error; err != nil {
		return err
	}
	seen := map[models.Permission]bool{}
	for _, permission := range permissions {
		if seen[permission] || !permission.IsValid() {
			continue
		}
		seen[permission] = true
		if err := tx.Create(&rolePermissionModel{RoleID: roleId, PermissionKey: string(permission)}).Error; err != nil {
			return err
		}
	}
	return nil
}
