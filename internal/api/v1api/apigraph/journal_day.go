package apigraph

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// Reading a whole day or period. A day is read from every source the caller
// may read, by the day's time range, and told with the journal's own parts;
// a period is the stored rows of its days, which a timeline can show without
// reading a source for each.

// journalKindPermission is the permission that reads the source a kind of
// record is from.
var journalKindPermission = map[models.AgentRecordKind]models.Permission{
	models.RecordContact:             models.PermissionContactsUse,
	models.RecordMail:                models.PermissionMailRead,
	models.RecordNote:                models.PermissionMailRead,
	models.RecordCalendarEvent:       models.PermissionCalendarUse,
	models.RecordCalendarOccurrence:  models.PermissionCalendarUse,
	models.RecordReminder:            models.PermissionCalendarUse,
	models.RecordPhoto:               models.PermissionPhotosUse,
	models.RecordPlaceVisit:          models.PermissionLocationUse,
	models.RecordHealthDay:           models.PermissionHealthUse,
	models.RecordChatMessage:         models.PermissionCircleUse,
	models.RecordConversationMessage: models.PermissionAgentUse,
	models.RecordAgentDocument:       models.PermissionAgentUse,
	models.RecordRecording:           models.PermissionAgentUse,
	models.RecordRecordingPassage:    models.PermissionAgentUse,
	models.RecordFinanceTransaction:  models.PermissionAgentUse,
	models.RecordFinanceReceipt:      models.PermissionAgentUse,
	models.RecordIdea:                models.PermissionAgentUse,
	models.RecordGoal:                models.PermissionAgentUse,
}

// isJournalKindReadable says the caller may read the source a kind of record
// is from; a kind this table does not know is not.
func isJournalKindReadable(permissions *models.EffectivePermissions, recordKind models.AgentRecordKind) bool {
	permission, isKnown := journalKindPermission[recordKind]
	return isKnown && permissions.Has(permission)
}

// journalDayRow is the day's stored row, or the row a day would have if it
// were kept now, for a day nothing was kept for.
func journalDayRow(read *journal.Day) *models.JournalDay {
	if read.Row != nil {
		return read.Row
	}
	return &models.JournalDay{Day: read.Day, TimeZone: read.TimeZone, NarrativeCorrections: []models.JournalCorrection{}}
}

// chosenReasons are the reasons the agent gave for the records it chose,
// by kind:id. A choice the person hid is not one.
func chosenReasons(links []*models.JournalLink) map[string]string {
	reasons := map[string]string{}
	for _, link := range links {
		if link.LinkSource == models.JournalLinkFromAgent && !link.IsHidden {
			reasons[string(link.RecordKind)+":"+link.RecordID] = link.LinkReason
		}
	}
	return reasons
}

func (self *graph) ReadJournalDay(ctx context.Context, arguments ReadJournalDayArguments) (*JournalDayView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := journal.ParseDay(arguments.Day); err != nil {
		return nil, fmt.Errorf("%w: day: %s", api.ErrInvalidArguments, err)
	}
	now, tx := time.Now(), self.transaction(ctx)
	today := journal.Today(principal.User, now)
	if arguments.Day > today {
		return nil, api.ErrNotFound
	}
	agent, err := tx.GetAgentByUser(principal.User.ID)
	if err != nil {
		return nil, err
	}
	options, err := self.journalReadOptions(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	read, err := journal.ReadDay(tx, principal.User, agent, arguments.Day, now, options)
	if err != nil {
		return nil, journalFailure(err)
	}
	read.Row = journal.ShownDay(read.Row, options)
	weekStart, err := journalWeekStartOf(tx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	firstOfWeek, _, err := journal.PeriodContaining(models.JournalPeriodWeek, read.Day, weekStart)
	if err != nil {
		return nil, err
	}
	weekday, err := journal.Weekday(read.Day)
	if err != nil {
		return nil, err
	}
	resolver, err := newJournalRecordResolver(ctx, tx, principal)
	if err != nil {
		return nil, err
	}

	row := journalDayRow(read)
	choices, pins := resolver.describeLinks(read.Links)
	narrative := describeJournalDayNarrative(row, today)
	narrative.References = resolver.references(row.Narrative)
	// A narrative is stale when what the day holds now is not what it was
	// written from. One written before the digest was kept, or not at all, is
	// not called stale: there is nothing to compare, or nothing to rewrite.
	narrative.IsStale = row.Narrative != "" && row.NarrativeInputDigest != "" && row.NarrativeInputDigest != journal.InputDigest(read)
	narrative.SourceCount = read.RecordCount()

	words := describeJournalDayWords(row)
	for _, note := range read.JournalNotes {
		words.JournalNotes = append(words.JournalNotes, &JournalNoteView{DocumentID: note.DocumentID, Title: note.Title, Text: note.Text})
	}

	view := &JournalDayView{
		Day: read.Day, TimeZone: read.TimeZone, IsToday: read.Day == today, Weekday: weekday, WeekStart: firstOfWeek,
		Words: words, Mood: describeJournalMood(row), Weather: describeJournalWeather(row.Weather),
		Facts: describeJournalFacts(journal.ComputeFacts(read)), Narrative: narrative,
		Choices: choices, Pins: pins, CoverPhotoID: row.CoverPhotoID,
		UnavailableSections: append([]string{}, read.UnavailableSections...),
	}
	describeJournalSections(view, read, chosenReasons(read.Links))
	return view, nil
}

// describeJournalSections puts the sections of a day read from the sources
// into the view, each a list and never null.
func describeJournalSections(view *JournalDayView, read *journal.Day, reasons map[string]string) {
	view.WorkProjects = []*JournalWorkProjectView{}
	for _, project := range read.WorkProjects {
		shipped := make([]*JournalShippedView, 0, len(project.Shipped))
		for _, request := range project.Shipped {
			shipped = append(shipped, &JournalShippedView{
				DocumentID: request.DocumentID, Title: request.Title, Address: request.Address, HappenedAt: request.HappenedAt, IsDraft: request.IsDraft,
			})
		}
		topics := project.Topics
		if topics == nil {
			topics = []string{}
		}
		view.WorkProjects = append(view.WorkProjects, &JournalWorkProjectView{
			Name: project.Name, FirstAt: project.FirstAt, LastAt: project.LastAt, ActiveMinutes: project.ActiveMinutes,
			StretchCount: project.StretchCount, Topics: topics, Shipped: shipped, DocumentID: project.DocumentID,
			Summary: reasons["agent_document:"+project.DocumentID],
		})
	}
	view.CodingSessions = []*JournalCodingSessionView{}
	for _, session := range read.CodingSessions {
		endedAt := session.EndedAt
		view.CodingSessions = append(view.CodingSessions, &JournalCodingSessionView{
			DocumentID: session.DocumentID, Title: session.Title, Directory: session.Directory, Assistant: session.Assistant,
			StartedAt: session.StartedAt, EndedAt: &endedAt, StretchCount: session.StretchCount, LinkReason: reasons["agent_document:"+session.DocumentID],
		})
	}
	view.Documents, view.DocumentTotal = []*JournalDocumentView{}, read.DocumentTotal
	for _, document := range read.Documents {
		reason, isChosen := reasons["agent_document:"+document.DocumentID]
		view.Documents = append(view.Documents, &JournalDocumentView{
			DocumentID: document.DocumentID, Title: document.Title, SourceName: document.SourceName, HappenedAt: document.HappenedAt,
			IsChosen: isChosen, LinkReason: reason,
		})
	}
	view.Conversations = []*JournalConversationView{}
	for _, conversation := range read.Conversations {
		view.Conversations = append(view.Conversations, &JournalConversationView{
			ConversationID: conversation.ConversationID, Title: conversation.Title, IsMain: conversation.IsMain, FirstAt: conversation.FirstAt,
			LastAt: conversation.LastAt, MessageCount: conversation.MessageCount,
		})
	}
	view.Chats = []*JournalChatView{}
	for _, chat := range read.Chats {
		view.Chats = append(view.Chats, &JournalChatView{
			ChatID: chat.ChatID, ChatName: chat.ChatName, FirstAt: chat.FirstAt, LastAt: chat.LastAt, MessageCount: chat.MessageCount,
		})
	}
	view.Mail, view.MailTotal = []*JournalMailView{}, read.MailReceivedTotal+read.MailSentTotal
	for _, mail := range read.Mail {
		reason, isChosen := reasons["mail:"+mail.ItemID]
		view.Mail = append(view.Mail, &JournalMailView{
			ItemID: mail.ItemID, FromName: mail.Counterpart, Subject: mail.Subject, ReceivedAt: mail.ReceivedAt, IsSent: mail.IsSent,
			IsImportant: mail.IsImportant, IsChosen: isChosen, LinkReason: reason, RepeatCount: mail.RepeatCount,
		})
	}
	view.Recordings = []*JournalRecordingView{}
	for _, recording := range read.Recordings {
		recordedAt := recording.RecordedAt
		view.Recordings = append(view.Recordings, &JournalRecordingView{
			RecordingID: recording.RecordingID, Title: recording.Title, RecordedAt: &recordedAt, DurationMS: recording.DurationMS, Summary: recording.Summary,
		})
	}
	view.Photos, view.PhotoTotal = []*JournalPhotoView{}, read.PhotoTotal
	for _, photo := range read.Photos {
		view.Photos = append(view.Photos, &JournalPhotoView{PhotoID: photo.PhotoID, TakenAt: photo.TakenAt, PlaceName: photo.PlaceName, IsVideo: photo.IsVideo})
	}
	view.Visits = []*JournalVisitView{}
	for _, visit := range read.Visits {
		view.Visits = append(view.Visits, &JournalVisitView{
			VisitID: visit.VisitID, PlaceID: visit.PlaceID, PlaceName: visit.PlaceName, ArrivedAt: visit.ArrivedAt, DepartedAt: visit.DepartedAt,
			Latitude: visit.Latitude, Longitude: visit.Longitude,
		})
	}
	view.Events = []*JournalEventView{}
	for _, event := range read.Events {
		view.Events = append(view.Events, &JournalEventView{
			EventID: event.EventID, Title: event.Title, CalendarName: event.CalendarName, StartsAt: event.StartsAt, EndsAt: event.EndsAt,
			IsAllDay: event.IsAllDay, Location: event.Location, Reference: string(models.RecordCalendarOccurrence) + ":" + recordlink.FormatOccurrenceReference(event.EventID, event.StartsAt),
		})
	}
	view.Reminders = []*JournalReminderView{}
	for _, reminder := range read.Reminders {
		view.Reminders = append(view.Reminders, &JournalReminderView{ReminderID: reminder.ReminderID, Title: reminder.Title, DueAt: reminder.DueAt, DoneAt: reminder.DoneAt})
	}
	view.Transactions = []*JournalTransactionView{}
	for _, transaction := range read.Transactions {
		view.Transactions = append(view.Transactions, &JournalTransactionView{
			TransactionID: transaction.TransactionID, PostedOn: transaction.PostedOn, TransactedAt: transaction.TransactedAt,
			Description: transaction.Description, AccountName: transaction.AccountName,
			AmountMinorUnits: transaction.AmountMinorUnits, Currency: transaction.Currency,
		})
	}
	view.Health = describeJournalHealth(read.Health)
	// Shopping trips wait for the shopping lists to be here.
	view.ShoppingTrips = []*JournalShoppingTripView{}
	view.Notes = []*JournalRecordView{}
	for _, note := range read.Notes {
		modifiedAt := note.ModifiedAt
		view.Notes = append(view.Notes, &JournalRecordView{
			RecordKind: string(models.RecordNote), RecordID: note.NoteID, Reference: string(models.RecordNote) + ":" + note.NoteID,
			Title: note.Title, HappenedAt: &modifiedAt, DashboardPath: recordlink.NotePath(note.FolderID, note.ItemID),
		})
	}
	view.MemoryPages = []*JournalMemoryPageView{}
	for _, page := range read.MemoryPages {
		view.MemoryPages = append(view.MemoryPages, &JournalMemoryPageView{NodeID: page.NodeID, Path: page.Path, Title: page.Title, FactCount: page.FactCount})
	}
	view.Moments = []*JournalMomentView{}
	for _, moment := range journal.Moments(read) {
		view.Moments = append(view.Moments, &JournalMomentView{
			At: moment.At, RecordKind: string(moment.RecordKind), RecordID: moment.RecordID, Title: moment.Title,
			Detail: moment.Detail, DashboardPath: moment.DashboardPath,
		})
	}
}

// describeJournalHealth is a day's Health, nil for none.
func describeJournalHealth(health *journal.Health) *JournalHealthView {
	if health == nil {
		return nil
	}
	view := &JournalHealthView{
		StepCount: health.StepCount, StepsByHour: append([]int{}, health.StepsByHour...), SleepMinutes: health.SleepMinutes,
		SleepStages: []*JournalSleepStageView{}, RestingHeartRate: health.RestingHeartRate, Workouts: []*JournalWorkoutView{},
	}
	for _, stage := range health.SleepStages {
		view.SleepStages = append(view.SleepStages, &JournalSleepStageView{SleepStage: stage.SleepStage, Minutes: stage.Minutes})
	}
	for _, workout := range health.Workouts {
		view.Workouts = append(view.Workouts, &JournalWorkoutView{
			SampleID: workout.SampleID, ActivityType: workout.ActivityType, StartedAt: workout.StartedAt, Minutes: workout.Minutes, Meters: workout.Meters,
		})
	}
	return view
}

func (self *graph) ReadJournalPeriod(ctx context.Context, arguments ReadJournalPeriodArguments) (*JournalPeriodView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	periodKind := models.JournalPeriodKind(arguments.PeriodKind)
	if !periodKind.IsValid() {
		return nil, fmt.Errorf("%w: periodKind is week, month or year", api.ErrInvalidArguments)
	}
	if _, err := journal.ParseDay(arguments.Day); err != nil {
		return nil, fmt.Errorf("%w: day: %s", api.ErrInvalidArguments, err)
	}
	now, tx := time.Now(), self.transaction(ctx)
	weekStart, err := journalWeekStartOf(tx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	options, err := self.journalReadOptions(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	period, err := journal.ReadPeriod(tx, principal.User, periodKind, arguments.Day, weekStart, now, options)
	if err != nil {
		return nil, journalFailure(err)
	}
	// A period that has not begun has nothing in it and is not written ahead.
	if period.Start > period.Today {
		return nil, api.ErrNotFound
	}
	resolver, err := newJournalRecordResolver(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	return describeJournalPeriod(period, resolver), nil
}

// describeJournalPeriod is a period read as the view tells it.
func describeJournalPeriod(period *journal.Period, resolver *journalRecordResolver) *JournalPeriodView {
	row := period.Row
	narrative := describeJournalPeriodNarrative(row)
	if row != nil {
		narrative.References = resolver.references(row.Narrative)
	}
	choices, pins := resolver.describeLinks(period.Links)
	view := &JournalPeriodView{
		PeriodKind: string(period.Kind), PeriodStart: period.Start, PeriodEnd: period.End,
		IsCurrent:   period.Start <= period.Today && period.Today <= period.End,
		LookingBack: describeJournalPeriodWords(row), Narrative: narrative, Choices: append(choices, pins...),
		Days: []*JournalDaySummaryView{}, Months: []*JournalMonthSummaryView{},
		Totals: describeJournalFacts(period.Totals), MoodCounts: period.MoodCounts[:], DayCount: period.DayCount,
		Chapters: []*JournalChapterView{},
	}
	if row != nil {
		for _, chapter := range row.Chapters {
			view.Chapters = append(view.Chapters, &JournalChapterView{FromDay: chapter.FromDay, UntilDay: chapter.UntilDay, Title: chapter.Title, Detail: chapter.Detail})
		}
	}
	// A year is told by its months, and leaves the days out.
	if period.Kind != models.JournalPeriodYear {
		for _, day := range period.Days {
			view.Days = append(view.Days, describeJournalDaySummary(day, period.Today))
		}
	}
	for _, month := range period.Months {
		view.Months = append(view.Months, &JournalMonthSummaryView{
			PeriodStart: month.Start, Headline: month.Headline, DayMoods: month.DayMoods, PlaceCount: month.PlaceCount, PhotoCount: month.PhotoCount,
		})
	}
	return view
}
