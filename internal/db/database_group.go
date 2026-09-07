package db

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// GroupOperation changes the group table and the three tables that hang off
// it, in a transaction and with an audit row for every change.
type GroupOperation interface {
	ListGroups() ([]*models.Group, error)
	GetGroup(groupId string) (*models.Group, error)
	GetGroupByName(name string) (*models.Group, error)
	CreateGroup(group *models.Group) (*models.Group, error)

	// UpdateGroup applies a change to the group and its users, roles and
	// domains as they stand, and records the before and after.
	UpdateGroup(groupId string, modify func(*models.Group) error) (*models.Group, error)

	DeleteGroup(groupId string) error

	// EffectivePermissions is what a user may do, from every group they are
	// in: the groups' roles crossed with the groups' domains.
	EffectivePermissions(userId string) (*models.EffectivePermissions, error)
}

type groupModel struct {
	ID          string    `gorm:"column:id;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Name        string    `gorm:"column:name"`
	Description string    `gorm:"column:description"`
	IDPGroup    string    `gorm:"column:idp_group"`
}

func (groupModel) TableName() string { return "group" }

type groupRoleModel struct {
	GroupID string `gorm:"column:group_id;primaryKey"`
	RoleID  string `gorm:"column:role_id;primaryKey"`
}

func (groupRoleModel) TableName() string { return "group_role" }

type groupDomainModel struct {
	GroupID  string `gorm:"column:group_id;primaryKey"`
	DomainID string `gorm:"column:domain_id;primaryKey"`
}

func (groupDomainModel) TableName() string { return "group_domain" }

type groupLinks struct {
	users   map[string][]string
	roles   map[string][]string
	domains map[string][]string
}

func loadGroupLinks(db *gorm.DB, groupIds []string) (*groupLinks, error) {
	links := &groupLinks{users: map[string][]string{}, roles: map[string][]string{}, domains: map[string][]string{}}
	if len(groupIds) == 0 {
		return links, nil
	}
	var users []userGroupModel
	if err := db.Where("\"group_id\" IN ?", groupIds).Order("\"user_id\" ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	for _, row := range users {
		links.users[row.GroupID] = append(links.users[row.GroupID], row.UserID)
	}
	var roles []groupRoleModel
	if err := db.Where("\"group_id\" IN ?", groupIds).Order("\"role_id\" ASC").Find(&roles).Error; err != nil {
		return nil, err
	}
	for _, row := range roles {
		links.roles[row.GroupID] = append(links.roles[row.GroupID], row.RoleID)
	}
	var domains []groupDomainModel
	if err := db.Where("\"group_id\" IN ?", groupIds).Order("\"domain_id\" ASC").Find(&domains).Error; err != nil {
		return nil, err
	}
	for _, row := range domains {
		links.domains[row.GroupID] = append(links.domains[row.GroupID], row.DomainID)
	}
	return links, nil
}

func groupFromModel(model *groupModel, links *groupLinks) *models.Group {
	group := &models.Group{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt.In(time.Local),
		ModifiedAt:  model.ModifiedAt.In(time.Local),
		Name:        model.Name,
		Description: model.Description,
		IDPGroup:    model.IDPGroup,
		UserIDs:     links.users[model.ID],
		RoleIDs:     links.roles[model.ID],
		DomainIDs:   links.domains[model.ID],
	}
	if group.UserIDs == nil {
		group.UserIDs = []string{}
	}
	if group.RoleIDs == nil {
		group.RoleIDs = []string{}
	}
	if group.DomainIDs == nil {
		group.DomainIDs = []string{}
	}
	return group
}

func (self *transaction) getGroup(condition string, value string) (*models.Group, error) {
	var model groupModel
	result := self.tx.Where(condition, value).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	links, err := loadGroupLinks(self.tx, []string{model.ID})
	if err != nil {
		return nil, err
	}
	return groupFromModel(&model, links), nil
}

func (self *transaction) GetGroup(groupId string) (*models.Group, error) {
	if groupId == "" {
		return nil, nil
	}
	return self.getGroup("\"id\" = ?", groupId)
}

func (self *transaction) GetGroupByName(name string) (*models.Group, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	return self.getGroup("lower(\"name\") = lower(?)", name)
}

func (self *transaction) ListGroups() ([]*models.Group, error) {
	var rows []groupModel
	if err := self.tx.Order("lower(\"name\") ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	groupIds := make([]string, 0, len(rows))
	for _, row := range rows {
		groupIds = append(groupIds, row.ID)
	}
	links, err := loadGroupLinks(self.tx, groupIds)
	if err != nil {
		return nil, err
	}
	groups := make([]*models.Group, 0, len(rows))
	for index := range rows {
		groups = append(groups, groupFromModel(&rows[index], links))
	}
	return groups, nil
}

func (self *transaction) CreateGroup(group *models.Group) (*models.Group, error) {
	if err := group.Validate(); err != nil {
		return nil, err
	}
	if existing, err := self.GetGroupByName(group.Name); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrAlreadyExists
	}
	now := time.Now()
	created := *group
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	if err := self.applyMutation(models.AuditResourceGroup, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		if err := tx.Create(&groupModel{ID: created.ID, CreatedAt: now, ModifiedAt: now, Name: created.Name, Description: created.Description, IDPGroup: created.IDPGroup}).Error; err != nil {
			return err
		}
		return replaceGroupLinks(tx, created.ID, &created)
	}); err != nil {
		return nil, err
	}
	return self.GetGroup(created.ID)
}

func (self *transaction) UpdateGroup(groupId string, modify func(*models.Group) error) (*models.Group, error) {
	if err := lockRow(self.tx, &groupModel{}, groupId); err != nil {
		return nil, err
	}
	before, err := self.GetGroup(groupId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.UserIDs = append([]string(nil), before.UserIDs...)
	after.RoleIDs = append([]string(nil), before.RoleIDs...)
	after.DomainIDs = append([]string(nil), before.DomainIDs...)
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	if !strings.EqualFold(after.Name, before.Name) {
		if other, err := self.GetGroupByName(after.Name); err != nil {
			return nil, err
		} else if other != nil && other.ID != groupId {
			return nil, ErrAlreadyExists
		}
	}
	after.ID = before.ID
	after.CreatedAt = before.CreatedAt
	after.ModifiedAt = time.Now()
	if err := self.applyMutation(models.AuditResourceGroup, groupId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		if err := tx.Model(&groupModel{}).Where("\"id\" = ?", groupId).Updates(map[string]any{
			"modified_at": after.ModifiedAt, "name": after.Name, "description": after.Description, "idp_group": after.IDPGroup,
		}).Error; err != nil {
			return err
		}
		return replaceGroupLinks(tx, groupId, &after)
	}); err != nil {
		return nil, err
	}
	return self.GetGroup(groupId)
}

func (self *transaction) DeleteGroup(groupId string) error {
	before, err := self.GetGroup(groupId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceGroup, groupId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", groupId).Delete(&groupModel{}).Error
	})
}

func replaceGroupLinks(tx *gorm.DB, groupId string, group *models.Group) error {
	for _, model := range []any{&userGroupModel{}, &groupRoleModel{}, &groupDomainModel{}} {
		if err := tx.Where("\"group_id\" = ?", groupId).Delete(model).Error; err != nil {
			return err
		}
	}
	for _, userId := range uniqueStrings(group.UserIDs) {
		if err := tx.Create(&userGroupModel{UserID: userId, GroupID: groupId}).Error; err != nil {
			return err
		}
	}
	for _, roleId := range uniqueStrings(group.RoleIDs) {
		if err := tx.Create(&groupRoleModel{GroupID: groupId, RoleID: roleId}).Error; err != nil {
			return err
		}
	}
	for _, domainId := range uniqueStrings(group.DomainIDs) {
		if err := tx.Create(&groupDomainModel{GroupID: groupId, DomainID: domainId}).Error; err != nil {
			return err
		}
	}
	return nil
}

// EffectivePermissions crosses the user's groups' roles with the groups'
// domains: one query for the permissions, one for the domains, joined in Go
// by group so that a domain permission is scoped to exactly the group that
// granted it.
func (self *transaction) EffectivePermissions(userId string) (*models.EffectivePermissions, error) {
	if userId == "" {
		return models.NewEffectivePermissions(nil), nil
	}
	type permissionRow struct {
		GroupID       string
		PermissionKey string
	}
	var permissionRows []permissionRow
	if err := self.tx.
		Table("\"user_group\" AS ug").
		Select("DISTINCT gr.\"group_id\" AS group_id, rp.\"permission_key\" AS permission_key").
		Joins("INNER JOIN \"group_role\" AS gr ON gr.\"group_id\" = ug.\"group_id\"").
		Joins("INNER JOIN \"role_permission\" AS rp ON rp.\"role_id\" = gr.\"role_id\"").
		Where("ug.\"user_id\" = ?", userId).
		Scan(&permissionRows).Error; err != nil {
		return nil, err
	}
	if len(permissionRows) == 0 {
		return models.NewEffectivePermissions(nil), nil
	}
	type domainRow struct {
		GroupID  string
		DomainID string
	}
	var domainRows []domainRow
	if err := self.tx.
		Table("\"user_group\" AS ug").
		Select("gd.\"group_id\" AS group_id, gd.\"domain_id\" AS domain_id").
		Joins("INNER JOIN \"group_domain\" AS gd ON gd.\"group_id\" = ug.\"group_id\"").
		Where("ug.\"user_id\" = ?", userId).
		Scan(&domainRows).Error; err != nil {
		return nil, err
	}
	domainsByGroup := map[string][]string{}
	for _, row := range domainRows {
		domainsByGroup[row.GroupID] = append(domainsByGroup[row.GroupID], row.DomainID)
	}
	var grants []models.Grant
	for _, row := range permissionRows {
		permission := models.Permission(row.PermissionKey)
		switch permission.Kind() {
		case models.PermissionKindDomain:
			for _, domainId := range domainsByGroup[row.GroupID] {
				grants = append(grants, models.Grant{Permission: permission, DomainID: domainId})
			}
		case models.PermissionKindServer, models.PermissionKindAllDomains:
			grants = append(grants, models.Grant{Permission: permission})
		}
	}
	return models.NewEffectivePermissions(grants), nil
}
