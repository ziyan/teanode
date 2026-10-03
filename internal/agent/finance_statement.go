package agent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"path/filepath"
	"sort"
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

	// statementMailImportsKept is how many mailed imports the statement
	// source remembers by the message they came from: enough for every
	// job whose notice is still waiting, and small enough to keep in the
	// cursor.
	statementMailImportsKept = 32
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

// ErrNotStatementAccount refuses renaming or deleting a finance account a
// provider reports: its next sync would bring it back as it was.
var ErrNotStatementAccount = errors.New("only an account from imported statements can be renamed or deleted; " +
	"an account a provider reports comes back as it was with the next sync, so switch off or delete its finance source instead")

// lockStatementAccount is one of the agent's finance accounts with its
// statement source locked, as an import locks it, so a rename or a delete
// and an import of the same account take turns. db.ErrNotFound when the
// agent has no such account, ErrNotStatementAccount when a provider
// reports it.
func lockStatementAccount(tx db.Transaction, agentId, financeAccountId string) (*models.FinanceAccount, error) {
	account, err := tx.GetFinanceAccount(agentId, strings.TrimSpace(financeAccountId))
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, db.ErrNotFound
	}
	source, err := tx.LockAgentSource(agentId, account.SourceID)
	if err != nil {
		return nil, err
	}
	if !IsStatementSource(source) {
		return nil, ErrNotStatementAccount
	}
	return account, nil
}

// RenameStatementAccount gives an account of imported statements the
// person's own name, which later imports keep.
func (self *Agent) RenameStatementAccount(ctx context.Context, agentRow *models.Agent, financeAccountId, accountName string) (*models.FinanceAccount, error) {
	var renamed *models.FinanceAccount
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		account, err := lockStatementAccount(tx, agentRow.ID, financeAccountId)
		if err != nil {
			return err
		}
		renamed, err = tx.RenameFinanceAccount(agentRow.ID, account.ID, accountName)
		return err
	})
	return renamed, err
}

// DeleteStatementAccount deletes an account of imported statements with
// its transactions and the assets that value it. A transfer one of its
// transactions was paired in is judged again in the same transaction:
// the other side is paired with what remains, or given the provider
// category mapping and the spending rules again, and left to the
// categorize job when nothing places it.
func (self *Agent) DeleteStatementAccount(ctx context.Context, agentRow *models.Agent, financeAccountId string) (*db.FinanceAccountDeleted, error) {
	var deleted *db.FinanceAccountDeleted
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		account, err := lockStatementAccount(tx, agentRow.ID, financeAccountId)
		if err != nil {
			return err
		}
		if deleted, err = tx.DeleteFinanceAccount(agentRow.ID, account.ID); err != nil {
			return err
		}
		if len(deleted.ReleasedTransactionIDs) == 0 {
			return nil
		}
		since := deleted.EarliestPostedOn
		if earliest, err := time.Parse(time.DateOnly, since); err == nil {
			since = earliest.AddDate(0, 0, -financeTransferWindowDays).Format(time.DateOnly)
		}
		if _, err := tx.DetectFinanceTransfers(agentRow.ID, "", since); err != nil {
			return err
		}
		if _, err := tx.ApplySpendingRules(agentRow.ID); err != nil {
			return err
		}
		if err := applyProviderCategoryMapping(tx, agentRow.ID, deleted.ReleasedTransactionIDs); err != nil {
			return err
		}
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(agentRow.ID, 1)
		if err != nil || len(uncategorized) == 0 {
			return err
		}
		_, err = self.Enqueue(tx, models.AgentJobCategorize, agentRow.ID, "", agentRow.ID)
		return err
	})
	return deleted, err
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
		// Decoded as it is read, so the limit is on what it decodes to,
		// whatever the line breaks it is laid out with add.
		decoded = base64.NewDecoder(base64.StdEncoding, whitespaceDroppingReader{reader: reader})
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

// whitespaceDroppingReader passes on what it reads without the spaces,
// tabs and line breaks base64 in a message is laid out with, which the
// base64 decoder would refuse.
type whitespaceDroppingReader struct {
	reader io.Reader
}

func (self whitespaceDroppingReader) Read(buffer []byte) (int, error) {
	for {
		count, err := self.reader.Read(buffer)
		kept := 0
		for _, character := range buffer[:count] {
			switch character {
			case ' ', '\t', '\r', '\n', '\v', '\f':
				continue
			}
			buffer[kept] = character
			kept++
		}
		if kept > 0 || err != nil {
			return kept, err
		}
	}
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
	result := &models.FinanceStatementImport{
		ImportedAt: time.Now(), StatementImportOrigin: origin, StatementFileNames: []string{},
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
		document, err := ofx.Parse(ctx, file.Content)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s could not be read: %s", file.StatementFileName, strings.TrimPrefix(err.Error(), "ofx: ")))
			continue
		}
		parsedFiles = append(parsedFiles, parsedFile{statementFileName: file.StatementFileName, document: document})
	}
	if len(files) == 0 {
		failures = append(failures, "there was no OFX file (.ofx, .qfx or .qbo) to import")
	}
	var builds []statementBuild
	for _, parsed := range parsedFiles {
		for _, statement := range parsed.document.Statements {
			document := parsed.document
			builds = append(builds, statementBuild{statementName: parsed.statementFileName, build: func(accountKey []byte, existingAccounts []finance.ExistingStatementAccount) (*finance.StatementImport, error) {
				return finance.NewStatementImport(accountKey, document, statement, existingAccounts)
			}})
		}
	}
	return self.importStatements(ctx, agentRow, owner, source, result, failures, builds, mailId)
}

// ImportTransactionRows imports one account's transaction rows, checked
// already (finance.CheckTransactionRows), into the agent's statement
// source, the way a statement is imported, and records it as the source's
// last import.
func (self *Agent) ImportTransactionRows(ctx context.Context, agentRow *models.Agent, owner *models.User, check *finance.TransactionRowsCheck) (*models.FinanceStatementImport, error) {
	source, err := self.EnsureStatementSource(ctx, agentRow)
	if err != nil {
		return nil, err
	}
	result := &models.FinanceStatementImport{
		ImportedAt: time.Now(), StatementImportOrigin: models.StatementImportOriginTransactionRows, StatementFileNames: []string{},
		FinanceAccountIDs: []string{}, FinanceAccountNames: []string{},
	}
	location := Location(owner)
	builds := []statementBuild{{statementName: "the transactions", build: func(accountKey []byte, existingAccounts []finance.ExistingStatementAccount) (*finance.StatementImport, error) {
		return finance.NewTransactionRowsImport(accountKey, check, location, existingAccounts)
	}}}
	return self.importStatements(ctx, agentRow, owner, source, result, nil, builds, "")
}

// statementBuild is one statement to import: what to call it when it
// fails, and how it becomes what a sync writes once the statement source's
// account key and accounts are read, inside the transaction that imports.
type statementBuild struct {
	statementName string
	build         func(accountKey []byte, existingAccounts []finance.ExistingStatementAccount) (*finance.StatementImport, error)
}

// importStatements writes each statement into the statement source under
// its lock, records the import in the source's cursor (and by mailId, for
// a mailed one), and runs what follows a sync.
func (self *Agent) importStatements(ctx context.Context, agentRow *models.Agent, owner *models.User, source *models.AgentKnowledgeSource,
	result *models.FinanceStatementImport, failures []string, builds []statementBuild, mailId string) (*models.FinanceStatementImport, error) {
	today := time.Now().In(Location(owner)).Format(time.DateOnly)
	merged := &db.FinanceSyncApplied{FinanceTransactionIDsToCategorize: []string{}}
	isImported := false
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
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
			builds = nil
		}
		var accountKey []byte
		var existingAccounts []finance.ExistingStatementAccount
		if len(builds) > 0 {
			if accountKey, err = self.statementAccountKey(tx, source); err != nil {
				return err
			}
			accounts, err := tx.ListFinanceAccounts(agentRow.ID, source.ID)
			if err != nil {
				return err
			}
			for _, account := range accounts {
				existingAccounts = append(existingAccounts, finance.ExistingStatementAccount{ProviderAccountID: account.ProviderAccountID, ProviderMetadata: account.ProviderMetadata})
			}
		}
		accountProviderIds := []string{}
		for _, pending := range builds {
			statementImport, err := pending.build(accountKey, existingAccounts)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s could not be read: %s", pending.statementName, strings.TrimPrefix(err.Error(), "finance: ")))
				continue
			}
			syncedOn := statementImport.BalanceOn
			if syncedOn == "" {
				syncedOn = today
			}
			applied, err := tx.ApplyFinanceSync(agentRow.ID, source.ID, statementImport.SyncResult, syncedOn)
			if err != nil {
				if errors.Is(err, db.ErrInvalidArguments) {
					failures = append(failures, fmt.Sprintf("%s could not be imported: %s", pending.statementName, err))
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
			cursor[financeCursorStatementMailImports] = withStatementMailImport(cursor[financeCursorStatementMailImports], mailId, recorded)
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

// financeCursorStatementMailImports is the cursor key that keeps mailed
// imports by the message they came from, written in the transaction that
// imports. A job run again, after its notice waited or after it failed
// past the import, tells the import it did rather than doing it again,
// and tells its own even when an upload or another message was imported
// in between.
const financeCursorStatementMailImports = "statementMailImports"

// withStatementMailImport is the cursor's mailed imports with one more,
// the oldest dropped past statementMailImportsKept.
func withStatementMailImport(value any, mailId string, recorded map[string]any) map[string]any {
	imports := map[string]any{}
	if previous, isMap := value.(map[string]any); isMap {
		for key, entry := range previous {
			imports[key] = entry
		}
	}
	imports[mailId] = recorded
	if len(imports) <= statementMailImportsKept {
		return imports
	}
	// Oldest first, by when each was imported.
	mailIds := make([]string, 0, len(imports))
	for key := range imports {
		mailIds = append(mailIds, key)
	}
	importedAt := func(key string) time.Time {
		entry, _ := imports[key].(map[string]any)
		text, _ := entry["importedAt"].(string)
		moment, _ := time.Parse(time.RFC3339Nano, text)
		return moment
	}
	sort.Slice(mailIds, func(left, right int) bool { return importedAt(mailIds[left]).Before(importedAt(mailIds[right])) })
	for _, key := range mailIds[:len(mailIds)-statementMailImportsKept] {
		delete(imports, key)
	}
	return imports
}

// statementMailImport is the import the statement source recorded for one
// message, or nil.
func statementMailImport(source *models.AgentKnowledgeSource, mailId string) *models.FinanceStatementImport {
	imports, _ := source.Cursor[financeCursorStatementMailImports].(map[string]any)
	return decodeStatementImport(imports[mailId])
}

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
	return decodeStatementImport(source.Cursor[models.FinanceCursorLastStatementImport])
}

// decodeStatementImport is an import as the cursor keeps it, read back;
// nil for nothing recorded.
func decodeStatementImport(value any) *models.FinanceStatementImport {
	if value == nil {
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
	result := statementMailImport(source, mailId)
	if result == nil {
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
