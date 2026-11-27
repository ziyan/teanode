package agent

import (
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A card whose problem cleared is marked resolved instead of being followed by
// a card that says all is clear. The mark does not move the card, make it
// unread or notify anyone. See docs/subsystems/feed.md.

// FeedResolver is who marks a card resolved or takes the mark off.
type FeedResolver struct {
	Kind models.FeedResolverKind

	// ID is the goal's conversation for a goal, and the person's user for a
	// person; empty for the agent outside a goal's own turn.
	ID string
}

// feedResolutionNoteCharacters is how long the note of a resolution may be.
const feedResolutionNoteCharacters = models.FeedResolutionNoteMost

// ResolveFeedItem marks the card resolved, with the one line saying what
// cleared. The first resolution stands: a card already resolved is refused, so
// a later note never replaces the first (unresolve it first). The answer
// carries what was cut from the note in its post notes.
func ResolveFeedItem(tx db.Transaction, resolver *FeedResolver, item *models.AgentFeedItem, resolutionNote string, now time.Time) (*models.AgentFeedItem, error) {
	if item.IsResolved() {
		if item.ResolvedByKind == models.FeedResolverPerson {
			return nil, feedRefusal("this card is already resolved, by the person, and stays as they left it")
		}
		return nil, feedRefusal("this card is already resolved")
	}
	notes := &models.AgentFeedItem{}
	note := feedLine(notes, "resolutionNote", resolutionNote, feedResolutionNoteCharacters)
	isResolved, err := tx.ResolveAgentFeedItem(item.ID, &models.FeedResolution{ResolutionNote: note, ResolvedByKind: resolver.Kind, ResolvedByID: resolver.ID}, now)
	if err != nil {
		return nil, err
	}
	if !isResolved {
		return nil, feedRefusal("this card is already resolved")
	}
	resolved, err := tx.GetAgentFeedItem(item.ID)
	if err != nil || resolved == nil {
		return nil, err
	}
	resolved.PostNotes = notes.PostNotes
	return resolved, nil
}

// UnresolveFeedItem takes the mark off a card, whoever resolved it.
func UnresolveFeedItem(tx db.Transaction, resolver *FeedResolver, item *models.AgentFeedItem, now time.Time) (*models.AgentFeedItem, error) {
	if !item.IsResolved() {
		return nil, feedRefusal("this card is not resolved")
	}
	if _, err := tx.UnresolveAgentFeedItem(item.ID, now); err != nil {
		return nil, err
	}
	return tx.GetAgentFeedItem(item.ID)
}

// CheckFeedWithdrawal refuses deleting a card the person answered or
// resolved: it holds their answer, and the person's own way to put a card away
// is dismissing it, which the agent can do too.
func CheckFeedWithdrawal(item *models.AgentFeedItem) error {
	if item.IsChosen() || (item.IsResolved() && item.ResolvedByKind == models.FeedResolverPerson) {
		return feedRefusal("the person answered or resolved this card, so it is theirs now; dismiss it instead of withdrawing it")
	}
	return nil
}

// FeedPictureFileIDs are the files of the pictures a card holds in the file
// store, for deleting them when the card is deleted.
func FeedPictureFileIDs(item *models.AgentFeedItem) []string {
	var fileIds []string
	for _, entry := range item.FeedMedia {
		if entry.ImageFileID != "" {
			fileIds = append(fileIds, entry.ImageFileID)
		}
	}
	return fileIds
}
