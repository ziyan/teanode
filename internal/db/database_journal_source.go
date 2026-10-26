package db

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
)

// JournalSourceOperation answers, for a person, where their sources of a
// journal day end. The journal reads each source by the day's time range
// (see JournalOperation); this is the one question that has no day to ask
// about yet.
type JournalSourceOperation interface {
	// LatestJournalSourceTimeBefore is the latest time before the given one at
	// which any of the sources a journal day reads has something of the
	// person's: a photo, a visit, a mail, a recording, a message with the
	// agent or in a chat, a document, a calendar event, a transaction or a
	// Health day. Nil when there is nothing earlier. It is how the journal
	// walks back over past days without trying every empty one: the day it
	// finds is the next to work out.
	//
	// A source's own limits are not applied (a document a day does not count
	// because it does not involve the person still counts here), so a day it
	// names may turn out to have nothing in it; the journal then moves on to
	// the day before.
	//
	// A chat in the agent's documents counts when one of chatNames (the
	// person's username and the words of their name, lowered) took part in it
	// and it is an exchange of two posts or more, the rule the journal reads
	// a chat by; an archive of other people's channels is not the person's
	// day.
	LatestJournalSourceTimeBefore(userId, agentId string, chatNames []string, before time.Time) (*time.Time, error)
}

func (self *transaction) LatestJournalSourceTimeBefore(userId, agentId string, chatNames []string, before time.Time) (*time.Time, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	// One latest value per source, each by the source's own index with the
	// newest first and one row taken, so that a source with a hundred
	// thousand rows costs one probe. The days that are dates are placed at
	// noon UTC, which is the same calendar day in any zone within twelve
	// hours of it.
	var latest sql.NullTime
	if err := self.tx.Raw(`
		SELECT max("moment") FROM (
			(SELECT COALESCE("taken_at", "created_at") AS "moment" FROM "photo"
				WHERE "user_id" = @user AND "trashed_at" IS NULL AND COALESCE("taken_at", "created_at") < @before
				ORDER BY COALESCE("taken_at", "created_at") DESC LIMIT 1)
			UNION ALL
			(SELECT COALESCE("arrived_at", "departed_at") FROM "place_visit"
				WHERE "user_id" = @user AND COALESCE("arrived_at", "departed_at") < @before
				ORDER BY COALESCE("arrived_at", "departed_at") DESC LIMIT 1)
			UNION ALL
			(SELECT COALESCE("recorded_at", "created_at") FROM "recording"
				WHERE "user_id" = @user AND "deleted_at" IS NULL AND COALESCE("recorded_at", "created_at") < @before
				ORDER BY COALESCE("recorded_at", "created_at") DESC LIMIT 1)
			UNION ALL
			(SELECT mail."received_at" FROM "mailbox" mailbox
				JOIN "mailbox_folder" folder ON folder."mailbox_id" = mailbox."id"
				JOIN "mailbox_item" item ON item."folder_id" = folder."id"
				JOIN "mail" mail ON mail."id" = item."mail_id"
				WHERE mailbox."user_id" = @user AND folder."kind" NOT IN ('trash', 'junk', 'drafts')
				  AND mail."kind" <> 'note' AND mail."received_at" < @before
				ORDER BY mail."received_at" DESC LIMIT 1)
			UNION ALL
			(SELECT message."created_at" FROM "agent_conversation" conversation
				JOIN "agent_message" message ON message."conversation_id" = conversation."id"
				WHERE conversation."agent_id" = @agent AND conversation."kind" IN ('main', 'named')
				  AND message."role" IN ('user', 'assistant') AND message."created_at" < @before
				ORDER BY message."created_at" DESC LIMIT 1)
			UNION ALL
			(SELECT message."created_at" FROM "chat_member" member
				JOIN "chat_message" message ON message."chat_id" = member."chat_id"
				WHERE member."user_id" = @user AND message."deleted_at" IS NULL
				  AND message."created_at" >= member."joined_at" AND message."created_at" < @before
				ORDER BY message."created_at" DESC LIMIT 1)
			UNION ALL
			(SELECT "happened_at" FROM "agent_document"
				WHERE "agent_id" = @agent AND "kind" = 'chat' AND "happened_at" < @before
				  AND jsonb_exists_any("metadata"->'participants', (@names)::text[])
				  AND coalesce(("metadata"->>'posts')::int, 0) >= 2
				ORDER BY "happened_at" DESC LIMIT 1)
			UNION ALL
			(SELECT occurrence."starts_at" FROM "calendar" calendar
				JOIN "calendar_occurrence" occurrence ON occurrence."calendar_id" = calendar."id"
				WHERE calendar."user_id" = @user AND calendar."calendar_kind" = 'events' AND occurrence."starts_at" < @before
				ORDER BY occurrence."starts_at" DESC LIMIT 1)
			UNION ALL
			(SELECT ("posted_on"::timestamp + interval '12 hours') AT TIME ZONE 'UTC' FROM "agent_finance_transaction"
				WHERE "agent_id" = @agent AND "posted_on" <= ((@before)::timestamptz AT TIME ZONE 'UTC')::date
				  AND ("posted_on"::timestamp + interval '12 hours') AT TIME ZONE 'UTC' < @before
				ORDER BY "posted_on" DESC LIMIT 1)
			UNION ALL
			(SELECT ("summary_day"::timestamp + interval '12 hours') AT TIME ZONE 'UTC' FROM "health_day_summary"
				WHERE "user_id" = @user AND "summary_day" <= ((@before)::timestamptz AT TIME ZONE 'UTC')::date
				  AND ("summary_day"::timestamp + interval '12 hours') AT TIME ZONE 'UTC' < @before
				ORDER BY "summary_day" DESC LIMIT 1)
		) AS "latest_moments"`,
		map[string]any{"user": userId, "agent": agentId, "before": before.UTC(), "names": pq.Array(chatNames)}).Row().Scan(&latest); err != nil {
		return nil, err
	}
	if !latest.Valid {
		return nil, nil
	}
	moment := latest.Time.In(time.Local)
	return &moment, nil
}
