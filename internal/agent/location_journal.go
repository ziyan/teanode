package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// The location journal: for every person who has reported where they are,
// one document for each day that has ended in their own time zone, listing
// where they were and for how long. The dream's reading takes these like any
// other documents and files what they show about the person's routine.
//
// The text is written here from the recorded visits and no model is called,
// so a day can be written again whenever its visits change at no cost. The
// documents are put in directly (db.PutLocationDayDocument); the location
// source has no schedule and nothing reads it from anywhere.

// locationDayExternalIdPrefix begins the external ID of every day document
// (location:2026-03-04), so that anything reading documents can tell them
// from the rest.
const locationDayExternalIdPrefix = "location:"

const (
	// locationJournalEvery is how often the worker looks for days to write.
	// A day ends once, so an hour is soon enough.
	locationJournalEvery = time.Hour

	// locationJournalDays is how many days back every run looks. A departure
	// can be reported a day or two late, when the phone had no signal, and an
	// erase, a forgotten place or a new name changes the words of older days.
	locationJournalDays = 14

	// locationJournalVisitsMost is how many visits one day lists. A day with
	// more is a position flickering between two places, and the rest add
	// nothing to what the reading can learn.
	locationJournalVisitsMost = 60
)

// locationCategoryWords says in plain words what an OpenStreetMap category
// ("key=value") is, for the common ones. A category not here is left out of
// the text rather than shown as a tag nobody would say.
var locationCategoryWords = map[string]string{
	"leisure=park": "park", "leisure=playground": "playground", "leisure=garden": "garden",
	"leisure=fitness_centre": "gym", "leisure=sports_centre": "sports center",
	"leisure=swimming_pool": "swimming pool", "leisure=pitch": "sports field",
	"leisure=dog_park": "dog park", "leisure=nature_reserve": "nature reserve",
	"amenity=restaurant": "restaurant", "amenity=cafe": "cafe", "amenity=fast_food": "fast food",
	"amenity=bar": "bar", "amenity=pub": "pub", "amenity=library": "library",
	"amenity=school": "school", "amenity=kindergarten": "kindergarten",
	"amenity=university": "university", "amenity=college": "college",
	"amenity=hospital": "hospital", "amenity=clinic": "clinic", "amenity=doctors": "doctor",
	"amenity=dentist": "dentist", "amenity=pharmacy": "pharmacy", "amenity=bank": "bank",
	"amenity=place_of_worship": "place of worship", "amenity=cinema": "cinema",
	"amenity=theatre": "theater", "amenity=marketplace": "market", "amenity=parking": "parking",
	"amenity=fuel": "gas station", "amenity=post_office": "post office",
	"shop=supermarket": "supermarket", "shop=convenience": "convenience store",
	"shop=bakery": "bakery", "shop=mall": "shopping mall", "shop=clothes": "clothes shop",
	"shop=books": "bookshop", "shop=hairdresser": "hairdresser",
	"tourism=museum": "museum", "tourism=hotel": "hotel", "tourism=attraction": "attraction",
	"tourism=zoo": "zoo", "tourism=gallery": "gallery",
	"railway=station": "train station", "aeroway=aerodrome": "airport", "highway=bus_stop": "bus stop",
	"natural=beach": "beach", "natural=peak": "peak",
	"building=house": "house", "building=apartments": "apartment building",
	"building=residential": "residential building", "place=house": "house",
	"building=school": "school", "building=hospital": "hospital",
}

// locationCategoryWordsOf is the plain words for a category, or empty.
func locationCategoryWordsOf(osmCategory string) string {
	return locationCategoryWords[osmCategory]
}

// locationDurationText is a length of time as "1 h 50 min", "2 h" or
// "45 min", rounded down to the minute.
func locationDurationText(duration time.Duration) string {
	totalMinutes := int(duration / time.Minute)
	hours, minutes := totalMinutes/60, totalMinutes%60
	switch {
	case hours == 0:
		return fmt.Sprintf("%d min", minutes)
	case minutes == 0:
		return fmt.Sprintf("%d h", hours)
	}
	return fmt.Sprintf("%d h %d min", hours, minutes)
}

// placeTextRunesMost is how long a place's name or address may be when it is
// put in front of the model.
const placeTextRunesMost = 80

// placeTextForModel is a place's name or address made safe to put in text the
// model reads. The text comes from OpenStreetMap, where anybody writes a name,
// or from the person, and it ends up in the middle of a line the model reads
// as the person's own record: line breaks and other control characters would
// forge a line, angle brackets would close a tag the prompt reads, and length
// would crowd out the rest. Whitespace runs become one space, control and
// invisible formatting characters and angle brackets are dropped, and the text
// is cut to placeTextRunesMost.
func placeTextForModel(text string) string {
	cleaned := strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.IsControl(character), unicode.Is(unicode.Cf, character), character == '<', character == '>':
			return -1
		}
		return character
	}, text)
	return strings.TrimSpace(cutRunes(strings.Join(strings.Fields(cleaned), " "), placeTextRunesMost))
}

// locationPresence is what the person's phone has reported, for deciding how
// long a visit with no departure lasted.
type locationPresence struct {
	// latestPoint is the latest position, or nil when there is none.
	latestPoint *models.LocationPoint
	// latestReportAt is the latest of a position's measured time and a
	// visit's arrival or departure, or the zero time when there is none.
	latestReportAt time.Time
	// pauses are the times the person stopped their location being recorded
	// that touch the days being written, in any order. Nothing was reported
	// in them on purpose, so a day says that where they were is not known.
	pauses []*models.LocationPause
	now    time.Time
}

// openVisitSilenceMost is how long after the person's last report of any
// kind a visit with no departure is still taken to be going on. The app sends
// no positions while the person stays put (only visits, and moves of about
// 500 m), so a long quiet is normal at home, but a phone silent for days is
// not taken as a person still there.
const openVisitSilenceMost = 72 * time.Hour

// openVisitEnds says when each visit with an arrival and no departure was
// over. A phone that never delivered a departure would otherwise leave the
// person at the first place for ever, in the day documents and in the
// digest's hours, next to the places they really went on to, so it ends at
// the first of these:
//   - the moment the next visit began;
//   - the time of the latest position, when that was measured after the
//     arrival and is outside the place (place.IsStillThere);
//   - now, but at most openVisitSilenceMost after the latest report of any
//     kind, and never before the arrival.
//
// The key is the visit's ID.
func openVisitEnds(visits []*models.PlaceVisit, presence locationPresence) map[string]time.Time {
	starts := make([]time.Time, 0, len(visits))
	for _, visit := range visits {
		switch {
		case visit.ArrivedAt != nil:
			starts = append(starts, *visit.ArrivedAt)
		case visit.DepartedAt != nil:
			starts = append(starts, *visit.DepartedAt)
		}
	}
	sort.Slice(starts, func(left, right int) bool { return starts[left].Before(starts[right]) })
	ends := map[string]time.Time{}
	for _, visit := range visits {
		if visit.DepartedAt != nil || visit.ArrivedAt == nil {
			continue
		}
		next := sort.Search(len(starts), func(index int) bool { return starts[index].After(*visit.ArrivedAt) })
		if next < len(starts) {
			ends[visit.ID] = starts[next]
			continue
		}
		if presence.latestPoint != nil && presence.latestPoint.MeasuredAt.After(*visit.ArrivedAt) && !place.IsStillThere(visit, presence.latestPoint) {
			ends[visit.ID] = presence.latestPoint.MeasuredAt
			continue
		}
		endedAt := presence.now
		if silentSince := presence.latestReportAt.Add(openVisitSilenceMost); silentSince.Before(endedAt) {
			endedAt = silentSince
		}
		if endedAt.Before(*visit.ArrivedAt) {
			endedAt = *visit.ArrivedAt
		}
		ends[visit.ID] = endedAt
	}
	return ends
}

// locationVisitSpan is when a visit began and ended within one day, cut to the
// day. A visit with no departure lasts until a later visit began, or until the
// latest position the person reported (openVisitEnds).
// A visit with no arrival (iOS reported only the departure) is a moment.
type locationVisitSpan struct {
	visit               *models.PlaceVisit
	start, end          time.Time
	doesBeginEarlier    bool
	doesContinueNextDay bool
	isArrivalUnreported bool
}

// locationVisitSpans cuts the visits to the day [dayStart, dayEnd), in order
// of when they began, leaving out those that do not touch it.
func locationVisitSpans(visits []*models.PlaceVisit, dayStart, dayEnd time.Time, presence locationPresence) []locationVisitSpan {
	spans := []locationVisitSpan{}
	laterVisitStarts := openVisitEnds(visits, presence)
	for _, visit := range visits {
		span := locationVisitSpan{visit: visit}
		switch {
		case visit.ArrivedAt != nil:
			span.start = *visit.ArrivedAt
		case visit.DepartedAt != nil:
			span.start = *visit.DepartedAt
			span.isArrivalUnreported = true
		default:
			continue
		}
		laterVisitStart, isFollowed := laterVisitStarts[visit.ID]
		switch {
		case visit.DepartedAt != nil:
			span.end = *visit.DepartedAt
		case isFollowed:
			span.end = laterVisitStart
		default:
			span.end = dayEnd
		}
		if span.isArrivalUnreported {
			span.end = span.start
		}
		// A departure with no arrival is a moment, which is in the day when it
		// is at or after the day's start.
		isInDay := span.start.Before(dayEnd) && (span.end.After(dayStart) || (span.isArrivalUnreported && !span.start.Before(dayStart)))
		if !isInDay {
			continue
		}
		if span.start.Before(dayStart) {
			span.start, span.doesBeginEarlier = dayStart, true
		}
		if span.end.After(dayEnd) {
			span.end, span.doesContinueNextDay = dayEnd, true
		}
		spans = append(spans, span)
	}
	sort.SliceStable(spans, func(left, right int) bool { return spans[left].start.Before(spans[right].start) })
	return spans
}

// locationClock is a time of day as 09:40, with the end of the day as 24:00.
func locationClock(moment, dayEnd time.Time) string {
	if moment.Equal(dayEnd) {
		return "24:00"
	}
	return moment.Format("15:04")
}

// locationDayText is the document for one day: the places they were, in
// order, and, when they have a place labeled home, how long they were at
// places other than home. dayStart is the first moment of the day in the
// person's zone. It is empty when no visit and no pause touches the day. A day
// with pauses and no visits still has its pause lines: where the person was is
// not known, which is not the same as nothing happening.
func locationDayText(visits []*models.PlaceVisit, dayStart time.Time, presence locationPresence, hasHomePlace bool) string {
	dayEnd := dayStart.AddDate(0, 0, 1)
	spans := locationVisitSpans(visits, dayStart, dayEnd, presence)
	var pauseEntries []locationEntry
	for _, pause := range presence.pauses {
		if entry, isInDay := locationPauseEntry(pause, dayStart, dayEnd); isInDay {
			pauseEntries = append(pauseEntries, entry)
		}
	}
	if len(spans) == 0 && len(pauseEntries) == 0 {
		return ""
	}
	placeIds := map[string]bool{}
	var awayFromHome time.Duration
	for _, span := range spans {
		placeIds[span.visit.PlaceID] = true
		if place := span.visit.Place; place == nil || place.PlaceLabel != models.PlaceLabelHome {
			awayFromHome += span.end.Sub(span.start)
		}
	}
	// The totals above are over every visit of the day; only the list is cut.
	// A pause is a line of its own among the visits, and never a stay.
	entries := make([]locationEntry, 0, len(spans))
	moreText := ""
	for index, span := range spans {
		if index == locationJournalVisitsMost {
			moreText = fmt.Sprintf("and %d more", len(spans)-index)
			break
		}
		entries = append(entries, locationEntry{start: span.start, text: locationVisitLine(span, dayEnd)})
	}
	entries = append(entries, pauseEntries...)
	sort.SliceStable(entries, func(left, right int) bool { return entries[left].start.Before(entries[right].start) })
	lines := make([]string, 0, len(entries)+1)
	for _, entry := range entries {
		lines = append(lines, entry.text)
	}
	if moreText != "" {
		lines = append(lines, moreText)
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Places: %d", len(placeIds))
	if hasHomePlace && len(spans) > 0 {
		fmt.Fprintf(&builder, ", time away from home: %s", locationDurationText(awayFromHome))
	}
	// The lines carry names that strangers wrote, so they are fenced as other
	// record text is.
	builder.WriteString("\n" + fenced(strings.Join(lines, "\n")))
	return builder.String()
}

// locationEntry is one line of a day and when it began, for putting visits
// and pauses in order.
type locationEntry struct {
	start time.Time
	text  string
}

// locationPauseEntry is the line a pause makes in the day [dayStart, dayEnd),
// "paused from 14:00 to 15:00; where they were is not known", and whether the
// pause touches the day at all. It says plainly that the time is not known,
// so that it is not read as a stay anywhere, or as nowhere.
func locationPauseEntry(pause *models.LocationPause, dayStart, dayEnd time.Time) (locationEntry, bool) {
	if !pause.StartedAt.Before(dayEnd) || (pause.EndedAt != nil && !pause.EndedAt.After(dayStart)) {
		return locationEntry{}, false
	}
	doesBeginEarlier := pause.StartedAt.Before(dayStart)
	doesContinueNextDay := pause.EndedAt == nil || pause.EndedAt.After(dayEnd)
	// The clock times are the person's, whatever zone the pause was read in.
	zone := dayStart.Location()
	start := pause.StartedAt.In(zone)
	if doesBeginEarlier {
		start = dayStart
	}
	var endedAt time.Time
	if pause.EndedAt != nil {
		endedAt = pause.EndedAt.In(zone)
	}
	var words string
	switch {
	case doesBeginEarlier && doesContinueNextDay:
		words = "paused the whole day"
	case doesBeginEarlier:
		words = "paused from the day before to " + locationClock(endedAt, dayEnd)
	case doesContinueNextDay:
		words = "paused from " + start.Format("15:04") + " until after midnight"
	default:
		words = "paused from " + start.Format("15:04") + " to " + locationClock(endedAt, dayEnd)
	}
	return locationEntry{start: start, text: words + "; where they were is not known"}, true
}

// locationVisitLine is one visit: "09:40 to 11:30 (1 h 50 min): Riverside Park
// (park), labeled gym". The label is the person's word for the place, so it
// is said even when the place has a name of its own.
func locationVisitLine(span locationVisitSpan, dayEnd time.Time) string {
	var line string
	switch {
	case span.isArrivalUnreported:
		line = span.start.Format("15:04") + " (left)"
	default:
		line = fmt.Sprintf("%s to %s (%s)", locationClock(span.start, dayEnd), locationClock(span.end, dayEnd), locationDurationText(span.end.Sub(span.start)))
	}
	name := "an unnamed place"
	place := span.visit.Place
	if place != nil && placeTextForModel(place.PlaceName) != "" {
		name = placeTextForModel(place.PlaceName)
	}
	line += ": " + name
	if place != nil {
		if words := locationCategoryWordsOf(place.OSMCategory); words != "" {
			line += " (" + words + ")"
		}
		if place.PlaceLabel != "" {
			line += ", labeled " + string(place.PlaceLabel)
		}
	}
	switch {
	case span.doesBeginEarlier && span.doesContinueNextDay:
		line += ", there the whole day"
	case span.doesBeginEarlier:
		line += ", there since the day before"
	case span.doesContinueNextDay:
		line += ", still there at midnight"
	}
	return line
}

// writeLocationJournal files the days that have ended since the last one
// written, for one agent, and answers how many it wrote or rewrote. now picks
// the end of the last day: the start of today in the owner's zone.
// readLocationPresence reads the person's latest position and latest report
// of any kind, in the two queries openVisitEnds needs.
func readLocationPresence(tx db.Transaction, userId string, now time.Time) (locationPresence, error) {
	presence := locationPresence{now: now}
	var err error
	if presence.latestPoint, err = tx.GetLatestLocationPoint(userId); err != nil {
		return presence, err
	}
	presence.latestReportAt, err = tx.GetLatestLocationReportAt(userId)
	return presence, err
}

func (self *Agent) writeLocationJournal(ctx context.Context, agent *models.Agent, owner *models.User, now time.Time) (int, error) {
	zone := Location(owner)
	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	writtenCount := 0
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		// A person who has no visit has nothing to write about, whether or not
		// a phone reported positions: a person who only checks in from the
		// dashboard has visits and no positions.
		anyVisits, _, err := tx.ListPlaceVisits(owner.ID, db.PlaceVisitFilter{LimitCount: 1})
		if err != nil || len(anyVisits) == 0 {
			return err
		}
		source, err := tx.EnsureLocationSource(agent.ID)
		if err != nil {
			return err
		}
		if !source.Enabled {
			return nil
		}
		// Every run looks at all of the days it covers, not only the newest:
		// an erase of part of a day or the forgetting of a place deletes the
		// day's document, and a rename or a label changes what it says, and
		// each of those has to be written again from what is left. A day whose
		// text is unchanged is not written.
		from := today.AddDate(0, 0, -locationJournalDays)
		var visits []*models.PlaceVisit
		for offset := 0; ; {
			page, nextOffset, err := tx.ListPlaceVisits(owner.ID, db.PlaceVisitFilter{From: &from, Until: &today, Offset: offset, LimitCount: 200})
			if err != nil {
				return err
			}
			visits = append(visits, page...)
			if nextOffset == 0 {
				break
			}
			offset = nextOffset
		}
		homes, _, err := tx.ListPlaces(owner.ID, db.PlaceFilter{PlaceLabel: models.PlaceLabelHome, LimitCount: 1})
		if err != nil {
			return err
		}
		presence, err := readLocationPresence(tx, owner.ID, now)
		if err != nil {
			return err
		}
		if presence.pauses, err = tx.ListLocationPauses(owner.ID, from, today, 0); err != nil {
			return err
		}
		for dayStart := from; dayStart.Before(today); dayStart = dayStart.AddDate(0, 0, 1) {
			text := locationDayText(visits, dayStart, presence, len(homes) > 0)
			if text == "" {
				// A day filed before its visits were erased or its places
				// forgotten has nothing left to say.
				if err := tx.DeleteLocationDayDocument(source.ID, dayStart); err != nil {
					return err
				}
				continue
			}
			isChanged, err := tx.PutLocationDayDocument(agent.ID, source.ID, dayStart, text)
			if err != nil {
				return err
			}
			if isChanged {
				writtenCount++
			}
		}
		if writtenCount == 0 {
			return nil
		}
		documentCount, chunkCount, err := tx.CountAgentSourceDocuments(source.ID)
		if err != nil {
			return err
		}
		return tx.MarkAgentSourceRun(source.ID, source.Cursor, db.SourceCounts{Documents: documentCount, Chunks: chunkCount}, false, "", nil)
	})
	return writtenCount, err
}

// queueLocationJournals writes the location journal of every active agent
// whose person has reported a position. It makes no model call and takes the
// time of a few queries for each person, so it runs in the worker's tick like
// the follow-up backfill, at most once an hour.
func (self *Agent) queueLocationJournals(ctx context.Context, now time.Time) {
	if now.Sub(self.lastLocationJournal) < locationJournalEvery {
		return
	}
	self.lastLocationJournal = now
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to write location journals for: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Active() {
			continue
		}
		owner := self.ownerOf(ctx, agent)
		if owner == nil {
			continue
		}
		// Once an hour for the whole server: every instance looks at the same
		// days and would read the same visits and write the same texts.
		var isFree bool
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			isFree, err = takeSweep(tx, "location-journal:"+agent.ID, locationJournalEvery, now)
			return err
		}); err != nil {
			log.Warningf("cannot say whether another instance writes the location journal for agent %q: %s", agent.ID, err)
			continue
		}
		if !isFree {
			continue
		}
		writtenCount, err := self.writeLocationJournal(ctx, agent, owner, now)
		if err != nil {
			log.Warningf("cannot write the location journal for agent %q: %s", agent.ID, err)
			continue
		}
		if writtenCount > 0 {
			log.Infof("wrote %d days of location journal for agent %q", writtenCount, agent.ID)
		}
	}
}
