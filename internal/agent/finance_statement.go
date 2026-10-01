package agent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/ofx"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

// Imported statements: OFX files a person mails to their statement import
// address or uploads, for the accounts no provider reaches. They go into
// one finance source per agent, of provider kind statement, which has no
// credential and nothing to poll: it changes only when a statement
// arrives. Each statement is written the way a sync writes, through
// ApplyFinanceSync, so its transactions are categorized, matched as
// transfers and counted in budgets like any other, and a transaction
// imported twice is the same row, by its account and its FITID.
//
// The import address is the person's own mailbox address with a detail
// after a plus, statements- and a random token: nothing new in DNS and no
// alias, and the token is the whole of the gate. Regenerating it ends the
// old address.

const (
	// StatementAddressDetailPrefix begins the detail of a statement import
	// address: the part after the plus, before the token.
	StatementAddressDetailPrefix = "statements-"

	// statementTokenBytes is how much randomness a token carries: eighty
	// bits, sixteen characters in the address, far past guessing.
	statementTokenBytes = 10

	// statementAccountKeyBytes is the size of the key a statement source's
	// account identifiers are hashed with.
	statementAccountKeyBytes = 32

	// statementSourceName is what the statement finance source is called.
	statementSourceName = "Imported statements"

	// statementNoticeRetry is how long a statement's notice waits when a
	// turn is running in the conversation it is told in.
	statementNoticeRetry = time.Minute
)

// statementTokenEncoding writes a token in lower case letters and digits,
// which every mail program keeps as it is in an address.
var statementTokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// statementFileExtensions are the names OFX files go by: .qfx is Quicken's
// and .qbo QuickBooks', both OFX inside.
var statementFileExtensions = map[string]bool{".ofx": true, ".qfx": true, ".qbo": true}

// StatementFile is one OFX file to import, as it was named.
type StatementFile struct {
	StatementFileName string
	Content           []byte

	// IsTooLarge says the file was over ofx.MaximumFileBytes and Content
	// holds only its start.
	IsTooLarge bool
}

// errStatementImportOff is the statement source switched off.
var errStatementImportOff = errors.New("importing statements is switched off; switch the Imported statements source on to import again")

// IsStatementSource says a source is the finance source of imported
// statements.
func IsStatementSource(source *models.AgentKnowledgeSource) bool {
	return source != nil && source.Kind == models.SourceFinance && source.Specification.Type == string(finance.ProviderKindStatement)
}

// findStatementSource is the agent's statement source, or nil.
func findStatementSource(tx db.Transaction, agentId string) (*models.AgentKnowledgeSource, error) {
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if IsStatementSource(source) {
			return source, nil
		}
	}
	return nil, nil
}

// newStatementToken is a fresh token for an import address.
func newStatementToken() (string, error) {
	random := make([]byte, statementTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return strings.ToLower(statementTokenEncoding.EncodeToString(random)), nil
}

// EnsureStatementSource is the agent's statement source, made with a fresh
// import token and account key the first time it is asked for, in a
// transaction of its own so it is there for whatever asks next. Two asking
// at once make one: the second finds the first's.
func (self *Agent) EnsureStatementSource(ctx context.Context, agentRow *models.Agent) (*models.AgentKnowledgeSource, error) {
	var source *models.AgentKnowledgeSource
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		if source, err = findStatementSource(tx, agentRow.ID); err != nil || source != nil {
			return err
		}
		token, err := newStatementToken()
		if err != nil {
			return err
		}
		accountKey := make([]byte, statementAccountKeyBytes)
		if _, err := rand.Read(accountKey); err != nil {
			return err
		}
		sealedToken, err := self.SealSecret(token)
		if err != nil {
			return err
		}
		sealedAccountKey, err := self.SealSecret(statementTokenEncoding.EncodeToString(accountKey))
		if err != nil {
			return err
		}
		created, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agentRow.ID, Kind: models.SourceFinance, Name: statementSourceName, Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Type: string(finance.ProviderKindStatement), Settings: json.RawMessage(`{}`)},
		})
		if err != nil {
			return err
		}
		for key, value := range map[string]string{models.FinanceStatementTokenSecretKey: sealedToken, models.FinanceStatementAccountKeySecretKey: sealedAccountKey} {
			if err := tx.PutAgentSourceSecret(agentRow.ID, &models.AgentSourceSecret{SourceID: created.ID, Key: key, Value: value}); err != nil {
				return err
			}
		}
		if _, err := tx.EnsureDefaultSpendingCategories(agentRow.ID); err != nil {
			return err
		}
		source = created
		return nil
	})
	if err == nil {
		return source, nil
	}
	// Made by somebody else between the look and the insert: the unique
	// index on the agent's statement source refused the second.
	var found *models.AgentKnowledgeSource
	if readErr := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		found, err = findStatementSource(tx, agentRow.ID)
		return err
	}); readErr == nil && found != nil {
		return found, nil
	}
	return nil, err
}

// statementSecret opens one of the statement source's secrets.
func (self *Agent) statementSecret(tx db.Transaction, source *models.AgentKnowledgeSource, key string) (string, error) {
	secrets, err := tx.ListAgentSourceSecrets(source.ID)
	if err != nil {
		return "", err
	}
	for _, secret := range secrets {
		if secret.Key == key && secret.Value != "" {
			return self.OpenSecret(secret.Value)
		}
	}
	return "", fmt.Errorf("the statement source has no %s; delete it and it is made again", key)
}

// statementAccountKey is the key the statement source's account
// identifiers are hashed with.
func (self *Agent) statementAccountKey(tx db.Transaction, source *models.AgentKnowledgeSource) ([]byte, error) {
	encoded, err := self.statementSecret(tx, source, models.FinanceStatementAccountKeySecretKey)
	if err != nil {
		return nil, err
	}
	return statementTokenEncoding.DecodeString(encoded)
}

// StatementImportAddress is the address statements are mailed to: the
// local part of the person's first mailbox address, a plus,
// statements- and the token, at that address's domain. Empty when the
// person has no mailbox with an address, since there is then nothing for
// the address to be delivered into.
func (self *Agent) StatementImportAddress(tx db.Transaction, owner *models.User, source *models.AgentKnowledgeSource) (string, error) {
	token, err := self.statementSecret(tx, source, models.FinanceStatementTokenSecretKey)
	if err != nil {
		return "", err
	}
	mailboxes, err := tx.ListMailboxes(owner.ID)
	if err != nil {
		return "", err
	}
	for _, mailbox := range mailboxes {
		for _, address := range mailbox.Addresses {
			if address.LocalPart == "" || address.Domain == "" {
				continue
			}
			return mailparse.UnsplitAddress(address.LocalPart+"+"+StatementAddressDetailPrefix+token, address.Domain), nil
		}
	}
	return "", nil
}

// RegenerateStatementImportToken gives the statement source a new token,
// which ends the old import address at once.
func (self *Agent) RegenerateStatementImportToken(ctx context.Context, agentRow *models.Agent) (*models.AgentKnowledgeSource, error) {
	source, err := self.EnsureStatementSource(ctx, agentRow)
	if err != nil {
		return nil, err
	}
	token, err := newStatementToken()
	if err != nil {
		return nil, err
	}
	sealed, err := self.SealSecret(token)
	if err != nil {
		return nil, err
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if _, err := tx.LockAgentSource(agentRow.ID, source.ID); err != nil {
			return err
		}
		return tx.PutAgentSourceSecret(agentRow.ID, &models.AgentSourceSecret{SourceID: source.ID, Key: models.FinanceStatementTokenSecretKey, Value: sealed})
	}); err != nil {
		return nil, err
	}
	return source, nil
}

// IsStatementImportToken is the delivery hook's question: is this the
// token of the statement import address of the mailbox's owner? Only when
// the agent is on, finance is offered, the person's agent is active and
// their statement source is switched on. Asked inside the delivery
// transaction, so it reads and decides and nothing more.
func (self *Agent) IsStatementImportToken(tx db.Transaction, mailbox *models.Mailbox, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if mailbox == nil || token == "" {
		return false
	}
	// Statements need nothing of the operator's own, and ride on finance
	// being offered at all.
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled || !isFinanceOffered(configuration) {
		return false
	}
	agentRow, err := tx.GetAgentByUser(mailbox.UserID)
	if err != nil || agentRow == nil || !agentRow.Active() {
		return false
	}
	source, err := findStatementSource(tx, agentRow.ID)
	if err != nil || source == nil || !source.Enabled {
		return false
	}
	expected, err := self.statementSecret(tx, source, models.FinanceStatementTokenSecretKey)
	if err != nil || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(expected)), []byte(token)) == 1
}

// OnStatementDelivery is the delivery hook for a message that reached a
// statement import address, already filed: it queues the import, in the
// delivery transaction, and does nothing else there.
func (self *Agent) OnStatementDelivery(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail) {
	agentRow, err := tx.GetAgentByUser(mailbox.UserID)
	if err != nil || agentRow == nil {
		log.Warningf("cannot find the agent to import the statement in message %q: %v", mail.ID, err)
		return
	}
	if _, err := self.Enqueue(tx, models.AgentJobStatementImport, agentRow.ID, mailbox.ID, mail.ID); err != nil {
		log.Warningf("cannot queue the import of the statement in message %q: %s", mail.ID, err)
	}
}

// StatementFilesOf are the OFX files a message carries: every part named
// .ofx, .qfx or .qbo, and every part whose content is OFX whatever it is
// called or typed, since a phone sends a statement as
// application/octet-stream and some programs paste it into the body. A
// part is decoded and read; nothing in it is run or shown.
func StatementFilesOf(headers []string, body []byte) []*StatementFile {
	var files []*StatementFile
	_ = mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		mediaType, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			parameters = map[string]string{}
		}
		if strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") {
			return nil
		}
		fileName := parameters["name"]
		if _, dispositionParameters, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil && dispositionParameters["filename"] != "" {
			fileName = dispositionParameters["filename"]
		}
		fileName = strings.TrimSpace(mailparse.DecodeHeaderValue(fileName))
		content, isTooLarge, err := readStatementPart(header, reader)
		if err != nil {
			return nil
		}
		isNamedOFX := statementFileExtensions[strings.ToLower(filepath.Ext(fileName))]
		if !isNamedOFX && !ofx.IsOFX(content) {
			return nil
		}
		if fileName == "" {
			fileName = "statement.ofx"
		}
		files = append(files, &StatementFile{StatementFileName: fileName, Content: content, IsTooLarge: isTooLarge})
		return nil
	})
	return files
}

// readStatementPart decodes a part, reading at most one byte past the
// largest statement read, so a part too large is known to be.
func readStatementPart(header textproto.MIMEHeader, reader io.Reader) ([]byte, bool, error) {
	var decoded io.Reader
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		// Base64 is four bytes for every three, and the limit is on what
		// it decodes to.
		encoded, err := io.ReadAll(io.LimitReader(reader, int64(ofx.MaximumFileBytes)*4/3+8192))
		if err != nil {
			return nil, false, err
		}
		content, err := mailparse.DecodeBase64String(string(encoded))
		if err != nil {
			return nil, false, err
		}
		if len(content) > ofx.MaximumFileBytes {
			return content[:ofx.MaximumFileBytes], true, nil
		}
		return content, false, nil
	case "quoted-printable":
		decoded = quotedprintable.NewReader(reader)
	default:
		decoded = reader
	}
	content, err := io.ReadAll(io.LimitReader(decoded, int64(ofx.MaximumFileBytes)+1))
	if err != nil {
		return nil, false, err
	}
	if len(content) > ofx.MaximumFileBytes {
		return content[:ofx.MaximumFileBytes], true, nil
	}
	return content, false, nil
}

// ImportStatementFiles imports OFX files into the agent's statement
// source, records what came of it as the source's last import, and answers
// it. A file that cannot be read is named in the answer's error and the
// others are still imported; the error comes back as an error only when
// the database fails. mailId names the message the files came from, for
// the import job to know it has done this one already.
func (self *Agent) ImportStatementFiles(ctx context.Context, agentRow *models.Agent, owner *models.User, files []*StatementFile, origin models.StatementImportOrigin, mailId string) (*models.FinanceStatementImport, error) {
	source, err := self.EnsureStatementSource(ctx, agentRow)
	if err != nil {
		return nil, err
	}
	location := Location(owner)
	now := time.Now()
	today := now.In(location).Format(time.DateOnly)
	result := &models.FinanceStatementImport{
		ImportedAt: now, StatementImportOrigin: origin, StatementFileNames: []string{},
		FinanceAccountIDs: []string{}, FinanceAccountNames: []string{},
	}
	var failures []string
	type parsedFile struct {
		statementFileName string
		document          *ofx.Document
	}
	var parsedFiles []parsedFile
	for _, file := range files {
		result.StatementFileNames = append(result.StatementFileNames, file.StatementFileName)
		if file.IsTooLarge || len(file.Content) > ofx.MaximumFileBytes {
			failures = append(failures, fmt.Sprintf("%s is larger than %d MB, more than any statement", file.StatementFileName, ofx.MaximumFileBytes/(1024*1024)))
			continue
		}
		document, err := ofx.Parse(file.Content)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s could not be read: %s", file.StatementFileName, strings.TrimPrefix(err.Error(), "ofx: ")))
			continue
		}
		parsedFiles = append(parsedFiles, parsedFile{statementFileName: file.StatementFileName, document: document})
	}
	if len(files) == 0 {
		failures = append(failures, "there was no OFX file (.ofx, .qfx or .qbo) to import")
	}

	merged := &db.FinanceSyncApplied{FinanceTransactionIDsToCategorize: []string{}}
	isImported := false
	err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		locked, err := tx.LockAgentSource(agentRow.ID, source.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return errors.New("the statement source was deleted while importing")
		}
		source = locked
		cursor := map[string]any{}
		for key, value := range source.Cursor {
			cursor[key] = value
		}
		if !source.Enabled {
			failures = append([]string{errStatementImportOff.Error()}, failures...)
			parsedFiles = nil
		}
		var accountKey []byte
		if len(parsedFiles) > 0 {
			if accountKey, err = self.statementAccountKey(tx, source); err != nil {
				return err
			}
		}
		accountProviderIds := []string{}
		for _, parsed := range parsedFiles {
			for _, statement := range parsed.document.Statements {
				statementImport, err := finance.NewStatementImport(accountKey, parsed.document, statement)
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s could not be read: %s", parsed.statementFileName, strings.TrimPrefix(err.Error(), "finance: ")))
					continue
				}
				syncedOn := statementImport.BalanceOn
				if syncedOn == "" {
					syncedOn = today
				}
				applied, err := tx.ApplyFinanceSync(agentRow.ID, source.ID, statementImport.SyncResult, syncedOn)
				if err != nil {
					if errors.Is(err, db.ErrInvalidArguments) {
						failures = append(failures, fmt.Sprintf("%s could not be imported: %s", parsed.statementFileName, err))
						continue
					}
					return err
				}
				isImported = true
				added := len(statementImport.SyncResult.Added)
				result.AddedTransactionCount += applied.InsertedTransactionCount
				result.UpdatedTransactionCount += applied.WrittenTransactionCount - applied.InsertedTransactionCount
				result.SkippedTransactionCount += applied.SkippedTransactionCount
				result.UnchangedTransactionCount += added - applied.WrittenTransactionCount - applied.SkippedTransactionCount
				result.TransactionWithoutFITIDCount += statementImport.GeneratedIDCount
				merged.FinanceTransactionIDsToCategorize = append(merged.FinanceTransactionIDsToCategorize, applied.FinanceTransactionIDsToCategorize...)
				accountProviderIds = append(accountProviderIds, statementImport.SyncResult.Accounts[0].ProviderAccountID)
			}
		}
		if len(accountProviderIds) > 0 {
			accounts, err := tx.ListFinanceAccounts(agentRow.ID, source.ID)
			if err != nil {
				return err
			}
			isNamed := map[string]bool{}
			for _, providerAccountId := range accountProviderIds {
				for _, account := range accounts {
					if account.ProviderAccountID != providerAccountId || isNamed[account.ID] {
						continue
					}
					isNamed[account.ID] = true
					name := account.AccountName
					if account.AccountMask != "" {
						name += " ··" + account.AccountMask
					}
					result.FinanceAccountIDs = append(result.FinanceAccountIDs, account.ID)
					result.FinanceAccountNames = append(result.FinanceAccountNames, name)
				}
			}
		}
		result.ImportErrorMessage = strings.Join(failures, "; ")
		recorded, err := statementImportCursorValue(result)
		if err != nil {
			return err
		}
		cursor[models.FinanceCursorLastStatementImport] = recorded
		if mailId != "" {
			cursor[financeCursorLastStatementMailId] = mailId
		}
		return tx.MarkAgentSourceRun(source.ID, cursor, db.SourceCounts{}, false, result.ImportErrorMessage, nil)
	})
	if err != nil {
		return nil, err
	}
	if isImported {
		// What follows a sync follows an import: transfers across the
		// whole history the statements may reach back over, spending
		// rules, the provider category mapping, the categorize job and
		// budget alert candidates. Failing here leaves the rows written,
		// and the next import repeats it.
		run := &Run{Agent: agentRow, Owner: owner, settings: self.settings}
		if err := self.afterFinanceSync(ctx, run, source, merged, today, true, false); err != nil {
			log.Warningf("statements were imported for agent %q, but what follows an import failed: %s", agentRow.ID, err)
		}
	}
	return result, nil
}

// financeCursorLastStatementMailId is the cursor key that names the
// message the last mailed import came from, so a job run again after its
// notice waited does not import it again.
const financeCursorLastStatementMailId = "lastStatementMailId"

// statementImportCursorValue is an import as the cursor keeps it: the
// cursor is JSON, and reads back as maps.
func statementImportCursorValue(result *models.FinanceStatementImport) (map[string]any, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	value := map[string]any{}
	return value, json.Unmarshal(encoded, &value)
}

// LastStatementImport is the statement source's last import, or nil.
func LastStatementImport(source *models.AgentKnowledgeSource) *models.FinanceStatementImport {
	if source == nil {
		return nil
	}
	value, isRecorded := source.Cursor[models.FinanceCursorLastStatementImport]
	if !isRecorded || value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	result := &models.FinanceStatementImport{}
	if json.Unmarshal(encoded, result) != nil {
		return nil
	}
	return result
}

// runStatementImport is the job a message at the import address queues:
// read its OFX files, import them, and tell the person what came of it.
func (self *Agent) runStatementImport(ctx context.Context, run *Run) error {
	mailId := run.Job.SubjectID
	var mail *models.Mail
	var source *models.AgentKnowledgeSource
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if mail, err = tx.GetMail(mailId, nil); err != nil {
			return err
		}
		source, err = findStatementSource(tx, run.Agent.ID)
		return err
	}); err != nil {
		return err
	}
	if mail == nil || source == nil {
		return nil
	}
	result := LastStatementImport(source)
	if previous, _ := source.Cursor[financeCursorLastStatementMailId].(string); previous != mailId || result == nil {
		headers, body, err := run.Storage().Get(ctx, mailId)
		if err != nil {
			// The message is stored just after the delivery commits; a
			// job that runs first tries again on the ladder.
			return fmt.Errorf("the message with the statement cannot be read yet: %w", err)
		}
		if result, err = self.ImportStatementFiles(ctx, run.Agent, run.Owner, StatementFilesOf(headers, body), models.StatementImportOriginMail, mailId); err != nil {
			return err
		}
	}
	err := self.tellStatementImport(ctx, run, result)
	if errors.Is(err, errTurnRunning) {
		return &Deferral{Until: time.Now().Add(statementNoticeRetry), Reason: "a turn is running in the conversation the statement's notice goes to"}
	}
	return err
}

// StatementImportSummary is what an import did, in a sentence for the
// person.
func StatementImportSummary(result *models.FinanceStatementImport) string {
	if result == nil {
		return "Nothing was imported."
	}
	isImported := len(result.FinanceAccountNames) > 0
	if !isImported {
		reason := result.ImportErrorMessage
		if reason == "" {
			reason = "the statement held no account"
		}
		return "A statement reached your statement import address, but nothing was imported: " + reason + "."
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Imported your statement into %s: %s added", strings.Join(result.FinanceAccountNames, ", "), countOf(result.AddedTransactionCount, "transaction", "transactions"))
	if result.UpdatedTransactionCount > 0 {
		fmt.Fprintf(&builder, ", %d updated", result.UpdatedTransactionCount)
	}
	if result.UnchangedTransactionCount > 0 {
		fmt.Fprintf(&builder, ", %d already here", result.UnchangedTransactionCount)
	}
	if result.SkippedTransactionCount > 0 {
		fmt.Fprintf(&builder, ", %d skipped", result.SkippedTransactionCount)
	}
	builder.WriteString(".")
	if result.ImportErrorMessage != "" {
		builder.WriteString(" Not imported: " + result.ImportErrorMessage + ".")
	}
	return builder.String()
}

// countOf is a count with the word it counts, singular or plural.
func countOf(count int, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, plural)
}

// tellStatementImport says what a mailed statement's import did, in the
// person's main conversation, the way an alert is said: a line marking it
// as the agent's own and the sentence after it, written while no turn
// runs, then heard by the drawer as a turn's events would be. Not through
// the alert decision, which may stay quiet: the person sent the statement
// and is waiting to hear whether it arrived.
func (self *Agent) tellStatementImport(ctx context.Context, run *Run, result *models.FinanceStatementImport) error {
	summary := StatementImportSummary(result)
	checkIn := models.AlertMarker + " A statement arrived at " + personName(run.Owner) + "'s statement import address and was read; you told them what came of it. This is not the person speaking. What you said follows."
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		conversation, err = scheduleConversation(tx, run.Agent.ID, "")
		return err
	}); err != nil {
		return err
	}
	isWritten, err := self.whileNoTurnRuns(conversation.ID, func() error {
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: checkIn}); err != nil {
				return err
			}
			_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: summary})
			return err
		})
	})
	if err != nil {
		return err
	}
	if !isWritten {
		return errTurnRunning
	}
	runId := security.NewULID()
	at := time.Now()
	for sequence, event := range []Event{
		{Kind: EventAsked, Text: checkIn, Note: alertSurface},
		{Kind: EventMessage, Text: summary},
		{Kind: EventDone},
	} {
		event.RunID, event.ConversationID, event.Sequence, event.At = runId, conversation.ID, sequence, at
		self.publish(event, true)
	}
	return nil
}
