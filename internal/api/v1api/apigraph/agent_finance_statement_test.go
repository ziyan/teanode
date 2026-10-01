package apigraph

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// inventedStatement is an invented OFX card statement of two transactions.
const inventedStatement = `OFXHEADER:100
DATA:OFXSGML
VERSION:102

<OFX>
<SIGNONMSGSRSV1><SONRS><STATUS><CODE>0<SEVERITY>INFO</STATUS><DTSERVER>20260202093000[0:GMT]<LANGUAGE>ENG
<FI><ORG>Invented Card Issuer<FID>99999</FI></SONRS></SIGNONMSGSRSV1>
<CREDITCARDMSGSRSV1><CCSTMTTRNRS><TRNUID>0<STATUS><CODE>0<SEVERITY>INFO</STATUS>
<CCSTMTRS><CURDEF>USD<CCACCTFROM><ACCTID>11111a11-1aa1-1111-a11</CCACCTFROM>
<BANKTRANLIST><DTSTART>20260101[0:GMT]<DTEND>20260131[0:GMT]
<STMTTRN><TRNTYPE>DEBIT<DTPOSTED>20260105120000[0:GMT]<TRNAMT>-23.40<FITID>fit-invented-1<NAME>INVENTED COFFEE ROASTERS</STMTTRN>
<STMTTRN><TRNTYPE>PAYMENT<DTPOSTED>20260115120000[0:GMT]<TRNAMT>150.00<FITID>fit-invented-2<NAME>PAYMENT RECEIVED</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL><BALAMT>-311.25<DTASOF>20260131235959[0:GMT]</LEDGERBAL>
</CCSTMTRS></CCSTMTTRNRS></CREDITCARDMSGSRSV1>
</OFX>
`

// statementFixture is the finance fixture with a store for files and
// messages, and a mailbox at reader@example.com for the owner.
func newStatementFixture(test *testing.T) (*financeFixture, storage.Storage, *models.Mailbox) {
	test.Helper()
	fixture := newFinanceFixture(test, true)
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatalf("storage.Open: %s", err)
	}
	fixture.resolver.storage = store
	var mailbox *models.Mailbox
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Personal"}); err != nil {
			test.Fatal(err)
		}
		domain, err := tx.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := tx.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "^reader$", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			test.Fatal(err)
		}
	})
	return fixture, store, mailbox
}

// asReader runs one step as the owner, allowed to read their mail too.
func (self *financeFixture) asReader(test *testing.T, run func(ctx context.Context, tx db.Transaction)) {
	test.Helper()
	principal := &api.Principal{User: self.owner, Permissions: models.NewEffectivePermissions([]models.Grant{
		{Permission: models.PermissionAgentUse}, {Permission: models.PermissionMailRead},
	})}
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx), tx)
	})
}

// The import address is the person's own address with "+statements-" and
// a token; it is the same each time it is asked for, a new one once it is
// regenerated, and the source behind it is listed only once it holds an
// account.
func TestStatementImportAddressFromTheAPI(test *testing.T) {
	fixture, _, _ := newStatementFixture(test)
	var first, second, regenerated *StatementImportView
	var err error
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if first, err = fixture.resolver.StatementImport(ctx); err != nil {
			test.Fatalf("StatementImport: %s", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if second, err = fixture.resolver.StatementImport(ctx); err != nil {
			test.Fatalf("StatementImport: %s", err)
		}
	})
	if !strings.HasPrefix(first.ImportAddress, "reader+statements-") || !strings.HasSuffix(first.ImportAddress, "@example.com") ||
		first.ImportAddress != second.ImportAddress || first.SourceID != second.SourceID || !first.IsEnabled || first.LastStatementImport != nil {
		test.Errorf("first %+v, second %+v", first, second)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if regenerated, err = fixture.resolver.RegenerateStatementImportAddress(ctx); err != nil {
			test.Fatalf("RegenerateStatementImportAddress: %s", err)
		}
		sources, err := fixture.resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 0 {
			test.Errorf("an empty statement source is listed: %v %v", sources, err)
		}
	})
	if regenerated.ImportAddress == first.ImportAddress || regenerated.SourceID != first.SourceID {
		test.Errorf("regenerated %+v", regenerated)
	}
}

// An uploaded file is imported once, and again adds nothing; somebody
// else's upload is not found.
func TestImportStatementFromAnUpload(test *testing.T) {
	fixture, store, _ := newStatementFixture(test)
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: fixture.ownerAgent.ID, Name: "Invented Card Transactions.ofx", ContentType: "application/octet-stream", Size: int64(len(inventedStatement)),
		}); err != nil {
			test.Fatal(err)
		}
	})
	if err := store.PutFile(context.Background(), attachment.ID, []byte(inventedStatement)); err != nil {
		test.Fatal(err)
	}
	importOnce := func() *models.FinanceStatementImport {
		var imported *models.FinanceStatementImport
		fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
			var err error
			if imported, err = fixture.resolver.ImportStatement(ctx, ImportStatementArguments{AgentAttachmentID: attachment.ID}); err != nil {
				test.Fatalf("ImportStatement: %s", err)
			}
		})
		return imported
	}
	first := importOnce()
	if first.AddedTransactionCount != 2 || len(first.FinanceAccountNames) != 1 || first.StatementImportOrigin != models.StatementImportOriginUpload {
		test.Errorf("first %+v", first)
	}
	again := importOnce()
	if again.AddedTransactionCount != 0 || again.UnchangedTransactionCount != 2 {
		test.Errorf("again %+v", again)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		sources, err := fixture.resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 1 || sources[0].ProviderKind != "statement" || len(sources[0].FinanceAccounts) != 1 {
			test.Errorf("sources %+v %v", sources, err)
		}
		view, err := fixture.resolver.StatementImport(ctx)
		if err != nil || view.LastStatementImport == nil || view.LastStatementImport.UnchangedTransactionCount != 2 {
			test.Errorf("view %+v %v", view, err)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.ImportStatement(ctx, ImportStatementArguments{AgentAttachmentID: attachment.ID}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("somebody else's upload answered %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.ImportStatement(ctx, ImportStatementArguments{}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("no file answered %v", err)
		}
		if _, err := fixture.resolver.ImportStatement(ctx, ImportStatementArguments{AgentAttachmentID: attachment.ID, MailboxItemID: "item-invented"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("two files answered %v", err)
		}
	})
}

// A message in the person's mailbox has its OFX attachment imported; a
// message without one is refused, saying so.
func TestImportStatementFromAMessage(test *testing.T) {
	fixture, store, mailbox := newStatementFixture(test)
	headers := []string{"From: reader@example.com", "MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="invented"`}
	body := strings.Join([]string{
		"--invented", "Content-Type: text/plain", "", "", "--invented",
		`Content-Type: application/octet-stream; name="Invented Card Transactions.ofx"`, "Content-Transfer-Encoding: base64", "",
		base64.StdEncoding.EncodeToString([]byte(inventedStatement)), "--invented--", "",
	}, "\r\n")
	var withStatement, without *models.MailboxItem
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil {
			test.Fatal(err)
		}
		for index, item := range []**models.MailboxItem{&withStatement, &without} {
			mail, err := tx.CreateMail(&models.Mail{From: "reader@example.com", ReceivedAt: time.Now(), Kind: models.MailKindIncoming}, nil)
			if err != nil {
				test.Fatal(err)
			}
			if *item, err = tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				test.Fatal(err)
			}
			content := []byte(body)
			if index == 1 {
				content = []byte("nothing attached")
			}
			if err := store.Put(context.Background(), mail.ID, headers, content); err != nil {
				test.Fatal(err)
			}
		}
	})
	fixture.asReader(test, func(ctx context.Context, tx db.Transaction) {
		imported, err := fixture.resolver.ImportStatement(ctx, ImportStatementArguments{MailboxItemID: withStatement.ID})
		if err != nil || imported.AddedTransactionCount != 2 || imported.StatementImportOrigin != models.StatementImportOriginMessage {
			test.Errorf("imported %+v %v", imported, err)
		}
	})
	fixture.asReader(test, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.ImportStatement(ctx, ImportStatementArguments{MailboxItemID: without.ID}); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "no OFX file") {
			test.Errorf("a message without a statement answered %v", err)
		}
	})
}
