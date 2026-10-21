package passwords

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrNotUsableByAgent is an attempt to open an item for the agent when the
// item's own sealed access record does not let the agent use it. The server's
// flag alone is not enough: only the owner's device writes the record.
var ErrNotUsableByAgent = errors.New("passwords: the owner's sealed access record does not let the agent use this item")

// PasswordAccess is who may have an item and whether the agent may use it. It
// is a key inside the sealed fields of every kind of item, written only by the
// owner's device and sealed again whenever shares or agent use change, so the
// server cannot widen it. A missing record means nobody is granted and the
// agent may not use the item. The JSON is
//
//	{"isUsableByAgent": false, "grantees": [{"userId": "..."}, {"circleId": "..."}]}
//
// and a reader refuses anything of another shape, so an item with such a
// record is unreadable instead of half understood.
type PasswordAccess struct {
	IsUsableByAgent bool              `json:"isUsableByAgent"`
	Grantees        []PasswordGrantee `json:"grantees"`
}

// PasswordGrantee is one person or one circle that may have an item. Exactly
// one of the two is set.
type PasswordGrantee struct {
	UserID   string `json:"userId,omitempty"`
	CircleID string `json:"circleId,omitempty"`
}

// MarshalJSON writes an empty list, not null, for a record with no grantees.
func (self PasswordAccess) MarshalJSON() ([]byte, error) {
	type plainAccess PasswordAccess
	access := plainAccess(self)
	if access.Grantees == nil {
		access.Grantees = []PasswordGrantee{}
	}
	return json.Marshal(access)
}

// UnmarshalJSON reads a record and refuses any other shape: a missing or
// mistyped key, a key that is not one of the two, or a grantee that is not
// exactly one non-empty user id or circle id.
func (self *PasswordAccess) UnmarshalJSON(data []byte) error {
	var raw struct {
		IsUsableByAgent *bool              `json:"isUsableByAgent"`
		Grantees        *[]PasswordGrantee `json:"grantees"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("passwords: the access record is not readable: %w", err)
	}
	if raw.IsUsableByAgent == nil || raw.Grantees == nil {
		return errors.New("passwords: the access record needs isUsableByAgent and grantees")
	}
	self.IsUsableByAgent = *raw.IsUsableByAgent
	self.Grantees = *raw.Grantees
	return nil
}

// UnmarshalJSON reads a grantee and refuses one that is not exactly one
// non-empty user id or circle id.
func (self *PasswordGrantee) UnmarshalJSON(data []byte) error {
	var raw struct {
		UserID   *string `json:"userId"`
		CircleID *string `json:"circleId"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("passwords: a grantee is not readable: %w", err)
	}
	isUserSet := raw.UserID != nil
	isCircleSet := raw.CircleID != nil
	if isUserSet == isCircleSet {
		return errors.New("passwords: a grantee needs exactly one of userId and circleId")
	}
	if isUserSet && *raw.UserID == "" || isCircleSet && *raw.CircleID == "" {
		return errors.New("passwords: a grantee needs a non-empty id")
	}
	self.UserID = ""
	self.CircleID = ""
	if isUserSet {
		self.UserID = *raw.UserID
	} else {
		self.CircleID = *raw.CircleID
	}
	return nil
}

// IsReachedByAccess reports whether the access record grants an item to a
// recipient: a person grantee by user id, or a circle grantee when the
// recipient is a member of that circle. Circle membership is the server's word
// and is passed in; a recipient still needs a confirmed key before anything is
// sealed for them. A missing record reaches nobody.
func IsReachedByAccess(access *PasswordAccess, recipientUserId string, circleIdsOfRecipient []string) bool {
	if access == nil || recipientUserId == "" {
		return false
	}
	for _, grantee := range access.Grantees {
		if grantee.UserID != "" && grantee.UserID == recipientUserId {
			return true
		}
		if grantee.CircleID != "" && slices.Contains(circleIdsOfRecipient, grantee.CircleID) {
			return true
		}
	}
	return false
}

// AccessWithGrantees returns a copy of the record with these grantees and the
// same agent use. A missing record is treated as one that grants nobody and
// does not let the agent use the item. The given record is not changed.
func AccessWithGrantees(access *PasswordAccess, grantees []PasswordGrantee) *PasswordAccess {
	result := &PasswordAccess{Grantees: slices.Clone(grantees)}
	if access != nil {
		result.IsUsableByAgent = access.IsUsableByAgent
	}
	if result.Grantees == nil {
		result.Grantees = []PasswordGrantee{}
	}
	return result
}

// AccessWithAgentUse returns a copy of the record with agent use turned on or
// off and the same grantees. A missing record is treated as one that grants
// nobody. The given record is not changed.
func AccessWithAgentUse(access *PasswordAccess, isUsableByAgent bool) *PasswordAccess {
	result := &PasswordAccess{IsUsableByAgent: isUsableByAgent, Grantees: []PasswordGrantee{}}
	if access != nil {
		result.Grantees = slices.Clone(access.Grantees)
		if result.Grantees == nil {
			result.Grantees = []PasswordGrantee{}
		}
	}
	return result
}

// ParseAccessOfFields reads the access record out of an item's fields JSON, of
// any kind. It returns nil when the fields carry none, and an error when they
// carry one of the wrong shape.
func ParseAccessOfFields(fieldsJSON []byte) (*PasswordAccess, error) {
	var parsed struct {
		Access *PasswordAccess `json:"access"`
	}
	if err := json.Unmarshal(fieldsJSON, &parsed); err != nil {
		return nil, fmt.Errorf("passwords: read the access record: %w", err)
	}
	return parsed.Access, nil
}
