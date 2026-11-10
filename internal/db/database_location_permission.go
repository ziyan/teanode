package db

import (
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// LocationPermissionOperation is what each person's app last said about
// their phone's location permission, and the limit it and a pause put on how
// much of their location anybody sees. docs/subsystems/location.md says why
// that is also the most of anybody else's location they see.
type LocationPermissionOperation interface {
	// ReportLocationPermission keeps the app's newest report, replacing the
	// one before. ErrInvalidArguments for a permission that is not one of
	// the three.
	ReportLocationPermission(permission *models.LocationPermission) error

	// GetLocationPermission is the newest report, nil when the app never
	// sent one.
	GetLocationPermission(userId string) (*models.LocationPermission, error)

	// GetLocationSharingLimits is the limit for each person at the instant,
	// from their report and the pause in effect then. Everybody asked about is
	// in the answer.
	GetLocationSharingLimits(userIds []string, at time.Time) (map[string]models.LocationSharingLimit, error)
}

type locationPermissionModel struct {
	UserID          string    `gorm:"column:user_id;primaryKey"`
	PhonePermission string    `gorm:"column:phone_permission"`
	IsRecordingOn   bool      `gorm:"column:is_recording_on"`
	ReportedAt      time.Time `gorm:"column:reported_at"`
}

func (locationPermissionModel) TableName() string { return "location_permission" }

func (self *locationPermissionModel) toModel() *models.LocationPermission {
	return &models.LocationPermission{
		UserID:          self.UserID,
		PhonePermission: models.LocationPhonePermission(self.PhonePermission),
		IsRecordingOn:   self.IsRecordingOn,
		ReportedAt:      self.ReportedAt.In(time.Local),
	}
}

func (self *transaction) ReportLocationPermission(permission *models.LocationPermission) error {
	if permission == nil || permission.UserID == "" || !permission.PhonePermission.IsKnown() {
		return ErrInvalidArguments
	}
	row := locationPermissionModel{
		UserID: permission.UserID, PhonePermission: string(permission.PhonePermission),
		IsRecordingOn: permission.IsRecordingOn, ReportedAt: permission.ReportedAt,
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"phone_permission", "is_recording_on", "reported_at"}),
	}).Create(&row).Error
}

func (self *transaction) GetLocationPermission(userId string) (*models.LocationPermission, error) {
	permissions, err := self.getLocationPermissions([]string{userId})
	if err != nil {
		return nil, err
	}
	return permissions[userId], nil
}

func (self *transaction) getLocationPermissions(userIds []string) (map[string]*models.LocationPermission, error) {
	permissions := map[string]*models.LocationPermission{}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		return permissions, nil
	}
	var rows []locationPermissionModel
	if err := self.tx.Where("user_id IN ?", userIds).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		permissions[rows[index].UserID] = rows[index].toModel()
	}
	return permissions, nil
}

func (self *transaction) GetLocationSharingLimits(userIds []string, at time.Time) (map[string]models.LocationSharingLimit, error) {
	limits := map[string]models.LocationSharingLimit{}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		return limits, nil
	}
	permissions, err := self.getLocationPermissions(userIds)
	if err != nil {
		return nil, err
	}
	pauses, err := self.GetLocationPausesInEffect(userIds, at)
	if err != nil {
		return nil, err
	}
	for _, userId := range userIds {
		limits[userId] = models.LocationSharingLimitOf(permissions[userId], pauses[userId])
	}
	return limits, nil
}
