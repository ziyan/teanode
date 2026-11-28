package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A screenshot handed in is queued to be read as a statement, and the list
// of imports shows it waiting, by its name; an OFX file is refused, since
// it needs no reading, and somebody else's upload is not found.
func TestReadStatementQueuesAScreenshotThatTheImportsList(test *testing.T) {
	fixture, _, _ := newStatementFixture(test)
	upload := func(name, contentType string) *models.AgentAttachment {
		var attachment *models.AgentAttachment
		dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
			var err error
			if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: name, ContentType: contentType, Size: 10}); err != nil {
				test.Fatal(err)
			}
		})
		return attachment
	}
	screenshot := upload("picture-1.jpg", "image/jpeg")
	statement := upload("Invented Card Transactions.ofx", "application/octet-stream")
	var reading *StatementReadingView
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if reading, err = fixture.resolver.ReadStatement(ctx, ReadStatementArguments{AgentAttachmentID: screenshot.ID}); err != nil {
			test.Fatalf("ReadStatement: %s", err)
		}
		if _, err := fixture.resolver.ReadStatement(ctx, ReadStatementArguments{AgentAttachmentID: statement.ID}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("an OFX file answered %v", err)
		}
	})
	if reading.AgentAttachmentID != screenshot.ID || reading.AgentJobID == "" {
		test.Fatalf("reading %+v", reading)
	}
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.ReadStatement(ctx, ReadStatementArguments{AgentAttachmentID: screenshot.ID}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("somebody else's upload answered %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		page, err := fixture.resolver.FinanceImports(ctx, FinanceImportsArguments{})
		if err != nil {
			test.Fatalf("FinanceImports: %s", err)
		}
		if page.TotalCount != 1 || len(page.FinanceImports) != 1 || page.HasMore {
			test.Fatalf("page %+v", page)
		}
		listed := page.FinanceImports[0]
		if listed.AgentJobID != reading.AgentJobID || listed.FinanceImportKind != FinanceImportStatement || listed.FinanceImportSource != FinanceImportSourceUpload ||
			listed.AgentJobStatus != models.AgentJobQueued || listed.FileName != "picture-1.jpg" || listed.OutcomeText != "" ||
			listed.AgentAttachmentID != screenshot.ID || listed.AttachmentContentType != "image/jpeg" || listed.MailboxItemID != "" {
			test.Errorf("listed %+v", listed)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		page, err := fixture.resolver.FinanceImports(ctx, FinanceImportsArguments{})
		if err != nil || page.TotalCount != 0 {
			test.Errorf("somebody else sees %+v %v", page, err)
		}
	})
}
