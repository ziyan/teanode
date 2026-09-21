package mx

import (
	"context"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/clamav"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/periodic"
	"github.com/ziyan/teanode/internal/util/resolver"
)

type exchange struct {
	database   db.Database
	config     config.Store
	resolver   resolver.Resolver
	spamFilter spamfilter.Filter
	clamav     clamav.Client
	locator    geoip.Locator
	settings   *Settings

	ctx    context.Context
	cancel context.CancelFunc

	waitGroup        sync.WaitGroup
	deliveryWake     chan struct{}
	periodicScavenge periodic.Periodic
	periodicUsage    periodic.Periodic

	usagesMutex         sync.Mutex
	domainUsagesMap     map[string][]models.Usage
	aliasUsagesMap      map[string][]models.Usage
	credentialUsagesMap map[string][]models.Usage

	storage storage.Storage

	// directory caches what the domain table is read for per message.
	directory directory

	agentHookMutex sync.RWMutex
	agentHook      AgentHook

	calendarHookMutex sync.RWMutex
	calendarHook      CalendarHook
}

// SetAgentHook implements Exchange.
func (self *exchange) SetAgentHook(hook AgentHook) {
	self.agentHookMutex.Lock()
	defer self.agentHookMutex.Unlock()
	self.agentHook = hook
}

func (self *exchange) currentAgentHook() AgentHook {
	self.agentHookMutex.RLock()
	defer self.agentHookMutex.RUnlock()
	return self.agentHook
}

// SetCalendarHook implements Exchange.
func (self *exchange) SetCalendarHook(hook CalendarHook) {
	self.calendarHookMutex.Lock()
	defer self.calendarHookMutex.Unlock()
	self.calendarHook = hook
}

func (self *exchange) currentCalendarHook() CalendarHook {
	self.calendarHookMutex.RLock()
	defer self.calendarHookMutex.RUnlock()
	return self.calendarHook
}

func Open(database db.Database, configuration config.Store, storage storage.Storage, resolver resolver.Resolver, spamFilter spamfilter.Filter, clamav clamav.Client, locator geoip.Locator, settings *Settings) (Exchange, error) {
	self := &exchange{
		database:   database,
		config:     configuration,
		storage:    storage,
		resolver:   resolver,
		spamFilter: spamFilter,
		clamav:     clamav,
		locator:    locator,
		settings:   settings,
	}
	self.ctx, self.cancel = context.WithCancel(context.TODO())
	self.deliveryWake = make(chan struct{}, 1)
	self.periodicScavenge = periodic.New(self.ctx, &self.waitGroup, self.scavengeOnce, &periodic.Settings{
		Interval: 5 * time.Minute,
		Name:     "mx:scavenge",
	})
	self.periodicUsage = periodic.New(self.ctx, &self.waitGroup, self.trackUsageOnce, &periodic.Settings{
		Interval: 5 * time.Second,
		Name:     "mx:usage",
	})
	self.waitGroup.Add(1)
	go self.runDeliveryQueue()
	self.periodicScavenge.Start()
	self.periodicUsage.Start()
	return self, nil
}

func (self *exchange) Close() error {
	defer self.waitGroup.Wait()
	self.cancel()
	self.periodicScavenge.Stop()
	self.periodicUsage.Stop()
	return nil
}

func (self *exchange) HandleEnvelope(ctx context.Context, envelope *mailparse.Envelope) error {
	defer func() {
		log.Debugf("took %s handle mail %q", time.Since(envelope.ReceivedAt), envelope.ID)
	}()

	var deliveries []*models.Delivery
	if err := self.database.Transaction(func(tx db.Transaction) error {
		var err error

		// special dsn, rua, ruf mail
		if envelope.SpecialPrefix != "" {
			switch envelope.SpecialPrefix {
			case "dsn":
				deliveries, err = self.handleDsn(ctx, tx, envelope)
			case "rua":
				deliveries, err = self.handleRua(ctx, tx, envelope)
			}
			return err
		}

		// outgoing
		if envelope.CredentialID != "" || envelope.DomainID != "" || envelope.MailboxID != "" {
			deliveries, err = self.handleOutgoing(ctx, tx, envelope)
			return err
		}

		// incoming
		deliveries, err = self.handleIncoming(ctx, tx, envelope)
		return err
	}); err != nil {
		return err
	}
	if len(deliveries) == 0 {
		if envelope.SpecialPrefix != "" {
			return nil
		}
		return mailparse.ErrMailBoxUnavailable
	}

	// Store the message before queueing, so that a delivery being retried
	// after a restart can reload it, and so the dashboard has something to
	// show. Failing to store is not a reason to refuse mail that has already
	// been accepted, so it is logged rather than returned.
	for _, mail := range distinctMails(deliveries) {
		self.store(ctx, mail)
	}

	// queue the deliveries
	self.goDeliver(ctx, deliveries)
	return nil
}

// storeAttempts is how many times a message's content is offered to storage,
// and storeFirstWait the pause before the second attempt, doubling after.
//
// Storage is not always a local disk. With an object store it is a service
// across a network, and the row for this message is already committed by the
// time it is written -- so a failure here is not a message refused, it is a
// message that exists in somebody's mailbox and can never be opened. The
// object client retries what it considers transient; this is for the rest,
// and four attempts spend about three and a half seconds before giving up,
// which is nothing against the minutes a sender allows.
const storeAttempts = 4

// A variable rather than a constant so that a test can shorten it; nothing
// else writes it.
var storeFirstWait = 500 * time.Millisecond

// store keeps a message's content, whether it was accepted or refused.
//
// Failing to store is never a reason to change what was said to the sender:
// the message has already been accepted or already been refused by the time
// this runs, so it is logged rather than returned. That is why it tries more
// than once -- returning the error is not available, so the only thing left
// is to not need to.
func (self *exchange) store(ctx context.Context, mail *models.Mail) {
	if mail == nil || len(mail.Body) == 0 {
		// Refused before DATA, so there is genuinely nothing to keep.
		return
	}
	wait := storeFirstWait
	var err error
	for attempt := 1; attempt <= storeAttempts; attempt++ {
		if err = self.storage.Put(ctx, mail.ID, mail.Headers, mail.Body); err == nil {
			if attempt > 1 {
				log.Noticef("stored mail %q on attempt %d", mail.ID, attempt)
			}
			return
		}
		if attempt == storeAttempts {
			break
		}
		select {
		case <-ctx.Done():
			// Shutting down. The last error is reported below rather
			// than replaced by a cancellation nobody can act on.
		case <-time.After(wait):
			wait *= 2
			continue
		}
		break
	}
	// Loud, because this is the one failure here that loses something. The
	// message was taken from the sender, who will not send it again, and
	// what is left is a row naming content that was never written.
	log.Errorf("mail %q was accepted but its content could not be stored after %d attempts, so it is in the mailbox and cannot be read: %s", mail.ID, storeAttempts, err)
}

// distinctMails returns each mail referenced by a set of deliveries once. One
// message with several recipients produces several deliveries that share it.
func distinctMails(deliveries []*models.Delivery) []*models.Mail {
	seen := make(map[string]bool, len(deliveries))
	mails := make([]*models.Mail, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.Mail == nil || seen[delivery.Mail.ID] {
			continue
		}
		seen[delivery.Mail.ID] = true
		mails = append(mails, delivery.Mail)
	}
	return mails
}

func (self *exchange) scavengeOnce(ctx context.Context) error {
	if err := self.scavengeMailOnce(ctx); err != nil {
		log.Warningf("failed to scavenge mails: %s", err)
	}
	if err := self.database.Transaction(func(tx db.Transaction) error {
		return tx.ScavengeDomainUsages(nil)
	}); err != nil {
		log.Warningf("failed to scavenge domain usages: %s", err)
	}
	if err := self.database.Transaction(func(tx db.Transaction) error {
		return tx.ScavengeAliasUsages(nil)
	}); err != nil {
		log.Warningf("failed to scavenge alias usages: %s", err)
	}
	if err := self.database.Transaction(func(tx db.Transaction) error {
		return tx.ScavengeCredentialUsages(nil)
	}); err != nil {
		log.Warningf("failed to scavenge credential usages: %s", err)
	}
	return nil
}
