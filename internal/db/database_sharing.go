package db

import (
	"database/sql"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// SharingOperation is what the one place that lists everything a person
// shares (the Sharing page) needs beyond the questions each kind of share
// answers for itself: the shares that reach a person, with the row each is,
// where the kind's own operation answers only the highest level, and the
// shares of the person's albums and passwords in one read. It also ends a share
// from the side of the person it is for.
//
// Nothing here decides who may see what. Each reading query is the kind's own
// (reachingSharesQuery, reachingLocationSharesQuery, photoAlbumReachQuery,
// passwordReachSQL) with the columns that name the share added, so that the
// list and the access check cannot disagree about who is reached.
type SharingOperation interface {
	// ListCalendarSharesReaching are the shares of other people's calendars
	// that reach the person now, oldest first, each with the calendar's name
	// and its owner.
	ListCalendarSharesReaching(readerUserId string) ([]*models.ShareRow, error)

	// ListLocationSharesReaching are the shares of other people's location
	// that reach the person now, oldest first, with the owner.
	ListLocationSharesReaching(viewerUserId string) ([]*models.ShareRow, error)

	// ListPhotoAlbumSharesReaching are the shares of other people's albums
	// that reach the person now, oldest first, with the album's title and its
	// owner. An album the person owns or belongs to through a circle that
	// owns it is not a share.
	ListPhotoAlbumSharesReaching(readerUserId string) ([]*models.ShareRow, error)

	// ListPhotoAlbumSharesBy are the shares of all of the owner's albums,
	// oldest first, with the album's title and who each is for.
	ListPhotoAlbumSharesBy(ownerUserId string) ([]*models.ShareRow, error)

	// ListPasswordSharesBy are, for each person or circle the owner shares
	// items with, how many items that are not deleted. Never which: an item's
	// title is sealed. A row has no ShareID, since the shares are one for each
	// item.
	ListPasswordSharesBy(ownerUserId string) ([]*models.ShareRow, error)

	// ListPasswordSharesReaching are, for each owner and way of reaching
	// (direct, or through one circle), how many of the owner's items that are
	// not deleted reach the person now.
	ListPasswordSharesReaching(viewerUserId string) ([]*models.ShareRow, error)

	// ListCircleShareOwners are the members of the circle who have shared
	// something with it, with the kinds: calendar, location, Health, memory
	// pages, photo albums, recordings and finance accounts. A skill is shared
	// with one person, and passwords wait for a key, so neither is listed.
	ListCircleShareOwners(circleId string) ([]*models.CircleShareOwner, error)

	// GetPhotoAlbumShare is one share of an album the person manages, or
	// ErrNotFound: before it is changed, to tell whether the change widens it.
	GetPhotoAlbumShare(userId, photoAlbumShareId string) (*models.PhotoAlbumShare, error)

	// DeclineLocationShare ends a share of somebody's location that was made
	// to the person by name, and answers whose location it was. ErrNotFound
	// unless the share is to that person, which a share to a circle never is.
	DeclineLocationShare(granteeUserId, shareId string) (string, error)

	// DeclineHealthShare ends a share of somebody's Health that was made to
	// the person by name. ErrNotFound unless the share is to that person.
	DeclineHealthShare(granteeUserId, shareId string) error

	// DeclinePhotoAlbumShare ends a share of an album that was made to the
	// person by name, and takes out of the album the photos the person added.
	// ErrNotFound unless the share is to that person.
	DeclinePhotoAlbumShare(granteeUserId, shareId string) error

	// DeclineRecordingShare ends a share of somebody's recording that was
	// made to the person by name. ErrNotFound unless the share is to that
	// person, which a share to a circle never is.
	DeclineRecordingShare(granteeUserId, shareId string) error

	// DeclineFinanceShare ends a share of somebody's finance account that was
	// made to the person by name. ErrNotFound unless the share is to that
	// person, which a share to a circle never is.
	DeclineFinanceShare(granteeUserId, shareId string) error
}

// shareRowNames fills in who owns each row and who it is for, in one read of
// the accounts and one of the circles however many rows there are.
func (self *transaction) shareRowNames(rows []*models.ShareRow) error {
	userIds, circleIds := []string{}, []string{}
	for _, row := range rows {
		userIds = append(userIds, row.OwnerUserID)
		if row.GranteeUserID != "" {
			userIds = append(userIds, row.GranteeUserID)
		}
		if row.GranteeCircleID != "" {
			circleIds = append(circleIds, row.GranteeCircleID)
		}
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return err
	}
	circleNames, err := self.circleNamesById(circleIds)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if owner := users[row.OwnerUserID]; owner != nil {
			row.OwnerUsername, row.OwnerName, row.OwnerUser = owner.Username, owner.DisplayName(), owner
		}
		if grantee := users[row.GranteeUserID]; grantee != nil {
			row.GranteeUsername, row.GranteeName = grantee.Username, grantee.DisplayName()
		}
		row.GranteeCircleName = circleNames[row.GranteeCircleID]
	}
	return nil
}

// circleNamesById is the name of each circle given, none for one that is gone.
func (self *transaction) circleNamesById(circleIds []string) (map[string]string, error) {
	names := map[string]string{}
	if len(circleIds) == 0 {
		return names, nil
	}
	var circles []circleModel
	if err := self.tx.Where("\"id\" IN ?", uniqueStrings(circleIds)).Find(&circles).Error; err != nil {
		return nil, err
	}
	for _, circle := range circles {
		names[circle.ID] = circle.Name
	}
	return names, nil
}

func (self *transaction) ListCalendarSharesReaching(readerUserId string) ([]*models.ShareRow, error) {
	rows := []*models.ShareRow{}
	if readerUserId == "" {
		return rows, nil
	}
	var reaching []reachingShareRow
	if err := self.tx.Raw(reachingSharesQuery,
		sql.Named("reader", readerUserId), sql.Named("calendar", ""),
		sql.Named("permission", string(models.PermissionCalendarUse))).Scan(&reaching).Error; err != nil {
		return nil, err
	}
	if len(reaching) == 0 {
		return rows, nil
	}
	calendarIds := make([]string, 0, len(reaching))
	for _, row := range reaching {
		calendarIds = append(calendarIds, row.CalendarID)
	}
	var calendars []calendarModel
	if err := self.tx.Where("\"id\" IN ?", uniqueStrings(calendarIds)).Find(&calendars).Error; err != nil {
		return nil, err
	}
	calendarById := map[string]*calendarModel{}
	for index := range calendars {
		calendarById[calendars[index].ID] = &calendars[index]
	}
	for _, row := range reaching {
		found := calendarById[row.CalendarID]
		if found == nil || found.UserID == nil {
			continue
		}
		shareRow := &models.ShareRow{
			ShareKind: models.ShareKindCalendar, ShareID: row.ShareID, SubjectID: found.ID, SubjectName: found.Name,
			OwnerUserID: *found.UserID, ShareLevel: row.ShareLevel, CreatedAt: row.CreatedAt.In(time.Local),
		}
		if row.GranteeUserID != nil {
			shareRow.GranteeUserID = *row.GranteeUserID
		}
		if row.GranteeCircleID != nil {
			shareRow.GranteeCircleID = *row.GranteeCircleID
		}
		rows = append(rows, shareRow)
	}
	sortShareRows(rows)
	return rows, self.shareRowNames(rows)
}

func (self *transaction) ListLocationSharesReaching(viewerUserId string) ([]*models.ShareRow, error) {
	rows := []*models.ShareRow{}
	if viewerUserId == "" {
		return rows, nil
	}
	var reaching []reachingLocationShareRow
	if err := self.tx.Raw(reachingLocationSharesQuery,
		sql.Named("viewer", viewerUserId), sql.Named("owner", ""),
		sql.Named("permission", string(models.PermissionLocationUse))).Scan(&reaching).Error; err != nil {
		return nil, err
	}
	for _, row := range reaching {
		shareRow := &models.ShareRow{
			ShareKind: models.ShareKindLocation, ShareID: row.ShareID, OwnerUserID: row.OwnerUserID,
			ShareLevel: row.ShareLevel, CreatedAt: row.CreatedAt.In(time.Local),
		}
		if row.GranteeUserID != nil {
			shareRow.GranteeUserID = *row.GranteeUserID
		}
		if row.GranteeCircleID != nil {
			shareRow.GranteeCircleID = *row.GranteeCircleID
		}
		rows = append(rows, shareRow)
	}
	sortShareRows(rows)
	return rows, self.shareRowNames(rows)
}

func (self *transaction) ListPhotoAlbumSharesReaching(readerUserId string) ([]*models.ShareRow, error) {
	rows := []*models.ShareRow{}
	if readerUserId == "" {
		return rows, nil
	}
	var reaching []photoAlbumReachRow
	if err := self.tx.Raw(photoAlbumReachQuery, sql.Named("reader", readerUserId), sql.Named("album", ""),
		sql.Named("permission", string(models.PermissionPhotosUse))).Scan(&reaching).Error; err != nil {
		return nil, err
	}
	albumIds := []string{}
	for _, row := range reaching {
		if row.ReachKind == "share" {
			albumIds = append(albumIds, row.PhotoAlbumID)
		}
	}
	if len(albumIds) == 0 {
		return rows, nil
	}
	var albums []photoAlbumModel
	if err := self.tx.Where("\"id\" IN ?", uniqueStrings(albumIds)).Find(&albums).Error; err != nil {
		return nil, err
	}
	titleById := map[string]string{}
	for _, album := range albums {
		titleById[album.ID] = album.AlbumTitle
	}
	for _, row := range reaching {
		if row.ReachKind != "share" {
			continue
		}
		shareRow := &models.ShareRow{
			ShareKind: models.ShareKindPhotoAlbum, ShareID: row.ShareID, SubjectID: row.PhotoAlbumID,
			SubjectName: titleById[row.PhotoAlbumID], OwnerUserID: row.AlbumOwnerUserID, ShareLevel: row.ShareLevel,
			ShareScopes: models.AlbumShareScopes(row.IsLocationIncluded, row.IsFaceNamesIncluded), CreatedAt: row.ShareCreatedAt.In(time.Local),
		}
		if row.GranteeUserID != nil {
			shareRow.GranteeUserID = *row.GranteeUserID
		}
		if row.GranteeCircleID != nil {
			shareRow.GranteeCircleID = *row.GranteeCircleID
		}
		rows = append(rows, shareRow)
	}
	sortShareRows(rows)
	return rows, self.shareRowNames(rows)
}

func (self *transaction) ListPhotoAlbumSharesBy(ownerUserId string) ([]*models.ShareRow, error) {
	rows := []*models.ShareRow{}
	if ownerUserId == "" {
		return rows, nil
	}
	var shares []photoAlbumShareModel
	if err := self.tx.Raw(`SELECT share.* FROM "photo_album_share" share
		JOIN "photo_album" album ON album.id = share.photo_album_id
		WHERE album.user_id = ?
		ORDER BY share.created_at ASC, share.id ASC`, ownerUserId).Scan(&shares).Error; err != nil {
		return nil, err
	}
	albumIds := make([]string, 0, len(shares))
	for _, share := range shares {
		albumIds = append(albumIds, share.PhotoAlbumID)
	}
	titleById := map[string]string{}
	if len(albumIds) > 0 {
		var albums []photoAlbumModel
		if err := self.tx.Where("\"id\" IN ?", uniqueStrings(albumIds)).Find(&albums).Error; err != nil {
			return nil, err
		}
		for _, album := range albums {
			titleById[album.ID] = album.AlbumTitle
		}
	}
	for index := range shares {
		share := shares[index].toModel()
		rows = append(rows, &models.ShareRow{
			ShareKind: models.ShareKindPhotoAlbum, ShareID: share.ID, SubjectID: share.PhotoAlbumID,
			SubjectName: titleById[share.PhotoAlbumID], OwnerUserID: ownerUserId, ShareLevel: string(share.ShareLevel),
			ShareScopes: models.AlbumShareScopes(share.IsLocationIncluded, share.IsFaceNamesIncluded), GranteeUserID: share.GranteeUserID,
			GranteeCircleID: share.GranteeCircleID, CreatedAt: share.CreatedAt.In(time.Local),
		})
	}
	return rows, self.shareRowNames(rows)
}

// sortShareRows puts the oldest share first, as the kinds' own listings do.
func sortShareRows(rows []*models.ShareRow) {
	sort.SliceStable(rows, func(first, second int) bool {
		left, right := rows[first], rows[second]
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ShareID < right.ShareID
	})
}

// passwordShareCountRow is one line of a count of password shares.
type passwordShareCountRow struct {
	OwnerUserID     string    `gorm:"column:owner_user_id"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	ItemCount       int       `gorm:"column:item_count"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (self *transaction) passwordShareRows(rows []passwordShareCountRow) ([]*models.ShareRow, error) {
	shareRows := make([]*models.ShareRow, 0, len(rows))
	for _, row := range rows {
		shareRow := &models.ShareRow{
			ShareKind: models.ShareKindPasswords, OwnerUserID: row.OwnerUserID, ItemCount: row.ItemCount,
			CreatedAt: row.CreatedAt.In(time.Local),
		}
		if row.GranteeUserID != nil {
			shareRow.GranteeUserID = *row.GranteeUserID
		}
		if row.GranteeCircleID != nil {
			shareRow.GranteeCircleID = *row.GranteeCircleID
		}
		shareRows = append(shareRows, shareRow)
	}
	return shareRows, self.shareRowNames(shareRows)
}

func (self *transaction) ListPasswordSharesBy(ownerUserId string) ([]*models.ShareRow, error) {
	if ownerUserId == "" {
		return []*models.ShareRow{}, nil
	}
	var rows []passwordShareCountRow
	if err := self.tx.Raw(`SELECT share.owner_user_id AS owner_user_id, share.grantee_user_id AS grantee_user_id,
		share.grantee_circle_id AS grantee_circle_id, COUNT(DISTINCT share.item_id) AS item_count, MIN(share.created_at) AS created_at
		FROM "password_share" share
		JOIN "password_item" shared_item ON shared_item.id = share.item_id AND shared_item.deleted_at IS NULL
		WHERE share.owner_user_id = @owner
		GROUP BY share.owner_user_id, share.grantee_user_id, share.grantee_circle_id
		ORDER BY MIN(share.created_at) ASC, share.grantee_user_id ASC, share.grantee_circle_id ASC`,
		sql.Named("owner", ownerUserId)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return self.passwordShareRows(rows)
}

func (self *transaction) ListPasswordSharesReaching(viewerUserId string) ([]*models.ShareRow, error) {
	if viewerUserId == "" {
		return []*models.ShareRow{}, nil
	}
	var rows []passwordShareCountRow
	if err := self.tx.Raw(`SELECT reach.owner_user_id AS owner_user_id, share.grantee_user_id AS grantee_user_id,
		share.grantee_circle_id AS grantee_circle_id, COUNT(DISTINCT reach.item_id) AS item_count, MIN(share.created_at) AS created_at
		FROM `+passwordReachSQL(false)+`
		JOIN "password_share" share ON share.id = reach.share_id
		WHERE reach.recipient_user_id = @viewer
		GROUP BY reach.owner_user_id, share.grantee_user_id, share.grantee_circle_id
		ORDER BY MIN(share.created_at) ASC, reach.owner_user_id ASC, share.grantee_circle_id ASC`,
		sql.Named("viewer", viewerUserId), passwordsPermissionArgument()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return self.passwordShareRows(rows)
}

func (self *transaction) ListCircleShareOwners(circleId string) ([]*models.CircleShareOwner, error) {
	if circleId == "" {
		return []*models.CircleShareOwner{}, nil
	}
	var rows []struct {
		OwnerUserID string `gorm:"column:owner_user_id"`
		ShareKind   string `gorm:"column:share_kind"`
	}
	if err := self.tx.Raw(`SELECT DISTINCT shared.owner_user_id AS owner_user_id, shared.share_kind AS share_kind FROM (
		SELECT shared_calendar.user_id AS owner_user_id, 'calendar' AS share_kind
		FROM "calendar_share" share JOIN "calendar" shared_calendar ON shared_calendar.id = share.calendar_id
		WHERE share.grantee_circle_id = @circle AND shared_calendar.user_id IS NOT NULL
		UNION ALL
		SELECT share.user_id, 'location' FROM "location_share" share WHERE share.grantee_circle_id = @circle
		UNION ALL
		SELECT share.user_id, 'health' FROM "health_share" share WHERE share.grantee_circle_id = @circle
		UNION ALL
		SELECT owner.user_id, 'memory_page' FROM "agent_page_share" share JOIN "agent" owner ON owner.id = share.agent_id
		WHERE share.grantee_circle_id = @circle
		UNION ALL
		SELECT album.user_id, 'photo_album' FROM "photo_album_share" share JOIN "photo_album" album ON album.id = share.photo_album_id
		WHERE share.grantee_circle_id = @circle AND album.user_id IS NOT NULL
		UNION ALL
		SELECT shared_recording.user_id, 'recording' FROM "recording_share" share
		JOIN "recording" shared_recording ON shared_recording.id = share.recording_id
		WHERE share.grantee_circle_id = @circle AND shared_recording.user_id IS NOT NULL AND shared_recording.deleted_at IS NULL
		UNION ALL
		SELECT owner_agent.user_id, 'finance' FROM "finance_account_share" share
		JOIN "agent_finance_account" account ON account.id = share.finance_account_id
		JOIN "agent" owner_agent ON owner_agent.id = account.agent_id
		WHERE share.grantee_circle_id = @circle
	) shared ORDER BY 1, 2`, sql.Named("circle", circleId)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	owners := []*models.CircleShareOwner{}
	for _, row := range rows {
		if len(owners) == 0 || owners[len(owners)-1].OwnerUserID != row.OwnerUserID {
			owners = append(owners, &models.CircleShareOwner{OwnerUserID: row.OwnerUserID})
		}
		last := owners[len(owners)-1]
		last.ShareKinds = append(last.ShareKinds, models.ShareKind(row.ShareKind))
	}
	// The kinds are put in the order the Sharing page lists them, not the
	// alphabet the query sorted them in.
	for _, owner := range owners {
		ordered := make([]models.ShareKind, 0, len(owner.ShareKinds))
		for _, kind := range models.ShareKinds {
			for _, found := range owner.ShareKinds {
				if found == kind {
					ordered = append(ordered, kind)
				}
			}
		}
		owner.ShareKinds = ordered
	}
	return owners, nil
}

func (self *transaction) GetPhotoAlbumShare(userId, photoAlbumShareId string) (*models.PhotoAlbumShare, error) {
	found, err := self.shareToManage(userId, photoAlbumShareId)
	if err != nil {
		return nil, err
	}
	return found.toModel(), nil
}

func (self *transaction) DeclineLocationShare(granteeUserId, shareId string) (string, error) {
	if granteeUserId == "" || shareId == "" {
		return "", ErrNotFound
	}
	var ownerUserIds []string
	if err := self.tx.Raw(`DELETE FROM "location_share" WHERE "id" = ? AND "grantee_user_id" = ? RETURNING "user_id"`,
		shareId, granteeUserId).Scan(&ownerUserIds).Error; err != nil {
		return "", err
	}
	if len(ownerUserIds) == 0 {
		return "", ErrNotFound
	}
	return ownerUserIds[0], nil
}

func (self *transaction) DeclineHealthShare(granteeUserId, shareId string) error {
	if granteeUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"grantee_user_id\" = ?", shareId, granteeUserId).Delete(&healthShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) DeclinePhotoAlbumShare(granteeUserId, shareId string) error {
	if granteeUserId == "" || shareId == "" {
		return ErrNotFound
	}
	var found photoAlbumShareModel
	isFound, err := findOneRow(self.tx.Where("id = ? AND grantee_user_id = ?", shareId, granteeUserId), &found)
	if err != nil {
		return err
	}
	if !isFound {
		return ErrNotFound
	}
	if err := self.tx.Where("id = ?", found.ID).Delete(&photoAlbumShareModel{}).Error; err != nil {
		return err
	}
	return self.removeUnreachablePhotoAlbumItems([]string{found.PhotoAlbumID})
}

func (self *transaction) DeclineRecordingShare(granteeUserId, shareId string) error {
	if granteeUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"grantee_user_id\" = ?", shareId, granteeUserId).Delete(&recordingShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) DeclineFinanceShare(granteeUserId, shareId string) error {
	if granteeUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"grantee_user_id\" = ?", shareId, granteeUserId).Delete(&financeAccountShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
