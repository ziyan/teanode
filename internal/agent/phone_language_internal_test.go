package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// A notification is written in the language of the person the phone belongs
// to: the one they chose, else the one their dashboard or app was last shown
// in, else English. The names in it are as they were written.
func TestANotificationIsInTheLanguageOfThePersonItIsFor(t *testing.T) {
	fixture := newPhoneFixture(t)
	for _, each := range []struct {
		locale, localeSeen   string
		title, body, opening string
	}{
		{"zh", "", "邀请", "Maria 邀请你加入「Home」", "打开"},
		{"", "ja", "招待", "Maria さんから「Home」に招待されました", "開く"},
		{"", "zh-hans", "邀请", "Maria 邀请你加入「Home」", "打开"},
		{"", "", "Invitation", "Maria invited you to Home", "Open"},
		{"fr", "", "Invitation", "Maria invited you to Home", "Open"},
	} {
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			if _, err := tx.UpdateUser(fixture.owner.ID, func(user *models.User) error {
				user.Locale, user.LocaleSeen = each.locale, each.localeSeen
				return nil
			}); err != nil {
				t.Fatalf("UpdateUser: %s", err)
			}
		})
		before := len(fixture.relay.to("sealed:a-registration"))
		fixture.worker.Phones().NotifyCircleInvitation(fixture.agent.ID, "Maria", "Home")
		fixture.worker.Phones().NotifyPhones(fixture.agent.ID, models.NotificationCircle, &push.Content{
			NotificationKind: models.NotificationCircle, Title: "Maria", Body: "Hello",
			Buttons: []push.Button{{Label: "Open", ButtonAction: "open"}, {Label: "Thursday", ButtonAction: "answer:1"}},
		})
		fixture.worker.waitGroup.Wait()
		sent := fixture.relay.to("sealed:a-registration")
		if len(sent) != before+2 {
			t.Fatalf("locale %q, seen %q: the phone was sent %d notifications, not two", each.locale, each.localeSeen, len(sent)-before)
		}
		for _, delivery := range sent[before:] {
			content := fixture.read(t, delivery)
			if len(content.Buttons) == 0 {
				if content.Title != each.title || content.Body != each.body {
					t.Errorf("locale %q, seen %q: the invitation says %q, %q", each.locale, each.localeSeen, content.Title, content.Body)
				}
				continue
			}
			if content.Body != "Hello" || content.Buttons[0].Label != each.opening || content.Buttons[1].Label != "Thursday" {
				t.Errorf("locale %q, seen %q: the message says %q with buttons %+v", each.locale, each.localeSeen, content.Body, content.Buttons)
			}
		}
	}
}
