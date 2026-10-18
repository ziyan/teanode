package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// RelationshipOperation is what people who share a circle are to each other.
// There is one row for a pair, from the side of whoever said it last; see
// migration 0243. It goes in the transaction in which the two stop sharing
// any circle (endRelationshipsOutsideCircles), and is read only while they
// share one all the same.
type RelationshipOperation interface {
	// ListPersonRelationships returns every relationship a person is in with
	// somebody they share a circle with.
	ListPersonRelationships(userId string) ([]*models.PersonRelationship, error)

	// GetPersonRelationship returns the relationship between two people, or
	// nil when nothing was said or they share no circle.
	GetPersonRelationship(userId, otherUserId string) (*models.PersonRelationship, error)

	// SetPersonRelationship says, from userId's side, that relatedUserId is
	// their relationshipKind, replacing whatever either of them said before.
	// It does not check that they share a circle; the caller does.
	SetPersonRelationship(userId, relatedUserId string, relationshipKind models.RelationshipKind, relatedUserTermGender models.TermGender) (*models.PersonRelationship, error)

	// RemovePersonRelationship takes away the relationship between two
	// people, whichever of them said it, and says whether there was one.
	RemovePersonRelationship(userId, otherUserId string) (bool, error)

	// TakeRelationshipNotice says whether one person may tell another about
	// a relationship between them now, and if so records that they did: at
	// most once in each interval, whatever was said and taken back between.
	TakeRelationshipNotice(userId, toldUserId string, now time.Time, interval time.Duration) (bool, error)
}

type personRelationshipModel struct {
	ID                    string    `gorm:"column:id;primaryKey"`
	CreatedAt             time.Time `gorm:"column:created_at"`
	ModifiedAt            time.Time `gorm:"column:modified_at"`
	UserID                string    `gorm:"column:user_id"`
	RelatedUserID         string    `gorm:"column:related_user_id"`
	RelationshipKind      string    `gorm:"column:relationship_kind"`
	RelatedUserTermGender string    `gorm:"column:related_user_term_gender"`
}

func (personRelationshipModel) TableName() string { return "person_relationship" }

func personRelationshipFromModel(model *personRelationshipModel) *models.PersonRelationship {
	return &models.PersonRelationship{
		ID:                    model.ID,
		CreatedAt:             model.CreatedAt.In(time.Local),
		ModifiedAt:            model.ModifiedAt.In(time.Local),
		UserID:                model.UserID,
		RelatedUserID:         model.RelatedUserID,
		RelationshipKind:      models.RelationshipKind(model.RelationshipKind),
		RelatedUserTermGender: models.TermGender(model.RelatedUserTermGender),
	}
}

// sharedCircleCondition holds for a relationship row whose two people are in
// a circle together.
const sharedCircleCondition = `EXISTS (SELECT 1 FROM "circle_member" "mine" JOIN "circle_member" "theirs" ON "theirs"."circle_id" = "mine"."circle_id"
	WHERE "mine"."user_id" = "person_relationship"."user_id" AND "theirs"."user_id" = "person_relationship"."related_user_id")`

func (self *transaction) ListPersonRelationships(userId string) ([]*models.PersonRelationship, error) {
	relationships := []*models.PersonRelationship{}
	if userId == "" {
		return relationships, nil
	}
	var rows []*personRelationshipModel
	if err := self.tx.Model(&personRelationshipModel{}).
		Where(`("user_id" = ? OR "related_user_id" = ?) AND `+sharedCircleCondition, userId, userId).
		Order(`"created_at" ASC, "id" ASC`).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		relationships = append(relationships, personRelationshipFromModel(row))
	}
	return relationships, nil
}

func (self *transaction) GetPersonRelationship(userId, otherUserId string) (*models.PersonRelationship, error) {
	if userId == "" || otherUserId == "" || userId == otherUserId {
		return nil, nil
	}
	var rows []*personRelationshipModel
	if err := self.tx.Model(&personRelationshipModel{}).
		Where(`(("user_id" = ? AND "related_user_id" = ?) OR ("user_id" = ? AND "related_user_id" = ?)) AND `+sharedCircleCondition,
			userId, otherUserId, otherUserId, userId).
		Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return personRelationshipFromModel(rows[0]), nil
}

func (self *transaction) SetPersonRelationship(userId, relatedUserId string, relationshipKind models.RelationshipKind, relatedUserTermGender models.TermGender) (*models.PersonRelationship, error) {
	if userId == "" || relatedUserId == "" || userId == relatedUserId || !relationshipKind.IsValid() || !relatedUserTermGender.IsValid() {
		return nil, ErrInvalidArguments
	}
	now := time.Now()
	// Said again from either side, the row is the pair's: it keeps its id and
	// when it was first said, and takes the new speaker's side.
	var row personRelationshipModel
	if err := self.tx.Raw(`INSERT INTO "person_relationship"
			("id", "created_at", "modified_at", "user_id", "related_user_id", "relationship_kind", "related_user_term_gender")
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (LEAST("user_id", "related_user_id"), GREATEST("user_id", "related_user_id")) DO UPDATE SET
			"modified_at" = EXCLUDED."modified_at",
			"user_id" = EXCLUDED."user_id",
			"related_user_id" = EXCLUDED."related_user_id",
			"relationship_kind" = EXCLUDED."relationship_kind",
			"related_user_term_gender" = EXCLUDED."related_user_term_gender"
		RETURNING *`,
		newID(), now, now, userId, relatedUserId, string(relationshipKind), string(relatedUserTermGender)).Scan(&row).Error; err != nil {
		return nil, err
	}
	return personRelationshipFromModel(&row), nil
}

func (self *transaction) RemovePersonRelationship(userId, otherUserId string) (bool, error) {
	if userId == "" || otherUserId == "" || userId == otherUserId {
		return false, nil
	}
	result := self.tx.Where(`("user_id" = ? AND "related_user_id" = ?) OR ("user_id" = ? AND "related_user_id" = ?)`,
		userId, otherUserId, otherUserId, userId).Delete(&personRelationshipModel{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (self *transaction) TakeRelationshipNotice(userId, toldUserId string, now time.Time, interval time.Duration) (bool, error) {
	if userId == "" || toldUserId == "" || userId == toldUserId {
		return false, ErrInvalidArguments
	}
	// One statement, so that two changes at once cannot both find room: the
	// row is written only when there was none or it is old enough, and
	// RETURNING says which.
	var toldAts []time.Time
	if err := self.tx.Raw(`INSERT INTO "person_relationship_notice" ("user_id", "told_user_id", "told_at") VALUES (?, ?, ?)
		ON CONFLICT ("user_id", "told_user_id") DO UPDATE SET "told_at" = EXCLUDED."told_at"
		WHERE "person_relationship_notice"."told_at" <= ?
		RETURNING "told_at"`, userId, toldUserId, now, now.Add(-interval)).Pluck("told_at", &toldAts).Error; err != nil {
		return false, err
	}
	return len(toldAts) > 0, nil
}

// endRelationshipsOutsideCircles deletes every relationship between this
// person and somebody they no longer share any circle with, in the
// transaction that changed the circles, as endSharesOutsideCircles does for
// shares: a relationship is between people in a circle together, and does not
// come back unannounced if they are in one again.
func endRelationshipsOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "person_relationship" WHERE ("user_id" = ? OR "related_user_id" = ?) AND NOT `+sharedCircleCondition, userId, userId).Error
}
