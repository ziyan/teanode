package journal

import (
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// The sections of a day, one reader each. Every one asks its source for the
// day's range and keeps the few fields a page shows.

// postClock is the first line of a post in a chat unit: "15:04 author: words".
var postClock = regexp.MustCompile(`(?m)^(\d{1,2}):(\d{2}) [^:\n]{1,80}: `)

// journalNoteTextMost is how much of a note from the notes folder a day keeps.
const journalNoteTextMost = 4000

// documentKindsOfADay are the kinds of ingested document a day looks at:
// chat, and the pages, posts and messages of wiki, drive and forum sources.
// Files, attachments and commits are not: a folder of files is not something
// that happened to the person that day.
var documentKindsOfADay = []models.AgentDocumentKind{
	models.DocumentChat, models.DocumentPage, models.DocumentPost, models.DocumentMessage, models.DocumentRequest,
}

// involvementKeys are the metadata a document may carry to say who wrote it,
// who edited it or who was named in it.
var involvementKeys = []string{
	"author", "authors", "editor", "editors", "lastModifiedBy", "owner", "participants", "from", "to",
	"assignee", "mentions", "attendees",
}

func (self *dayReader) loadSources() error {
	if self.sources != nil {
		return nil
	}
	self.sources = map[string]*models.AgentKnowledgeSource{}
	sources, err := self.transaction.ListAgentSources(self.agent.ID)
	if err != nil {
		return err
	}
	for _, source := range sources {
		self.sources[source.ID] = source
	}
	return nil
}

// isOtherwiseRead says the source of a document is read by another section
// of the day, or is not the person's doing: where they went (the visits),
// the recordings, their sent mail (the mail), and the circle chats (the
// chats).
func isOtherwiseRead(source *models.AgentKnowledgeSource) bool {
	if source == nil {
		return false
	}
	switch source.Kind {
	case models.SourceLocation, models.SourceRecording, models.SourceSent, models.SourceChat:
		return true
	}
	return false
}

// codingAssistantOf is claude-code or codex for a chat document a coding
// tool's reader filed, and empty for any other. The readers name the tool
// as the person knows it ("Claude Code").
func codingAssistantOf(document *models.AgentDocument) string {
	if document.Kind != models.DocumentChat {
		return ""
	}
	text, _ := document.Metadata["assistant"].(string)
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer(" ", "-", "_", "-").Replace(text)
	switch text {
	case "claude-code", "codex":
		return text
	}
	return ""
}

func metadataText(document *models.AgentDocument, key string) string {
	text, _ := document.Metadata[key].(string)
	return strings.TrimSpace(text)
}

// postsOf is how many posts a chat unit holds.
func postsOf(document *models.AgentDocument) int {
	switch count := document.Metadata["posts"].(type) {
	case float64:
		return int(count)
	case int:
		return count
	case int64:
		return int(count)
	}
	return 0
}

// isInChatUnit says the person took part in a chat unit and it is an
// exchange rather than a remark: the rule the dream's digest reads a chat by
// (two posts or more, the person among the participants).
func isInChatUnit(document *models.AgentDocument, names []string) bool {
	if postsOf(document) < 2 {
		return false
	}
	participants, _ := document.Metadata["participants"].([]any)
	for _, participant := range participants {
		name, _ := participant.(string)
		for _, word := range words(name) {
			if containsName(names, word) {
				return true
			}
		}
		if containsName(names, strings.ToLower(strings.TrimSpace(name))) {
			return true
		}
	}
	return false
}

func containsName(names []string, name string) bool {
	for _, known := range names {
		if known == name {
			return true
		}
	}
	return false
}

// words are the lowercase words of a text, cut at anything that is not a
// letter or a digit.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
}

// isNamedIn says the person is named in the metadata a document carries about
// who made it: its author, an editor, a participant. A document that says
// nothing of who was involved is not counted as theirs, since there is no
// telling.
func isNamedIn(document *models.AgentDocument, names []string) bool {
	for _, key := range involvementKeys {
		value, isSet := document.Metadata[key]
		if !isSet {
			continue
		}
		var texts []string
		switch typed := value.(type) {
		case string:
			texts = []string{typed}
		case []any:
			for _, element := range typed {
				if text, isText := element.(string); isText {
					texts = append(texts, text)
				}
			}
		}
		for _, text := range texts {
			for _, word := range words(text) {
				if containsName(names, word) {
					return true
				}
			}
		}
	}
	return false
}

// postClockSpan is the minutes between the first and the last post of a chat
// unit's text, counted on the clock the unit writes them in, which a post
// that crosses midnight wraps.
func postClockSpan(chunks []*models.AgentChunk) (minutes int, isFound bool) {
	first, last := -1, -1
	for _, chunk := range chunks {
		for _, match := range postClock.FindAllStringSubmatch(chunk.Text, -1) {
			hour, minute := atoi(match[1]), atoi(match[2])
			if hour > 23 || minute > 59 {
				continue
			}
			clock := hour*60 + minute
			if first < 0 {
				first = clock
			}
			last = clock
		}
	}
	if first < 0 {
		return 0, false
	}
	return (last - first + 24*60) % (24 * 60), true
}

func atoi(text string) int {
	value := 0
	for _, digit := range text {
		value = value*10 + int(digit-'0')
	}
	return value
}

func (self *dayReader) readDocuments() error {
	if err := self.loadSources(); err != nil {
		return err
	}
	names := reading.ChatNamesOf(self.user)
	documents, err := self.transaction.ListAgentDocumentsBetween(self.agent.ID, documentKindsOfADay, self.day.From, self.day.Until, documentsScanMost)
	if err != nil {
		return err
	}
	// A day of coding is many stretches (each ended by a pause of half an
	// hour), and the page reads it as the projects worked on: one session per
	// assistant, directory and title, from its first stretch to its last.
	sessionIndexes := map[string]int{}
	lastStretches := map[string]*models.AgentDocument{}
	for _, document := range documents {
		source := self.sources[document.SourceID]
		if isOtherwiseRead(source) || document.HappenedAt == nil {
			continue
		}
		if assistant := codingAssistantOf(document); assistant != "" {
			session := codingSessionOf(document, assistant)
			key := session.Assistant + "\x00" + session.Directory + "\x00" + session.Title
			index, isKnown := sessionIndexes[key]
			if !isKnown {
				sessionIndexes[key] = len(self.day.CodingSessions)
				self.day.CodingSessions = append(self.day.CodingSessions, session)
				lastStretches[key] = document
				continue
			}
			grouped := &self.day.CodingSessions[index]
			grouped.StretchCount++
			if document.HappenedAt.Before(grouped.StartedAt) {
				grouped.StartedAt, grouped.DocumentID = *document.HappenedAt, document.ID
			}
			if document.HappenedAt.After(*lastStretches[key].HappenedAt) {
				lastStretches[key] = document
			}
			continue
		}
		if document.Kind == models.DocumentChat {
			if !isInChatUnit(document, names) {
				continue
			}
		} else if !isNamedIn(document, names) {
			continue
		}
		self.day.DocumentTotal++
		if len(self.day.Documents) < DocumentsMost {
			sourceName := ""
			if source != nil {
				sourceName = source.Name
			}
			entry := Document{DocumentID: document.ID, Title: document.Title, SourceName: sourceName, HappenedAt: *document.HappenedAt}
			entry.ModifiedAt = entry.HappenedAt
			if document.ModifiedAt != nil {
				entry.ModifiedAt = *document.ModifiedAt
			}
			self.day.Documents = append(self.day.Documents, entry)
		}
	}
	// Only the last stretch of each session is read for when it ended: the
	// others end before it.
	for key, index := range sessionIndexes {
		last := lastStretches[key]
		session := &self.day.CodingSessions[index]
		session.EndedAt = *last.HappenedAt
		chunks, err := self.transaction.ListAgentChunks(self.agent.ID, last.ID)
		if err != nil {
			return err
		}
		// The stretch says when it began and the clock of each post; the last
		// post's clock, counted from the first, is when it ended.
		if minutes, isFound := postClockSpan(chunks); isFound {
			session.EndedAt = last.HappenedAt.Add(time.Duration(minutes) * time.Minute)
		}
	}
	sort.SliceStable(self.day.CodingSessions, func(left, right int) bool {
		return self.day.CodingSessions[left].StartedAt.Before(self.day.CodingSessions[right].StartedAt)
	})
	return self.readJournalNotes()
}

func codingSessionOf(document *models.AgentDocument, assistant string) CodingSession {
	session := CodingSession{
		DocumentID: document.ID, Title: metadataText(document, "channel"), Directory: metadataText(document, "directory"),
		Assistant: assistant, StartedAt: *document.HappenedAt, EndedAt: *document.HappenedAt, StretchCount: 1,
	}
	if session.Title == "" {
		session.Title = document.Title
	}
	return session
}

// readJournalNotes finds the person's own dated notes of the day. A note is
// dated at noon UTC of its file's date, which in a zone far east of Greenwich
// is already the next morning, so a time of exactly noon UTC is placed by its
// date and not by the zone.
func (self *dayReader) readJournalNotes() error {
	const slack = 14 * time.Hour
	documents, err := self.transaction.ListAgentDocumentsBetween(self.agent.ID, []models.AgentDocumentKind{models.DocumentJournal}, self.day.From.Add(-slack), self.day.Until.Add(slack), 500)
	if err != nil {
		return err
	}
	for _, document := range documents {
		if document.HappenedAt == nil || isOtherwiseRead(self.sources[document.SourceID]) {
			continue
		}
		happenedAt := document.HappenedAt.UTC()
		isDateOnly := happenedAt.Hour() == 12 && happenedAt.Minute() == 0 && happenedAt.Second() == 0
		if isDateOnly {
			if happenedAt.Format(DayLayout) != self.day.Day {
				continue
			}
		} else if happenedAt.Before(self.day.From) || !happenedAt.Before(self.day.Until) {
			continue
		}
		chunks, err := self.transaction.ListAgentChunks(self.agent.ID, document.ID)
		if err != nil {
			return err
		}
		var text strings.Builder
		for _, chunk := range chunks {
			text.WriteString(chunk.Text)
			text.WriteString("\n")
		}
		self.day.JournalNotes = append(self.day.JournalNotes, JournalNote{
			DocumentID: document.ID, Title: document.Title, Text: cutCharacters(strings.TrimSpace(text.String()), journalNoteTextMost),
		})
	}
	return nil
}

func cutCharacters(text string, most int) string {
	runes := []rune(text)
	if len(runes) <= most {
		return text
	}
	return string(runes[:most])
}

func (self *dayReader) readConversations() error {
	activity, err := self.transaction.ListAgentConversationActivityBetween(self.agent.ID, self.day.From, self.day.Until)
	if err != nil {
		return err
	}
	for _, conversation := range activity {
		title := strings.TrimSpace(conversation.Title)
		if title == "" {
			title = strings.TrimSpace(conversation.Summary)
		}
		if title == "" {
			title = "Conversation"
		}
		self.day.Conversations = append(self.day.Conversations, Conversation{
			ConversationID: conversation.ConversationID, Title: title, FirstMessageID: conversation.FirstMessageID,
			FirstAt: conversation.FirstAt, LastAt: conversation.LastAt, MessageCount: conversation.MessageCount,
		})
	}
	return nil
}

func (self *dayReader) readChats() error {
	activity, err := self.transaction.ListChatActivityBetween(self.user.ID, self.day.From, self.day.Until)
	if err != nil {
		return err
	}
	conversationIds := map[string]string{}
	if self.agent != nil && len(activity) > 0 {
		conversations, err := self.transaction.ListChatConversations(self.agent.ID)
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			conversationIds[conversation.ChatID] = conversation.ID
		}
	}
	for _, chat := range activity {
		name, err := self.chatName(chat.ChatID)
		if err != nil {
			return err
		}
		self.day.Chats = append(self.day.Chats, Chat{
			ChatID: chat.ChatID, ChatName: name, ConversationID: conversationIds[chat.ChatID], FirstMessageID: chat.FirstMessageID,
			FirstAt: chat.FirstAt, LastAt: chat.LastAt, MessageCount: chat.MessageCount,
		})
	}
	return nil
}

// chatName is what the person calls a chat: a group's title, a circle's name
// for its chat, else the other people in it.
func (self *dayReader) chatName(chatId string) (string, error) {
	chat, err := self.transaction.GetChat(chatId)
	if err != nil || chat == nil {
		return "Chat", err
	}
	if title := strings.TrimSpace(chat.Title); title != "" {
		return title, nil
	}
	if chat.CircleID != "" {
		circle, err := self.transaction.GetCircle(chat.CircleID)
		if err != nil {
			return "", err
		}
		if circle != nil && strings.TrimSpace(circle.Name) != "" {
			return strings.TrimSpace(circle.Name), nil
		}
	}
	others := []string{}
	for _, member := range chat.ActiveMembers() {
		if member.UserID != self.user.ID && strings.TrimSpace(member.Name) != "" {
			others = append(others, strings.TrimSpace(member.Name))
		}
	}
	if len(others) == 0 {
		return "Chat", nil
	}
	return strings.Join(others, ", "), nil
}

// importantMailOf are the messages the agent made something of: the sorting
// called them high priority or in need of a reply or an alert, the agent
// alerted about them, or a follow-up was made from them. The sets are of
// item identifiers and mail identifiers both, since each source names one.
type importantMail struct {
	mailIds map[string]bool
	itemIds map[string]bool
}

func (self *dayReader) importantMail() (*importantMail, error) {
	important := &importantMail{mailIds: map[string]bool{}, itemIds: map[string]bool{}}
	if self.agent == nil {
		return important, nil
	}
	alerts, err := self.transaction.ListAgentAlertsSince(self.agent.ID, self.day.From)
	if err != nil {
		return nil, err
	}
	candidateIds := []string{}
	for _, alert := range alerts {
		if alert.SentAt.Before(self.day.Until) {
			candidateIds = append(candidateIds, alert.CandidateIDs...)
		}
	}
	if len(candidateIds) > 0 {
		candidates, err := self.transaction.ListAgentAlertCandidatesByID(self.agent.ID, candidateIds)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			important.mailIds[candidate.MailID] = true
		}
	}
	followUps, err := self.transaction.ListAgentFollowUps(self.agent.ID, 300)
	if err != nil {
		return nil, err
	}
	for _, followUp := range followUps {
		for _, itemId := range followUp.SourceItemIDs {
			important.itemIds[itemId] = true
		}
	}
	return important, nil
}

func (self *dayReader) readMail() error {
	mailboxes, err := self.transaction.ListMailboxes(self.user.ID)
	if err != nil {
		return err
	}
	important, err := self.importantMail()
	if err != nil {
		return err
	}
	var found []Mail
	for _, mailbox := range mailboxes {
		folders, err := self.transaction.ListFolders(mailbox.ID, nil)
		if err != nil {
			return err
		}
		var sentFolderId string
		for _, folder := range folders {
			if folder.Kind == models.MailboxFolderKindSent {
				sentFolderId = folder.ID
				break
			}
		}
		// What arrived: every folder but the ones that are not mail the person
		// received. Not notes, which are the notes section's, and not drafts.
		received := &db.ItemOptions{
			MailboxID: mailbox.ID, Since: self.day.From, Before: self.day.Until, Limit: MailMost, ByReceived: true,
			ExcludeKinds: []models.MailboxFolderKind{
				models.MailboxFolderKindTrash, models.MailboxFolderKindJunk, models.MailboxFolderKindDrafts, models.MailboxFolderKindSent,
			},
			ExcludeMailKinds: []models.MailKind{models.MailKindNote, models.MailKindDraft},
		}
		total, err := self.transaction.CountItems("", received)
		if err != nil {
			return err
		}
		self.day.MailReceivedTotal += int(total)
		items, err := self.transaction.ListItems("", received)
		if err != nil {
			return err
		}
		more, err := self.mailOf(mailbox.ID, items, false, important)
		if err != nil {
			return err
		}
		found = append(found, more...)
		if sentFolderId == "" {
			continue
		}
		sent := &db.ItemOptions{Since: self.day.From, Before: self.day.Until, Limit: MailMost, ByReceived: true, ExcludeMailKinds: []models.MailKind{models.MailKindNote}}
		total, err = self.transaction.CountItems(sentFolderId, sent)
		if err != nil {
			return err
		}
		self.day.MailSentTotal += int(total)
		items, err = self.transaction.ListItems(sentFolderId, sent)
		if err != nil {
			return err
		}
		more, err = self.mailOf(mailbox.ID, items, true, important)
		if err != nil {
			return err
		}
		found = append(found, more...)
	}
	found = collapseMail(found)
	// Past the most, the important ones stay, then the newest.
	if len(found) > MailMost {
		sort.SliceStable(found, func(left, right int) bool {
			if found[left].IsImportant != found[right].IsImportant {
				return found[left].IsImportant
			}
			return found[left].ReceivedAt.After(found[right].ReceivedAt)
		})
		found = found[:MailMost]
	}
	sort.SliceStable(found, func(left, right int) bool { return found[left].ReceivedAt.Before(found[right].ReceivedAt) })
	self.day.Mail = found
	return nil
}

func (self *dayReader) mailOf(mailboxId string, items []*models.MailboxItem, isSent bool, important *importantMail) ([]Mail, error) {
	if len(items) == 0 {
		return nil, nil
	}
	mailIds := make([]string, 0, len(items))
	for _, item := range items {
		mailIds = append(mailIds, item.MailID)
	}
	mails, err := self.transaction.GetMails(mailIds, nil)
	if err != nil {
		return nil, err
	}
	byId := map[string]*models.Mail{}
	for _, stored := range mails {
		if stored != nil {
			byId[stored.ID] = stored
		}
	}
	insights, err := self.transaction.GetMailInsights(mailboxId, mailIds)
	if err != nil {
		return nil, err
	}
	var found []Mail
	for _, item := range items {
		stored := byId[item.MailID]
		if stored == nil {
			continue
		}
		entry := Mail{ItemID: item.ID, MailID: item.MailID, Subject: stored.Subject, ReceivedAt: stored.ReceivedAt, IsSent: isSent}
		if isSent {
			if len(stored.Recipients) > 0 {
				entry.Counterpart = stored.Recipients[0]
			}
		} else {
			entry.Counterpart = stored.FromName
			if entry.Counterpart == "" {
				entry.Counterpart = addressName(stored.From)
			}
		}
		if insight := insights[item.MailID]; insight != nil {
			entry.IsImportant = insight.Priority == "high" || insight.NeedsReply ||
				insight.AlertSignal == models.AlertSignalNow || insight.AlertSignal == models.AlertSignalSoon
		}
		if important.mailIds[item.MailID] || important.itemIds[item.ID] {
			entry.IsImportant = true
		}
		found = append(found, entry)
	}
	return found, nil
}

// collapseMail is the day's messages with each stored message once (one
// message filed in two folders or mailboxes is one), and with the same sender
// and subject arriving again folded into the first, counted: a newsletter to
// two of the person's addresses, or a code sent eleven times, is one line.
func collapseMail(found []Mail) []Mail {
	sort.SliceStable(found, func(left, right int) bool { return found[left].ReceivedAt.Before(found[right].ReceivedAt) })
	seenMailIds := map[string]bool{}
	firstIndexes := map[string]int{}
	collapsed := make([]Mail, 0, len(found))
	for _, entry := range found {
		if seenMailIds[entry.MailID] {
			continue
		}
		seenMailIds[entry.MailID] = true
		key := fmt.Sprintf("%t\x00%s\x00%s", entry.IsSent, strings.ToLower(strings.TrimSpace(entry.Counterpart)), strings.TrimSpace(entry.Subject))
		if index, isKnown := firstIndexes[key]; isKnown {
			collapsed[index].RepeatCount++
			collapsed[index].IsImportant = collapsed[index].IsImportant || entry.IsImportant
			continue
		}
		entry.RepeatCount = 1
		firstIndexes[key] = len(collapsed)
		collapsed = append(collapsed, entry)
	}
	return collapsed
}

// addressName is the name beside an address in a header, else the address.
func addressName(header string) string {
	header = strings.TrimSpace(header)
	if parsed, err := mail.ParseAddress(header); err == nil {
		if parsed.Name != "" {
			return parsed.Name
		}
		return parsed.Address
	}
	return header
}

func (self *dayReader) readRecordings() error {
	recordings, err := self.transaction.ListRecordingsBetween(self.user.ID, self.day.From, self.day.Until, 200)
	if err != nil {
		return err
	}
	for _, recording := range recordings {
		recordedAt := recording.CreatedAt
		if recording.RecordedAt != nil {
			recordedAt = *recording.RecordedAt
		}
		self.day.Recordings = append(self.day.Recordings, Recording{
			RecordingID: recording.ID, Title: recording.RecordingTitle, RecordedAt: recordedAt, UpdatedAt: recording.UpdatedAt,
			DurationMS: recording.TimelineDurationMS, Summary: recording.RecordingSummary,
		})
	}
	return nil
}

// photoPlacedAt is when a photo is placed in the person's timeline: when it
// was taken, else when it arrived.
func photoPlacedAt(photo *models.Photo) time.Time {
	if photo.TakenAt != nil {
		return *photo.TakenAt
	}
	return photo.CreatedAt
}

// readPhotos reads the day's photos by the instant they were placed at,
// inside the day's zone, so that a photo taken at 23:30 on the person's
// clock is on that day and not on the next one in UTC.
func (self *dayReader) readPhotos() error {
	from, until := self.day.From, self.day.Until
	var all []*models.Photo
	for offset := 0; offset < photosScanMost; offset += 500 {
		page, err := self.transaction.ListPhotos(self.user.ID, &db.PhotoFilter{TakenAfter: &from, TakenBefore: &until, PageSize: 500, Offset: offset})
		if err != nil {
			return err
		}
		all = append(all, page...)
		if len(page) < 500 {
			break
		}
	}
	total, err := self.transaction.CountPhotos(self.user.ID, &db.PhotoFilter{TakenAfter: &from, TakenBefore: &until})
	if err != nil {
		return err
	}
	if total < len(all) {
		total = len(all)
	}
	self.day.PhotoTotal = total
	// Earliest first, as the day is told.
	sort.SliceStable(all, func(left, right int) bool { return photoPlacedAt(all[left]).Before(photoPlacedAt(all[right])) })
	self.day.PhotoCandidateIDs = spreadPhotos(all, photoCandidatesMost)
	for _, photo := range all {
		if len(self.day.Photos) == PhotosMost {
			break
		}
		self.day.Photos = append(self.day.Photos, Photo{
			PhotoID: photo.ID, TakenAt: photoPlacedAt(photo), PlaceName: photo.PlaceName, IsVideo: photo.PhotoKind == models.PhotoKindVideo,
		})
	}
	return nil
}

// spreadPhotos is up to most photos spread evenly over the day's, in the order
// they were taken. Screenshots are passed over while there are enough that are
// not, since a picture of a screen is a poor one to stand for a day.
func spreadPhotos(photos []*models.Photo, most int) []string {
	pool := make([]*models.Photo, 0, len(photos))
	for _, photo := range photos {
		if !photo.IsScreenshot {
			pool = append(pool, photo)
		}
	}
	if len(pool) < most {
		pool = photos
	}
	if len(pool) == 0 {
		return nil
	}
	if len(pool) <= most {
		identifiers := make([]string, 0, len(pool))
		for _, photo := range pool {
			identifiers = append(identifiers, photo.ID)
		}
		return identifiers
	}
	identifiers := make([]string, 0, most)
	for index := 0; index < most; index++ {
		identifiers = append(identifiers, pool[index*len(pool)/most].ID)
	}
	return identifiers
}

func (self *dayReader) readVisits() error {
	from, until := self.day.From, self.day.Until
	offset := 0
	for {
		visits, nextOffset, err := self.transaction.ListPlaceVisits(self.user.ID, db.PlaceVisitFilter{From: &from, Until: &until, Offset: offset, LimitCount: db.PlacePageMost})
		if err != nil {
			return err
		}
		for _, visit := range visits {
			entry := Visit{VisitID: visit.ID, PlaceID: visit.PlaceID, ArrivedAt: visit.ArrivedAt, DepartedAt: visit.DepartedAt,
				Latitude: visit.LatitudeDegrees, Longitude: visit.LongitudeDegrees}
			if visit.Place != nil {
				entry.PlaceName = visit.Place.PlaceName
				entry.Latitude, entry.Longitude = visit.Place.LatitudeDegrees, visit.Place.LongitudeDegrees
			}
			self.day.Visits = append(self.day.Visits, entry)
		}
		if nextOffset == 0 {
			break
		}
		offset = nextOffset
	}
	sort.SliceStable(self.day.Visits, func(left, right int) bool {
		return visitStartedAt(self.day.Visits[left]).Before(visitStartedAt(self.day.Visits[right]))
	})
	return nil
}

// visitStartedAt is when a visit began, else when it ended for a visit whose
// arrival the phone did not see.
func visitStartedAt(visit Visit) time.Time {
	if visit.ArrivedAt != nil {
		return *visit.ArrivedAt
	}
	if visit.DepartedAt != nil {
		return *visit.DepartedAt
	}
	return time.Time{}
}

// readCalendar reads the events and the reminders of the day from the
// person's own calendars. An all-day event or a reminder due on a date is a
// date and does not move with the zone, so it is found by the date read as
// UTC; what has a time is found by the day's own range.
func (self *dayReader) readCalendar() error {
	calendars, err := self.transaction.ListCalendars(self.user.ID)
	if err != nil {
		return err
	}
	dateFrom := time.Date(self.dayDate().Year(), self.dayDate().Month(), self.dayDate().Day(), 0, 0, 0, 0, time.UTC)
	dateUntil := dateFrom.AddDate(0, 0, 1)
	for _, found := range calendars {
		if found.CalendarKind == models.CalendarReminders {
			if err := self.readReminders(found, dateFrom, dateUntil); err != nil {
				return err
			}
			continue
		}
		timed, err := self.transaction.ListOccurrences(found.ID, self.day.From, self.day.Until)
		if err != nil {
			return err
		}
		dated, err := self.transaction.ListOccurrences(found.ID, dateFrom, dateUntil)
		if err != nil {
			return err
		}
		objects := map[string]*models.CalendarObject{}
		keep := func(occurrence *models.Occurrence, isAllDay bool) error {
			if occurrence.AllDay != isAllDay {
				return nil
			}
			object, isCached := objects[occurrence.ObjectID]
			if !isCached {
				object, err = self.transaction.GetCalendarObject(found.ID, occurrence.ObjectID)
				if err != nil {
					return err
				}
				objects[occurrence.ObjectID] = object
			}
			if object == nil || strings.EqualFold(object.Status, "CANCELLED") {
				return nil
			}
			self.day.Events = append(self.day.Events, Event{
				EventID: object.ID, Title: object.Summary, CalendarName: found.Name,
				StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, IsAllDay: occurrence.AllDay,
				Location: object.Location, ModifiedAt: object.ModifiedAt,
			})
			return nil
		}
		for _, occurrence := range timed {
			if err := keep(occurrence, false); err != nil {
				return err
			}
		}
		for _, occurrence := range dated {
			if err := keep(occurrence, true); err != nil {
				return err
			}
		}
	}
	sort.SliceStable(self.day.Events, func(left, right int) bool {
		one, other := self.day.Events[left], self.day.Events[right]
		if one.IsAllDay != other.IsAllDay {
			return one.IsAllDay
		}
		return one.StartsAt.Before(other.StartsAt)
	})
	sort.SliceStable(self.day.Reminders, func(left, right int) bool {
		return reminderAt(self.day.Reminders[left]).Before(reminderAt(self.day.Reminders[right]))
	})
	return nil
}

func reminderAt(reminder Reminder) time.Time {
	switch {
	case reminder.DoneAt != nil:
		return *reminder.DoneAt
	case reminder.DueAt != nil:
		return *reminder.DueAt
	}
	return reminder.ModifiedAt
}

// dayDate is the day as a date, at midnight UTC.
func (self *dayReader) dayDate() time.Time {
	parsed, _ := ParseDay(self.day.Day)
	return parsed
}

func (self *dayReader) readReminders(list *models.Calendar, dateFrom, dateUntil time.Time) error {
	seen := map[string]bool{}
	add := func(object *models.CalendarObject) {
		if seen[object.ID] {
			return
		}
		parsed, err := calendar.ParseReminder([]byte(object.Data))
		if err != nil {
			// A file nobody can read is left out, as the reminders list does.
			return
		}
		entry := Reminder{ReminderID: object.ID, Title: parsed.Title, ModifiedAt: object.ModifiedAt}
		if !parsed.DueAt.IsZero() {
			dueAt := parsed.DueAt
			entry.DueAt = &dueAt
		}
		isDueToday := false
		if entry.DueAt != nil {
			if parsed.IsDueDate {
				isDueToday = !entry.DueAt.Before(dateFrom) && entry.DueAt.Before(dateUntil)
			} else {
				isDueToday = !entry.DueAt.Before(self.day.From) && entry.DueAt.Before(self.day.Until)
			}
		}
		isDoneToday := false
		if parsed.IsDone {
			doneAt := parsed.DoneAt
			if doneAt.IsZero() {
				doneAt = object.ModifiedAt
			}
			entry.DoneAt = &doneAt
			isDoneToday = !doneAt.Before(self.day.From) && doneAt.Before(self.day.Until)
		}
		if !isDueToday && !isDoneToday {
			return
		}
		seen[object.ID] = true
		self.day.Reminders = append(self.day.Reminders, entry)
	}
	dueTimed, err := self.transaction.ListCalendarObjectsStartingBetween(list.ID, self.day.From, self.day.Until.Add(-time.Nanosecond), 500)
	if err != nil {
		return err
	}
	dueDated, err := self.transaction.ListCalendarObjectsStartingBetween(list.ID, dateFrom, dateUntil.Add(-time.Nanosecond), 500)
	if err != nil {
		return err
	}
	done, err := self.transaction.ListCalendarObjectsDoneSince(list.ID, self.day.From.Add(-24*time.Hour), 500)
	if err != nil {
		return err
	}
	for _, objects := range [][]*models.CalendarObject{dueTimed, dueDated, done} {
		for _, object := range objects {
			add(object)
		}
	}
	return nil
}

func (self *dayReader) readTransactions() error {
	// A transaction is on the day it happened if it says when, else the day it
	// posted; the day before and after are read so that one that posted a day
	// after it happened is still found.
	before, _ := AddDays(self.day.Day, -1)
	after, _ := AddDays(self.day.Day, 1)
	page, err := self.transaction.ListFinanceTransactions(self.agent.ID, &db.FinanceTransactionFilter{
		From: before, To: after, Limit: db.FinanceTransactionLimitMost,
		IsTransferExcluded: true, IsDuplicateExcluded: true,
	})
	if err != nil {
		return err
	}
	accountNames := map[string]string{}
	for _, found := range page.FinanceTransactions {
		isOnDay := found.PostedOn == self.day.Day
		if found.TransactedAt != nil {
			isOnDay = !found.TransactedAt.Before(self.day.From) && found.TransactedAt.Before(self.day.Until)
		}
		if !isOnDay {
			continue
		}
		name, isCached := accountNames[found.FinanceAccountID]
		if !isCached {
			account, err := self.transaction.GetFinanceAccount(self.agent.ID, found.FinanceAccountID)
			if err != nil {
				return err
			}
			if account != nil {
				name = account.AccountName
			}
			accountNames[found.FinanceAccountID] = name
		}
		description := strings.TrimSpace(found.MerchantName)
		if description == "" {
			description = strings.TrimSpace(found.Description)
		}
		self.day.Transactions = append(self.day.Transactions, Transaction{
			TransactionID: found.ID, PostedOn: found.PostedOn, TransactedAt: found.TransactedAt, Description: description,
			AccountName: name, AmountMinorUnits: minorUnits(found.Amount, found.CurrencyCode), Currency: found.CurrencyCode,
		})
	}
	// By the time they happened, and those that say only a day by when they
	// were made known to us, which is the same on every read.
	sort.SliceStable(self.day.Transactions, func(left, right int) bool {
		one, other := self.day.Transactions[left], self.day.Transactions[right]
		if at, otherAt := transactionAt(one, self.day.From), transactionAt(other, self.day.From); !at.Equal(otherAt) {
			return at.Before(otherAt)
		}
		return one.TransactionID < other.TransactionID
	})
	return nil
}

// transactionAt is when a transaction happened, else the start of the day it
// posted on.
func transactionAt(transaction Transaction, dayStart time.Time) time.Time {
	if transaction.TransactedAt != nil {
		return *transaction.TransactedAt
	}
	return dayStart
}

// minorUnits is a decimal amount ("-12.50") in the smallest unit of its
// currency (-1250), rounded to the nearest, and zero for text that is not an
// amount.
func minorUnits(amount, currencyCode string) int64 {
	value, isParsed := new(big.Rat).SetString(strings.TrimSpace(amount))
	if !isParsed {
		return 0
	}
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(finance.CurrencyMinorUnits(currencyCode))), nil))
	scaled := new(big.Rat).Mul(value, scale)
	// Round half away from zero.
	half := big.NewRat(1, 2)
	if scaled.Sign() < 0 {
		scaled.Sub(scaled, half)
	} else {
		scaled.Add(scaled, half)
	}
	rounded := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	return rounded.Int64()
}

func (self *dayReader) readNotes() error {
	mailboxes, err := self.transaction.ListMailboxes(self.user.ID)
	if err != nil {
		return err
	}
	for _, mailbox := range mailboxes {
		items, err := self.transaction.ListNoteItemsBetween(mailbox.ID, self.day.From, self.day.Until)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, item := range items {
			if item.Mail == nil || item.Deleted || seen[item.Mail.NoteIdentifier] {
				continue
			}
			seen[item.Mail.NoteIdentifier] = true
			title := strings.TrimSpace(item.Mail.Subject)
			if title == "" {
				title = "Note"
			}
			self.day.Notes = append(self.day.Notes, Note{NoteID: item.Mail.NoteIdentifier, FolderID: item.FolderID, ItemID: item.ID, Title: title, ModifiedAt: item.AddedAt})
		}
	}
	sort.SliceStable(self.day.Notes, func(left, right int) bool {
		return self.day.Notes[left].ModifiedAt.Before(self.day.Notes[right].ModifiedAt)
	})
	return nil
}

func (self *dayReader) readMemoryPages() error {
	facts, err := self.transaction.ListAgentFactsBetween(self.agent.ID, self.day.From, self.day.Until, 500)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	var order []string
	for _, fact := range facts {
		// A fact known only to the month or the year is dated at its first
		// day, which is not this day.
		if fact.HappenedPrecision != "" && fact.HappenedPrecision != models.HappenedDay {
			continue
		}
		if counts[fact.NodeID] == 0 {
			order = append(order, fact.NodeID)
		}
		counts[fact.NodeID]++
	}
	for _, nodeId := range order {
		node, err := self.transaction.GetAgentNodeByID(self.agent.ID, nodeId)
		if err != nil {
			return err
		}
		if node == nil {
			continue
		}
		self.day.MemoryPages = append(self.day.MemoryPages, MemoryPage{NodeID: node.ID, Path: node.Path, Title: node.Name, FactCount: counts[nodeId]})
	}
	return nil
}
