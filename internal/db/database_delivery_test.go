package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A delivery has no domain column of its own — it reaches its domain through
// the mail it belongs to. The queue view still has to say which domain each
// entry is for, so the query that scopes to a domain says so on the way out.
func TestListDeliveriesByDomainIDReportsTheDomain(t *testing.T) {
	t.Parallel()
	dbtest.RunTransaction(t, func(tx db.Transaction) {
		mail, err := tx.CreateMail(&models.Mail{
			DomainID: "domain-one",
			Sender:   "sender@example.com",
		}, nil)
		if err != nil {
			t.Fatalf("failed to create mail: %s", err)
		}
		other, err := tx.CreateMail(&models.Mail{
			DomainID: "domain-two",
			Sender:   "sender@example.com",
		}, nil)
		if err != nil {
			t.Fatalf("failed to create mail: %s", err)
		}

		for _, mailId := range []string{mail.ID, other.ID} {
			if _, err := tx.CreateDelivery(&models.Delivery{
				MailID:    mailId,
				Recipient: "recipient@example.com",
				Kind:      models.DeliveryKindExternal,
				Status:    models.DeliveryStatusQueued,
			}, nil); err != nil {
				t.Fatalf("failed to create delivery: %s", err)
			}
		}

		deliveries, err := tx.ListDeliveriesByDomainID("domain-one", nil)
		if err != nil {
			t.Fatalf("failed to list deliveries: %s", err)
		}
		if len(deliveries) != 1 {
			t.Fatalf("expected the deliveries of one domain only, got %d", len(deliveries))
		}
		if deliveries[0].DomainID != "domain-one" {
			t.Errorf("expected the delivery to report domain-one, got %q", deliveries[0].DomainID)
		}

		// The other query reaches deliveries through their mail, where the
		// caller already knows the domain, so it does not claim one.
		byMail, err := tx.ListDeliveries([]string{mail.ID}, nil)
		if err != nil {
			t.Fatalf("failed to list deliveries by mail: %s", err)
		}
		if len(byMail) != 1 {
			t.Fatalf("expected one delivery for the mail, got %d", len(byMail))
		}
		if byMail[0].DomainID != "" {
			t.Errorf("expected no domain claimed when listing by mail, got %q", byMail[0].DomainID)
		}
	})
}
