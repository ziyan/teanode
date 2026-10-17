package agent

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// The look around: once a week, on the day and at the time the person set
// (Thursday at 17:00 by default, early enough to plan the weekend by), a run
// with nobody present searches the web for what is on in the person's town
// from tomorrow through the week after the coming weekend (events, places
// worth knowing, weekend activities that suit the household, dates that
// matter) and writes what it finds as follow-ups of kind event or deadline,
// with the page each came from. It then tells the weekend's, with any the
// mail brought, in one message; the Sunday roundup (follow_up_sweep.go)
// tells the rest of the week's.
//
// The town is the one of the person's place labeled home, when they have one,
// and otherwise read from memory. When neither says, the run searches for
// nothing and asks once, as an idea the agent offers unasked.

const (
	// nearbyCatchUpUntilHour is the hour, on the day after the run's day,
	// until which a run missed while the server was down is still queued.
	nearbyCatchUpUntilHour = 12

	// nearbyQueueEvery is how often the worker looks for a run that is due.
	nearbyQueueEvery = 10 * time.Minute

	// nearbySearchMost and nearbyPageReadMost are how many times the run
	// may call web_search and web_fetch. The loop refuses a call past them
	// (AskSettings.CallLimits), so the cost of a week's look is bounded
	// whatever the model decides.
	nearbySearchMost   = 12
	nearbyPageReadMost = 10

	// nearbyRounds is the most rounds of the loop the run may take: a round
	// that calls nothing but one tool is the slowest way to spend the
	// calls, so every call could have a round of its own, and one more
	// round answers.
	nearbyRounds = nearbySearchMost + nearbyPageReadMost + 2

	// nearbyItemsMost is how many items one week's run writes.
	nearbyItemsMost = 8

	// nearbyToldWithin is how long an item already told keeps the same one
	// from being written again.
	nearbyToldWithin = 4 * 7 * 24 * time.Hour

	// nearbyHorizonDays is how far ahead an item's day may be. A tax
	// deadline months off is worth keeping; a year beyond that is a page
	// that has not been updated.
	nearbyHorizonDays = 370

	// nearbyHouseholdMost is how many lines of the household reach the
	// search run, each cut to nearbyHouseholdCharacters.
	nearbyHouseholdMost       = 6
	nearbyHouseholdCharacters = 100

	// nearbyMemoryFacts and nearbyMemoryPages bound what is read of memory
	// to find the town: the self page's facts, and this many place pages
	// with a few facts each.
	nearbyMemoryFacts = 60
	nearbyMemoryPages = 12
	nearbyPageFacts   = 5

	// nearbyKeyPrefix begins the key of everything this run writes, after
	// the kind, so that it can never be the key of what mail said.
	nearbyKeyPrefix = "nearby:"

	// nearbyHomeAreaIdeaKey is the idea that asks which town to look
	// around. One row per agent, whatever became of it, is what makes the
	// question asked once.
	nearbyHomeAreaIdeaKey = "nearby_home_area"
)

// nearbyTools are the only tools the run may reach.
var nearbyTools = map[string]bool{"web_search": true, "web_fetch": true}

// nearbyCallLimits are the run's limits on those tools.
var nearbyCallLimits = map[string]int{"web_search": nearbySearchMost, "web_fetch": nearbyPageReadMost}

// nearbySlot names the run that is due at now in the person's zone, or is
// empty when none is: the person's weekday from their time (Thursday at
// 17:00 unless they said otherwise), and the day after until noon, for a run
// missed while the server was down. The name is the date and time of the
// run, such as "2026-10-15T17:00".
func nearbySlot(now time.Time, location *time.Location, weekdayName, clock string) string {
	weekday, runMinute := nearbySchedule(weekdayName, clock)
	local := now.In(location)
	minute := local.Hour()*60 + local.Minute()
	switch {
	case local.Weekday() == weekday && minute >= runMinute:
	case local.Weekday() == (weekday+1)%7 && local.Hour() < nearbyCatchUpUntilHour:
		local = local.AddDate(0, 0, -1)
	default:
		return ""
	}
	return fmt.Sprintf("%sT%02d:%02d", local.Format(time.DateOnly), runMinute/60, runMinute%60)
}

// nearbySchedule reads the day name and the "HH:MM" the person set, falling
// back to the default for one that does not parse, as clockMinute does for
// the quiet hours.
func nearbySchedule(weekdayName, clock string) (weekday time.Weekday, runMinute int) {
	weekday = time.Thursday
	for index, name := range models.LookAroundWeekdays {
		if name == weekdayName {
			weekday = time.Weekday(index)
		}
	}
	return weekday, clockMinute(clock, models.LookAroundTimeDefault)
}

// queueNearbyRuns queues the week's look around for each agent that has
// alerts on, once: the job's subject is the slot, so however often the
// worker looks it is queued once, and a run missed while the server was
// down is queued at the next look.
func (self *Agent) queueNearbyRuns(ctx context.Context, now time.Time) {
	if now.Sub(self.lastNearbyQueue) < nearbyQueueEvery {
		return
	}
	self.lastNearbyQueue = now
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to look around for: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Active() || !agent.IsAlertsEnabled {
			continue
		}
		owner := self.ownerOf(ctx, agent)
		if owner == nil {
			continue
		}
		weekdayName, clock := agent.LookAroundSchedule()
		slot := nearbySlot(now, Location(owner), weekdayName, clock)
		if slot == "" {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			queued, err := tx.CountAgentJobs(&db.AgentJobFilter{AgentID: agent.ID, Kinds: []models.AgentJobKind{models.AgentJobNearby}, SubjectID: slot})
			if err != nil || queued > 0 {
				return err
			}
			_, err = self.Enqueue(tx, models.AgentJobNearby, agent.ID, "", slot)
			return err
		}); err != nil {
			log.Warningf("cannot queue a look around for agent %q: %s", agent.ID, err)
		}
	}
}

// nearbyWindow is the days the run looks at: from tomorrow through the
// Sunday of the week after the coming weekend, so that the weekend's message
// and the next Sunday's roundup both have something to tell.
func nearbyWindow(today time.Time) (windowStart, windowEnd time.Time) {
	return today.AddDate(0, 0, 1), today.AddDate(0, 0, nearbyDaysToSunday(today)+7)
}

// nearbyDaysToSunday is how many days from the day given the coming Sunday
// is, seven on a Sunday.
func nearbyDaysToSunday(today time.Time) int {
	days := (7 - int(today.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	return days
}

// nearbyHome is what memory says of where the person lives and who is in
// the household.
type nearbyHome struct {
	// HomeArea is the town or neighborhood, empty when memory does not say.
	HomeArea string

	// Household is short lines on who lives there, for choosing among what
	// is found.
	Household []string
}

type nearbyHomeAnswer struct {
	HomeArea  string   `json:"home_area"`
	Household []string `json:"household"`
}

var (
	// streetWord is a word that makes a piece of an address a street, a
	// unit or a box rather than a town.
	streetWord = regexp.MustCompile(`(?i)\b(?:street|avenue|road|lane|drive|boulevard|court|terrace|apartment|apt|unit|suite|floor|po box|p\.o\. box)\b`)

	// digitToken is a word with a digit in it: a house number, a unit, a
	// postal code.
	digitToken = regexp.MustCompile(`\S*\d\S*`)
)

// cleanHomeArea reduces what was written as the person's home to a place a
// stranger could search for: the pieces between commas, semicolons and
// lines that are not a street, a unit or a box, with house numbers and
// postal codes taken out of what is left. The prompt asks for this too;
// this is the same rule where a model cannot be talked out of it, because
// the result goes into a search sent to a provider outside this server.
func cleanHomeArea(text string) string {
	var kept []string
	for _, piece := range strings.FieldsFunc(text, func(character rune) bool {
		return character == ',' || character == ';' || character == '\n'
	}) {
		piece = strings.Join(strings.Fields(digitToken.ReplaceAllString(piece, "")), " ")
		if piece == "" || streetWord.MatchString(piece) {
			continue
		}
		kept = append(kept, piece)
		if len(kept) == 3 {
			break
		}
	}
	return cutRunes(strings.Join(kept, ", "), 80)
}

// nearbyTownOf is the town of a place, to search for what is on near it. A
// short address ("street, city, country" as Photon writes it, or "city,
// country") has its city in it; a long one, as Nominatim writes, is not
// taken apart, since which part is the town differs from country to country,
// and the nearest city to the coordinates is used instead. Empty when there
// is no city near.
func nearbyTownOf(home *models.Place) string {
	var parts []string
	for _, part := range strings.Split(home.AddressText, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	town := ""
	switch len(parts) {
	case 2:
		town = cleanHomeArea(parts[0])
	case 3:
		town = cleanHomeArea(parts[1])
	}
	if town != "" {
		return town
	}
	town, _ = place.Nearest(home.LatitudeDegrees, home.LongitudeDegrees)
	return town
}

// nearbyMemoryLines is what memory says that could name the town and the
// household: the self page's summary and facts, and the place pages with a
// few facts each.
func nearbyMemoryLines(tx db.Transaction, agentId string) ([]string, error) {
	if err := tx.EnsureAgentRoots(agentId); err != nil {
		return nil, err
	}
	var lines []string
	selfPage, err := tx.GetAgentNode(agentId, models.PathSelf)
	if err != nil {
		return nil, err
	}
	if selfPage != nil {
		if summary := strings.TrimSpace(selfPage.Summary); summary != "" {
			lines = append(lines, "About them: "+cutRunes(summary, 800))
		}
		facts, err := tx.ListAgentFacts(agentId, selfPage.ID, false, nearbyMemoryFacts)
		if err != nil {
			return nil, err
		}
		for _, fact := range facts {
			lines = append(lines, "- "+cutRunes(fact.Line(), 300))
		}
	}
	places, err := tx.ListAgentNodesUnder(agentId, models.PathPlaces, nearbyMemoryPages*4)
	if err != nil {
		return nil, err
	}
	pageCount := 0
	for _, place := range places {
		if place.Kind == models.NodeFolder || pageCount == nearbyMemoryPages {
			continue
		}
		pageCount++
		heading := "A place they know: " + place.Name
		if summary := strings.TrimSpace(place.Summary); summary != "" {
			heading += ": " + cutRunes(summary, 300)
		}
		lines = append(lines, heading)
		facts, err := tx.ListAgentFacts(agentId, place.ID, false, nearbyPageFacts)
		if err != nil {
			return nil, err
		}
		for _, fact := range facts {
			lines = append(lines, "- "+cutRunes(fact.Line(), 300))
		}
	}
	return lines, nil
}

// readNearbyHome asks the model where the person lives and who is in the
// household, from the memory lines, and reduces the town to a place that
// can be searched for.
func (self *Agent) readNearbyHome(ctx context.Context, run *Run, memory []string) (*nearbyHome, error) {
	prompt, err := render("nearby_home.txt", map[string]any{
		"PersonName":    personName(run.Owner),
		"HouseholdMost": nearbyHouseholdMost,
		"Memory":        memory,
	})
	if err != nil {
		return nil, err
	}
	thinking, err := self.oneShot(ctx, run, "Read where they live", prompt, models.AgentJobNearby, config.AgentWorkTriage)
	if err != nil {
		return nil, err
	}
	answer, err := llm.Extract[nearbyHomeAnswer](thinking.Text)
	if err != nil {
		log.Warningf("the look around for agent %q did not get an object about where they live: %s", run.Agent.ID, err)
		return &nearbyHome{}, nil
	}
	home := &nearbyHome{HomeArea: cleanHomeArea(answer.HomeArea)}
	for _, line := range trimmed(answer.Household) {
		if home.Household = append(home.Household, cutRunes(line, nearbyHouseholdCharacters)); len(home.Household) == nearbyHouseholdMost {
			break
		}
	}
	return home, nil
}

// askNearbyHomeAreaOnce offers the person the question of which town to
// look around, as an idea the agent puts to them when they are at the
// dashboard and quiet (ideaReason). It is one row per agent whatever
// became of it, answered, dismissed or let go, so it is never asked a
// second time. When they answer, the conversation the idea opens notes the
// town in memory like anything else they say about themselves, and the next
// run reads it there.
func askNearbyHomeAreaOnce(tx db.Transaction, agent *models.Agent) (isAsked bool, err error) {
	kept, err := tx.ListAgentIdeas(agent.ID, nil, nil)
	if err != nil {
		return false, err
	}
	for _, idea := range kept {
		if idea.IdeaKey == nearbyHomeAreaIdeaKey {
			return false, nil
		}
	}
	if _, err := tx.UpsertAgentIdea(&models.AgentIdea{
		AgentID: agent.ID, IdeaKey: nearbyHomeAreaIdeaKey, IdeaKind: models.IdeaPersonal,
		IdeaCategory: "home", Emoji: "🏠",
		Headline:         "To tell you what is on near you each week, which town or neighborhood should I look around?",
		Body:             "Tell me the town or neighborhood you live in, not a street address. Each week, on the day you choose in the agent settings, I look up events, markets, new places and dates that matter there, and tell you what suits your household for the weekend and the week after.",
		OpeningRequest:   "I live in ",
		NeededToolNames:  []string{},
		Evidence:         []models.AgentIdeaEvidence{},
		SuggestionReason: "Without it I cannot look around for you.",
		RankScore:        personalIdeaRank,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// nearbyItem is one thing the run found, as the model wrote it.
type nearbyItem struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	DueOn   string `json:"due_on"`
	Place   string `json:"place"`
	URL     string `json:"url"`
	Because string `json:"because"`
}

type nearbyAnswer struct {
	Items []nearbyItem `json:"items"`
}

// normalizeNearbyURL is a page address in the one form it is kept and
// compared in: a web address with a host, without a fragment or a login, in
// no more than 500 characters. False for anything else, which includes
// every scheme that would run something when it was opened.
func normalizeNearbyURL(text string) (string, bool) {
	address, err := url.Parse(strings.TrimSpace(text))
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Hostname() == "" || address.User != nil {
		return "", false
	}
	address.Scheme = strings.ToLower(address.Scheme)
	address.Host = strings.ToLower(address.Host)
	address.Fragment = ""
	normalized := address.String()
	return normalized, len(normalized) <= 500
}

// isPageSeen says the page was in something the run read: a search result
// or a page it fetched. A model asked for the address of the page that says
// a thing will, now and then, write one that looks right, so an address the
// run never saw is not kept.
func isPageSeen(normalized, seen string) bool {
	address, err := url.Parse(normalized)
	if err != nil {
		return false
	}
	return strings.Contains(seen, strings.ToLower(address.Host+strings.TrimSuffix(address.Path, "/")))
}

// nearbyFollowUpKey is the stored key of an item: the kind, the marker that
// keeps it from the keys of mail, the page and the day.
func nearbyFollowUpKey(kind models.AgentFollowUpKind, address, dueOn string) string {
	return FollowUpKeyOf(kind, nearbyKeyPrefix+address+" "+dueOn)
}

// isNearbyFollowUp says the follow-up was written by the look around.
func isNearbyFollowUp(followUp *models.AgentFollowUp) bool {
	_, rest, _ := strings.Cut(followUp.FollowUpKey, ":")
	return strings.HasPrefix(rest, nearbyKeyPrefix)
}

// interpretNearbyItems turns the model's items into follow-ups for the
// agent, keeping only what is usable: a kind of event or deadline, a title,
// a day from today to a little over a year ahead, and a web page that the
// run saw (seen is the lower-cased text of what its tools returned). At
// most nearbyItemsMost, in the order given, one for each key.
func interpretNearbyItems(items []nearbyItem, agentId string, today time.Time, seen string) []*models.AgentFollowUp {
	followUps := []*models.AgentFollowUp{}
	isKept := map[string]bool{}
	for _, item := range items {
		if len(followUps) == nearbyItemsMost {
			break
		}
		kind := models.AgentFollowUpKind(strings.ToLower(strings.TrimSpace(item.Kind)))
		if kind != models.AgentFollowUpEvent && kind != models.AgentFollowUpDeadline {
			continue
		}
		title := cutRunes(strings.TrimSpace(item.Title), 200)
		day, err := time.Parse(time.DateOnly, strings.TrimSpace(item.DueOn))
		if title == "" || err != nil || day.Before(today) || day.After(today.AddDate(0, 0, nearbyHorizonDays)) {
			continue
		}
		address, isAddress := normalizeNearbyURL(item.URL)
		if !isAddress || !isPageSeen(address, seen) {
			continue
		}
		dueOn := day.Format(time.DateOnly)
		followUpKey := nearbyFollowUpKey(kind, address, dueOn)
		if followUpKey == "" || isKept[followUpKey] {
			continue
		}
		isKept[followUpKey] = true
		followUps = append(followUps, &models.AgentFollowUp{
			AgentID: agentId, FollowUpKind: kind, FollowUpKey: followUpKey, FollowUpTitle: title,
			DueOn: dueOn, Place: cutRunes(strings.TrimSpace(item.Place), 200), SourceURL: address,
			FollowUpState: models.AgentFollowUpOpen, SourceItemIDs: []string{},
		})
	}
	return followUps
}

// storeNearbyFollowUps writes the follow-ups the run found, except the ones
// already told about in the last four weeks, and answers how many it wrote
// or refreshed. One the person stopped stays stopped: the upsert does not
// touch a follow-up's state.
func storeNearbyFollowUps(tx db.Transaction, agentId string, followUps []*models.AgentFollowUp, now time.Time) (int, error) {
	known, err := tx.ListAgentFollowUps(agentId, followUpsListedMost)
	if err != nil {
		return 0, err
	}
	toldAtByKey := map[string]time.Time{}
	for _, followUp := range known {
		if followUp.ToldAt != nil {
			toldAtByKey[followUp.FollowUpKey] = *followUp.ToldAt
		}
	}
	storedCount := 0
	for _, followUp := range followUps {
		if toldAt, isTold := toldAtByKey[followUp.FollowUpKey]; isTold && now.Sub(toldAt) < nearbyToldWithin {
			continue
		}
		if _, _, err := tx.UpsertAgentFollowUp(followUp); err != nil {
			return 0, err
		}
		storedCount++
	}
	return storedCount, nil
}

// pagesSeenBy is the lower-cased text of what the tools returned in a
// conversation, to check the addresses the model gives against.
func pagesSeenBy(tx db.Transaction, conversationId string) (string, error) {
	messages, err := tx.ListAgentMessages(conversationId, nil)
	if err != nil {
		return "", err
	}
	var seen strings.Builder
	for _, message := range messages {
		if message.Role == string(llm.RoleTool) {
			seen.WriteString(strings.ToLower(message.Content))
			seen.WriteString("\n")
		}
	}
	return seen.String(), nil
}

// runNearby is the handler for a nearby job.
func (self *Agent) runNearby(ctx context.Context, run *Run) error {
	return self.lookAround(ctx, run, time.Now())
}

// lookAround is one week's look around at the moment given: it reads the
// town from memory, asks for it once when memory has none, and otherwise
// searches and writes what it finds as follow-ups.
func (self *Agent) lookAround(ctx context.Context, run *Run, now time.Time) error {
	configuration := run.Configuration()
	if !run.Agent.Active() || !run.Agent.IsAlertsEnabled || !FeatureAllowed(configuration, "search") || !self.canThink(configuration) {
		return nil
	}
	// A search service the operator did not configure leaves the run
	// nothing to look with, and a question about the town nothing to be
	// for.
	if search := configuration.Agent.Search; search.Kind == "" || search.APIKey == "" {
		return nil
	}
	var memory []string
	homeTown := ""
	isMuted := false
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, now); err != nil {
			return err
		}
		mutes, err := tx.ListAgentAlertMutes(run.Agent.ID)
		if err != nil {
			return err
		}
		// Muting every follow-up stops the look around with the rest.
		if isMuted = mutedBy(mutes, &alertFacts{kinds: []string{models.AlertKindFollowUp, models.AlertKindNearby}}) != nil; isMuted {
			return nil
		}
		// Where they say they live, in the place they labeled home, comes
		// before what memory happens to hold.
		homes, _, err := tx.ListPlaces(run.Owner.ID, db.PlaceFilter{PlaceLabel: models.PlaceLabelHome, LimitCount: 1})
		if err != nil {
			return err
		}
		if len(homes) > 0 {
			homeTown = nearbyTownOf(homes[0])
		}
		memory, err = nearbyMemoryLines(tx, run.Agent.ID)
		return err
	}); err != nil || isMuted {
		return err
	}

	home := &nearbyHome{}
	if len(memory) > 0 {
		var err error
		if home, err = self.readNearbyHome(ctx, run, memory); err != nil {
			return err
		}
	}
	if homeTown != "" {
		home.HomeArea = homeTown
	}
	if home.HomeArea == "" {
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := askNearbyHomeAreaOnce(tx, run.Agent)
			return err
		})
	}
	// The town is known, so the question, if it was asked and not yet
	// answered, is answered.
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		kept, err := tx.ListAgentIdeas(run.Agent.ID, []models.AgentIdeaStatus{models.IdeaOpen}, nil)
		if err != nil {
			return err
		}
		for _, idea := range kept {
			if idea.IdeaKey == nearbyHomeAreaIdeaKey {
				if _, err := self.SetIdeaStatus(tx, run.Agent, idea.ID, models.IdeaDone); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	local := now.In(Location(run.Owner))
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	weekStart, weekEnd := nearbyWindow(today)
	// The coming weekend, the one the evening's message is about.
	sunday := today.AddDate(0, 0, nearbyDaysToSunday(today))
	prompt, err := render("nearby.txt", map[string]any{
		"PersonName":   personName(run.Owner),
		"Language":     languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"HomeArea":     home.HomeArea,
		"Household":    home.Household,
		"Today":        local.Format("Monday 2 January 2006"),
		"Zone":         Location(run.Owner).String(),
		"WeekStart":    weekStart.Format("Monday 2 January"),
		"WeekEnd":      weekEnd.Format("Monday 2 January"),
		"WeekendStart": sunday.AddDate(0, 0, -1).Format("Monday 2 January"),
		"WeekendEnd":   sunday.Format("Monday 2 January"),
		"ItemsMost":    nearbyItemsMost,
		"SearchMost":   nearbySearchMost,
		"FetchMost":    nearbyPageReadMost,
	})
	if err != nil {
		return err
	}
	thinking, err := self.think(withPersonOmitted(withToolCallLimits(ctx, nearbyCallLimits)), run, "Looked around "+home.HomeArea, prompt,
		nearbyTools, nearbyRounds, models.AgentJobNearby, config.AgentWorkResearch)
	if err != nil {
		return err
	}
	answer, err := llm.Extract[nearbyAnswer](thinking.Text)
	if err != nil {
		log.Warningf("the look around for agent %q did not answer with an object: %s", run.Agent.ID, err)
		return nil
	}
	var storedCount int
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		seen, err := pagesSeenBy(tx, thinking.Conversation.ID)
		if err != nil {
			return err
		}
		storedCount, err = storeNearbyFollowUps(tx, run.Agent.ID, interpretNearbyItems(answer.Items, run.Agent.ID, today, seen), now)
		if err != nil {
			return err
		}
		return self.tellWeekend(tx, run, today, now)
	}); err != nil {
		return err
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Looked around %s: %s", home.HomeArea, plural(storedCount, "one thing for the week", "%d things for the week")))
	return nil
}

// tellWeekend writes the weekend message: the open events and deadlines
// from tomorrow through Sunday, from the look around and from mail, in one
// candidate, and queues the alert job. What it lists is marked, so the
// Sunday roundup tells only what comes after.
func (self *Agent) tellWeekend(tx db.Transaction, run *Run, today, now time.Time) error {
	followUps, err := tx.ListOpenAgentFollowUps(run.Agent.ID)
	if err != nil {
		return err
	}
	mutes, err := tx.ListAgentAlertMutes(run.Agent.ID)
	if err != nil {
		return err
	}
	daysToSunday := nearbyDaysToSunday(today)
	reason := fmt.Sprintf("this weekend, from tomorrow to %s", today.AddDate(0, 0, daysToSunday).Format(time.DateOnly))
	candidate, err := writeRoundup(tx, run.Agent, mutes, followUps, today, daysToSunday, reason, followUpWeekendSubjectKey(today), now)
	if err != nil || candidate == nil {
		return err
	}
	return queueAlert(tx, run.Agent.ID, false, now)
}
