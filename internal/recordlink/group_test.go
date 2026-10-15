package recordlink_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

func passageLink(recordId string, relation models.AgentRecordRelation, status models.AgentEdgeStatus, startMS, endMS int64) *models.AgentRecordLink {
	return &models.AgentRecordLink{
		ID: recordId + "-" + string(rune('a'+startMS/1000)), NodeID: "node", RecordKind: models.RecordRecordingPassage,
		RecordID: recordId, RecordRelation: relation, LinkStatus: status, PassageStartMS: startMS, PassageEndMS: endMS,
	}
}

func TestGroupPassagesMakesOneRowPerRecordingAndRelation(t *testing.T) {
	links := []*models.AgentRecordLink{
		{ID: "mail-link", NodeID: "node", RecordKind: models.RecordMail, RecordID: "mail-1", RecordRelation: models.RecordRelationAbout, LinkStatus: models.EdgeStated},
		passageLink("recording-1", models.RecordRelationSpokeIn, models.EdgeStated, 9000, 9500),
		passageLink("recording-2", models.RecordRelationSpokeIn, models.EdgeStated, 1000, 2000),
		passageLink("recording-1", models.RecordRelationSpokeIn, models.EdgeProposed, 3000, 4000),
		passageLink("recording-1", models.RecordRelationSpokeIn, models.EdgeStated, 5000, 6000),
	}
	rows := recordlink.GroupPassages(links)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (the mail, and one per recording)", len(rows))
	}
	if rows[0].Link.RecordID != "mail-1" || rows[0].PassageCount != 0 {
		t.Fatalf("the mail should pass through with no passage count: %+v", rows[0])
	}
	first := rows[1]
	if first.Link.RecordID != "recording-1" || first.PassageCount != 3 {
		t.Fatalf("recording-1 should be one row of 3 passages: %+v", first)
	}
	if first.Link.PassageStartMS != 3000 || first.Link.PassageEndMS != 4000 {
		t.Fatalf("the row opens at the first passage, got %d-%d", first.Link.PassageStartMS, first.Link.PassageEndMS)
	}
	if first.Link.LinkStatus != models.EdgeProposed {
		t.Fatalf("a row with one guess is still a guess to confirm, got %s", first.Link.LinkStatus)
	}
	if rows[2].Link.RecordID != "recording-2" || rows[2].PassageCount != 1 {
		t.Fatalf("recording-2: %+v", rows[2])
	}
	if links[1].PassageStartMS != 9000 {
		t.Fatalf("the input link was changed")
	}
}

func TestGroupPassagesKeepsRelationsApart(t *testing.T) {
	rows := recordlink.GroupPassages([]*models.AgentRecordLink{
		passageLink("recording-1", models.RecordRelationSpokeIn, models.EdgeStated, 1000, 2000),
		passageLink("recording-1", models.RecordRelationMentionedIn, models.EdgeStated, 3000, 4000),
	})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want one per relation", len(rows))
	}
}
