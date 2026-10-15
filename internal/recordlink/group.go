package recordlink

import (
	"github.com/ziyan/teanode/internal/models"
)

// Row is what a page lists for one record: the link itself, or for a
// recording the first of its passage links, and how many passage links the
// page has to that recording in that relation.
type Row struct {
	// Link is the link the row shows. For a recording it is the passage
	// that starts first, and its LinkStatus is proposed while any of the
	// passages is only a guess, so that the guess can still be confirmed.
	Link *models.AgentRecordLink

	// PassageCount is how many passage links the row stands for: zero for
	// a record that is not a passage of a recording.
	PassageCount int
}

// GroupPassages is a page's links as the rows it lists. A person's page
// holds one link per passage of a recording they spoke in, and a list that
// repeats the recording once per passage is unreadable, so the passages of
// one recording in one relation are one row, at the place of the first of
// them in links. Links of any other kind pass through, in order.
func GroupPassages(links []*models.AgentRecordLink) []Row {
	rows := make([]Row, 0, len(links))
	rowOf := map[string]int{}
	for _, link := range links {
		if link.RecordKind != models.RecordRecordingPassage {
			rows = append(rows, Row{Link: link})
			continue
		}
		key := models.RecordLinkKey(link.NodeID, link.RecordKind, link.RecordID, link.RecordRelation, 0, 0)
		index, isGrouped := rowOf[key]
		if !isGrouped {
			rowOf[key] = len(rows)
			copied := *link
			rows = append(rows, Row{Link: &copied, PassageCount: 1})
			continue
		}
		row := &rows[index]
		row.PassageCount++
		if link.PassageStartMS < row.Link.PassageStartMS ||
			(link.PassageStartMS == row.Link.PassageStartMS && link.PassageEndMS < row.Link.PassageEndMS) {
			status := row.Link.LinkStatus
			*row.Link = *link
			row.Link.LinkStatus = status
		}
		if link.Proposed() {
			row.Link.LinkStatus = models.EdgeProposed
		}
	}
	return rows
}

// RowLinks is the links that rows show.
func RowLinks(rows []Row) []*models.AgentRecordLink {
	links := make([]*models.AgentRecordLink, len(rows))
	for index := range rows {
		links[index] = rows[index].Link
	}
	return links
}
