package passwords

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// accessRecordLabel starts the bytes an access record is signed over and is
// the HKDF info of the key that signs them, so the key is used for nothing
// else and a change of the recipe changes every signature.
const accessRecordLabel = "teanode passwords access record v1"

// MaximumRecordVersion is the highest record version any implementation
// accepts: the largest integer a JavaScript number holds exactly.
const MaximumRecordVersion = 1<<53 - 1

// ErrAccessNotVerified is a record that cannot be trusted: there is none, it
// has no version or no signature, or the signature does not match. A device
// that meets it makes nothing automatic from the item (no re-key, no agent
// copy) and asks the person to set sharing again.
var ErrAccessNotVerified = errors.New("passwords: the access record is missing or not signed by the owner's devices")

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
//	{"isUsableByAgent": false, "grantees": [{"userId": "..."}, {"circleId": "..."}],
//	 "recordVersion": 3, "recordMac": "..."}
//
// and a reader refuses anything of another shape, so an item with such a
// record is unreadable instead of half understood. The last two keys are the
// signature: RecordVersion counts the owner's changes and RecordMac is an
// HMAC-SHA256 under a key made from the account key (see MACOfAccess), which
// only the owner's devices hold. A record without them is read, but VerifyAccess
// does not trust it. The functions that change a record (AccessWithGrantees
// and AccessWithAgentUse) return one without them, to be signed again.
type PasswordAccess struct {
	IsUsableByAgent bool              `json:"isUsableByAgent"`
	Grantees        []PasswordGrantee `json:"grantees"`
	// RecordVersion is 0 for a record that has none, else a positive integer.
	RecordVersion int `json:"recordVersion,omitempty"`
	// RecordMac is the 32 byte signature, or nil for a record that has none.
	// It is standard base64 with padding in the JSON.
	RecordMac []byte `json:"recordMac,omitempty"`
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
// mistyped key, a key that is not one of the four, a grantee that is not
// exactly one non-empty user id or circle id, a version that is not a positive
// whole number (1.0 is 1), or a signature that is not canonical base64 of 32
// bytes. The version and the signature may be missing.
func (self *PasswordAccess) UnmarshalJSON(data []byte) error {
	var raw struct {
		IsUsableByAgent *bool              `json:"isUsableByAgent"`
		Grantees        *[]PasswordGrantee `json:"grantees"`
		RecordVersion   json.RawMessage    `json:"recordVersion"`
		RecordMac       json.RawMessage    `json:"recordMac"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("passwords: the access record is not readable: %w", err)
	}
	if raw.IsUsableByAgent == nil || raw.Grantees == nil {
		return errors.New("passwords: the access record needs isUsableByAgent and grantees")
	}
	recordVersion := 0
	if raw.RecordVersion != nil {
		version, err := readRecordVersion(raw.RecordVersion)
		if err != nil {
			return err
		}
		recordVersion = version
	}
	var recordMac []byte
	if raw.RecordMac != nil {
		var text string
		if err := json.Unmarshal(raw.RecordMac, &text); err != nil {
			return errors.New("passwords: the access record's recordMac is not text")
		}
		decoded, err := DecodeCanonicalBase64(text)
		if err != nil || len(decoded) != KeyLength {
			return errors.New("passwords: the access record's recordMac is not base64 of 32 bytes")
		}
		recordMac = decoded
	}
	self.IsUsableByAgent = *raw.IsUsableByAgent
	self.Grantees = *raw.Grantees
	self.RecordVersion = recordVersion
	self.RecordMac = recordMac
	return nil
}

// readRecordVersion reads the recordVersion number of an access record. It is
// a JSON number that is a whole number from 1 to MaximumRecordVersion, so 1,
// 1.0 and 1e0 are the same version, as they are in the dashboard (a JavaScript
// number) and on the iPhone. Text, null, a fraction, and anything out of range
// are refused.
func readRecordVersion(raw json.RawMessage) (int, error) {
	text := string(raw)
	if text == "" || text[0] != '-' && (text[0] < '0' || text[0] > '9') {
		return 0, errors.New("passwords: the access record's recordVersion is not an integer")
	}
	version, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		number, numberErr := strconv.ParseFloat(text, 64)
		if numberErr != nil || number != math.Trunc(number) || number < 1 || number > MaximumRecordVersion {
			return 0, errors.New("passwords: the access record's recordVersion is not a positive integer")
		}
		version = int64(number)
	}
	if version < 1 || version > MaximumRecordVersion {
		return 0, errors.New("passwords: the access record's recordVersion is not a positive integer")
	}
	return int(version), nil
}

// readGranteeID reads one id of a grantee. It returns "" when the key is
// missing. A key that is there must be a non-empty JSON string, so null beside
// the other id is refused as it is in the dashboard and on the iPhone.
func readGranteeID(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil || bytes.Equal(raw, []byte("null")) {
		return "", errors.New("passwords: a grantee's id is not text")
	}
	if id == "" {
		return "", errors.New("passwords: a grantee needs a non-empty id")
	}
	return id, nil
}

// UnmarshalJSON reads a grantee and refuses one that is not exactly one
// non-empty user id or circle id, written as text. A key that is present with
// null counts as present and is refused.
func (self *PasswordGrantee) UnmarshalJSON(data []byte) error {
	var raw struct {
		UserID   json.RawMessage `json:"userId"`
		CircleID json.RawMessage `json:"circleId"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("passwords: a grantee is not readable: %w", err)
	}
	if (raw.UserID == nil) == (raw.CircleID == nil) {
		return errors.New("passwords: a grantee needs exactly one of userId and circleId")
	}
	userID, err := readGranteeID(raw.UserID)
	if err != nil {
		return err
	}
	circleID, err := readGranteeID(raw.CircleID)
	if err != nil {
		return err
	}
	self.UserID = userID
	self.CircleID = circleID
	return nil
}

// hasControlCharacter reports whether the text has a NUL or any other control
// character (Unicode category Cc: U+0000 to U+001F and U+007F to U+009F). A
// grantee id with one is refused when signing and when verifying: the signed
// bytes separate their parts with a zero byte, and a control character hides in
// a display.
func hasControlCharacter(text string) bool {
	return strings.ContainsFunc(text, unicode.IsControl)
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

// accessMacKey is the key the access record is signed with: HKDF-SHA256 of the
// account key with no salt and the label as the info, 32 bytes. Only the
// owner's devices hold the account key, so only they can sign a record.
func accessMacKey(accountKey []byte) ([]byte, error) {
	if len(accountKey) != KeyLength {
		return nil, fmt.Errorf("passwords: an account key is %d bytes, not %d", KeyLength, len(accountKey))
	}
	key, err := hkdf.Key(sha256.New, accountKey, nil, accessRecordLabel, KeyLength)
	if err != nil {
		return nil, fmt.Errorf("passwords: derive access record key: %w", err)
	}
	return key, nil
}

// accessMacBytes is what the signature covers: the label, a zero byte, the item
// id, a zero byte, the item kind, a zero byte, the record version in decimal, a
// zero byte, "1" or "0" for agent use, a zero byte, then each grantee as "u:"
// and the user id or "c:" and the circle id, in byte order, each followed by a
// zero byte. Order in the record does not matter, and nothing the server can
// change is in it.
func accessMacBytes(itemId string, itemKind ItemKind, access *PasswordAccess) ([]byte, error) {
	if access == nil {
		return nil, errors.New("passwords: there is no access record to sign")
	}
	if !itemKind.IsValid() {
		return nil, fmt.Errorf("passwords: %q is not a kind of item", itemKind)
	}
	if access.RecordVersion < 1 || access.RecordVersion > MaximumRecordVersion {
		return nil, errors.New("passwords: the access record needs a positive version to be signed")
	}
	grantees := make([]string, 0, len(access.Grantees))
	for _, grantee := range access.Grantees {
		switch {
		case grantee.UserID != "" && grantee.CircleID == "":
			grantees = append(grantees, "u:"+grantee.UserID)
		case grantee.CircleID != "" && grantee.UserID == "":
			grantees = append(grantees, "c:"+grantee.CircleID)
		default:
			return nil, errors.New("passwords: a grantee needs exactly one non-empty user id or circle id")
		}
		if hasControlCharacter(grantees[len(grantees)-1]) {
			return nil, errors.New("passwords: a grantee's id has a control character")
		}
	}
	slices.Sort(grantees)
	agentUse := "0"
	if access.IsUsableByAgent {
		agentUse = "1"
	}
	signed := []byte(accessRecordLabel)
	for _, part := range []string{itemId, string(itemKind), strconv.Itoa(access.RecordVersion), agentUse} {
		signed = append(signed, 0)
		signed = append(signed, part...)
	}
	signed = append(signed, 0)
	for _, grantee := range grantees {
		signed = append(signed, grantee...)
		signed = append(signed, 0)
	}
	return signed, nil
}

// MACOfAccess is the signature of an access record for an item: HMAC-SHA256
// over the bytes described at accessMacBytes, under the key made from the
// account key, as standard base64 with padding. It uses the record's
// RecordVersion, which must be set, and ignores its RecordMac.
func MACOfAccess(accountKey []byte, itemId string, itemKind ItemKind, access *PasswordAccess) (string, error) {
	key, err := accessMacKey(accountKey)
	if err != nil {
		return "", err
	}
	signed, err := accessMacBytes(itemId, itemKind, access)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(signed)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// SignAccess returns a copy of the record with its version set to one more than
// the highest version this device has seen with a valid signature for the item
// (0 if none), and the signature for that version. The given record is not
// changed and its own version and signature are ignored: a version the server
// supplied is never a reason to count higher.
func SignAccess(accountKey []byte, itemId string, itemKind ItemKind, access *PasswordAccess, highestVersionSeen int) (*PasswordAccess, error) {
	if highestVersionSeen < 0 || highestVersionSeen >= MaximumRecordVersion {
		return nil, errors.New("passwords: the highest record version seen is out of range")
	}
	signed := AccessWithAgentUse(access, access != nil && access.IsUsableByAgent)
	signed.RecordVersion = highestVersionSeen + 1
	text, err := MACOfAccess(accountKey, itemId, itemKind, signed)
	if err != nil {
		return nil, err
	}
	signed.RecordMac, err = DecodeCanonicalBase64(text)
	if err != nil {
		return nil, err
	}
	return signed, nil
}

// WithSignedAccess returns the item's fields JSON with the given access record
// signed (see SignAccess) in place of the one it had. Every other key is kept as
// it was, including keys this code does not know, though the order of keys may
// change.
func WithSignedAccess(accountKey []byte, itemId string, itemKind ItemKind, fieldsJSON []byte, access *PasswordAccess, highestVersionSeen int) ([]byte, error) {
	signed, err := SignAccess(accountKey, itemId, itemKind, access, highestVersionSeen)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(fieldsJSON, &fields); err != nil || fields == nil {
		return nil, errors.New("passwords: the fields are not a JSON object")
	}
	encoded, err := json.Marshal(signed)
	if err != nil {
		return nil, fmt.Errorf("passwords: write the access record: %w", err)
	}
	fields["access"] = encoded
	return json.Marshal(fields)
}

// CheckAccess reports whether the record is signed by the owner's devices for
// this item: it has a version and a signature, and the signature is the one
// MACOfAccess gives. A missing record, version or signature, and a wrong
// signature, are all ErrAccessNotVerified. The comparison takes the same time
// whatever the bytes.
func CheckAccess(accountKey []byte, itemId string, itemKind ItemKind, access *PasswordAccess) error {
	if access == nil || access.RecordVersion == 0 || len(access.RecordMac) != KeyLength {
		return ErrAccessNotVerified
	}
	text, err := MACOfAccess(accountKey, itemId, itemKind, access)
	if err != nil {
		return ErrAccessNotVerified
	}
	expected, err := DecodeCanonicalBase64(text)
	if err != nil || !hmac.Equal(expected, access.RecordMac) {
		return ErrAccessNotVerified
	}
	return nil
}

// VerifyAccess reads the access record out of an item's fields JSON and
// returns it when it is signed for this item (see CheckAccess). It returns
// ErrAccessNotVerified when there is no record or it is not signed, and another
// error when the fields carry a record of the wrong shape; a device treats
// either as a record that reaches nobody. The server cannot call this: it has
// no account key.
func VerifyAccess(accountKey []byte, itemId string, itemKind ItemKind, fieldsJSON []byte) (*PasswordAccess, error) {
	access, err := ParseAccessOfFields(fieldsJSON)
	if err != nil {
		return nil, err
	}
	if err := CheckAccess(accountKey, itemId, itemKind, access); err != nil {
		return nil, err
	}
	return access, nil
}
