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

	// locationJournalFirstDays is how many days back the first run goes.
	locationJournalFirstDays = 14

	// locationJournalRewriteDays is how many days before the newest written
	// day a run looks again. A departure can be reported a day or two late,
	// when the phone had no signal, and that changes the day's words.
	locationJournalRewriteDays = 3

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

// openVisitEnds says when each visit with an arrival and no departure was
// over, for the visits that a later visit followed: the moment the next visit
// began. A phone that never delivered a departure would otherwise leave the
// person at the first place for ever, in the day documents and in the digest's
// hours, next to the places they really went on to. The visit that is last is
// not in the answer: it may be going on. The key is the visit's ID.
func openVisitEnds(visits []*models.PlaceVisit) map[string]time.Time {
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
		}
	}
	return ends
}

// locationVisitSpan is when a visit began and ended within one day, cut to the
// day. A visit with no departure is taken to last to the end of the day: a
// finished day ended with the person still there, as far as anyone reported,
// unless a later visit began, which ended it.
// A visit with no arrival (iOS reported only the departure) is a moment.
type locationVisitSpan struct {
	visit               *models.PlaceVisit
	start, end          time.Time
	beganEarlier        bool
	continuesNextDay    bool
	isArrivalUnreported bool
}

// locationVisitSpans cuts the visits to the day [dayStart, dayEnd), in order
// of when they began, leaving out those that do not touch it.
func locationVisitSpans(visits []*models.PlaceVisit, dayStart, dayEnd time.Time) []locationVisitSpan {
	spans := []locationVisitSpan{}
	laterVisitStarts := openVisitEnds(visits)
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
			span.start, span.beganEarlier = dayStart, true
		}
		if span.end.After(dayEnd) || (visit.DepartedAt == nil && !isFollowed) {
			span.end, span.continuesNextDay = dayEnd, true
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
// person's zone. It is empty when no visit touches the day.
func locationDayText(visits []*models.PlaceVisit, dayStart time.Time, hasHomePlace bool) string {
	dayEnd := dayStart.AddDate(0, 0, 1)
	spans := locationVisitSpans(visits, dayStart, dayEnd)
	if len(spans) == 0 {
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
	var lines []string
	for index, span := range spans {
		if index == locationJournalVisitsMost {
			lines = append(lines, fmt.Sprintf("and %d more", len(spans)-index))
			break
		}
		lines = append(lines, locationVisitLine(span, dayEnd))
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Places: %d", len(placeIds))
	if hasHomePlace {
		fmt.Fprintf(&builder, ", time away from home: %s", locationDurationText(awayFromHome))
	}
	// The lines carry names that strangers wrote, so they are fenced as other
	// record text is.
	builder.WriteString("\n" + fenced(strings.Join(lines, "\n")))
	return builder.String()
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
	case span.beganEarlier && span.continuesNextDay:
		line += ", there the whole day"
	case span.beganEarlier:
		line += ", there since the day before"
	case span.continuesNextDay:
		line += ", still there at midnight"
	}
	return line
}

// writeLocationJournal files the days that have ended since the last one
// written, for one agent, and answers how many it wrote or rewrote. now picks
// the end of the last day: the start of today in the owner's zone.
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
		from := today.AddDate(0, 0, -locationJournalFirstDays)
		latest, err := tx.GetLatestLocationDayStart(source.ID)
		if err != nil {
			return err
		}
		if latest != nil {
			latestLocal := latest.In(zone)
			rewriteFrom := time.Date(latestLocal.Year(), latestLocal.Month(), latestLocal.Day()-locationJournalRewriteDays, 0, 0, 0, 0, zone)
			if rewriteFrom.After(from) {
				from = rewriteFrom
			}
		}
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
		for dayStart := from; dayStart.Before(today); dayStart = dayStart.AddDate(0, 0, 1) {
			text := locationDayText(visits, dayStart, len(homes) > 0)
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
