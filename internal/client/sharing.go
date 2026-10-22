package client

import (
	"context"
	"time"
)

// Sharing is what the signed-in account shares and what is shared with it,
// across every kind of thing that can be shared.
type Sharing struct {
	SharesMade     []*SharingMade     `json:"sharesMade"`
	SharesReceived []*SharingReceived `json:"sharesReceived"`
}

// SharingMade is one share the account made. ShareKind is calendar, location,
// health, memory_page, photo_album, recording, finance, skill or passwords;
// ShareID is empty for passwords, whose shares are one for each item and are
// counted (PasswordItemCount), never named. SubjectMask is the last digits of
// a finance account's number. IsSubjectDeleted is true for a share of a memory
// page that no longer exists, which can still be stopped. Exactly one of
// GranteeUserID and GranteeCircleID is set.
type SharingMade struct {
	ShareKind         string    `json:"shareKind"`
	ShareID           string    `json:"shareId"`
	SubjectID         string    `json:"subjectId"`
	SubjectName       string    `json:"subjectName"`
	SubjectMask       string    `json:"subjectMask"`
	IsSubjectDeleted  bool      `json:"isSubjectDeleted"`
	ShareLevel        string    `json:"shareLevel"`
	ShareScopes       []string  `json:"shareScopes"`
	PasswordItemCount int       `json:"passwordItemCount"`
	GranteeUserID     *string   `json:"granteeUserId"`
	GranteeUsername   string    `json:"granteeUsername"`
	GranteeName       string    `json:"granteeName"`
	GranteeCircleID   *string   `json:"granteeCircleId"`
	GranteeCircleName string    `json:"granteeCircleName"`
	CreatedAt         time.Time `json:"createdAt"`
}

// SharingReceived is one share made to the account. A share through a circle
// names the circle (GranteeCircleName) and cannot be declined; one made by
// name (IsDirect) can, except passwords.
type SharingReceived struct {
	ShareKind         string    `json:"shareKind"`
	ShareID           string    `json:"shareId"`
	SubjectID         string    `json:"subjectId"`
	SubjectName       string    `json:"subjectName"`
	SubjectMask       string    `json:"subjectMask"`
	ShareLevel        string    `json:"shareLevel"`
	ShareScopes       []string  `json:"shareScopes"`
	PasswordItemCount int       `json:"passwordItemCount"`
	OwnerUserID       string    `json:"ownerUserId"`
	OwnerUsername     string    `json:"ownerUsername"`
	OwnerName         string    `json:"ownerName"`
	GranteeCircleID   *string   `json:"granteeCircleId"`
	GranteeCircleName string    `json:"granteeCircleName"`
	IsDirect          bool      `json:"isDirect"`
	CanDecline        bool      `json:"canDecline"`
	CreatedAt         time.Time `json:"createdAt"`
}

const (
	// DocumentListSharing is the query behind "teanode sharing" and the
	// agent's circle tool.
	DocumentListSharing = `query { ListSharing {
  sharesMade { shareKind shareId subjectId subjectName subjectMask isSubjectDeleted shareLevel shareScopes passwordItemCount granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName createdAt }
  sharesReceived { shareKind shareId subjectId subjectName subjectMask shareLevel shareScopes passwordItemCount ownerUserId ownerUsername ownerName granteeCircleId granteeCircleName isDirect canDecline createdAt }
} }`

	// DocumentDeclineShare ends a share made to the account by name.
	DocumentDeclineShare = `mutation ($shareKind: String!, $shareId: String!) { DeclineShare(shareKind: $shareKind, shareId: $shareId) }`
)

// ListSharing returns what the signed-in account shares and what is shared
// with it.
func ListSharing(ctx context.Context, connection Executor) (*Sharing, error) {
	var result struct {
		ListSharing *Sharing `json:"ListSharing"`
	}
	if err := connection.Execute(ctx, DocumentListSharing, nil, &result); err != nil {
		return nil, err
	}
	return result.ListSharing, nil
}

// DeclineShare ends a share that was made to the signed-in account by name.
func DeclineShare(ctx context.Context, connection Executor, shareKind, shareId string) error {
	var result struct {
		DeclineShare bool `json:"DeclineShare"`
	}
	return connection.Execute(ctx, DocumentDeclineShare, map[string]any{"shareKind": shareKind, "shareId": shareId}, &result)
}
