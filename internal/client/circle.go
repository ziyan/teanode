package client

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Circle is a named set of people on this server who know each other.
type Circle struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CircleKind string    `json:"circleKind"`
	CreatedAt  time.Time `json:"createdAt"`

	// MemberRole is the signed-in account's own role in it.
	MemberRole string `json:"memberRole"`

	Members []*CircleMember `json:"members"`

	// PendingInvitations is empty unless the signed-in account is an admin.
	PendingInvitations []*CircleInvitation `json:"pendingInvitations"`
}

// CircleMember is one person in a circle.
type CircleMember struct {
	UserID           string    `json:"userId"`
	Username         string    `json:"username"`
	Name             string    `json:"name"`
	AvatarURL        *string   `json:"avatarUrl"`
	AvatarColorIndex int       `json:"avatarColorIndex"`
	MemberRole       string    `json:"memberRole"`
	JoinedAt         time.Time `json:"joinedAt"`
}

// CircleInvitation asks one person to join one circle.
type CircleInvitation struct {
	ID              string    `json:"id"`
	CircleID        string    `json:"circleId"`
	CircleName      string    `json:"circleName"`
	CircleKind      string    `json:"circleKind"`
	InvitedByName   string    `json:"invitedByName"`
	InvitedUsername string    `json:"invitedUsername"`
	MemberRole      string    `json:"memberRole"`
	CreatedAt       time.Time `json:"createdAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

const circleMemberFields = `{ userId username name avatarUrl avatarColorIndex memberRole joinedAt }`

const circleInvitationFields = `{ id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt }`

const circleFields = `{ id name circleKind createdAt memberRole members ` + circleMemberFields + ` pendingInvitations ` + circleInvitationFields + ` }`

// ListCircles returns the circles the signed-in account is in.
func ListCircles(ctx context.Context, connection Executor) ([]*Circle, error) {
	var result struct {
		ListCircles []*Circle `json:"ListCircles"`
	}
	if err := connection.Execute(ctx, `query { ListCircles `+circleFields+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListCircles, nil
}

// ReadCircle returns one circle the signed-in account is in.
func ReadCircle(ctx context.Context, connection *Client, circleId string) (*Circle, error) {
	var result struct {
		ReadCircle *Circle `json:"ReadCircle"`
	}
	query := `query ($circleId: String!) { ReadCircle(circleId: $circleId) ` + circleFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"circleId": circleId}, &result); err != nil {
		return nil, err
	}
	return result.ReadCircle, nil
}

// ListCircleInvitations returns the invitations to the signed-in account that
// can still be answered.
func ListCircleInvitations(ctx context.Context, connection *Client) ([]*CircleInvitation, error) {
	var result struct {
		ListCircleInvitations []*CircleInvitation `json:"ListCircleInvitations"`
	}
	if err := connection.Execute(ctx, `query { ListCircleInvitations `+circleInvitationFields+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListCircleInvitations, nil
}

// ListCirclePeople returns everybody the signed-in account shares a circle
// with, once each.
func ListCirclePeople(ctx context.Context, connection Executor) ([]*CircleMember, error) {
	var result struct {
		ListCirclePeople []*CircleMember `json:"ListCirclePeople"`
	}
	if err := connection.Execute(ctx, `query { ListCirclePeople `+circleMemberFields+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListCirclePeople, nil
}

// CreateCircle makes a circle with the signed-in account as its admin.
func CreateCircle(ctx context.Context, connection *Client, name, circleKind string) (*Circle, error) {
	var result struct {
		CreateCircle *Circle `json:"CreateCircle"`
	}
	query := `mutation ($name: String!, $circleKind: String!) {
		CreateCircle(name: $name, circleKind: $circleKind) ` + circleFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"name": name, "circleKind": circleKind}, &result); err != nil {
		return nil, err
	}
	return result.CreateCircle, nil
}

// RenameCircle changes a circle's name.
func RenameCircle(ctx context.Context, connection *Client, circleId, name string) (*Circle, error) {
	var result struct {
		RenameCircle *Circle `json:"RenameCircle"`
	}
	query := `mutation ($circleId: String!, $name: String!) {
		RenameCircle(circleId: $circleId, name: $name) ` + circleFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"circleId": circleId, "name": name}, &result); err != nil {
		return nil, err
	}
	return result.RenameCircle, nil
}

// DeleteCircle removes a circle.
func DeleteCircle(ctx context.Context, connection *Client, circleId string) error {
	query := `mutation ($circleId: String!) { DeleteCircle(circleId: $circleId) }`
	return connection.Execute(ctx, query, map[string]any{"circleId": circleId}, nil)
}

// InviteToCircle invites an account by username. The server answers the same
// whether or not the username is an account.
func InviteToCircle(ctx context.Context, connection *Client, circleId, username, memberRole string) error {
	query := `mutation ($circleId: String!, $username: String!, $memberRole: String!) {
		InviteToCircle(circleId: $circleId, username: $username, memberRole: $memberRole)
	}`
	return connection.Execute(ctx, query, map[string]any{"circleId": circleId, "username": username, "memberRole": memberRole}, nil)
}

// AnswerCircleInvitation accepts or declines an invitation. Accepting returns
// the circle joined; declining returns nil.
func AnswerCircleInvitation(ctx context.Context, connection *Client, invitationId string, isAccepted bool) (*Circle, error) {
	var result struct {
		AnswerCircleInvitation *Circle `json:"AnswerCircleInvitation"`
	}
	query := `mutation ($invitationId: String!, $isAccepted: Boolean!) {
		AnswerCircleInvitation(invitationId: $invitationId, isAccepted: $isAccepted) ` + circleFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"invitationId": invitationId, "isAccepted": isAccepted}, &result); err != nil {
		return nil, err
	}
	return result.AnswerCircleInvitation, nil
}

// WithdrawCircleInvitation takes back an invitation nobody has answered.
func WithdrawCircleInvitation(ctx context.Context, connection *Client, invitationId string) error {
	query := `mutation ($invitationId: String!) { WithdrawCircleInvitation(invitationId: $invitationId) }`
	return connection.Execute(ctx, query, map[string]any{"invitationId": invitationId}, nil)
}

// circleMemberId turns the username the command line speaks in into the
// identifier the schema takes, from the circle's own members.
func circleMemberId(ctx context.Context, connection *Client, circleId, username string) (string, error) {
	circle, err := ReadCircle(ctx, connection, circleId)
	if err != nil {
		return "", err
	}
	for _, member := range circle.Members {
		if strings.EqualFold(member.Username, strings.TrimSpace(username)) {
			return member.UserID, nil
		}
	}
	return "", fmt.Errorf("client: nobody called %q is in the circle %q", username, circle.Name)
}

// SetCircleMemberRole makes a member an admin, or an admin a member.
func SetCircleMemberRole(ctx context.Context, connection *Client, circleId, username, memberRole string) (*Circle, error) {
	var result struct {
		SetCircleMemberRole *Circle `json:"SetCircleMemberRole"`
	}
	userId, err := circleMemberId(ctx, connection, circleId, username)
	if err != nil {
		return nil, err
	}
	query := `mutation ($circleId: String!, $userId: String!, $memberRole: String!) {
		SetCircleMemberRole(circleId: $circleId, userId: $userId, memberRole: $memberRole) ` + circleFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"circleId": circleId, "userId": userId, "memberRole": memberRole}, &result); err != nil {
		return nil, err
	}
	return result.SetCircleMemberRole, nil
}

// RemoveCircleMember takes a member out of a circle. The circle it returns is
// nil when the caller took themselves out.
func RemoveCircleMember(ctx context.Context, connection *Client, circleId, username string) (*Circle, error) {
	var result struct {
		RemoveCircleMember *Circle `json:"RemoveCircleMember"`
	}
	userId, err := circleMemberId(ctx, connection, circleId, username)
	if err != nil {
		return nil, err
	}
	query := `mutation ($circleId: String!, $userId: String!) {
		RemoveCircleMember(circleId: $circleId, userId: $userId) ` + circleFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"circleId": circleId, "userId": userId}, &result); err != nil {
		return nil, err
	}
	return result.RemoveCircleMember, nil
}

// LeaveCircle takes the signed-in account out of a circle.
func LeaveCircle(ctx context.Context, connection *Client, circleId string) error {
	query := `mutation ($circleId: String!) { LeaveCircle(circleId: $circleId) }`
	return connection.Execute(ctx, query, map[string]any{"circleId": circleId}, nil)
}

// AddCircleMember puts an account in a circle without an invitation. Needs
// user:manage.
func AddCircleMember(ctx context.Context, connection *Client, circleId, username, memberRole string) (*Circle, error) {
	var result struct {
		AddCircleMember *Circle `json:"AddCircleMember"`
	}
	query := `mutation ($circleId: String!, $username: String!, $memberRole: String!) {
		AddCircleMember(circleId: $circleId, username: $username, memberRole: $memberRole) ` + circleFields + `
	}`
	if err := connection.Execute(ctx, query, map[string]any{"circleId": circleId, "username": username, "memberRole": memberRole}, &result); err != nil {
		return nil, err
	}
	return result.AddCircleMember, nil
}
