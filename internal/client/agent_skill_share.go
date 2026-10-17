package client

import (
	"context"
	"time"
)

// Skills shared with other people, and the calls made through those shares.
// The same operations the dashboard, the app and the agent's skill tool use.

// SkillShare is one skill share the signed-in account made or was given.
type SkillShare struct {
	ID        string   `json:"id"`
	SkillName string   `json:"skillName"`
	ToolNames []string `json:"toolNames"`

	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	GranteeUserID           string  `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`

	// The limits are in the minor unit of CurrencyCode (cents).
	PaymentLimitMinor  *int64 `json:"paymentLimitMinor"`
	MonthlyLimitMinor  *int64 `json:"monthlyLimitMinor"`
	CurrencyCode       string `json:"currencyCode"`
	IsApprovalRequired bool   `json:"isApprovalRequired"`

	IsOwnedByCaller  bool      `json:"isOwnedByCaller"`
	WaitingCallCount int       `json:"waitingCallCount"`
	MonthTotalMinor  int64     `json:"monthTotalMinor"`
	CreatedAt        time.Time `json:"createdAt"`
}

// SkillShareCall is one call made through a share.
type SkillShareCall struct {
	ID           string `json:"id"`
	SkillShareID string `json:"skillShareId"`
	SkillName    string `json:"skillName"`

	GranteeUserID           string  `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`
	OwnerName               string  `json:"ownerName"`

	ToolName      string `json:"toolName"`
	ToolArguments string `json:"toolArguments"`
	RequestReason string `json:"requestReason"`

	AmountMinor  *int64 `json:"amountMinor"`
	CurrencyCode string `json:"currencyCode"`

	CallState    string     `json:"callState"`
	ToolOutput   string     `json:"toolOutput"`
	DecidedAt    *time.Time `json:"decidedAt"`
	DecisionNote string     `json:"decisionNote"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

const (
	skillShareFields = `id skillName toolNames ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
    granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex
    paymentLimitMinor monthlyLimitMinor currencyCode isApprovalRequired isOwnedByCaller waitingCallCount monthTotalMinor createdAt`

	skillShareCallFields = `id skillShareId skillName granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex ownerName
    toolName toolArguments requestReason amountMinor currencyCode callState toolOutput decidedAt decisionNote expiresAt createdAt`

	// DocumentListSkillShares lists the shares the caller made and was given.
	DocumentListSkillShares = `query { ListSkillShares { ` + skillShareFields + ` } }`

	// DocumentShareSkill shares tools of a skill with a person.
	DocumentShareSkill = `mutation ($skillName: String!, $toolNames: [String!]!, $granteeUserId: String!, $paymentLimitMinor: Int64, $monthlyLimitMinor: Int64, $currencyCode: String, $isApprovalRequired: Boolean) {
  ShareSkill(skillName: $skillName, toolNames: $toolNames, granteeUserId: $granteeUserId, paymentLimitMinor: $paymentLimitMinor, monthlyLimitMinor: $monthlyLimitMinor, currencyCode: $currencyCode, isApprovalRequired: $isApprovalRequired) {
    ` + skillShareFields + `
  }
}`

	// DocumentUnshareSkill takes a share back.
	DocumentUnshareSkill = `mutation ($shareId: String!) { UnshareSkill(shareId: $shareId) }`

	// DocumentListSkillShareCalls lists the calls made through a share.
	DocumentListSkillShareCalls = `query ($shareId: String!, $callState: String, $limit: Int) {
  ListSkillShareCalls(shareId: $shareId, callState: $callState, limit: $limit) { ` + skillShareCallFields + ` }
}`

	// DocumentAnswerSkillShareCall approves or declines a call that waits.
	DocumentAnswerSkillShareCall = `mutation ($callId: String!, $isApproved: Boolean!, $decisionNote: String) {
  AnswerSkillShareCall(callId: $callId, isApproved: $isApproved, decisionNote: $decisionNote) { ` + skillShareCallFields + ` }
}`
)

// ListSkillShares are the shares the signed-in account made and was given.
func ListSkillShares(ctx context.Context, connection *Client) ([]*SkillShare, error) {
	var answer struct {
		ListSkillShares []*SkillShare `json:"ListSkillShares"`
	}
	if err := connection.Execute(ctx, DocumentListSkillShares, nil, &answer); err != nil {
		return nil, err
	}
	return answer.ListSkillShares, nil
}

// SkillShareTerms are what a share lets the other person do.
type SkillShareTerms struct {
	SkillName     string
	ToolNames     []string
	GranteeUserID string

	// The limits are in the minor unit of CurrencyCode; nil is none.
	PaymentLimitMinor  *int64
	MonthlyLimitMinor  *int64
	CurrencyCode       string
	IsApprovalRequired bool
}

// ShareSkill shares tools of an installed skill with a person, or changes the
// terms when it is shared with them already.
func ShareSkill(ctx context.Context, connection *Client, terms *SkillShareTerms) (*SkillShare, error) {
	variables := map[string]any{
		"skillName": terms.SkillName, "toolNames": terms.ToolNames, "granteeUserId": terms.GranteeUserID,
		"isApprovalRequired": terms.IsApprovalRequired, "paymentLimitMinor": nil, "monthlyLimitMinor": nil, "currencyCode": nil,
	}
	if terms.PaymentLimitMinor != nil {
		variables["paymentLimitMinor"] = *terms.PaymentLimitMinor
	}
	if terms.MonthlyLimitMinor != nil {
		variables["monthlyLimitMinor"] = *terms.MonthlyLimitMinor
	}
	if terms.CurrencyCode != "" {
		variables["currencyCode"] = terms.CurrencyCode
	}
	var answer struct {
		ShareSkill *SkillShare `json:"ShareSkill"`
	}
	if err := connection.Execute(ctx, DocumentShareSkill, variables, &answer); err != nil {
		return nil, err
	}
	return answer.ShareSkill, nil
}

// UnshareSkill takes a share back.
func UnshareSkill(ctx context.Context, connection *Client, shareId string) error {
	var answer struct {
		UnshareSkill bool `json:"UnshareSkill"`
	}
	return connection.Execute(ctx, DocumentUnshareSkill, map[string]any{"shareId": shareId}, &answer)
}

// ListSkillShareCalls are the calls made through a share, waiting ones first.
// A state narrows them; empty is every state.
func ListSkillShareCalls(ctx context.Context, connection *Client, shareId, callState string) ([]*SkillShareCall, error) {
	variables := map[string]any{"shareId": shareId, "callState": nil, "limit": nil}
	if callState != "" {
		variables["callState"] = callState
	}
	var answer struct {
		ListSkillShareCalls []*SkillShareCall `json:"ListSkillShareCalls"`
	}
	if err := connection.Execute(ctx, DocumentListSkillShareCalls, variables, &answer); err != nil {
		return nil, err
	}
	return answer.ListSkillShareCalls, nil
}

// AnswerSkillShareCall approves or declines a call that waits for the
// signed-in account, which owns the share. Approving runs it.
func AnswerSkillShareCall(ctx context.Context, connection *Client, callId string, isApproved bool, decisionNote string) (*SkillShareCall, error) {
	variables := map[string]any{"callId": callId, "isApproved": isApproved, "decisionNote": nil}
	if decisionNote != "" {
		variables["decisionNote"] = decisionNote
	}
	var answer struct {
		AnswerSkillShareCall *SkillShareCall `json:"AnswerSkillShareCall"`
	}
	if err := connection.Execute(ctx, DocumentAnswerSkillShareCall, variables, &answer); err != nil {
		return nil, err
	}
	return answer.AnswerSkillShareCall, nil
}
