package mx

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type domainUsage struct {
	bytesReceived       uint64
	bytesSent           uint64
	mailsAccepted       uint64
	mailsRejected       uint64
	deliveriesSucceeded uint64
	deliveriesFailed    uint64
	deliveriesBounced   uint64
}

func (self domainUsage) values() []uint64 {
	return []uint64{
		self.bytesReceived,
		self.bytesSent,
		self.mailsAccepted,
		self.mailsRejected,
		self.deliveriesSucceeded,
		self.deliveriesFailed,
		self.deliveriesBounced,
	}
}

func (self *exchange) trackDomainUsage(now time.Time, domainId string, domainUsage domainUsage) {
	newUsages := models.NewUsages(now, domainUsage.values())

	self.usagesMutex.Lock()
	defer self.usagesMutex.Unlock()

	if self.domainUsagesMap == nil {
		self.domainUsagesMap = make(map[string][]models.Usage)
	}
	self.domainUsagesMap[domainId] = models.MergeUsages(self.domainUsagesMap[domainId], newUsages)
}

type credentialUsage struct {
	bytesReceived       uint64
	bytesSent           uint64
	mailsAccepted       uint64
	mailsRejected       uint64
	deliveriesSucceeded uint64
	deliveriesFailed    uint64
	deliveriesBounced   uint64
}

func (self credentialUsage) values() []uint64 {
	return []uint64{
		self.bytesReceived,
		self.bytesSent,
		self.mailsAccepted,
		self.mailsRejected,
		self.deliveriesSucceeded,
		self.deliveriesFailed,
		self.deliveriesBounced,
	}
}

func (self *exchange) trackCredentialUsage(now time.Time, credentialId string, credentialUsage credentialUsage) {
	newUsages := models.NewUsages(now, credentialUsage.values())

	self.usagesMutex.Lock()
	defer self.usagesMutex.Unlock()

	if self.credentialUsagesMap == nil {
		self.credentialUsagesMap = make(map[string][]models.Usage)
	}
	self.credentialUsagesMap[credentialId] = models.MergeUsages(self.credentialUsagesMap[credentialId], newUsages)
}

type aliasUsage struct {
	bytesReceived       uint64
	bytesSent           uint64
	mailsAccepted       uint64
	mailsRejected       uint64
	deliveriesSucceeded uint64
	deliveriesFailed    uint64
	deliveriesBounced   uint64
}

func (self aliasUsage) values() []uint64 {
	return []uint64{
		self.bytesReceived,
		self.bytesSent,
		self.mailsAccepted,
		self.mailsRejected,
		self.deliveriesSucceeded,
		self.deliveriesFailed,
		self.deliveriesBounced,
	}
}

func (self *exchange) trackAliasUsage(now time.Time, aliasId string, aliasUsage aliasUsage) {
	newUsages := models.NewUsages(now, aliasUsage.values())

	self.usagesMutex.Lock()
	defer self.usagesMutex.Unlock()

	if self.aliasUsagesMap == nil {
		self.aliasUsagesMap = make(map[string][]models.Usage)
	}
	self.aliasUsagesMap[aliasId] = models.MergeUsages(self.aliasUsagesMap[aliasId], newUsages)
}

func (self *exchange) trackUsageOnce(ctx context.Context) error {
	var domainUsagesMap map[string][]models.Usage
	var aliasUsagesMap map[string][]models.Usage
	var credentialUsagesMap map[string][]models.Usage
	func() {
		self.usagesMutex.Lock()
		defer self.usagesMutex.Unlock()
		domainUsagesMap = self.domainUsagesMap
		self.domainUsagesMap = nil
		aliasUsagesMap = self.aliasUsagesMap
		self.aliasUsagesMap = nil
		credentialUsagesMap = self.credentialUsagesMap
		self.credentialUsagesMap = nil
	}()

	if len(domainUsagesMap) > 0 {
		if err := self.database.Transaction(func(tx db.Transaction) error {
			return tx.PutDomainUsages(domainUsagesMap, nil)
		}); err != nil {
			log.Warningf("failed to save domain usages: %s", err)
		}
	}
	if len(aliasUsagesMap) > 0 {
		if err := self.database.Transaction(func(tx db.Transaction) error {
			return tx.PutAliasUsages(aliasUsagesMap, nil)
		}); err != nil {
			log.Warningf("failed to save alias usages: %s", err)
		}
	}
	if len(credentialUsagesMap) > 0 {
		if err := self.database.Transaction(func(tx db.Transaction) error {
			return tx.PutCredentialUsages(credentialUsagesMap, nil)
		}); err != nil {
			log.Warningf("failed to save credential usages: %s", err)
		}
	}
	return nil
}
