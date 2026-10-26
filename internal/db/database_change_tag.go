package db

import "fmt"

// changeTagRow is a collection's object count and a digest of each object's
// name and version, taken in one query so that no object is read whole.
type changeTagRow struct {
	ObjectCount int64  `gorm:"column:object_count"`
	Digest      string `gorm:"column:digest"`
}

// changeTag is the count and the digest together. Every version is named by
// its ETag, so an edit changes the digest; a deletion changes the count and
// the digest; and nothing else does, so two readings with no change between
// them agree.
func (self *transaction) changeTag(table, collectionColumn, collectionId string) (string, error) {
	var found changeTagRow
	query := fmt.Sprintf(`SELECT COUNT(*) AS object_count,
		COALESCE(md5(string_agg("id" || ':' || "etag", ',' ORDER BY "id")), '') AS digest
		FROM %q WHERE %q = ?`, table, collectionColumn)
	if err := self.tx.Raw(query, collectionId).Scan(&found).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", found.ObjectCount, found.Digest), nil
}

func (self *transaction) CalendarChangeTag(calendarId string) (string, error) {
	return self.changeTag(calendarObjectModel{}.TableName(), "calendar_id", calendarId)
}

func (self *transaction) AddressBookChangeTag(addressBookId string) (string, error) {
	return self.changeTag(contactModel{}.TableName(), "addressbook_id", addressBookId)
}
