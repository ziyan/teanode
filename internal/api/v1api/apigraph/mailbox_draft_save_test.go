package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type draftSaveStorage struct {
	storage.Storage
	putError error
}

func (self *draftSaveStorage) Put(ctx context.Context, id string, headers []string, body []byte) error {
	if self.putError != nil {
		return self.putError
	}
	return self.Storage.Put(ctx, id, headers, body)
}

func TestDraftSaveRollsBackCompositionAndReplacementTogether(test *testing.T) {
	for _, failurePoint := range []string{"storage", "replacement", "none", "upload"} {
		test.Run(failurePoint, func(test *testing.T) {
			database, resolver, principal, sendArguments, _ := submissionAPIFixture(test)
			principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailWrite}})
			spool, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() { _ = spool.Close() })
			resolver.storage = &draftSaveStorage{Storage: spool}
			resolver.mailer, err = mailer.New(database, resolver.config, nil, nil)
			if err != nil {
				test.Fatal(err)
			}
			originalMailId := ""
			domainId := ""
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				item, err := transaction.GetItem(sendArguments.Message.DraftItemID)
				if err != nil {
					test.Fatal(err)
				}
				originalMailId = item.MailID
				if err := spool.Put(test.Context(), originalMailId, []string{"Subject: Original\r\n"}, []byte("Original body\r\n")); err != nil {
					test.Fatal(err)
				}
				domain, err := transaction.GetDomainByName("example.com")
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.UpdateDomain(domain.ID, func(domain *models.Domain) error { domain.LinkHost = "images.example.com"; return nil }); err != nil {
					test.Fatal(err)
				}
				domainId = domain.ID
			})
			if _, err := database.CreateMedia(&models.Media{ID: "fixtureimage", DomainID: domainId, Filename: "fixture.png", ContentType: "image/png"}); err != nil {
				test.Fatal(err)
			}
			if failurePoint == "storage" {
				resolver.storage.(*draftSaveStorage).putError = errors.New("fixture storage unavailable")
			}
			if failurePoint == "replacement" {
				dbtest.Exec(test, database, `CREATE FUNCTION refuse_draft_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture replacement refused'; END $$`)
				dbtest.Exec(test, database, `CREATE TRIGGER refuse_draft_delete BEFORE DELETE ON mailbox_item FOR EACH ROW EXECUTE FUNCTION refuse_draft_delete()`)
			}
			arguments := SaveMailboxDraftArguments{MailboxID: sendArguments.MailboxID, Message: sendArguments.Message}
			arguments.Message.HTMLContent = `<img src="/media/fixtureimage">`
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
				var item *models.MailboxItem
				var err error
				if failurePoint == "upload" {
					owned, lookupErr := transaction.GetMailbox(arguments.MailboxID)
					if lookupErr != nil {
						test.Fatal(lookupErr)
					}
					item, err = resolver.saveDraft(ctx, transaction, owned, &arguments.Message, []*mailparse.Attachment{{Filename: "fixture.txt", ContentType: "text/plain", Content: []byte("Uploaded fixture")}})
				} else {
					item, err = resolver.SaveMailboxDraft(ctx, arguments)
				}
				if failurePoint == "none" || failurePoint == "upload" {
					if err != nil || item == nil {
						test.Fatalf("save=%+v, %v", item, err)
					}
					if failurePoint == "upload" {
						headers, body, err := spool.Get(test.Context(), item.MailID)
						if err != nil || !strings.Contains(strings.Join(headers, ""), "multipart/mixed") || !strings.Contains(string(body), "fixture.txt") {
							test.Fatalf("uploaded draft=%q, %v", body, err)
						}
					}
				} else if err == nil || item != nil {
					test.Fatalf("failed save=%+v, %v", item, err)
				}
				// The caller may handle the error and commit unrelated work.
			})
			mailCount, linkCount := "1", "0"
			if failurePoint == "none" || failurePoint == "upload" {
				mailCount, linkCount = "2", "1"
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`); count != mailCount {
				test.Fatalf("mail count=%s, want %s", count, mailCount)
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM media_link`); count != linkCount {
				test.Fatalf("media links=%s, want %s", count, linkCount)
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM mailbox_item`); count != "1" {
				test.Fatalf("draft count=%s", count)
			}
			_, body, err := spool.Get(test.Context(), originalMailId)
			if err != nil || string(body) != "Original body\r\n" {
				test.Fatalf("original bytes=%q, %v", body, err)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				item, err := transaction.GetItem(sendArguments.Message.DraftItemID)
				if err != nil || (item != nil) != (failurePoint == "storage" || failurePoint == "replacement") {
					test.Fatalf("original item=%+v, %v", item, err)
				}
			})
		})
	}
}
