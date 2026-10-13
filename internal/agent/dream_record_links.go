package agent

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// Links between pages and the person's own records, made while dreaming.
//
// A page about a person, an organization or a project and a record of
// the person's own (a contact card, a mail message, an event, a
// transaction, a passage of a recording) are often the same thing seen
// from two sides, and nobody should have to say so by hand. This pass
// finds the ones that are, and asks no model: it only compares exact
// addresses, confirmed names and matches somebody already made.
//
// The rule it keeps is the one the graph keeps for links between pages. It
// states a link only where the match is certain: an address that is on
// exactly one card, a speaker name the person confirmed that is exactly the
// name of exactly one page, an address on a message that belongs to exactly
// one page, a receipt already matched to a transaction. Where a match is a
// guess (a card with the same full name, an event whose title names a
// project, a merchant with the name of an organization) it proposes, and the
// person confirms or removes it.
//
// Four things keep it harmless to run every dream:
//
//   - Idempotent. A link is written once; one already there is left alone,
//     and a statement is never turned into a proposal.
//   - Bounded. At most recordLinksPerDreamCount new links, and each source reads
//     only the newest of its records (a calendar, only the events starting
//     within recordLinksEventWindow of now), so a large mailbox or a long
//     history costs the same each time. A record older than that is not
//     reached by this pass; it is linked by hand or by the agent.
//   - Granted. The agent reads only what the person gave it, so a record is
//     linked only when the same resolver the agent reads links with says it
//     is readable: a granted address book, a granted calendar or mailbox, a
//     finance source that is switched on.
//   - Respectful of removal. A link the person took off a page is recorded
//     (agent_record_link_dismissal) and not made again.
const (
	// recordLinksPerDreamCount is the most links one dream makes.
	recordLinksPerDreamCount = 300

	// recordLinksMailItemCount is how many of the newest messages of a folder
	// are looked at, per folder, and recordLinksRecordingCount how many of the
	// newest recordings. recordLinksTransactionCount is the same for finance
	// transactions and for receipts.
	recordLinksMailItemCount    = 100
	recordLinksRecordingCount   = 50
	recordLinksTransactionCount = 200

	// recordLinksEventWindow is how far either side of now an event may
	// start to be looked at.
	recordLinksEventWindow = 180 * 24 * time.Hour

	// recordLinksNameLength is the least a name may be to be found inside
	// the title of an event: a short word is a word of anything else too.
	recordLinksNameLength = 4

	// recordLinksTurnGapMS is the longest silence between two passages of
	// one speaker that still reads as the same turn.
	recordLinksTurnGapMS = 2000

	// recordLinksPassageSegmentCount reads a recording this many segments at a
	// time, the most the database returns.
	recordLinksPassageSegmentCount = 250

	// recordLinksPagesPerKindCount is how many pages of each kind are matched.
	recordLinksPagesPerKindCount = 5000
)

// recordLinkPass is one run of the pass over one agent.
type recordLinkPass struct {
	agent *models.Agent
	owner *models.User

	// People, organizations and projects by the pages themselves, and the
	// one page of the person.
	people        []*models.AgentNode
	organizations []*models.AgentNode
	projects      []*models.AgentNode
	selfPage      *models.AgentNode

	// existing is every link already there (by models.RecordLinkKey) and
	// dismissed what the person took away.
	existing  map[string]models.AgentEdgeStatus
	dismissed map[string]bool

	// pageByAddress is the page an address belongs to, only where it
	// belongs to exactly one.
	pageByAddress map[string]*models.AgentNode

	remaining     int
	statedCount   int
	proposedCount int
	reader        recordlink.Reader
}

// recordLinkCount is what a pass did.
type recordLinkCount struct {
	StatedCount   int
	ProposedCount int
}

// dreamRecordLinks makes the links, one source at a time. A source that fails
// is logged and the rest go on: a calendar that cannot be read is no reason
// for the recordings not to be.
func (self *Agent) dreamRecordLinks(ctx context.Context, run *Run) recordLinkCount {
	pass, err := self.newRecordLinkPass(ctx, run)
	if err != nil {
		log.Warningf("cannot start linking records for agent %q: %s", run.Agent.ID, err)
		return recordLinkCount{}
	}
	if len(pass.people)+len(pass.organizations)+len(pass.projects) == 0 && pass.selfPage == nil {
		return recordLinkCount{}
	}
	for _, source := range []struct {
		name string
		link func(context.Context, *Run, *recordLinkPass) error
	}{
		{"contacts", linkContactsOfPeople},
		{"recordings", linkRecordingSpeakers},
		{"mail", linkMailParticipants},
		{"calendars", linkCalendarParticipants},
		{"finance", linkFinanceRecords},
	} {
		if ctx.Err() != nil || pass.remaining <= 0 {
			break
		}
		if err := source.link(ctx, run, pass); err != nil {
			log.Warningf("cannot link %s for agent %q: %s", source.name, run.Agent.ID, err)
		}
	}
	if pass.statedCount+pass.proposedCount > 0 {
		log.Infof("linked records for agent %q: %d stated, %d proposed", run.Agent.ID, pass.statedCount, pass.proposedCount)
	}
	return recordLinkCount{StatedCount: pass.statedCount, ProposedCount: pass.proposedCount}
}

func (self *Agent) newRecordLinkPass(ctx context.Context, run *Run) (*recordLinkPass, error) {
	pass := &recordLinkPass{
		agent: run.Agent, owner: run.Owner, remaining: recordLinksPerDreamCount,
		reader: recordlink.Reader{UserID: run.Owner.ID, AgentID: run.Agent.ID, IsAgent: true},
	}
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		nodes, err := tx.ListAgentNodesOfKinds(run.Agent.ID,
			[]models.AgentNodeKind{models.NodePerson, models.NodeOrganization, models.NodeProject, models.NodeSelf}, recordLinksPagesPerKindCount)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			switch node.Kind {
			case models.NodePerson:
				pass.people = append(pass.people, node)
			case models.NodeOrganization:
				pass.organizations = append(pass.organizations, node)
			case models.NodeProject:
				pass.projects = append(pass.projects, node)
			case models.NodeSelf:
				if pass.selfPage == nil {
					pass.selfPage = node
				}
			}
		}
		if pass.existing, err = tx.ListAgentRecordLinkStatuses(run.Agent.ID); err != nil {
			return err
		}
		pass.dismissed, err = tx.ListAgentRecordLinkDismissals(run.Agent.ID)
		return err
	})
	return pass, err
}

// normalizedName is a name as it is compared: one line, lower case.
func normalizedName(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// normalizedAddress is an email address as it is compared, or empty where
// the text is not one. A display name around it is dropped.
func normalizedAddress(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToLower(text), "mailto:") {
		text = text[len("mailto:"):]
	}
	if parsed, err := mail.ParseAddress(text); err == nil {
		text = parsed.Address
	}
	text = strings.ToLower(strings.TrimSpace(text))
	at := strings.LastIndex(text, "@")
	if at <= 0 || at == len(text)-1 || strings.ContainsAny(text, " \t<>,;") {
		return ""
	}
	return text
}

// pagesByName groups pages by every name they go by, so that a name two
// pages share is known to be ambiguous.
func pagesByName(pages []*models.AgentNode) map[string][]*models.AgentNode {
	byName := map[string][]*models.AgentNode{}
	for _, page := range pages {
		seen := map[string]bool{}
		for _, name := range append([]string{page.Name}, page.Aliases...) {
			key := normalizedName(name)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			byName[key] = append(byName[key], page)
		}
	}
	return byName
}

// only is the one page of a list, or nil where there are none or several.
func only(pages []*models.AgentNode) *models.AgentNode {
	if len(pages) != 1 {
		return nil
	}
	return pages[0]
}

// put writes one link if it is new, wanted and readable. It reports whether
// a link was written.
func (self *recordLinkPass) put(tx db.Transaction, link *models.AgentRecordLink) (bool, error) {
	if self.remaining <= 0 {
		return false, nil
	}
	key := models.RecordLinkKey(link.NodeID, link.RecordKind, link.RecordID, link.RecordRelation, link.PassageStartMS, link.PassageEndMS)
	if self.dismissed[key] {
		return false, nil
	}
	if status, isThere := self.existing[key]; isThere && (status == models.EdgeStated || link.LinkStatus == models.EdgeProposed) {
		return false, nil
	}
	resolved, err := recordlink.ResolveRecord(tx, self.reader, link.RecordKind, link.RecordID, link.RecordDetail, link.PassageStartMS, link.PassageEndMS)
	if err != nil {
		return false, err
	}
	if !resolved.IsReadable() {
		return false, nil
	}
	link.AgentID = self.agent.ID
	if link.HappenedAt == nil {
		link.HappenedAt = resolved.HappenedAt
	}
	if _, err := tx.PutAgentRecordLink(link); err != nil {
		return false, err
	}
	self.existing[key] = link.LinkStatus
	self.remaining--
	if link.LinkStatus == models.EdgeProposed {
		self.proposedCount++
	} else {
		self.statedCount++
	}
	return true, nil
}

// buildAddressIndex finds the page each address belongs to: the addresses a
// person page goes by are those of its name and aliases and those of the
// card it is linked to, and an address two pages go by belongs to neither.
func (self *recordLinkPass) buildAddressIndex(cardsByID map[string]*models.Contact) {
	owners := map[string][]*models.AgentNode{}
	for _, page := range self.people {
		seen := map[string]bool{}
		candidates := append([]string{page.Name}, page.Aliases...)
		if card := cardsByID[page.ContactID]; card != nil {
			candidates = append(candidates, card.Emails...)
		}
		for _, candidate := range candidates {
			address := normalizedAddress(candidate)
			if address == "" || seen[address] {
				continue
			}
			seen[address] = true
			owners[address] = append(owners[address], page)
		}
	}
	self.pageByAddress = map[string]*models.AgentNode{}
	for address, pages := range owners {
		if page := only(pages); page != nil {
			self.pageByAddress[address] = page
		}
	}
}

// grantedCards are the cards of the address books the agent was given, by
// id and by the addresses and full names on them.
func grantedCards(tx db.Transaction, userId string) ([]*models.Contact, error) {
	books, err := tx.ListAddressBooks(userId)
	if err != nil {
		return nil, err
	}
	var cards []*models.Contact
	for _, book := range books {
		if !book.AgentGranted {
			continue
		}
		found, err := tx.ListContacts(book.ID, "", 0)
		if err != nil {
			return nil, err
		}
		cards = append(cards, found...)
	}
	return cards, nil
}

// linkContactsOfPeople links a person page to its contact card.
//
// Stated: an address of the page (an alias that is one) that is on exactly one
// card, and on no other page. Proposed: the page's full name is exactly the
// name of exactly one card and of no other page. A page that already has a
// card, or a link to one, is left alone.
func linkContactsOfPeople(ctx context.Context, run *Run, pass *recordLinkPass) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		cards, err := grantedCards(tx, pass.owner.ID)
		if err != nil {
			return err
		}
		cardsByID := make(map[string]*models.Contact, len(cards))
		cardsByAddress := map[string][]*models.Contact{}
		cardsByName := map[string][]*models.Contact{}
		for _, card := range cards {
			cardsByID[card.ID] = card
			seen := map[string]bool{}
			for _, address := range card.Emails {
				if key := normalizedAddress(address); key != "" && !seen[key] {
					seen[key] = true
					cardsByAddress[key] = append(cardsByAddress[key], card)
				}
			}
			if key := normalizedName(card.Name); key != "" {
				cardsByName[key] = append(cardsByName[key], card)
			}
		}
		pass.buildAddressIndex(cardsByID)
		pagesNamed := pagesByName(pass.people)
		hasContactLink := pagesWithContactLink(pass)

		for _, page := range pass.people {
			if pass.remaining <= 0 {
				return nil
			}
			if page.ContactID != "" || hasContactLink[page.ID] {
				continue
			}
			// By address: every address the page has must lead to the same one
			// card, and the page must be the only one with the address.
			var card *models.Contact
			isAmbiguous := false
			for _, candidate := range page.Aliases {
				address := normalizedAddress(candidate)
				if address == "" {
					continue
				}
				found := cardsByAddress[address]
				if len(found) == 0 {
					continue
				}
				if len(found) > 1 || pass.pageByAddress[address] != page || (card != nil && card.ID != found[0].ID) {
					isAmbiguous = true
					break
				}
				card = found[0]
			}
			if card != nil && !isAmbiguous {
				written, err := pass.put(tx, &models.AgentRecordLink{
					NodeID: page.ID, RecordKind: models.RecordContact, RecordID: card.ID,
					RecordRelation: models.RecordRelationIsContact, LinkStatus: models.EdgeStated,
					Evidence: []models.Evidence{{Kind: models.EvidenceContact, ID: card.ID, Quote: "the same email address is on the page and on the card"}},
				})
				if err != nil {
					return err
				}
				if written {
					// The database names the card on the page when it stores
					// a stated link to it.
					page.ContactID = card.ID
				}
				continue
			}
			if isAmbiguous {
				continue
			}
			// By full name alone: a guess, so it is proposed.
			nameKey := normalizedName(page.Name)
			if nameKey == "" || !strings.Contains(nameKey, " ") {
				continue
			}
			byName := cardsByName[nameKey]
			if len(byName) != 1 || only(pagesNamed[nameKey]) != page {
				continue
			}
			if _, err := pass.put(tx, &models.AgentRecordLink{
				NodeID: page.ID, RecordKind: models.RecordContact, RecordID: byName[0].ID,
				RecordRelation: models.RecordRelationIsContact, LinkStatus: models.EdgeProposed,
				Evidence: []models.Evidence{{Kind: models.EvidenceContact, ID: byName[0].ID, Quote: "the page and the card have the same full name"}},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// pagesWithContactLink are the pages that already have a link to a card, or
// had one the person took away, which a guess must not pile a second onto or
// put back.
func pagesWithContactLink(pass *recordLinkPass) map[string]bool {
	pages := map[string]bool{}
	mark := func(key string) {
		parts := strings.SplitN(key, "|", 3)
		if len(parts) == 3 && parts[1] == string(models.RecordContact) {
			pages[parts[0]] = true
		}
	}
	for key := range pass.existing {
		mark(key)
	}
	for key := range pass.dismissed {
		mark(key)
	}
	return pages
}

// A person page needs a card's addresses to match mail and events, and the
// contact pass has built them. A source that runs without it builds them
// itself.
func (self *recordLinkPass) ensureAddressIndex(tx db.Transaction) error {
	if self.pageByAddress != nil {
		return nil
	}
	cards, err := grantedCards(tx, self.owner.ID)
	if err != nil {
		return err
	}
	cardsByID := make(map[string]*models.Contact, len(cards))
	for _, card := range cards {
		cardsByID[card.ID] = card
	}
	self.buildAddressIndex(cardsByID)
	return nil
}

// linkRecordingSpeakers links the page of a person to the passages of the
// recordings where the person confirmed it was them speaking.
//
// The name is the identity's confirmed name, which the person typed, and a page
// is chosen only where that name is exactly one page's name or alias. The
// identity that is the person themselves links to the self page. A passage is
// a turn: the segments one speaker spoke without a long silence between.
func linkRecordingSpeakers(ctx context.Context, run *Run, pass *recordLinkPass) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		identities, err := tx.ListRecordingSpeakerIdentities(pass.owner.ID)
		if err != nil {
			return err
		}
		pagesNamed := pagesByName(pass.people)
		pageByIdentity := map[string]*models.AgentNode{}
		for _, identity := range identities {
			if identity.IsAccountOwner {
				if pass.selfPage != nil {
					pageByIdentity[identity.ID] = pass.selfPage
				}
				continue
			}
			if page := only(pagesNamed[normalizedName(identity.ConfirmedName)]); page != nil {
				pageByIdentity[identity.ID] = page
			}
		}
		if len(pageByIdentity) == 0 {
			return nil
		}
		recordings, err := tx.ListRecordings(pass.owner.ID, "", "", recordLinksRecordingCount)
		if err != nil {
			return err
		}
		for _, recording := range recordings {
			if pass.remaining <= 0 {
				return nil
			}
			if err := linkRecordingTurns(tx, pass, recording, pageByIdentity); err != nil {
				return err
			}
		}
		return nil
	})
}

// turnOfSegments is a stretch of one confirmed speaker.
type turnOfSegments struct {
	identityID      string
	transcriptRunID string
	startMS, endMS  int64
}

func linkRecordingTurns(tx db.Transaction, pass *recordLinkPass, recording *models.Recording, pageByIdentity map[string]*models.AgentNode) error {
	var turns []*turnOfSegments
	var current *turnOfSegments
	for offset := 0; ; {
		segments, _, segmentCount, err := tx.ReadRecordingTranscriptTimelinePage(pass.owner.ID, recording.ID, offset, recordLinksPassageSegmentCount)
		if err != nil {
			return err
		}
		if err := tx.ProjectRecordingSpeakerCorrections(pass.owner.ID, segments); err != nil {
			return err
		}
		for _, segment := range segments {
			identityID := ""
			if segment.HasUserConfirmedSpeaker {
				identityID = segment.EffectiveSpeakerIdentityID
			}
			if pageByIdentity[identityID] == nil {
				current = nil
				continue
			}
			if current != nil && current.identityID == identityID && current.transcriptRunID == segment.RecordingTranscriptRunID &&
				segment.SegmentStartMS-current.endMS <= recordLinksTurnGapMS {
				if segment.SegmentEndMS > current.endMS {
					current.endMS = segment.SegmentEndMS
				}
				continue
			}
			current = &turnOfSegments{identityID: identityID, transcriptRunID: segment.RecordingTranscriptRunID,
				startMS: segment.SegmentStartMS, endMS: segment.SegmentEndMS}
			turns = append(turns, current)
		}
		offset += len(segments)
		if len(segments) == 0 || int64(offset) >= segmentCount {
			break
		}
	}
	for _, turn := range turns {
		page := pageByIdentity[turn.identityID]
		// A turn whose bounds shifted since it was linked (a later naming,
		// a new transcription) replaces the link it overlaps, so the page
		// does not collect one link per version of the same passage.
		overlapping, err := tx.ListAgentRecordBacklinks(pass.agent.ID, models.RecordRecordingPassage, recording.ID,
			turn.startMS, turn.endMS, recordLinksPerDreamCount)
		if err != nil {
			return err
		}
		written, err := pass.put(tx, &models.AgentRecordLink{
			NodeID: page.ID, RecordKind: models.RecordRecordingPassage, RecordID: recording.ID,
			RecordDetail:   models.AgentRecordDetail{TranscriptRunID: turn.transcriptRunID},
			PassageStartMS: turn.startMS, PassageEndMS: turn.endMS,
			RecordRelation: models.RecordRelationSpokeIn, LinkStatus: models.EdgeStated,
			Evidence: []models.Evidence{{
				Kind: models.EvidenceRecordingPassage, ID: recording.ID, TranscriptRunID: turn.transcriptRunID,
				StartMS: turn.startMS, EndMS: turn.endMS, Quote: "the person confirmed this speaker is " + page.Name,
			}},
		})
		if err != nil {
			return err
		}
		if written {
			for _, older := range overlapping {
				if older.NodeID != page.ID || older.RecordRelation != models.RecordRelationSpokeIn || older.Proposed() ||
					(older.PassageStartMS == turn.startMS && older.PassageEndMS == turn.endMS) {
					continue
				}
				// Withdrawn, not removed by the person: the dream may link
				// the passage again.
				if _, err := tx.WithdrawAgentRecordLink(pass.agent.ID, page.ID, models.RecordRecordingPassage, recording.ID,
					models.RecordRelationSpokeIn, older.PassageStartMS, older.PassageEndMS); err != nil {
					return err
				}
				delete(pass.existing, models.RecordLinkKey(page.ID, older.RecordKind, older.RecordID, older.RecordRelation,
					older.PassageStartMS, older.PassageEndMS))
			}
		}
		if pass.remaining <= 0 {
			return nil
		}
	}
	return nil
}

// linkMailParticipants links the page an address belongs to with the messages
// it appears on: the sender of a message in the Inbox or the Archive wrote
// it, and a recipient of a message in Sent received it. Only mailboxes the
// agent was given are read, and only the newest messages of each folder.
func linkMailParticipants(ctx context.Context, run *Run, pass *recordLinkPass) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		if err := pass.ensureAddressIndex(tx); err != nil {
			return err
		}
		if len(pass.pageByAddress) == 0 {
			return nil
		}
		mailboxes, err := tx.ListMailboxes(pass.owner.ID)
		if err != nil {
			return err
		}
		for _, mailbox := range mailboxes {
			if mailbox.Agent == nil || !mailbox.Agent.Granted {
				continue
			}
			for _, kind := range []models.MailboxFolderKind{models.MailboxFolderKindInbox, models.MailboxFolderKindArchive, models.MailboxFolderKindSent} {
				if pass.remaining <= 0 {
					return nil
				}
				folder, err := tx.GetFolderByKind(mailbox.ID, kind)
				if err != nil {
					return err
				}
				if folder == nil {
					continue
				}
				items, err := tx.ListItems(folder.ID, &db.ItemOptions{Limit: recordLinksMailItemCount, ByReceived: true})
				if err != nil {
					return err
				}
				if err := linkMailItems(tx, pass, mailbox.ID, kind, items); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func linkMailItems(tx db.Transaction, pass *recordLinkPass, mailboxId string, kind models.MailboxFolderKind, items []*models.MailboxItem) error {
	mailIds := make([]string, 0, len(items))
	for _, item := range items {
		mailIds = append(mailIds, item.MailID)
	}
	if len(mailIds) == 0 {
		return nil
	}
	mails, err := tx.GetMails(mailIds, nil)
	if err != nil {
		return err
	}
	mailById := make(map[string]*models.Mail, len(mails))
	for _, found := range mails {
		mailById[found.ID] = found
	}
	for _, item := range items {
		found := mailById[item.MailID]
		if found == nil {
			continue
		}
		var addresses []string
		relation := models.RecordRelationWrote
		linkStatus := models.EdgeStated
		if kind == models.MailboxFolderKindSent {
			relation = models.RecordRelationReceived
			addresses = found.Recipients
		} else {
			// The From header of a message from outside is whatever its
			// sender typed. Who wrote it is stated only for a message that
			// proved its sender or was sent from an account here, proposed
			// for one that proved nothing, and left alone for one the
			// server itself distrusts.
			if found.LooksLikeSpam() {
				continue
			}
			if !found.DMARCPassed() && !found.SubmittedHere() {
				linkStatus = models.EdgeProposed
			}
			addresses = []string{found.From}
		}
		seen := map[string]bool{}
		for _, text := range addresses {
			address := normalizedAddress(text)
			page := pass.pageByAddress[address]
			if address == "" || page == nil || seen[page.ID] {
				continue
			}
			seen[page.ID] = true
			happenedAt := found.ReceivedAt
			quote := address + " is the sender"
			if linkStatus == models.EdgeProposed {
				quote = address + " is the sender, but the message did not prove it"
			}
			if relation == models.RecordRelationReceived {
				quote = address + " is a recipient"
			}
			if _, err := pass.put(tx, &models.AgentRecordLink{
				NodeID: page.ID, RecordKind: models.RecordMail, RecordID: item.ID,
				RecordDetail:   models.AgentRecordDetail{MailboxID: mailboxId},
				RecordRelation: relation, LinkStatus: linkStatus, HappenedAt: &happenedAt,
				Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: item.ID, Quote: quote}},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// titleNamesPage says the title holds the name whole, between boundaries, and
// that the name is long enough to mean something.
func titleNamesPage(title string, page *models.AgentNode) bool {
	normalizedTitle := " " + normalizedName(strings.Map(func(character rune) rune {
		switch character {
		case '.', ',', ':', ';', '-', '/', '(', ')', '"', '\'', '[', ']', '!', '?':
			return ' '
		}
		return character
	}, title)) + " "
	for _, name := range append([]string{page.Name}, page.Aliases...) {
		key := normalizedName(name)
		if len([]rune(key)) < recordLinksNameLength || normalizedAddress(key) != "" {
			continue
		}
		if strings.Contains(normalizedTitle, " "+key+" ") {
			return true
		}
	}
	return false
}

// linkCalendarParticipants links the page an address belongs to with the
// events it is asked to or arranges, in the calendars the agent was given,
// and proposes the project whose name an event or a reminder is titled with.
func linkCalendarParticipants(ctx context.Context, run *Run, pass *recordLinkPass) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		if err := pass.ensureAddressIndex(tx); err != nil {
			return err
		}
		calendars, err := tx.ListCalendars(pass.owner.ID)
		if err != nil {
			return err
		}
		ownAddresses, err := addressesOfPerson(tx, pass.owner)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, owned := range calendars {
			if !owned.AgentGranted {
				continue
			}
			// Only the window is read, not every event the calendar holds.
			objects, err := tx.ListCalendarObjectsStartingBetween(owned.ID,
				now.Add(-recordLinksEventWindow), now.Add(recordLinksEventWindow), db.ObjectsPerCalendar)
			if err != nil {
				return err
			}
			for _, object := range objects {
				if pass.remaining <= 0 {
					return nil
				}
				if object.StartsAt.IsZero() || strings.EqualFold(object.Status, "CANCELLED") {
					continue
				}
				if err := linkCalendarObject(tx, pass, owned, object, ownAddresses); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// addressesOfPerson are the addresses that are the person: their account's
// and those of their mailboxes.
func addressesOfPerson(tx db.Transaction, owner *models.User) (map[string]bool, error) {
	addresses := map[string]bool{}
	if address := normalizedAddress(owner.Email); address != "" {
		addresses[address] = true
	}
	mailboxes, err := tx.ListMailboxes(owner.ID)
	if err != nil {
		return nil, err
	}
	for _, mailbox := range mailboxes {
		for _, mailboxAddress := range mailbox.Addresses {
			if address := normalizedAddress(mailboxAddress.Address); address != "" {
				addresses[address] = true
			}
		}
	}
	return addresses, nil
}

func linkCalendarObject(tx db.Transaction, pass *recordLinkPass, owned *models.Calendar, object *models.CalendarObject, ownAddresses map[string]bool) error {
	recordKind := models.RecordCalendarEvent
	if owned.CalendarKind == models.CalendarReminders {
		recordKind = models.RecordReminder
	}
	startsAt := object.StartsAt
	if recordKind == models.RecordCalendarEvent && len(pass.pageByAddress) > 0 {
		parsed, err := calendar.Parse([]byte(object.Data))
		if err == nil {
			organizer := normalizedAddress(parsed.Organizer)
			// The people on an event the person arranged are the people
			// they put there. An invitation is a file anyone can mail, so
			// the people on one are only proposed.
			linkStatus := models.EdgeProposed
			if organizer == "" || ownAddresses[organizer] {
				linkStatus = models.EdgeStated
			}
			seen := map[string]bool{}
			if page := pass.pageByAddress[organizer]; page != nil && organizer != "" {
				seen[page.ID] = true
				if _, err := pass.put(tx, &models.AgentRecordLink{
					NodeID: page.ID, RecordKind: recordKind, RecordID: object.ID,
					RecordRelation: models.RecordRelationOrganized, LinkStatus: linkStatus, HappenedAt: &startsAt,
					Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: object.ID, Quote: organizer + " is the organizer"}},
				}); err != nil {
					return err
				}
			}
			for _, attendee := range parsed.Attendees {
				address := normalizedAddress(attendee.Address)
				page := pass.pageByAddress[address]
				if address == "" || page == nil || seen[page.ID] {
					continue
				}
				seen[page.ID] = true
				if _, err := pass.put(tx, &models.AgentRecordLink{
					NodeID: page.ID, RecordKind: recordKind, RecordID: object.ID,
					RecordRelation: models.RecordRelationAttended, LinkStatus: linkStatus, HappenedAt: &startsAt,
					Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: object.ID, Quote: address + " is on the guest list"}},
				}); err != nil {
					return err
				}
			}
		}
	}
	// A title naming a project is a guess, however exact the name.
	evidenceKind := models.EvidenceCalendarEvent
	if recordKind == models.RecordReminder {
		evidenceKind = models.EvidenceReminder
	}
	for _, project := range pass.projects {
		if !titleNamesPage(object.Summary, project) {
			continue
		}
		if _, err := pass.put(tx, &models.AgentRecordLink{
			NodeID: project.ID, RecordKind: recordKind, RecordID: object.ID,
			RecordRelation: models.RecordRelationAbout, LinkStatus: models.EdgeProposed, HappenedAt: &startsAt,
			Evidence: []models.Evidence{{Kind: evidenceKind, ID: object.ID, Quote: "the title names the project"}},
		}); err != nil {
			return err
		}
	}
	return nil
}

// linkFinanceRecords proposes the organization whose name a transaction's
// merchant has, and states, for every receipt already matched to a
// transaction, the same page's link to the receipt. It runs only when a
// finance source of the agent is switched on; a transaction of a source that
// is switched off fails the resolver's check and is not linked.
func linkFinanceRecords(ctx context.Context, run *Run, pass *recordLinkPass) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		sources, err := tx.ListAgentSources(pass.agent.ID)
		if err != nil {
			return err
		}
		isFinanceOn := false
		for _, source := range sources {
			if source.Kind == models.SourceFinance && source.Enabled {
				isFinanceOn = true
			}
		}
		if !isFinanceOn {
			return nil
		}
		organizationsNamed := pagesByName(pass.organizations)
		page, err := tx.ListFinanceTransactions(pass.agent.ID, &db.FinanceTransactionFilter{
			Limit: recordLinksTransactionCount, IsDuplicateExcluded: true,
		})
		if err != nil {
			return err
		}
		for _, transactionRecord := range page.FinanceTransactions {
			if pass.remaining <= 0 {
				return nil
			}
			organization := only(organizationsNamed[normalizedName(transactionRecord.MerchantName)])
			if organization == nil || normalizedName(transactionRecord.MerchantName) == "" {
				continue
			}
			var happenedAt *time.Time
			if transactionRecord.TransactedAt != nil {
				happenedAt = transactionRecord.TransactedAt
			}
			if _, err := pass.put(tx, &models.AgentRecordLink{
				NodeID: organization.ID, RecordKind: models.RecordFinanceTransaction, RecordID: transactionRecord.ID,
				RecordRelation: models.RecordRelationPaid, LinkStatus: models.EdgeProposed, HappenedAt: happenedAt,
				Evidence: []models.Evidence{{Kind: models.EvidenceFinanceTransaction, ID: transactionRecord.ID, Quote: "the merchant has the name of the page"}},
			}); err != nil {
				return err
			}
		}
		return linkMatchedReceipts(tx, pass)
	})
}

// linkMatchedReceipts gives a receipt the link its transaction has: the pages
// that paid or were paid by a transaction are the pages the receipt that was
// matched to it is for, with the same status. The match is the person's or
// the receipt matcher's, which is already as sure as that link is.
func linkMatchedReceipts(tx db.Transaction, pass *recordLinkPass) error {
	receipts, err := tx.ListFinanceReceipts(pass.agent.ID, &db.FinanceReceiptFilter{Limit: recordLinksTransactionCount})
	if err != nil {
		return err
	}
	for _, receipt := range receipts.FinanceReceipts {
		for _, match := range receipt.ReceiptMatches {
			if pass.remaining <= 0 {
				return nil
			}
			links, err := tx.ListAgentRecordBacklinks(pass.agent.ID, models.RecordFinanceTransaction, match.FinanceTransactionID, 0, 0, 20)
			if err != nil {
				return err
			}
			for _, link := range links {
				if link.RecordRelation != models.RecordRelationPaid && link.RecordRelation != models.RecordRelationPaidBy {
					continue
				}
				if _, err := pass.put(tx, &models.AgentRecordLink{
					NodeID: link.NodeID, RecordKind: models.RecordFinanceReceipt, RecordID: receipt.ID,
					RecordRelation: models.RecordRelationReceiptFor, LinkStatus: link.LinkStatus, HappenedAt: receipt.PurchasedAt,
					Evidence: []models.Evidence{{Kind: models.EvidenceFinanceReceipt, ID: receipt.ID, Quote: "the receipt is matched to the transaction the page is linked to"}},
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
