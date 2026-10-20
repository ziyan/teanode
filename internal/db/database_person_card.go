package db

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// PersonCardOperation is what a person's card reads that no other part of the
// app asks the same way.
type PersonCardOperation interface {
	// ListMailWithAddresses is the person's recent mail from or to any of the
	// addresses, across every mailbox they own, newest first, and whether
	// there is more than limit of it. An address matches only as a whole, in
	// any case: an address inside a longer one is another address. The trash,
	// the junk folder, drafts and a phone's notes are left out, and so are
	// the copies of one mail that a sender made by writing to each of the
	// person's addresses separately.
	ListMailWithAddresses(userId string, addresses []string, limit int) ([]*PersonCardMail, bool, error)
}

// PersonCardMail is one mail with one of the addresses: the item, with its
// message, and the mailbox it is in.
type PersonCardMail struct {
	Item      *models.MailboxItem
	MailboxID string

	// IsFromAddress says one of the addresses wrote it, rather than only
	// being among its recipients.
	IsFromAddress bool
}

// personCardMailCopyWindow is how close in time two mails from one sender with
// one subject arrive to be taken as copies of one mail.
const personCardMailCopyWindow = 10 * time.Minute

// personCardMailPageCount is how many pages of the person's mail are read at
// most to find the mails to keep once copies are left out.
const personCardMailPageCount = 5

// personCardMailRow is a mailbox item with the mailbox its folder is in.
type personCardMailRow struct {
	ID        string    `gorm:"column:id"`
	FolderID  string    `gorm:"column:folder_id"`
	MailID    string    `gorm:"column:mail_id"`
	UID       int64     `gorm:"column:uid"`
	ModSeq    int64     `gorm:"column:modseq"`
	Seen      bool      `gorm:"column:seen"`
	Flagged   bool      `gorm:"column:flagged"`
	Answered  bool      `gorm:"column:answered"`
	Forwarded bool      `gorm:"column:forwarded"`
	AddedAt   time.Time `gorm:"column:added_at"`
	MailboxID string    `gorm:"column:mailbox_id"`
}

// addressColumnMatch is the SQL that says a column holds one of the addresses
// as a whole: the address alone, or the address in angle brackets at the end,
// as a header written with a name has it. The arguments are in the order the
// placeholders are.
func addressColumnMatch(column string, addresses []string) (string, []any) {
	condition := "lower(" + column + ") IN ?"
	arguments := []any{addresses}
	for _, address := range addresses {
		condition += " OR lower(" + column + ") LIKE ?"
		arguments = append(arguments, "%<"+escapeLike(address)+">")
	}
	return "(" + condition + ")", arguments
}

// isAddressIn says the text is one of the addresses, alone or in angle
// brackets at the end, in any case.
func isAddressIn(text string, addresses []string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, address := range addresses {
		if text == address || strings.HasSuffix(text, "<"+address+">") {
			return true
		}
	}
	return false
}

func (self *transaction) ListMailWithAddresses(userId string, addresses []string, limit int) ([]*PersonCardMail, bool, error) {
	wanted := make([]string, 0, len(addresses))
	for _, address := range addresses {
		address = strings.ToLower(strings.TrimSpace(address))
		if address == "" {
			continue
		}
		isKnown := false
		for _, known := range wanted {
			isKnown = isKnown || known == address
		}
		if !isKnown {
			wanted = append(wanted, address)
		}
	}
	if userId == "" || len(wanted) == 0 || limit < 1 {
		return []*PersonCardMail{}, false, nil
	}

	fromMatch, fromArguments := addressColumnMatch(`"mail"."from"`, wanted)
	senderMatch, senderArguments := addressColumnMatch(`"mail"."sender"`, wanted)
	arguments := []any{userId, models.MailKindNote.String(), models.MailKindDraft.String()}
	arguments = append(arguments, fromArguments...)
	arguments = append(arguments, senderArguments...)
	arguments = append(arguments, wanted)
	// Recipients are the envelope's, which for a mail the person sent are
	// the addresses it went to.
	condition := `"mailbox"."user_id" = ? AND "mailbox_item"."deleted" = false AND "mailbox_item"."draft" = false` +
		` AND "mailbox_folder"."kind" NOT IN ('junk', 'trash', 'drafts')` +
		` AND "mail"."kind" NOT IN (?, ?)` +
		` AND (` + fromMatch + ` OR ` + senderMatch +
		` OR EXISTS (SELECT 1 FROM unnest("mail"."recipients") AS recipient WHERE lower(recipient) IN ?))`

	pageSize := max(limit*4, 20)
	kept := []*PersonCardMail{}
	seenMessageIds := map[string]bool{}
	seenCopies := map[string]time.Time{}
	hasMore := false
	for page := range personCardMailPageCount {
		var rows []personCardMailRow
		if err := self.tx.Table(`"mailbox_item"`).
			Select(`"mailbox_item"."id", "mailbox_item"."folder_id", "mailbox_item"."mail_id", "mailbox_item"."uid", "mailbox_item"."modseq", `+
				`"mailbox_item"."seen", "mailbox_item"."flagged", "mailbox_item"."answered", "mailbox_item"."forwarded", "mailbox_item"."added_at", `+
				`"mailbox_folder"."mailbox_id" AS mailbox_id`).
			Joins(`INNER JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"`).
			Joins(`INNER JOIN "mailbox" ON "mailbox"."id" = "mailbox_folder"."mailbox_id"`).
			Joins(`INNER JOIN "mail" ON "mail"."id" = "mailbox_item"."mail_id"`).
			Where(condition, arguments...).
			Order(`"mail"."received_at" DESC, "mailbox_item"."id" DESC`).
			Offset(page * pageSize).Limit(pageSize).
			Scan(&rows).Error; err != nil {
			return nil, false, err
		}
		mailIds := make([]string, 0, len(rows))
		for index := range rows {
			mailIds = append(mailIds, rows[index].MailID)
		}
		mails, err := self.GetMails(mailIds, nil)
		if err != nil {
			return nil, false, err
		}
		for index := range rows {
			mail := mails[index]
			if mail == nil {
				continue
			}
			// One message delivered to several of the person's addresses is
			// several items, in one mailbox or across them; it is one mail.
			if messageId := strings.TrimSpace(mail.MessageID); messageId != "" {
				if seenMessageIds[messageId] {
					continue
				}
				seenMessageIds[messageId] = true
			}
			// A sender that writes to each address separately gives each
			// copy its own Message-ID: the same sender and subject within a
			// few minutes is the same mail to the person.
			copyKey := strings.ToLower(strings.TrimSpace(mail.From)) + "\x00" + strings.TrimSpace(mail.Subject)
			if lastAt, isSeen := seenCopies[copyKey]; isSeen && lastAt.Sub(mail.ReceivedAt).Abs() <= personCardMailCopyWindow {
				continue
			}
			seenCopies[copyKey] = mail.ReceivedAt
			row := &rows[index]
			item := &models.MailboxItem{
				ID: row.ID, FolderID: row.FolderID, MailID: row.MailID, Mail: mail,
				UID: uint64(row.UID), ModSeq: uint64(row.ModSeq), Seen: row.Seen, Flagged: row.Flagged,
				Answered: row.Answered, Forwarded: row.Forwarded, AddedAt: row.AddedAt.In(time.Local),
			}
			kept = append(kept, &PersonCardMail{
				Item: item, MailboxID: row.MailboxID,
				IsFromAddress: isAddressIn(mail.From, wanted) || isAddressIn(mail.Sender, wanted),
			})
		}
		if len(kept) > limit {
			hasMore = true
			break
		}
		if len(rows) < pageSize {
			break
		}
		// The last page that is read is full, so there may be more mail
		// behind it that this stopped before reading.
		if page == personCardMailPageCount-1 {
			hasMore = true
		}
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept, hasMore, nil
}
