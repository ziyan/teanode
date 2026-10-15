package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

const (
	// priceCheckEvery is how often the prices are checked against the bill.
	priceCheckEvery = 24 * time.Hour

	// priceCheckWindow is how much of the bill each check reads.
	priceCheckWindow = 14 * 24 * time.Hour

	// priceCheckSlack lets a check that ran a moment ago, by this
	// instance or another, count as today's although the tick that ran it
	// was a few seconds late.
	priceCheckSlack = time.Hour

	// priceTolerance is how far a configured price may be from the billed
	// one before it is replaced. Rounding and the bill's clock skew stay
	// well under it, so a smaller figure would flip a price back and forth.
	priceTolerance = 0.05

	// priceCheckLockKey is the advisory lock a check holds for its
	// transaction, so two servers sharing a database do not both read the
	// bill and both tell the operators. An arbitrary constant; "pricebil"
	// in ASCII.
	priceCheckLockKey int64 = 0x707269636562696c

	priceCheckClientTimeout = 45 * time.Second

	// billedDayRetention is how long the billed amounts are kept: a year
	// of spending to compare with, and a margin for a year view that starts
	// a few weeks back.
	billedDayRetention = 400 * 24 * time.Hour
)

// snapshotDateSuffix is the date a report adds to a model's name, as in
// "chat-model-2026-03-17". TeaNode names the model without it.
var snapshotDateSuffix = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// checkPricesInBackground runs the daily check without holding the tick: it
// reads the bill over the network. One at a time; a tick that finds the last
// still going leaves it.
func (self *Agent) checkPricesInBackground(ctx context.Context, now time.Time) {
	if now.Sub(self.lastPriceCheck) < priceCheckEvery || !self.checkingPrices.CompareAndSwap(false, true) {
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		defer self.checkingPrices.Store(false)
		if _, err := self.checkProviderPricesAt(ctx, now, false); err != nil {
			log.Warningf("cannot check the model prices against the providers' bills: %s", err)
		}
	}()
}

// CheckProviderPrices compares the configured prices of every openai
// provider that has a billing key with what its bill says, sets the ones
// that differ, and tells the operators. Unless isForced it does nothing
// when a check ran within the day, here or on another server. It answers
// the checks it made, one per provider.
func (self *Agent) CheckProviderPrices(ctx context.Context, isForced bool) ([]*models.AgentBillingCheck, error) {
	return self.checkProviderPricesAt(ctx, time.Now(), isForced)
}

func (self *Agent) checkProviderPricesAt(ctx context.Context, now time.Time, isForced bool) ([]*models.AgentBillingCheck, error) {
	if !isForced && now.Sub(self.lastPriceCheck) < priceCheckEvery {
		return nil, nil
	}
	self.lastPriceCheck = now

	var providerNames []string
	for _, provider := range self.settings.Configuration().Agent.Providers {
		if provider.Kind == config.AgentProviderKindOpenAI && provider.BillingAPIKey != "" {
			providerNames = append(providerNames, provider.Name)
		}
	}
	if len(providerNames) == 0 {
		return nil, nil
	}

	var checks []*models.AgentBillingCheck
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(priceCheckLockKey)
		if err != nil {
			return err
		}
		if !isLocked {
			log.Debugf("another server is checking the model prices; this check is dropped")
			return nil
		}
		for _, providerName := range providerNames {
			if !isForced {
				previous, err := tx.GetAgentBillingCheck(providerName)
				if err != nil {
					return err
				}
				if previous != nil && now.Sub(previous.CheckedAt) < priceCheckEvery-priceCheckSlack {
					continue
				}
			}
			check, err := self.checkProviderPrices(ctx, tx, now, providerName)
			if err != nil {
				return err
			}
			checks = append(checks, check)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	self.tellOperatorsOfPriceChanges(ctx, checks)
	return checks, nil
}

// checkProviderPrices reads one provider's bill and sets its prices, inside
// the transaction that holds the check's lock. A bill that cannot be read is
// the check's answer, not an error: it is stored with the reason and no price
// changes.
func (self *Agent) checkProviderPrices(ctx context.Context, tx db.Transaction, now time.Time, providerName string) (*models.AgentBillingCheck, error) {
	check := &models.AgentBillingCheck{
		ProviderName: providerName,
		CheckedAt:    now,
		Compared:     []models.AgentBilledPrice{},
		Changed:      []models.AgentPriceChange{},
	}
	provider := findProvider(self.settings.Configuration(), providerName)
	if provider == nil || provider.BillingAPIKey == "" {
		return check, nil
	}
	since := now.Add(-priceCheckWindow)

	baseURL := provider.BaseURL
	if baseURL == "" {
		baseURL = llm.OpenAIDefaultBaseURL
	}
	billing, err := llm.ReadBilling(ctx, &http.Client{Timeout: priceCheckClientTimeout}, baseURL, provider.BillingAPIKey, since)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		check.ErrorMessage = err.Error()
		return check, tx.PutAgentBillingCheck(check)
	}

	usedModels, err := usedModelsOf(tx, providerName, since)
	if err != nil {
		return nil, err
	}
	check.Compared, check.Changed = comparePrices(provider, billing.Rates(), usedModels)

	// The bill is kept per day beside the estimate, whether or not a price
	// changed. A failure here fails the check: it shares this transaction.
	if err := tx.PutAgentBilledDays(providerName, billedDaysOfUsedModels(billing.DailyAmounts(), usedModels)); err != nil {
		return nil, err
	}
	if err := tx.DeleteAgentBilledDaysBefore(now.Add(-billedDayRetention)); err != nil {
		return nil, err
	}

	if len(check.Changed) > 0 {
		if self.settings.UpdateConfiguration == nil {
			check.ErrorMessage = "the server has no way to change its configuration here"
			check.Changed = []models.AgentPriceChange{}
			return check, tx.PutAgentBillingCheck(check)
		}
		if err := self.settings.UpdateConfiguration(func(configuration *config.Configuration) error {
			// Applied to the provider as it is now: this function may be
			// run again against a newer configuration.
			if current := findProvider(configuration, providerName); current != nil {
				applyPriceChanges(current, check.Changed)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("cannot save the prices of the %s provider: %w", providerName, err)
		}
		for _, model := range changedModelsOf(check.Changed) {
			before, after := map[string]float64{}, map[string]float64{}
			for _, change := range check.Changed {
				if change.Model == model {
					before[change.TokenKind] = change.BeforePerMillion
					after[change.TokenKind] = change.AfterPerMillion
				}
			}
			if err := tx.RecordAgentPriceChange(providerName, model, before, after); err != nil {
				return nil, err
			}
		}
	}
	return check, tx.PutAgentBillingCheck(check)
}

// findProvider is the provider of that name, or nil. The pointer is into
// the configuration it was found in.
func findProvider(configuration *config.Configuration, providerName string) *config.AgentProvider {
	for index := range configuration.Agent.Providers {
		if configuration.Agent.Providers[index].Name == providerName {
			return &configuration.Agent.Providers[index]
		}
	}
	return nil
}

// usedModelsOf is the models a provider served TeaNode since a time, named
// as the provider names them: usage is recorded as "provider:model", and an
// embedding's width ("@512") is not part of the provider's name for it.
func usedModelsOf(tx db.Transaction, providerName string, since time.Time) (map[string]bool, error) {
	rows, err := tx.QueryAgentUsageByModel("", since, time.Time{}, "model")
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, row := range rows {
		model, isOurs := strings.CutPrefix(row.Model, providerName+":")
		if !isOurs {
			continue
		}
		model, _, _ = strings.Cut(model, "@")
		used[model] = true
	}
	return used, nil
}

// pricePointer is the field of a provider's pricing that holds a kind of
// token's price per million, or nil for a kind that has no field.
func pricePointer(pricing *config.AgentPricing, tokenKind string) *float64 {
	switch tokenKind {
	case llm.BilledTokenKindInput:
		return &pricing.Input
	case llm.BilledTokenKindCachedInput:
		return &pricing.CacheRead
	case llm.BilledTokenKindCacheWrite:
		return &pricing.CacheWrite
	case llm.BilledTokenKindOutput:
		return &pricing.Output
	case llm.BilledTokenKindAudioInput:
		return &pricing.AudioInput
	}
	return nil
}

func modelPricingPointer(entry *config.AgentModelPricing, tokenKind string) *float64 {
	switch tokenKind {
	case llm.BilledTokenKindInput:
		return &entry.Input
	case llm.BilledTokenKindCachedInput:
		return &entry.CacheRead
	case llm.BilledTokenKindCacheWrite:
		return &entry.CacheWrite
	case llm.BilledTokenKindOutput:
		return &entry.Output
	case llm.BilledTokenKindAudioInput:
		return &entry.AudioInput
	}
	return nil
}

func isPricePattern(model string) bool {
	return strings.ContainsAny(model, "*?[")
}

// comparePrices sets what the bill implies beside what the provider is
// configured with, for the models TeaNode used. Rates for a model nobody
// here used are the account's other programs, and are left out.
func comparePrices(provider *config.AgentProvider, rates []llm.BilledRate, usedModels map[string]bool) (compared []models.AgentBilledPrice, changed []models.AgentPriceChange) {
	compared, changed = []models.AgentBilledPrice{}, []models.AgentPriceChange{}
	type key struct{ model, tokenKind string }
	seen := map[key]bool{}
	// A report that names a model exactly as TeaNode does is read before
	// one that names a dated snapshot of it, so the exact one wins.
	for _, isExact := range []bool{true, false} {
		for _, rate := range rates {
			model, isMatch := rate.Model, usedModels[rate.Model]
			if !isExact {
				model = snapshotDateSuffix.ReplaceAllString(rate.Model, "")
				isMatch = model != rate.Model && usedModels[model]
			}
			if !isMatch || seen[key{model, rate.TokenKind}] {
				continue
			}
			pricing := provider.PricingFor(model)
			configured := pricePointer(&pricing, rate.TokenKind)
			if configured == nil {
				continue
			}
			seen[key{model, rate.TokenKind}] = true
			index := provider.PricingEntryIndexFor(model)
			isPattern := index >= 0 && isPricePattern(provider.ModelPricing[index].Model)
			compared = append(compared, models.AgentBilledPrice{
				Model: model, TokenKind: rate.TokenKind,
				ConfiguredPerMillion: *configured, BilledPerMillion: rate.PerMillion,
				DayCount: rate.DayCount, IsPattern: isPattern,
			})
			if isPattern || !isPriceWrong(*configured, rate.PerMillion) {
				continue
			}
			changed = append(changed, models.AgentPriceChange{
				Model: model, TokenKind: rate.TokenKind,
				BeforePerMillion: *configured, AfterPerMillion: rate.PerMillion,
			})
		}
	}
	return compared, changed
}

// billedDaysOfUsedModels keeps the days billed for models TeaNode used, under
// TeaNode's name for each (a report's snapshot date removed), matched as
// comparePrices matches. An exact name and a dated one on the same day are
// the same model billed twice, so they are added. The account's other
// programs bill other models, and are left out.
func billedDaysOfUsedModels(billed []llm.BilledDay, usedModels map[string]bool) []models.AgentBilledDay {
	type key struct {
		day   time.Time
		model string
	}
	sums := map[key]*models.AgentBilledDay{}
	var order []key
	for _, day := range billed {
		model := day.Model
		if !usedModels[model] {
			model = snapshotDateSuffix.ReplaceAllString(day.Model, "")
			if model == day.Model || !usedModels[model] {
				continue
			}
		}
		at := key{day.Day, model}
		if sum, isSeen := sums[at]; isSeen {
			sum.BilledAmount += day.BilledAmount
			continue
		}
		sums[at] = &models.AgentBilledDay{BilledOn: day.Day, Model: model, BilledAmount: day.BilledAmount, CurrencyCode: day.Currency}
		order = append(order, at)
	}
	result := make([]models.AgentBilledDay, 0, len(order))
	for _, at := range order {
		result = append(result, *sums[at])
	}
	return result
}

// isPriceWrong says the configured price is missing while the provider
// billed for the kind, or is further from the billed one than the
// tolerance.
func isPriceWrong(configured, billed float64) bool {
	if billed <= 0 {
		return false
	}
	if configured == 0 {
		return true
	}
	return math.Abs(billed-configured)/configured > priceTolerance
}

// applyPriceChanges sets each price on the provider's entry for the model,
// or on a new entry when only the provider's own prices applied. A new entry
// starts from the prices the model was charged at, so the kinds the bill did
// not speak about keep their value. An entry that is a pattern is left, as
// it prices other models too.
func applyPriceChanges(provider *config.AgentProvider, changes []models.AgentPriceChange) {
	for _, change := range changes {
		index := provider.PricingEntryIndexFor(change.Model)
		if index >= 0 && isPricePattern(provider.ModelPricing[index].Model) {
			continue
		}
		if index < 0 {
			effective := provider.PricingFor(change.Model)
			provider.ModelPricing = append(provider.ModelPricing, config.AgentModelPricing{
				Model: change.Model, Input: effective.Input, Output: effective.Output,
				CacheRead: effective.CacheRead, CacheWrite: effective.CacheWrite, AudioInput: effective.AudioInput,
			})
			index = len(provider.ModelPricing) - 1
		}
		if price := modelPricingPointer(&provider.ModelPricing[index], change.TokenKind); price != nil {
			*price = change.AfterPerMillion
		}
	}
}

func changedModelsOf(changes []models.AgentPriceChange) []string {
	seen := map[string]bool{}
	var names []string
	for _, change := range changes {
		if !seen[change.Model] {
			seen[change.Model] = true
			names = append(names, change.Model)
		}
	}
	sort.Strings(names)
	return names
}

// priceKindWords is a kind of token as a person reads it.
func priceKindWords(tokenKind string) string {
	switch tokenKind {
	case llm.BilledTokenKindCachedInput:
		return "cached input"
	case llm.BilledTokenKindCacheWrite:
		return "cache writes"
	case llm.BilledTokenKindAudioInput:
		return "audio input"
	}
	return tokenKind
}

func priceMoney(amount float64, currency string) string {
	return strconv.FormatFloat(amount, 'f', -1, 64) + " " + currency
}

// priceChangeSummary is what the operators are told: every change, by
// provider, in the configured currency.
func priceChangeSummary(checks []*models.AgentBillingCheck, currency string) string {
	var builder strings.Builder
	builder.WriteString("Prices now follow the provider's bill.")
	for _, check := range checks {
		if len(check.Changed) == 0 {
			continue
		}
		builder.WriteString("\n\n" + check.ProviderName + ":")
		for _, change := range check.Changed {
			fmt.Fprintf(&builder, "\n- %s %s: %s → %s per million tokens", change.Model, priceKindWords(change.TokenKind),
				priceMoney(change.BeforePerMillion, currency), priceMoney(change.AfterPerMillion, currency))
		}
	}
	builder.WriteString("\n\nEach change is in the audit log with its old value, and can be set back in the provider's settings.")
	return builder.String()
}

// tellOperatorsOfPriceChanges says what the check changed to everyone who
// holds server:manage and has an agent, in their main conversation. Nothing
// is said when nothing changed. One operator who cannot be told, because a
// turn is running in their conversation, does not stop the others.
func (self *Agent) tellOperatorsOfPriceChanges(ctx context.Context, checks []*models.AgentBillingCheck) {
	isChanged := false
	for _, check := range checks {
		isChanged = isChanged || len(check.Changed) > 0
	}
	if !isChanged {
		return
	}
	var agentIds []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		agentIds, err = operatorAgentIds(tx)
		return err
	}); err != nil {
		log.Warningf("cannot find the operators to tell of the price changes: %s", err)
		return
	}
	summary := priceChangeSummary(checks, self.settings.Configuration().Agent.CurrencyOf())
	checkIn := models.AlertMarker + " The server compared the model prices with the provider's bill and changed some; you told the operator. This is not the person speaking. What you said follows."
	for _, agentId := range agentIds {
		if err := self.tellAgent(ctx, self.settings.Database, agentId, checkIn, summary); err != nil {
			if errors.Is(err, errTurnRunning) {
				log.Warningf("the price changes were not told to agent %s: a turn was running in its conversation", agentId)
				continue
			}
			log.Warningf("cannot tell agent %s of the price changes: %s", agentId, err)
		}
	}
}

// operatorAgentIds is the agents of the people who may manage the server.
func operatorAgentIds(tx db.Transaction) ([]string, error) {
	users, err := tx.ListUsers()
	if err != nil {
		return nil, err
	}
	var agentIds []string
	for _, user := range users {
		permissions, err := tx.EffectivePermissions(user.ID)
		if err != nil {
			return nil, err
		}
		if !permissions.Has(models.PermissionServerManage) {
			continue
		}
		found, err := tx.GetAgentByUser(user.ID)
		if err != nil {
			return nil, err
		}
		if found != nil {
			agentIds = append(agentIds, found.ID)
		}
	}
	return agentIds, nil
}
