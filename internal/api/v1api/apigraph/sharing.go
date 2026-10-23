package apigraph

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Everything a person shares, and everything shared with them, in one place.
// Each kind keeps its own table, its own manager and its own rules for who
// reaches what; this reads them all through the questions each already answers
// and says nothing of its own about who may see what. docs/subsystems/circles.md
// ("Sharing, in one place") says what it lists and what it leaves out.
//
// Stopping a share is the kind's own operation (UnshareCalendar,
// UnshareLocation, SetHealthShare with no categories, UnshareAgentPage,
// UnshareSkill, RemovePhotoAlbumShare, UnshareRecording,
// UnshareFinanceAccount), and none of them asks for the kind's permission
// (requireShareOwner). What is new here is the other side: DeclineShare, which
// ends a share that was made to the caller by name.

// requireShareOwner is the caller, if they may end what they shared. It asks
// for no permission of the kind, so that somebody whose role lost it can still
// take back a share they made while they had it, which the Sharing page keeps
// listing. Each operation that uses it deletes only rows the caller owns.
func (self *graph) requireShareOwner(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// SharingQuery reads what is shared.
type SharingQuery interface {
	// Everything the caller shares and everything shared with them, across
	// calendar, location, Health, memory pages, photo albums, recordings,
	// finance accounts, skills and passwords: the kind, the thing, the person or circle it is shared with
	// or by, the level or scope, and since when. A share of passwords is
	// counted, never named, since an item's title is sealed. A share to a
	// circle is listed with the circle it came through; one made to the caller
	// by name says so, and can be declined. A kind the caller may not use is
	// left out, and finance accounts are never listed to the person's agent.
	// Needs circle:use.
	ListSharing(ctx context.Context) (*SharingView, error)
}

// SharingMutation ends shares from the side of the person they are for.
type SharingMutation interface {
	// Stop receiving a share that was made to the caller by name: a
	// calendar, location, Health, memory page, photo album, recording,
	// finance account or skill. The owner is not told. A share to a circle cannot be declined; it ends when
	// the caller leaves the circle. Passwords shared with the caller are
	// removed by their owner. Needs circle:use.
	DeclineShare(ctx context.Context, arguments DeclineShareArguments) (bool, error)
}

// SharingView is what the caller shares and what is shared with them.
type SharingView struct {
	// What the caller shares, one row for each person or circle it is shared
	// with
	SharesMade []*SharingMadeView `json:"sharesMade"`

	// What is shared with the caller, one row for each share
	SharesReceived []*SharingReceivedView `json:"sharesReceived"`
}

// SharingMadeView is one share the caller made.
type SharingMadeView struct {
	// calendar, location, health, memory_page, photo_album, recording, finance,
	// skill or passwords
	ShareKind string `json:"shareKind"`

	// The share's identifier, which the kind's own unshare operation takes;
	// empty for passwords, whose shares are one for each item
	ShareID string `json:"shareId"`

	// The thing shared: the calendar's, album's, recording's or finance
	// account's identifier, the page's path or the skill's name; empty for
	// location, Health and passwords. SubjectName is a recording's title or an
	// account's name, and SubjectMask the last digits of an account's number.
	SubjectID   string `json:"subjectId"`
	SubjectName string `json:"subjectName"`
	SubjectMask string `json:"subjectMask"`

	// Whether the thing shared is gone: a memory page that was deleted, whose
	// SubjectName is then its path. The owner can still stop the share.
	IsSubjectDeleted bool `json:"isSubjectDeleted"`

	// The level for a calendar (busy, titles, details, edit), location
	// (current, history), album (view, contribute), recording (summary,
	// transcript) or finance account (balance, spending, transactions); empty
	// for the rest
	ShareLevel string `json:"shareLevel"`

	// What else it gives: the Health categories, the skill's tools, quotes for
	// a page shared with the words each fact was read in, location for an
	// album shared with where its photos were taken
	ShareScopes []string `json:"shareScopes"`

	// How many password items it reaches; zero for the other kinds
	PasswordItemCount int `json:"passwordItemCount"`

	// Who it is shared with: a person or a circle, never both
	GranteeUserID           *string `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeCircleID         *string `json:"granteeCircleId"`
	GranteeCircleName       string  `json:"granteeCircleName"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`

	CreatedAt time.Time `json:"createdAt"`
}

// SharingReceivedView is one share made to the caller.
type SharingReceivedView struct {
	ShareKind   string   `json:"shareKind"`
	ShareID     string   `json:"shareId"`
	SubjectID   string   `json:"subjectId"`
	SubjectName string   `json:"subjectName"`
	SubjectMask string   `json:"subjectMask"`
	ShareLevel  string   `json:"shareLevel"`
	ShareScopes []string `json:"shareScopes"`

	PasswordItemCount int `json:"passwordItemCount"`

	// Whose it is
	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	// The circle it was shared with, which the caller is in; null for a share
	// made to the caller by name
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	// Whether it was made to the caller by name and alone gives the level
	// shown: a finance account shared by name at a lower level than a circle
	// shares it is not
	IsDirect bool `json:"isDirect"`

	// Whether DeclineShare ends it: a share made by name, of any kind but
	// passwords. The circle's share stays when a finance account has both.
	CanDecline bool `json:"canDecline"`

	CreatedAt time.Time `json:"createdAt"`
}

type DeclineShareArguments struct {
	// calendar, location, health, memory_page, photo_album, recording, finance
	// or skill
	ShareKind string `json:"shareKind"`

	// The share, from ListSharing
	ShareID string `json:"shareId"`
}

// canListShareKind says whether the lists include a kind for the caller. What
// is shared with them is left out for a kind they may not use, because they
// could not open the page it would link to. What they shared is always listed,
// whatever they may use now: a person who lost a permission can still end the
// shares they made, and the queries of what reaches people already keep a
// dormant share from its readers. A memory page and a skill are received
// through circle:use, which the caller holds already. What others share of
// finance is not the agent's to read (requireFinanceNotAgent), so the person's
// agent is not shown it either, on either side. A kind whose service is off is
// left out on both.
func (self *graph) canListShareKind(ctx context.Context, principal *api.Principal, shareKind models.ShareKind, isReceived bool) bool {
	canUse := func(permission models.Permission) bool {
		return !isReceived || principal.Permissions.Has(permission)
	}
	switch shareKind {
	case models.ShareKindCalendar:
		return self.isServiceEnabled(models.ServiceCalendar) && canUse(models.PermissionCalendarUse)
	case models.ShareKindLocation:
		return canUse(models.PermissionLocationUse)
	case models.ShareKindHealth:
		return canUse(models.PermissionHealthUse)
	case models.ShareKindMemoryPage, models.ShareKindSkill:
		return true
	case models.ShareKindPhotoAlbum:
		return self.isServiceEnabled(models.ServicePhotos) && canUse(models.PermissionPhotosUse)
	case models.ShareKindRecording:
		return canUse(models.PermissionAgentUse)
	case models.ShareKindFinance:
		return requireFinanceNotAgent(ctx) == nil && canUse(models.PermissionAgentUse)
	case models.ShareKindPasswords:
		return canUse(models.PermissionPasswordsUse)
	}
	return false
}

// sharesMadeBy are the rows of what the person shares, of one kind, read
// through the kind's own listing.
func (self *graph) sharesMadeBy(tx db.Transaction, user *models.User, shareKind models.ShareKind) ([]*models.ShareRow, error) {
	rows := []*models.ShareRow{}
	switch shareKind {
	case models.ShareKindCalendar:
		found, err := tx.FindCalendar(user.ID, models.CalendarEvents)
		if err != nil || found == nil {
			return rows, err
		}
		shares, err := tx.ListCalendarShares(found.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, SubjectID: found.ID, SubjectName: found.Name, OwnerUserID: user.ID,
				GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
				GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
				ShareLevel: string(share.ShareLevel), CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindLocation:
		shares, err := tx.ListLocationShares(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, OwnerUserID: user.ID,
				GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
				GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
				ShareLevel: string(share.ShareLevel), CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindHealth:
		shares, err := tx.ListHealthShares(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, OwnerUserID: user.ID,
				GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
				GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
				ShareScopes: share.HealthCategories, CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindMemoryPage:
		shares, err := tx.ListPageSharesBy(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			subjectName := share.Path
			node, err := tx.GetAgentNode(share.AgentID, share.Path)
			if err != nil {
				return nil, err
			}
			if node != nil {
				subjectName = node.Name
			}
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, SubjectID: share.Path, SubjectName: subjectName, IsSubjectDeleted: node == nil, OwnerUserID: user.ID,
				GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
				GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
				ShareScopes: models.PageShareScopes(share.IsQuoteShared), CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindSkill:
		shares, err := tx.ListSkillSharesBy(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, SubjectID: share.SkillName, SubjectName: share.SkillName, OwnerUserID: user.ID,
				GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
				ShareScopes: share.ToolNames, CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindPhotoAlbum:
		return tx.ListPhotoAlbumSharesBy(user.ID)
	case models.ShareKindRecording:
		shares, err := tx.ListRecordingSharesBy(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, recordingShareRow(share))
		}
	case models.ShareKindFinance:
		shares, err := tx.ListFinanceSharesBy(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, financeShareRow(share))
		}
	case models.ShareKindPasswords:
		return tx.ListPasswordSharesBy(user.ID)
	}
	return rows, nil
}

// sharesReachingFor are the rows of what is shared with the person, of one
// kind, read through the kind's own listing of the shares that reach them.
func (self *graph) sharesReachingFor(tx db.Transaction, user *models.User, shareKind models.ShareKind) ([]*models.ShareRow, error) {
	rows := []*models.ShareRow{}
	switch shareKind {
	case models.ShareKindCalendar:
		return tx.ListCalendarSharesReaching(user.ID)
	case models.ShareKindLocation:
		return tx.ListLocationSharesReaching(user.ID)
	case models.ShareKindHealth:
		shares, err := tx.ListHealthSharesReaching(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, OwnerUserID: share.UserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName,
				GranteeUserID: share.GranteeUserID, GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
				ShareScopes: share.HealthCategories, CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindMemoryPage:
		shares, err := tx.ListPageSharesReaching(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			node, err := tx.GetAgentNode(share.AgentID, share.Path)
			if err != nil {
				return nil, err
			}
			// A share left by a page that was deleted reaches nothing to read,
			// so the person is not shown it. Its owner sees it, to clean up.
			if node == nil {
				continue
			}
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, SubjectID: share.Path, SubjectName: node.Name,
				OwnerUserID: share.OwnerUserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName, OwnerUser: share.OwnerUser,
				GranteeUserID: share.GranteeUserID, GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
				ShareScopes: models.PageShareScopes(share.IsQuoteShared), CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindSkill:
		shares, err := tx.ListSkillSharesReaching(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, &models.ShareRow{
				ShareKind: shareKind, ShareID: share.ID, SubjectID: share.SkillName, SubjectName: share.SkillName,
				OwnerUserID: share.OwnerUserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName, OwnerUser: share.OwnerUser,
				GranteeUserID: share.GranteeUserID, ShareScopes: share.ToolNames, CreatedAt: share.CreatedAt,
			})
		}
	case models.ShareKindPhotoAlbum:
		return tx.ListPhotoAlbumSharesReaching(user.ID)
	case models.ShareKindRecording:
		shares, err := tx.ListRecordingSharesReaching(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, recordingShareRow(share))
		}
	case models.ShareKindFinance:
		shares, err := tx.ListFinanceSharesReaching(user.ID)
		if err != nil {
			return nil, err
		}
		for _, share := range shares {
			rows = append(rows, financeShareRow(share))
		}
	case models.ShareKindPasswords:
		return tx.ListPasswordSharesReaching(user.ID)
	}
	return rows, nil
}

// recordingShareRow is a share of a recording as a row of the list: the same
// for the shares a person made and the ones that reach them, since the
// listings fill in the owner and the grantee both ways.
func recordingShareRow(share *models.RecordingShare) *models.ShareRow {
	return &models.ShareRow{
		ShareKind: models.ShareKindRecording, ShareID: share.ID, SubjectID: share.RecordingID, SubjectName: share.RecordingTitle,
		OwnerUserID: share.OwnerUserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName, OwnerUser: share.OwnerUser,
		GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
		GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
		ShareLevel: string(share.RecordingShareLevel), CreatedAt: share.CreatedAt,
	}
}

// financeShareRow is a share of a finance account as a row of the list, for
// the same reason as recordingShareRow.
func financeShareRow(share *models.FinanceShare) *models.ShareRow {
	return &models.ShareRow{
		ShareKind: models.ShareKindFinance, ShareID: share.ID, SubjectID: share.FinanceAccountID, SubjectName: share.AccountName,
		SubjectMask: share.AccountMask, OwnerUserID: share.OwnerUserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName, OwnerUser: share.OwnerUser,
		GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
		GranteeCircleID: share.GranteeCircleID, GranteeCircleName: share.GranteeCircleName,
		ShareLevel: string(share.FinanceShareLevel), CreatedAt: share.CreatedAt,
	}
}

func describeSharingMade(row *models.ShareRow) *SharingMadeView {
	view := &SharingMadeView{
		ShareKind: string(row.ShareKind), ShareID: row.ShareID, SubjectID: row.SubjectID, SubjectName: row.SubjectName,
		SubjectMask: row.SubjectMask, IsSubjectDeleted: row.IsSubjectDeleted, ShareLevel: row.ShareLevel,
		ShareScopes: nonNilStrings(row.ShareScopes), PasswordItemCount: row.ItemCount,
		GranteeUsername: row.GranteeUsername, GranteeName: row.GranteeName, GranteeCircleName: row.GranteeCircleName,
		CreatedAt: row.CreatedAt,
	}
	if row.GranteeUserID != "" {
		view.GranteeUserID = &row.GranteeUserID
		view.GranteeAvatarColorIndex = avatar.InitialColorIndex(row.GranteeUserID)
	}
	if row.GranteeCircleID != "" {
		view.GranteeCircleID = &row.GranteeCircleID
	}
	return view
}

func describeSharingReceived(row *models.ShareRow) *SharingReceivedView {
	// A row made to the person by name that also carries a circle is one a
	// circle share raises to a higher level: it can be declined, and the circle's
	// share stays.
	isDirect := row.GranteeUserID != "" && row.GranteeCircleID == ""
	canDecline := row.GranteeUserID != "" && row.ShareKind != models.ShareKindPasswords
	view := &SharingReceivedView{
		ShareKind: string(row.ShareKind), ShareID: row.ShareID, SubjectID: row.SubjectID, SubjectName: row.SubjectName,
		SubjectMask: row.SubjectMask, ShareLevel: row.ShareLevel, ShareScopes: nonNilStrings(row.ShareScopes), PasswordItemCount: row.ItemCount,
		OwnerUserID: row.OwnerUserID, OwnerUsername: row.OwnerUsername, OwnerName: row.OwnerName,
		OwnerAvatarURL: avatarUrl(row.OwnerUser), OwnerAvatarColorIndex: avatar.InitialColorIndex(row.OwnerUserID),
		GranteeCircleName: row.GranteeCircleName, IsDirect: isDirect,
		CanDecline: canDecline, CreatedAt: row.CreatedAt,
	}
	if row.GranteeCircleID != "" {
		view.GranteeCircleID = &row.GranteeCircleID
	}
	return view
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// sortShareRowsForList puts the rows in the order of the kinds, then the
// owner's name for what is received and who it is shared with for what is made,
// then the oldest first.
func sortShareRowsForList(rows []*models.ShareRow, isReceived bool) {
	kindOrder := map[models.ShareKind]int{}
	for index, shareKind := range models.ShareKinds {
		kindOrder[shareKind] = index
	}
	whoseName := func(row *models.ShareRow) string {
		if isReceived {
			return strings.ToLower(row.OwnerName)
		}
		if row.GranteeCircleID != "" {
			return strings.ToLower(row.GranteeCircleName)
		}
		return strings.ToLower(row.GranteeName)
	}
	sort.SliceStable(rows, func(first, second int) bool {
		left, right := rows[first], rows[second]
		if kindOrder[left.ShareKind] != kindOrder[right.ShareKind] {
			return kindOrder[left.ShareKind] < kindOrder[right.ShareKind]
		}
		if leftName, rightName := whoseName(left), whoseName(right); leftName != rightName {
			return leftName < rightName
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ShareID < right.ShareID
	})
}

func (self *graph) ListSharing(ctx context.Context) (*SharingView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionCircleUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	view := &SharingView{SharesMade: []*SharingMadeView{}, SharesReceived: []*SharingReceivedView{}}
	madeRows, receivedRows := []*models.ShareRow{}, []*models.ShareRow{}
	for _, shareKind := range models.ShareKinds {
		if self.canListShareKind(ctx, principal, shareKind, false) {
			rows, err := self.sharesMadeBy(tx, principal.User, shareKind)
			if err != nil {
				return nil, err
			}
			madeRows = append(madeRows, rows...)
		}
		if self.canListShareKind(ctx, principal, shareKind, true) {
			rows, err := self.sharesReachingFor(tx, principal.User, shareKind)
			if err != nil {
				return nil, err
			}
			receivedRows = append(receivedRows, rows...)
		}
	}
	sortShareRowsForList(madeRows, false)
	sortShareRowsForList(receivedRows, true)
	for _, row := range madeRows {
		view.SharesMade = append(view.SharesMade, describeSharingMade(row))
	}
	for _, row := range receivedRows {
		view.SharesReceived = append(view.SharesReceived, describeSharingReceived(row))
	}
	return view, nil
}

func (self *graph) DeclineShare(ctx context.Context, arguments DeclineShareArguments) (bool, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return false, err
	}
	shareKind := models.ShareKind(strings.ToLower(strings.TrimSpace(arguments.ShareKind)))
	shareId := strings.TrimSpace(arguments.ShareID)
	tx := self.transaction(ctx)
	switch shareKind {
	case models.ShareKindCalendar:
		share, err := tx.GetCalendarShare(shareId)
		if err != nil {
			return false, err
		}
		// A share to a circle, and one made to somebody else, are as good as
		// none.
		if share == nil || share.GranteeUserID != user.ID {
			return false, api.ErrNotFound
		}
		if err := tx.DeleteCalendarShare(share.ID); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindLocation:
		ownerUserId, err := tx.DeclineLocationShare(user.ID, shareId)
		if err != nil {
			return false, translateError(err)
		}
		// The caller's map would keep the last position it was shown, so the
		// listeners are told as they are when an owner takes a share back.
		level, err := tx.GetLocationShareLevel(user.ID, ownerUserId)
		if err != nil {
			return false, err
		}
		if level == "" {
			callerUserId := user.ID
			tx.AfterCommit(func() {
				if err := self.database.NotifyLocationEvent(ownerUserId, callerUserId); err != nil {
					log.Warningf("cannot tell the listeners that a location share was declined: %s", err)
				}
			})
		}
	case models.ShareKindHealth:
		if err := tx.DeclineHealthShare(user.ID, shareId); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindMemoryPage:
		share, err := tx.GetPageShare(shareId)
		if err != nil {
			return false, err
		}
		if share == nil || share.GranteeUserID != user.ID {
			return false, api.ErrNotFound
		}
		if err := tx.DeletePageShare(share.ID); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindSkill:
		share, err := tx.GetSkillShare(shareId)
		if err != nil {
			return false, err
		}
		if share == nil || share.GranteeUserID != user.ID {
			return false, api.ErrNotFound
		}
		if err := tx.DeleteSkillShare(share.ID); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindPhotoAlbum:
		if err := tx.DeclinePhotoAlbumShare(user.ID, shareId); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindRecording:
		if err := tx.DeclineRecordingShare(user.ID, shareId); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindFinance:
		// The agent neither reads nor ends what others share of finance.
		if err := requireFinanceNotAgent(ctx); err != nil {
			return false, err
		}
		if err := tx.DeclineFinanceShare(user.ID, shareId); err != nil {
			return false, translateError(err)
		}
	case models.ShareKindPasswords:
		return false, fmt.Errorf("%w: passwords shared with you are removed by the person who shared them, on their Passwords page", api.ErrInvalidArguments)
	default:
		return false, fmt.Errorf("%w: the kind is calendar, location, health, memory_page, photo_album, recording, finance or skill", api.ErrInvalidArguments)
	}
	log.Noticef("%s declined a share of %s", user.Username, shareKind)
	return true, nil
}
