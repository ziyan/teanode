package agent

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// The tax document token is made once and kept sealed; it opens the
// owner's mailbox and nobody else's; regenerated, the old one stops
// opening.
func TestTaxDocumentToken(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	token, err := fixture.worker.taxDocumentToken(t.Context(), fixture.agent, false)
	if err != nil || len(token) != 16 || strings.ToLower(token) != token {
		t.Fatalf("token %q %v", token, err)
	}
	again, err := fixture.worker.taxDocumentToken(t.Context(), fixture.agent, false)
	if err != nil || again != token {
		t.Fatalf("asked again, the same token: %q %v", again, err)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		stored, _ := tx.GetAgent(fixture.agent.ID)
		if stored.TaxDocumentToken == "" || strings.Contains(stored.TaxDocumentToken, token) {
			t.Errorf("the token is kept sealed: %q", stored.TaxDocumentToken)
		}
	})
	mailbox := &models.Mailbox{ID: "mailbox-one", UserID: fixture.owner.ID}
	isOpen := func(mailbox *models.Mailbox, candidate string) bool {
		var answer bool
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			answer = fixture.worker.IsTaxDocumentToken(tx, mailbox, candidate)
		})
		return answer
	}
	if !isOpen(mailbox, token) || !isOpen(mailbox, strings.ToUpper(token)) || isOpen(mailbox, "aaaaaaaaaaaaaaaa") || isOpen(mailbox, "") {
		t.Error("only the token opens")
	}
	if isOpen(&models.Mailbox{ID: "mailbox-two", UserID: "somebody-else"}, token) {
		t.Error("the token opens somebody else's mailbox")
	}
	renewed, err := fixture.worker.taxDocumentToken(t.Context(), fixture.agent, true)
	if err != nil || renewed == token || !isOpen(mailbox, renewed) || isOpen(mailbox, token) {
		t.Errorf("regenerated %q: the new one opens and the old one does not (%v)", renewed, err)
	}
}

// The job a mailed paystub queues copies each picture it carries into a
// tax document upload, queues its reading, and tells the person; run
// again, it copies nothing twice.
func TestTaxMailJob(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	fixture.worker.settings.Storage = store
	var mail *models.Mail
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if mail, err = tx.CreateMail(&models.Mail{From: "payroll@example.org", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
	})
	picture := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	headers := []string{"From: payroll@example.org", "MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="invented"`}
	body := strings.Join([]string{
		"--invented", "Content-Type: text/plain", "", "Your paystub is attached.", "--invented",
		`Content-Type: image/png; name="paystub.png"`, `Content-Disposition: attachment; filename="paystub.png"`, "Content-Transfer-Encoding: base64", "",
		base64.StdEncoding.EncodeToString(picture), "--invented--", "",
	}, "\r\n")
	if err := store.Put(t.Context(), mail.ID, headers, []byte(body)); err != nil {
		t.Fatalf("Put: %s", err)
	}
	run := fixture.run()
	run.Job = &models.AgentJob{ID: "job-tax-mail", AgentID: fixture.agent.ID, Kind: models.AgentJobTaxMail, SubjectID: mail.ID}
	for range 2 {
		if err := fixture.worker.runTaxMail(t.Context(), run); err != nil {
			var deferral *Deferral
			if errors.As(err, &deferral) {
				t.Fatalf("deferred with no turn running: %s", err)
			}
			t.Fatalf("runTaxMail: %s", err)
		}
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		attachments, err := tx.ListAgentAttachments(fixture.agent.ID, "")
		if err != nil || len(attachments) != 1 || attachments[0].Name != "paystub.png" || !attachments[0].IsTaxDocument {
			t.Fatalf("one tax document upload: %+v %v", attachments, err)
		}
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: fixture.agent.ID}, nil)
		reading := 0
		for _, job := range jobs {
			if job.Kind == models.AgentJobTaxDocument && job.SubjectID == attachments[0].ID {
				reading++
			}
		}
		if reading == 0 {
			t.Errorf("the upload's reading is queued: %+v", jobs)
		}
		conversation, err := scheduleConversation(tx, fixture.agent.ID, "")
		if err != nil {
			t.Fatalf("scheduleConversation: %s", err)
		}
		messages, _ := tx.ListAgentMessages(conversation.ID, nil)
		said := 0
		for _, message := range messages {
			if message.Role == "assistant" && strings.Contains(message.Content, "Received 1 file at your tax document address (paystub.png)") {
				said++
			}
		}
		if said == 0 {
			t.Errorf("the person is told what arrived: %+v", messages)
		}
	})
}

func TestTaxMailSummary(t *testing.T) {
	if summary := TaxMailSummary(nil, true); !strings.Contains(summary, "carries no PDF or picture") {
		t.Errorf("nothing to read: %q", summary)
	}
	files := []*models.AgentAttachment{{Name: "w2.pdf", ContentType: "application/pdf"}, {Name: "stub.png", ContentType: "image/png"}}
	if summary := TaxMailSummary(files, false); !strings.Contains(summary, "2 files") || !strings.Contains(summary, "w2.pdf, stub.png") || !strings.Contains(summary, "cannot read PDFs") {
		t.Errorf("two files, PDFs unreadable: %q", summary)
	}
}
