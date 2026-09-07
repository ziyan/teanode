package mx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/bufferpool"
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
	periodicDeliver  periodic.Periodic
	periodicScavenge periodic.Periodic
	periodicUsage    periodic.Periodic

	usagesMutex         sync.Mutex
	domainUsagesMap     map[string][]models.Usage
	aliasUsagesMap      map[string][]models.Usage
	credentialUsagesMap map[string][]models.Usage

	storage storage.Storage

	// directory caches what the domain table is read for per message.
	directory directory
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
	self.periodicDeliver = periodic.New(self.ctx, &self.waitGroup, self.deliverOnce, &periodic.Settings{
		Interval:       time.Minute,
		Name:           "mx:deliver",
		SkipInitialRun: true,
	})
	self.periodicScavenge = periodic.New(self.ctx, &self.waitGroup, self.scavengeOnce, &periodic.Settings{
		Interval: 5 * time.Minute,
		Name:     "mx:scavenge",
	})
	self.periodicUsage = periodic.New(self.ctx, &self.waitGroup, self.trackUsageOnce, &periodic.Settings{
		Interval: 5 * time.Second,
		Name:     "mx:usage",
	})
	self.periodicDeliver.Start()
	self.periodicScavenge.Start()
	self.periodicUsage.Start()
	return self, nil
}

func (self *exchange) Close() error {
	defer self.waitGroup.Wait()
	self.cancel()
	self.periodicDeliver.Stop()
	self.periodicScavenge.Stop()
	self.periodicUsage.Stop()
	return nil
}

func (self *exchange) HandleEnvelope(ctx context.Context, envelope *mailparse.Envelope) error {
	defer func() {
		log.Debugf("took %s handle mail %q", time.Since(envelope.ReceivedAt), envelope.ID)
	}()

	// log
	if envelope.SpecialPrefix != "" {
		switch envelope.SpecialPrefix {
		case "dsn":
			self.logEnvelope(models.MailKindDSN, envelope)
		case "rua":
			self.logEnvelope(models.MailKindRUA, envelope)
		case "ruf":
			self.logEnvelope(models.MailKindRUF, envelope)
		}
	} else if envelope.CredentialID != "" || envelope.DomainID != "" || envelope.MailboxID != "" {
		self.logEnvelope(models.MailKindOutgoing, envelope)
	} else {
		self.logEnvelope(models.MailKindIncoming, envelope)
	}

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

// store keeps a message's content, whether it was accepted or refused.
//
// Failing to store is never a reason to change what was said to the sender:
// the message has already been accepted or already been refused by the time
// this runs, so it is logged rather than returned.
func (self *exchange) store(ctx context.Context, mail *models.Mail) {
	if mail == nil || len(mail.Body) == 0 {
		// Refused before DATA, so there is genuinely nothing to keep.
		return
	}
	if err := self.storage.Put(ctx, mail.ID, mail.Headers, mail.Body); err != nil {
		log.Warningf("failed to store mail %q: %s", mail.ID, err)
	}
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

func (self *exchange) logEnvelope(kind models.MailKind, envelope *mailparse.Envelope) {
	// Raw message logging is off unless a directory is configured. Without
	// this guard the path below resolves to the working directory and every
	// received message is dropped there, which is how a copy of somebody's
	// mail ends up in a source tree.
	if self.settings.LogDirectory == "" {
		return
	}

	// get a buffer from pool
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()

	if err := mailparse.Unsplit(buffer, envelope.Body, envelope.Headers); err != nil {
		log.Warningf("failed to log mail %s: %s", envelope, err)
		return
	}

	if err := os.WriteFile(filepath.Join(self.settings.LogDirectory, fmt.Sprintf("%s.%s.eml", envelope.ID, kind)), buffer.Bytes(), 0664); err != nil {
		log.Warningf("failed to log mail %s: %s", envelope, err)
		return
	}
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
