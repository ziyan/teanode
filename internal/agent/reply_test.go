package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// fakeExchange climbs no ladder of its own: it refuses what the test says
// to refuse, so that the agent's rungs above it are what is tested.
type fakeExchange struct {
	mx.Exchange
	refusal string
}

func (self *fakeExchange) RunInsightRules(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail, insight *models.MailInsight) error {
	return nil
}

func (self *fakeExchange) AutoReplyRefusal(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail, now time.Time, quiet time.Duration) (string, error) {
	return self.refusal, nil
}

// fakeMailer composes without a domain key and records what was sent.
type fakeMailer struct {
	mutex sync.Mutex
	sent  []*mailer.Message
}

func (self *fakeMailer) Close() error { return nil }
func (self *fakeMailer) Send(ctx context.Context, envelope *mailparse.Envelope, message *mailer.Message) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	envelope.ID = fmt.Sprintf("envelope-%d", len(self.sent)+1)
	self.sent = append(self.sent, message)
	return nil
}
func (self *fakeMailer) Compose(ctx context.Context, message *mailer.Message) (*mailer.Composed, error) {
	headers := append([]string{
		"Message-ID: <draft-" + fmt.Sprint(time.Now().UnixNano()) + "@example.com>",
		"From: " + message.From,
		"To: " + strings.Join(message.To, ", "),
		"Subject: " + message.Subject,
		"Content-Type: text/plain; charset=utf-8",
	}, message.Headers...)
	return &mailer.Composed{ID: "composed", Headers: headers, Body: []byte(message.Text)}, nil
}
func (self *fakeMailer) SendMail(ctx context.Context, envelope *mailparse.Envelope, templateName string, locale string, variables map[string]interface{}) error {
	return nil
}

// A message from a known contact that needs a reply is answered: the reply
// is held as a draft in the conversation and sent when the hold ends, with
// the headers that stop anything answering it back. Along the way, every
// rung of the ladder that refuses leaves its reason.
func TestAutoReplyIsHeldThenSent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		prompt := body.Messages[len(body.Messages)-1].Content
		answer := `{\"category\":\"personal\",\"priority\":\"high\",\"needs_reply\":true,\"research\":false,\"summary\":\"Maria asks about Thursday.\",\"action_items\":[]}`
		if strings.Contains(prompt, "<guidance>") {
			if strings.Contains(prompt, "money") && strings.Contains(prompt, "Please wire") {
				answer = `{\"reply\": null, \"reason\": \"the message asks for money\"}`
			} else {
				answer = `{\"reply\": \"Thursday works, see you at three.\", \"reason\": null}`
			}
		}
		_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":"%s"},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":10}}`, answer)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	exchange := &fakeExchange{}
	sender := &fakeMailer{}
	worker := agent.New(&agent.Settings{
		Database:      database,
		Storage:       store,
		Registry:      registry,
		Exchange:      exchange,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test",
		Tick:          time.Hour,
	})
	worker.SetMailer(sender)

	var owner *models.User
	var mailbox *models.Mailbox
	var inbox *models.MailboxFolder
	var found *models.Agent
	deliver := func(tx db.Transaction, from, subject, body string, headers ...string) *models.Mail {
		mail := &models.Mail{Subject: subject, Kind: models.MailKindIncoming, Sender: from, Recipients: []string{"alice@example.com"}, ReceivedAt: time.Now(), MessageID: fmt.Sprintf("<%d@example.net>", time.Now().UnixNano()),
			Headers: append([]string{"From: " + from, "To: alice@example.com", "Subject: " + subject}, headers...)}
		mail, err := tx.CreateMail(mail, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if err := store.Put(context.Background(), mail.ID, []string{"From: " + from, "Content-Type: text/plain"}, []byte(body)); err != nil {
			t.Fatalf("store.Put: %s", err)
		}
		if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		if err := tx.TouchContact(mailbox.ID, from, "", mail.ReceivedAt); err != nil {
			t.Fatalf("TouchContact: %s", err)
		}
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
		return mail
	}
	run := func() {
		if err := worker.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		worker.Wait()
	}
	repliesTo := func(mailId string) *models.AgentReply {
		var reply *models.AgentReply
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			found, err := tx.ListAgentReplies(&db.AgentReplyFilter{MailID: mailId}, nil)
			if err != nil {
				t.Fatalf("ListAgentReplies: %s", err)
			}
			if len(found) > 0 {
				reply = found[0]
			}
		})
		return reply
	}

	var first *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		domain, err := tx.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true,
			Triage:    &models.AgentTriage{Enabled: true},
			AutoReply: &models.AgentAutoReply{Enabled: true, Guidance: "Say yes to meetings on Thursday. Never agree to pay money.", Scope: "known", HoldMinutes: 10}}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if _, err := tx.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "alice", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
		if mailbox, err = tx.GetMailbox(mailbox.ID); err != nil {
			t.Fatalf("GetMailbox: %s", err)
		}
		if inbox, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox); err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind: %v %s", inbox, err)
		}
		// A stranger first: sorted, needs a reply, but not a contact.
		first = deliver(tx, "maria@example.net", "Thursday?", "Can you do Thursday at 3?")
	})
	run() // triage
	run() // reply
	if reply := repliesTo(first.ID); reply == nil || reply.Status != models.AgentReplyRefused || reply.Reason != "the sender is not a contact" {
		t.Fatalf("a stranger is not answered under scope known: %+v", reply)
	}

	// Now a contact: the second message from the same address.
	var second *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		second = deliver(tx, "maria@example.net", "Re: Thursday?", "So, Thursday at 3?")
	})
	run()
	run()
	reply := repliesTo(second.ID)
	if reply == nil || reply.Status != models.AgentReplyHeld || reply.DraftItemID == "" || reply.SendAfter == nil || reply.Text != "Thursday works, see you at three." || reply.From != "alice@example.com" || reply.To != "maria@example.net" {
		t.Fatalf("the reply should be held: %+v", reply)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		draft, err := tx.GetItem(reply.DraftItemID)
		if err != nil || draft == nil || !draft.Draft {
			t.Fatalf("the held reply should be a draft item: %v %s", draft, err)
		}
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobSend}}, nil)
		if len(jobs) != 1 || jobs[0].SubjectID != reply.ID || jobs[0].NotBefore == nil {
			t.Fatalf("a send job for the hold was expected: %+v", jobs)
		}
	})
	// The hold has not ended: nothing goes.
	run()
	if len(sender.sent) != 0 {
		t.Fatal("nothing should be sent before the hold ends")
	}
	// After the hold: sent, as the person, marked automatic, threaded.
	if err := worker.TickAt(context.Background(), time.Now().Add(11*time.Minute)); err != nil {
		t.Fatalf("TickAt: %s", err)
	}
	worker.Wait()
	if len(sender.sent) != 1 {
		t.Fatalf("the reply should have been sent once, got %d", len(sender.sent))
	}
	sent := sender.sent[0]
	if sent.From != "alice@example.com" || sent.To[0] != "maria@example.net" || sent.Subject != "Re: Thursday?" || sent.Text != reply.Text {
		t.Fatalf("sent %+v", sent)
	}
	joined := strings.Join(sent.Headers, "\n")
	for _, want := range []string{"Auto-Submitted: auto-replied", "In-Reply-To: " + second.MessageID, "References: "} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the sent reply lacks %q in %q", want, joined)
		}
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		after, _ := tx.GetAgentReply(reply.ID)
		if after == nil || after.Status != models.AgentReplySent || after.SentAt == nil || after.DraftItemID != "" {
			t.Fatalf("after sending: %+v", after)
		}
		if draft, _ := tx.GetItem(reply.DraftItemID); draft != nil {
			t.Fatal("the draft should be gone once sent")
		}
		items, _ := tx.ListItemsByMail(second.ID)
		if len(items) != 1 || !items[0].Answered {
			t.Fatalf("the answered message should be flagged: %+v", items)
		}
	})

	// The model declines what the guidance forbids.
	var third *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		third = deliver(tx, "maria@example.net", "Money", "Please wire me 500 euros for the boat.")
	})
	run()
	run()
	if reply := repliesTo(third.ID); reply == nil || reply.Status != models.AgentReplyRefused || !strings.Contains(reply.Reason, "asks for money") {
		t.Fatalf("the agent should have declined: %+v", reply)
	}

	// And the out-of-office ladder is asked first: list mail is never
	// answered, whatever the policy says.
	exchange.refusal = "the message came through a mailing list"
	var fourth *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		fourth = deliver(tx, "maria@example.net", "[club] Thursday", "Thursday?", "List-Id: <club.example.net>")
	})
	run()
	run()
	if reply := repliesTo(fourth.ID); reply == nil || reply.Status != models.AgentReplyRefused || reply.Reason != "the message came through a mailing list" {
		t.Fatalf("list mail should be refused with the ladder's reason: %+v", reply)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("only the one reply should ever have gone, got %d", len(sender.sent))
	}
}
