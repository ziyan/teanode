package client

import (
	"context"
	"time"
)

// A page's links to the person's own records, from the command line.

// AgentRecordLink is a link from a page to one of the person's own
// records, with the record found again by the server: its title and date,
// and the dashboard path that opens it.
type AgentRecordLink struct {
	ID       string `json:"id"`
	NodePath string `json:"nodePath"`
	NodeName string `json:"nodeName"`
	NodeKind string `json:"nodeKind"`

	RecordKind     string `json:"recordKind"`
	RecordID       string `json:"recordId"`
	RecordRelation string `json:"recordRelation"`

	// LinkStatus is stated, or proposed where the agent only guessed.
	LinkStatus string `json:"linkStatus"`

	PassageStartMS  int64  `json:"passageStartMS"`
	PassageEndMS    int64  `json:"passageEndMS"`
	TranscriptRunID string `json:"transcriptRunId"`

	// PassageCount is how many passage links of a recording the row stands
	// for, the range above being the first; zero for any other kind.
	PassageCount int `json:"passageCount"`

	Note       string     `json:"note"`
	HappenedAt *time.Time `json:"happenedAt"`
	CreatedAt  time.Time  `json:"createdAt"`

	RecordTitle      string     `json:"recordTitle"`
	RecordHappenedAt *time.Time `json:"recordHappenedAt"`
	DashboardPath    string     `json:"dashboardPath"`
	IsUnavailable    bool       `json:"isUnavailable"`
}

// AgentRecordLinks is a part of a page's record links.
type AgentRecordLinks struct {
	RecordLinks     []*AgentRecordLink `json:"recordLinks"`
	RecordLinkCount int                `json:"recordLinkCount"`
	NextOffset      int                `json:"nextOffset"`
}

const recordLinkFields = `{ id nodePath nodeName nodeKind recordKind recordId recordRelation linkStatus passageStartMS passageEndMS transcriptRunId passageCount note happenedAt createdAt recordTitle recordHappenedAt dashboardPath isUnavailable }`

// The documents.
const (
	DocumentListAgentRecordLinks = `query ($path: String!, $first: Int, $offset: Int) {
		ListAgentRecordLinks(path: $path, first: $first, offset: $offset) {
			recordLinks ` + recordLinkFields + `
			recordLinkCount nextOffset
		}
	}`
	DocumentReadRecordMemoryLinks = `query ($recordKind: String!, $recordId: String!, $passageStartMS: Int64, $passageEndMS: Int64) {
		ReadRecordMemoryLinks(recordKind: $recordKind, recordId: $recordId, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS) ` + recordLinkFields + `
	}`
	DocumentLinkAgentRecord = `mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String!, $passageStartMS: Int64, $passageEndMS: Int64, $transcriptRunId: String, $note: String) {
		LinkAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS, transcriptRunId: $transcriptRunId, note: $note) ` + recordLinkFields + `
	}`
	DocumentUnlinkAgentRecord = `mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String, $passageStartMS: Int64, $passageEndMS: Int64) {
		UnlinkAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS)
	}`
	DocumentConfirmAgentRecord = `mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String, $passageStartMS: Int64, $passageEndMS: Int64) {
		ConfirmAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS)
	}`
	DocumentSetAgentNodeContact = `mutation ($path: String!, $contactId: String) { SetAgentNodeContact(path: $path, contactId: $contactId) }`
)

// ListAgentRecordLinks is first of a page's record links past offset; zero
// first is as many as the server gives at once.
func ListAgentRecordLinks(ctx context.Context, connection *Client, path string, first, offset int) (*AgentRecordLinks, error) {
	var result struct {
		ListAgentRecordLinks *AgentRecordLinks `json:"ListAgentRecordLinks"`
	}
	variables := map[string]any{"path": path}
	if first > 0 {
		variables["first"] = first
	}
	if offset > 0 {
		variables["offset"] = offset
	}
	if err := connection.Execute(ctx, DocumentListAgentRecordLinks, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentRecordLinks, nil
}

// ReadRecordMemoryLinks is the pages that link to one record; for a
// passage of a recording, the range narrows it to the passages that overlap.
func ReadRecordMemoryLinks(ctx context.Context, connection *Client, recordKind, recordId string, startMS, endMS int64) ([]*AgentRecordLink, error) {
	var result struct {
		ReadRecordMemoryLinks []*AgentRecordLink `json:"ReadRecordMemoryLinks"`
	}
	variables := map[string]any{"recordKind": recordKind, "recordId": recordId}
	if startMS != 0 || endMS != 0 {
		variables["passageStartMS"], variables["passageEndMS"] = startMS, endMS
	}
	if err := connection.Execute(ctx, DocumentReadRecordMemoryLinks, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadRecordMemoryLinks, nil
}

// LinkAgentRecord joins a page to a record of the person's own, stated.
func LinkAgentRecord(ctx context.Context, connection *Client, path, recordKind, recordId, recordRelation string, startMS, endMS int64, transcriptRunId, note string) (*AgentRecordLink, error) {
	var result struct {
		LinkAgentRecord *AgentRecordLink `json:"LinkAgentRecord"`
	}
	variables := map[string]any{"path": path, "recordKind": recordKind, "recordId": recordId, "recordRelation": recordRelation}
	if startMS != 0 || endMS != 0 {
		variables["passageStartMS"], variables["passageEndMS"] = startMS, endMS
	}
	if transcriptRunId != "" {
		variables["transcriptRunId"] = transcriptRunId
	}
	if note != "" {
		variables["note"] = note
	}
	if err := connection.Execute(ctx, DocumentLinkAgentRecord, variables, &result); err != nil {
		return nil, err
	}
	return result.LinkAgentRecord, nil
}

// UnlinkAgentRecord takes a page's links to a record away, and says
// whether there were any. No relation is every relation.
func UnlinkAgentRecord(ctx context.Context, connection *Client, path, recordKind, recordId, recordRelation string, startMS, endMS int64) (bool, error) {
	var result struct {
		UnlinkAgentRecord bool `json:"UnlinkAgentRecord"`
	}
	variables := map[string]any{"path": path, "recordKind": recordKind, "recordId": recordId}
	if recordRelation != "" {
		variables["recordRelation"] = recordRelation
	}
	if startMS != 0 || endMS != 0 {
		variables["passageStartMS"], variables["passageEndMS"] = startMS, endMS
	}
	if err := connection.Execute(ctx, DocumentUnlinkAgentRecord, variables, &result); err != nil {
		return false, err
	}
	return result.UnlinkAgentRecord, nil
}

// SetAgentNodeContact names the card a person's page is about; empty
// clears it.
func SetAgentNodeContact(ctx context.Context, connection *Client, path, contactId string) error {
	var result struct {
		SetAgentNodeContact bool `json:"SetAgentNodeContact"`
	}
	return connection.Execute(ctx, DocumentSetAgentNodeContact, map[string]any{"path": path, "contactId": contactId}, &result)
}

// ConfirmAgentRecord states the agent's guesses at a page's links to a
// record, every passage of a recording at once unless a range names one,
// and says how many. No relation is every relation.
func ConfirmAgentRecord(ctx context.Context, connection *Client, path, recordKind, recordId, recordRelation string, startMS, endMS int64) (int, error) {
	var result struct {
		ConfirmAgentRecord int `json:"ConfirmAgentRecord"`
	}
	variables := map[string]any{"path": path, "recordKind": recordKind, "recordId": recordId}
	if recordRelation != "" {
		variables["recordRelation"] = recordRelation
	}
	if startMS != 0 || endMS != 0 {
		variables["passageStartMS"], variables["passageEndMS"] = startMS, endMS
	}
	if err := connection.Execute(ctx, DocumentConfirmAgentRecord, variables, &result); err != nil {
		return 0, err
	}
	return result.ConfirmAgentRecord, nil
}
