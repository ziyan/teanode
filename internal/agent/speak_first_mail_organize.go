package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// SpeakFirstMailOrganize is the look at how the person's mail could be
// filed: when the Inbox holds a pile of routine mail no rule files, the agent
// reviews the last week and the folders and rules there are, and offers one
// card with what it would do. It changes nothing until the person approves.
const SpeakFirstMailOrganize = "mail_organize"

const (
	// mailOrganizePileLeast is how many routine messages the Inbox holds from
	// the last week before a look is worth a model's turn.
	mailOrganizePileLeast = 30

	// mailOrganizeLookBack is how far back the pile is counted, and the
	// turn is told to read.
	mailOrganizeLookBack = 7 * 24 * time.Hour

	// mailOrganizeItemsRead bounds how many Inbox items a mailbox is counted
	// from.
	mailOrganizeItemsRead = 500
)

// mailOrganizeLines is what the turn is told: read, change nothing, and post
// one card or none.
const mailOrganizeLines = "Review the last 7 days of their mail, archived mail included, and the folders and rules they already have, with the mail tools you have (mail_search, mail_read, and mailbox_settings for the folders and rules). " +
	"Change nothing in this turn: no folder, no rule, no move, no archive. Everything you would do waits for their approval.\n\n" +
	"If a few new folders and rules would file a meaningful pile of routine mail (newsletters, notifications, receipts, anything from a sender that always says the same sort of thing), post ONE card with the feed tool: " +
	"feed_item_kind decision, a title with the real numbers such as \"File 60 routine messages into 6 folders\", a body of a few lines saying what you would do, fact_rows with one row per folder (fact_label the folder, fact_value how many messages), picture_symbol tray.full, coalesce_key mail-organize, " +
	"and three choices in their language: Approve (choice_key approve, action), Discuss (choice_key discuss, discuss) and Not now (choice_key not_now, dismiss). " +
	"The body must say exactly what approving does, because approving sends \"Approve: <the title>\" as their message in the main conversation with this card attached, and the turn that follows creates the folders and the rules and files the mail as the card says, and nothing more. " +
	"If it would not be worth their time, post nothing and write nothing: a week with nothing to tidy is a good week."

// mailOrganizeReason offers to file a pile of routine mail.
func (self *Agent) mailOrganizeReason() speakFirstReason {
	return speakFirstReason{
		name:      SpeakFirstMailOrganize,
		feedLines: mailOrganizeLines,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			// A card in the feed: for a person whose agent speaks in the main
			// conversation it would only be another interruption.
			if !agent.IsSuggestionsEnabled || agent.OnboardedAt == nil || idle < ideaIdle || !wantsFeed(agent) {
				return false, nil
			}
			// Mail a look has already seen is not counted again, so a pile is
			// raised once.
			since := now.Add(-mailOrganizeLookBack)
			looks, err := tx.ListAgentJobs(&db.AgentJobFilter{
				AgentID: agent.ID, Kinds: []models.AgentJobKind{models.AgentJobSpeakFirst},
				SubjectID: SpeakFirstMailOrganize,
			}, &db.Options{Limit: 1})
			if err != nil {
				return false, err
			}
			if len(looks) > 0 && looks[0].CreatedAt.After(since) {
				since = looks[0].CreatedAt
			}
			pile, err := routineInboxCount(tx, agent, owner, since)
			return pile >= mailOrganizePileLeast, err
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time, prepared map[string]string) (string, error) {
			return strings.Join([]string{
				models.SpeakFirstMarker + " A look at how " + personName(owner) + "'s mail could be filed. Nobody asked for this turn.",
				"",
				"It is " + now.In(Location(owner)).Format("Monday 2 January, 15:04") + " where they are.",
				"",
			}, "\n"), nil
		},
	}
}

// routineInboxCount is how many messages that arrived since a moment sit in
// the Inbox of the mailboxes the agent was given, that nothing asks the person
// to act on:
// not flagged, and not marked by the sorting as needing a reply. Whatever a
// rule files does not reach the Inbox, so what is left there is what no rule
// files.
func routineInboxCount(tx db.Transaction, agent *models.Agent, owner *models.User, since time.Time) (int, error) {
	mailboxes, err := tx.ListMailboxes(owner.ID)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, mailbox := range mailboxes {
		if mailbox.Agent == nil || !mailbox.Agent.Granted {
			continue
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			if err != nil {
				return 0, err
			}
			continue
		}
		items, err := tx.ListItems(inbox.ID, &db.ItemOptions{Limit: mailOrganizeItemsRead})
		if err != nil {
			return 0, err
		}
		for _, item := range items {
			if item.AddedAt.Before(since) || item.Flagged || item.Draft {
				continue
			}
			if item.Insight != nil && (item.Insight.NeedsReply || item.Insight.Priority == "high") {
				continue
			}
			total++
		}
	}
	return total, nil
}
