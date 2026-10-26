package agent

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// What the worker's tick does for the journal without a model: every hour it
// brings each person's day numbers, moods from Apple Health and weather up to
// date (journal.Refresh), and through the evening it asks "How was today?" on
// the person's phones, once a day, when they have not chosen a mood.

const (
	// journalRefreshEvery is how often the numbers are brought up to date.
	journalRefreshEvery = time.Hour

	// journalRefreshTimeout bounds the refresh of one person, weather
	// requests included.
	journalRefreshTimeout = 5 * time.Minute

	// journalEveningQuestionEvery is how often the worker looks for people
	// whose evening has come. Ten minutes keeps the question within a few
	// minutes of the time the person chose.
	journalEveningQuestionEvery = 10 * time.Minute

	// journalEveningQuestionSubjectPrefix begins the subject of the job that
	// remembers a day's question was asked: a finished job of the agent whose
	// subject is this and the day. The job does nothing; its row is the memory,
	// the way a look around town keeps its slot.
	journalEveningQuestionSubjectPrefix = "journal-evening-question:"
)

// What the evening question says, in each language a phone is told in.
var (
	phraseJournalEveningTitle = push.Phrase{English: "How was today?", Chinese: "今天过得怎么样？", Japanese: "今日はどうでしたか？"}
	phraseJournalEveningBody  = push.Phrase{English: "One tap is enough.", Chinese: "点一下就好。", Japanese: "タップするだけです。"}
)

// refreshJournals brings the journal numbers of every active person who keeps
// a journal up to date, at most once an hour. It makes no model call.
func (self *Agent) refreshJournals(ctx context.Context, now time.Time) {
	if now.Sub(self.lastJournalRefresh) < journalRefreshEvery {
		return
	}
	self.lastJournalRefresh = now
	for _, person := range self.journalPeople(ctx) {
		refreshContext, cancel := context.WithTimeout(ctx, journalRefreshTimeout)
		var result journal.RefreshResult
		err := self.settings.Database.TransactionContext(refreshContext, func(transaction db.Transaction) (err error) {
			result, err = journal.Refresh(refreshContext, transaction, person.owner, person.agent, now, self.journalWeather)
			return err
		})
		cancel()
		if err != nil {
			log.Warningf("cannot refresh the journal of agent %q: %s", person.agent.ID, err)
			continue
		}
		if result.WeatherFailedCount > 0 {
			log.Debugf("the weather service had no answer for %d days of agent %q", result.WeatherFailedCount, person.agent.ID)
		}
	}
}

// journalPerson is an active agent and the person it is for, who may keep a
// journal.
type journalPerson struct {
	agent *models.Agent
	owner *models.User
}

// journalPeople are the active agents whose person holds journal:use.
func (self *Agent) journalPeople(ctx context.Context) []journalPerson {
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		agents, err = transaction.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to keep journals for: %s", err)
		return nil
	}
	var people []journalPerson
	for _, agent := range agents {
		if !agent.Active() {
			continue
		}
		owner := self.ownerOf(ctx, agent)
		if owner == nil {
			continue
		}
		var permissions *models.EffectivePermissions
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			permissions, err = transaction.EffectivePermissions(owner.ID)
			return err
		}); err != nil {
			log.Debugf("cannot read the permissions of agent %q: %s", agent.ID, err)
			continue
		}
		if permissions.Has(models.PermissionJournalUse) {
			people = append(people, journalPerson{agent: agent, owner: owner})
		}
	}
	return people
}

// askJournalEveningQuestions sends "How was today?" to the phones of each
// person whose evening has come and who has not chosen a mood today.
func (self *Agent) askJournalEveningQuestions(ctx context.Context, now time.Time) {
	if now.Sub(self.lastJournalEveningQuestion) < journalEveningQuestionEvery {
		return
	}
	self.lastJournalEveningQuestion = now
	if sender, err := self.phoneSender(); err != nil || sender == nil {
		return
	}
	for _, person := range self.journalPeople(ctx) {
		if err := self.askJournalEveningQuestion(ctx, person.agent, person.owner, now); err != nil {
			log.Warningf("cannot ask the evening question of agent %q: %s", person.agent.ID, err)
		}
	}
}

// askJournalEveningQuestion asks one person, if it is time and they have not
// been asked today. The day is remembered before the phones are told, so a
// question is never sent twice, and not remembered when no phone can be told,
// so that a phone registered later in the evening still gets it.
func (self *Agent) askJournalEveningQuestion(ctx context.Context, agent *models.Agent, owner *models.User, now time.Time) error {
	var day string
	var phones []*models.AgentPhone
	err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		settings, err := transaction.GetJournalSettings(owner.ID)
		if err != nil || !settings.IsEveningQuestionOn {
			return err
		}
		local := now.In(journal.Zone(owner))
		if local.Format("15:04") < settings.EveningQuestionTime {
			return nil
		}
		today := local.Format(journal.DayLayout)
		row, err := transaction.GetJournalDay(owner.ID, today)
		switch {
		case err == nil:
			// A mood from Apple Health is not one the person chose.
			if row.MoodLevel != nil && row.MoodSource == models.JournalMoodFromPerson {
				return nil
			}
		case !errors.Is(err, db.ErrNotFound):
			return err
		}
		subject := journalEveningQuestionSubjectPrefix + today
		asked, err := transaction.CountAgentJobs(&db.AgentJobFilter{AgentID: agent.ID, Kinds: []models.AgentJobKind{models.AgentJobNoop}, SubjectID: subject})
		if err != nil || asked > 0 {
			return err
		}
		all, err := transaction.ListAgentPhones(agent.ID)
		if err != nil {
			return err
		}
		for _, phone := range all {
			if phone.CanBeNotified() && phone.Wants(models.NotificationJournalEveningQuestion) {
				phones = append(phones, phone)
			}
		}
		if len(phones) == 0 {
			return nil
		}
		if _, err := self.Enqueue(transaction, models.AgentJobNoop, agent.ID, "", subject); err != nil {
			return err
		}
		day = today
		return nil
	})
	if err != nil || day == "" {
		return err
	}
	self.notifyPhones(agent.ID, models.NotificationJournalEveningQuestion, &push.Content{
		NotificationKind: models.NotificationJournalEveningQuestion,
		JournalDay:       day,
		Title:            phraseJournalEveningTitle.English,
		Body:             phraseJournalEveningBody.English,
		ThreadID:         "journal-evening-question",
		Wording: func(content *push.Content, language string) {
			content.Title, content.Body = phraseJournalEveningTitle.In(language), phraseJournalEveningBody.In(language)
		},
	})
	return nil
}
