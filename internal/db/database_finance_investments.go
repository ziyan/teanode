package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// FinanceTradeFilter narrows a page of trades. Every field is optional.
type FinanceTradeFilter struct {
	// From and To bound the traded day, both included, "2006-01-02".
	From string
	To   string

	FinanceAccountID  string
	FinanceSecurityID string

	// Limit is at most FinanceTradeLimitMost; zero is
	// FinanceTradeLimitDefault.
	Limit int

	// After is the NextCursor of the page before. Offset is how many to
	// pass over first, for a page by its number; with After it counts
	// from the cursor.
	After  string
	Offset int

	// ShouldCountTotal fills the page's TotalCount, which costs one more
	// statement.
	ShouldCountTotal bool
}

// How many trades one page holds.
const (
	FinanceTradeLimitDefault = 50
	FinanceTradeLimitMost    = 200
)

// FinanceTradePage is one page of trades, and the cursor for the next,
// empty on the last. TotalCount is how many match the filter on every
// page, the cursor and the offset aside, when the filter asked for it.
type FinanceTradePage struct {
	FinanceTrades []*models.FinanceTrade
	NextCursor    string
	TotalCount    int
}

type agentFinanceSecurityModel struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	AgentID            string     `gorm:"column:agent_id"`
	ProviderKind       string     `gorm:"column:provider_kind"`
	ProviderSecurityID string     `gorm:"column:provider_security_id"`
	TickerSymbol       string     `gorm:"column:ticker_symbol"`
	SecurityName       string     `gorm:"column:security_name"`
	SecurityKind       string     `gorm:"column:security_kind"`
	CurrencyCode       string     `gorm:"column:currency_code"`
	ClosePrice         *string    `gorm:"column:close_price"`
	ClosePriceOn       *time.Time `gorm:"column:close_price_on"`
	ProviderMetadata   []byte     `gorm:"column:provider_metadata;type:jsonb"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	ModifiedAt         time.Time  `gorm:"column:modified_at"`
}

func (agentFinanceSecurityModel) TableName() string { return "agent_finance_security" }

func (self *agentFinanceSecurityModel) toModel() *models.FinanceSecurity {
	return &models.FinanceSecurity{
		ID: self.ID, AgentID: self.AgentID, ProviderKind: self.ProviderKind, ProviderSecurityID: self.ProviderSecurityID,
		TickerSymbol: self.TickerSymbol, SecurityName: self.SecurityName, SecurityKind: models.SecurityKind(self.SecurityKind),
		CurrencyCode: self.CurrencyCode, ClosePrice: optionalString(self.ClosePrice), ClosePriceOn: formatOptionalDay(self.ClosePriceOn),
		ProviderMetadata: rawJSON(self.ProviderMetadata),
		CreatedAt:        self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

type agentFinanceTradeModel struct {
	ID                string    `gorm:"column:id;primaryKey"`
	AgentID           string    `gorm:"column:agent_id"`
	FinanceAccountID  string    `gorm:"column:finance_account_id"`
	FinanceSecurityID *string   `gorm:"column:finance_security_id"`
	ProviderTradeID   string    `gorm:"column:provider_trade_id"`
	TradedOn          time.Time `gorm:"column:traded_on"`
	TradeKind         string    `gorm:"column:trade_kind"`
	TradeSubkind      string    `gorm:"column:trade_subkind"`
	TradedQuantity    *string   `gorm:"column:traded_quantity"`
	UnitPrice         *string   `gorm:"column:unit_price"`
	TradeAmount       string    `gorm:"column:trade_amount"`
	FeeAmount         *string   `gorm:"column:fee_amount"`
	CurrencyCode      string    `gorm:"column:currency_code"`
	Description       string    `gorm:"column:description"`
	ProviderMetadata  []byte    `gorm:"column:provider_metadata;type:jsonb"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	ModifiedAt        time.Time `gorm:"column:modified_at"`
}

func (agentFinanceTradeModel) TableName() string { return "agent_finance_trade" }

func (self *agentFinanceTradeModel) toModel() *models.FinanceTrade {
	return &models.FinanceTrade{
		ID: self.ID, AgentID: self.AgentID, FinanceAccountID: self.FinanceAccountID,
		FinanceSecurityID: optionalString(self.FinanceSecurityID), ProviderTradeID: self.ProviderTradeID,
		TradedOn: formatDay(self.TradedOn), TradeKind: models.TradeKind(self.TradeKind), TradeSubkind: self.TradeSubkind,
		TradedQuantity: optionalString(self.TradedQuantity), UnitPrice: optionalString(self.UnitPrice),
		TradeAmount: self.TradeAmount, FeeAmount: optionalString(self.FeeAmount), CurrencyCode: self.CurrencyCode,
		Description: self.Description, ProviderMetadata: rawJSON(self.ProviderMetadata),
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

// canonicalOptionalQuantity is a quantity or a unit price written the one
// way the tables keep it, nil when empty.
func canonicalOptionalQuantity(field, quantity string) (*string, error) {
	if strings.TrimSpace(quantity) == "" {
		return nil, nil
	}
	canonical, err := finance.CanonicalQuantity(quantity)
	if err != nil {
		return nil, fmt.Errorf("%w: the %s %q is not a number", ErrInvalidArguments, field, quantity)
	}
	return &canonical, nil
}

// upsertFinanceSecurities writes the securities a sync reported and
// answers the id of each by the provider's id for it.
func (self *transaction) upsertFinanceSecurities(agentId, providerKind string, securities []finance.Security, now time.Time) (map[string]string, error) {
	securityIdByProviderSecurityId := make(map[string]string, len(securities))
	for _, security := range securities {
		if security.ProviderSecurityID == "" {
			return nil, fmt.Errorf("%w: a security needs the provider's id", ErrInvalidArguments)
		}
		securityKind := models.SecurityKind(security.SecurityKind)
		if !securityKind.IsValid() {
			securityKind = models.SecurityKindOther
		}
		securityName := strings.TrimSpace(security.SecurityName)
		if securityName == "" {
			securityName = firstNonEmptyText(security.TickerSymbol, security.ProviderSecurityID)
		}
		closePrice, err := canonicalOptionalQuantity("close price", security.ClosePrice)
		if err != nil {
			return nil, err
		}
		closePriceOn, err := parseOptionalDay(security.ClosePriceOn)
		if err != nil {
			return nil, err
		}
		var closePriceOnValue *string
		if closePriceOn != "" {
			closePriceOnValue = &closePriceOn
		}
		providerMetadata, err := providerMetadataJSON(security.ProviderMetadata)
		if err != nil {
			return nil, err
		}
		var securityId string
		if err := self.tx.Raw(`INSERT INTO "agent_finance_security" ("id", "agent_id", "provider_kind", "provider_security_id",
				"ticker_symbol", "security_name", "security_kind", "currency_code", "close_price", "close_price_on",
				"provider_metadata", "created_at", "modified_at")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::numeric, ?::date, ?::jsonb, ?, ?)
			ON CONFLICT ("agent_id", "provider_kind", "provider_security_id") DO UPDATE SET
				"ticker_symbol" = EXCLUDED."ticker_symbol", "security_name" = EXCLUDED."security_name",
				"security_kind" = EXCLUDED."security_kind", "currency_code" = EXCLUDED."currency_code",
				"close_price" = COALESCE(EXCLUDED."close_price", "agent_finance_security"."close_price"),
				"close_price_on" = COALESCE(EXCLUDED."close_price_on", "agent_finance_security"."close_price_on"),
				"provider_metadata" = EXCLUDED."provider_metadata", "modified_at" = EXCLUDED."modified_at"
			RETURNING "id"`,
			newID(), agentId, providerKind, security.ProviderSecurityID, strings.TrimSpace(security.TickerSymbol), securityName,
			string(securityKind), security.CurrencyCode, closePrice, closePriceOnValue, providerMetadata, now, now).
			Scan(&securityId).Error; err != nil {
			return nil, err
		}
		securityIdByProviderSecurityId[security.ProviderSecurityID] = securityId
	}
	return securityIdByProviderSecurityId, nil
}

// knownFinanceSecurityIds adds to the map the securities the agent already
// has that a sync's holdings or trades name without describing them.
func (self *transaction) knownFinanceSecurityIds(agentId, providerKind string, providerSecurityIds []string, securityIdByProviderSecurityId map[string]string) error {
	var missing []string
	for _, providerSecurityId := range providerSecurityIds {
		if _, isKnown := securityIdByProviderSecurityId[providerSecurityId]; !isKnown && providerSecurityId != "" {
			missing = append(missing, providerSecurityId)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var found []agentFinanceSecurityModel
	if err := self.tx.Where(`"agent_id" = ? AND "provider_kind" = ? AND "provider_security_id" = ANY(?::text[])`,
		agentId, providerKind, pq.Array(missing)).Find(&found).Error; err != nil {
		return err
	}
	for _, security := range found {
		securityIdByProviderSecurityId[security.ProviderSecurityID] = security.ID
	}
	return nil
}

// upsertFinanceTrade writes one trade a provider reported, by the
// provider's id for it within its finance account, and says whether it
// wrote anything: a trade sent again unchanged is left as it was.
func (self *transaction) upsertFinanceTrade(agentId, financeAccountId string, securityId *string, trade finance.Trade, now time.Time) (bool, error) {
	if trade.ProviderTradeID == "" {
		return false, fmt.Errorf("%w: a trade needs the provider's id", ErrInvalidArguments)
	}
	tradeKind := models.TradeKind(trade.TradeKind)
	if !tradeKind.IsValid() {
		return false, fmt.Errorf("%w: %q is not a kind of trade", ErrInvalidArguments, trade.TradeKind)
	}
	tradedOn, err := parseDay(trade.TradedOn)
	if err != nil {
		return false, err
	}
	tradeAmount, err := canonicalAmount("trade amount", trade.TradeAmount)
	if err != nil {
		return false, err
	}
	feeAmount, err := canonicalOptionalAmount("fee amount", trade.FeeAmount)
	if err != nil {
		return false, err
	}
	tradedQuantity, err := canonicalOptionalQuantity("traded quantity", trade.TradedQuantity)
	if err != nil {
		return false, err
	}
	unitPrice, err := canonicalOptionalQuantity("unit price", trade.UnitPrice)
	if err != nil {
		return false, err
	}
	providerMetadata, err := providerMetadataJSON(trade.ProviderMetadata)
	if err != nil {
		return false, err
	}
	written := self.tx.Exec(`INSERT INTO "agent_finance_trade" ("id", "agent_id", "finance_account_id", "finance_security_id",
			"provider_trade_id", "traded_on", "trade_kind", "trade_subkind", "traded_quantity", "unit_price", "trade_amount",
			"fee_amount", "currency_code", "description", "provider_metadata", "created_at", "modified_at")
		VALUES (?, ?, ?, ?, ?, ?::date, ?, ?, ?::numeric, ?::numeric, ?::numeric, ?::numeric, ?, ?, ?::jsonb, ?, ?)
		ON CONFLICT ("finance_account_id", "provider_trade_id") DO UPDATE SET
			"finance_security_id" = EXCLUDED."finance_security_id", "traded_on" = EXCLUDED."traded_on",
			"trade_kind" = EXCLUDED."trade_kind", "trade_subkind" = EXCLUDED."trade_subkind",
			"traded_quantity" = EXCLUDED."traded_quantity", "unit_price" = EXCLUDED."unit_price",
			"trade_amount" = EXCLUDED."trade_amount", "fee_amount" = EXCLUDED."fee_amount",
			"currency_code" = EXCLUDED."currency_code", "description" = EXCLUDED."description",
			"provider_metadata" = EXCLUDED."provider_metadata", "modified_at" = EXCLUDED."modified_at"
		WHERE ("agent_finance_trade"."finance_security_id", "agent_finance_trade"."traded_on", "agent_finance_trade"."trade_kind",
			"agent_finance_trade"."trade_subkind", "agent_finance_trade"."traded_quantity", "agent_finance_trade"."unit_price",
			"agent_finance_trade"."trade_amount", "agent_finance_trade"."fee_amount", "agent_finance_trade"."currency_code",
			"agent_finance_trade"."description")
			IS DISTINCT FROM (EXCLUDED."finance_security_id", EXCLUDED."traded_on", EXCLUDED."trade_kind", EXCLUDED."trade_subkind",
			EXCLUDED."traded_quantity", EXCLUDED."unit_price", EXCLUDED."trade_amount", EXCLUDED."fee_amount",
			EXCLUDED."currency_code", EXCLUDED."description")`,
		newID(), agentId, financeAccountId, securityId, trade.ProviderTradeID, tradedOn, string(tradeKind), trade.TradeSubkind,
		tradedQuantity, unitPrice, tradeAmount, feeAmount, trade.CurrencyCode, trade.Description, providerMetadata, now, now)
	if written.Error != nil {
		return false, written.Error
	}
	return written.RowsAffected > 0, nil
}

// financeHoldingsApplied is what applying a sync's holdings decided about
// the valuations of its finance accounts' own assets: the balance to
// record for an account whose holdings were read, which is its cash, and
// the accounts to record nothing for today because their holdings were
// not read and recording the whole balance would count them twice.
type financeHoldingsApplied struct {
	accountByFinanceAccountId map[string]finance.Account
	isValuationSkipped        map[string]bool
}

// applyFinanceHoldings makes and values the holdings of each finance
// account whose holdings the sync read, values at zero and closes the ones
// no longer held, and works out what each account's own asset is worth.
func (self *transaction) applyFinanceHoldings(agentId string, syncResult *finance.SyncResult, financeAccountIdByProviderAccountId map[string]string,
	reportedBalances map[string]finance.Account, securityIdByProviderSecurityId map[string]string, syncedOn string, now time.Time,
	applied *FinanceSyncApplied) (*financeHoldingsApplied, error) {
	holdingsApplied := &financeHoldingsApplied{accountByFinanceAccountId: map[string]finance.Account{}, isValuationSkipped: map[string]bool{}}

	isHoldingsRead := map[string]bool{}
	for _, providerAccountId := range syncResult.HoldingsReadAccountIDs {
		if financeAccountId, isKnown := financeAccountIdByProviderAccountId[providerAccountId]; isKnown {
			isHoldingsRead[financeAccountId] = true
		}
	}
	holdingsByFinanceAccountId := map[string][]finance.Holding{}
	for _, holding := range syncResult.Holdings {
		financeAccountId, isKnown := financeAccountIdByProviderAccountId[holding.ProviderAccountID]
		if !isKnown || !isHoldingsRead[financeAccountId] {
			continue
		}
		holdingsByFinanceAccountId[financeAccountId] = append(holdingsByFinanceAccountId[financeAccountId], holding)
	}

	for financeAccountId, account := range reportedBalances {
		if !isHoldingsRead[financeAccountId] {
			if models.FinanceAccountKind(account.AccountKind) != models.FinanceAccountKindInvestment {
				continue
			}
			var hasOpenHoldings bool
			if err := self.tx.Raw(`SELECT EXISTS (SELECT 1 FROM "agent_asset" WHERE "agent_id" = ? AND "finance_account_id" = ?
					AND "finance_security_id" IS NOT NULL AND "closed_on" IS NULL)`, agentId, financeAccountId).
				Scan(&hasOpenHoldings).Error; err != nil {
				return nil, err
			}
			if hasOpenHoldings {
				holdingsApplied.isValuationSkipped[financeAccountId] = true
			}
			continue
		}

		accountAsset, err := self.financeAccountOwnAsset(agentId, financeAccountId)
		if err != nil {
			return nil, err
		}
		// The person's choice about the account stands for what is in it:
		// an account whose asset they deleted, or value by hand, gets no
		// holdings, which would count again what they said, or bring back
		// what they took out of net worth.
		if accountAsset == nil || accountAsset.ValuationSource != string(models.ValuationSourceFinanceSync) {
			continue
		}
		// A balance the provider did not report stays unknown: the cash is
		// the balance less the holdings only where there is a balance.
		isBalanceKnown := strings.TrimSpace(account.CurrentBalance) != ""
		cashValue, err := finance.ParseAmount(firstNonEmptyText(account.CurrentBalance, "0"))
		if err != nil {
			return nil, fmt.Errorf("%w: the balance of %s is not a number", ErrInvalidArguments, account.ProviderAccountID)
		}
		isCashUnknown := false
		heldSecurityIds := []string{}
		for _, holding := range holdingsByFinanceAccountId[financeAccountId] {
			securityId, isKnown := securityIdByProviderSecurityId[holding.ProviderSecurityID]
			if !isKnown {
				return nil, fmt.Errorf("%w: a holding names security %s, which the sync did not describe", ErrInvalidArguments, holding.ProviderSecurityID)
			}
			heldSecurityIds = append(heldSecurityIds, securityId)
			currencyCode := firstNonEmptyText(holding.CurrencyCode, account.CurrencyCode)
			assetId, valuationSource, isCreated, err := self.holdingAsset(agentId, financeAccountId, securityId, account, accountAsset, currencyCode, now)
			if err != nil {
				return nil, err
			}
			if isCreated {
				applied.CreatedAssetIDs = append(applied.CreatedAssetIDs, assetId)
			}
			// The account's balance holds every holding, in the account's
			// currency: one in another is converted at the day's rate
			// before it is taken off. Without a rate the cash is not known
			// for the day, and the account is not valued rather than
			// counting the holding twice.
			if isBalanceKnown {
				holdingValue, err := finance.ParseAmount(holding.HoldingValue)
				if err != nil {
					return nil, fmt.Errorf("%w: a holding's value is not a number", ErrInvalidArguments)
				}
				if currencyCode != account.CurrencyCode && account.CurrencyCode != "" {
					pairRate, err := self.ExchangeRate(currencyCode, account.CurrencyCode, syncedOn)
					var noRate *finance.ErrNoExchangeRate
					switch {
					case errors.As(err, &noRate):
						isCashUnknown = true
						holdingValue = nil
					case err != nil:
						return nil, err
					default:
						rate, err := finance.ParseAmount(pairRate.Rate)
						if err != nil {
							return nil, err
						}
						holdingValue.Mul(holdingValue, rate)
					}
				}
				if holdingValue != nil {
					cashValue.Sub(cashValue, holdingValue)
				}
			}
			if valuationSource != string(models.ValuationSourceFinanceSync) {
				continue
			}
			if _, err := self.upsertAssetValuation(&models.AssetValuation{
				AgentID: agentId, AssetID: assetId, ValuedOn: syncedOn, Value: holding.HoldingValue, CurrencyCode: currencyCode,
				ValuationSource: models.ValuationSourceFinanceSync, HeldQuantity: holding.HeldQuantity, UnitPrice: holding.UnitPrice,
				CostBasis: holding.CostBasis,
			}, now); err != nil {
				return nil, err
			}
			applied.RecordedValuationCount++
		}

		// What is no longer held is worth nothing from today, so its last
		// value does not carry forward into the days until it is bought
		// again, and it closes.
		var soldAssets []agentAssetModel
		if err := self.tx.Where(`"agent_id" = ? AND "finance_account_id" = ? AND "finance_security_id" IS NOT NULL
				AND NOT ("finance_security_id" = ANY(?::text[])) AND "closed_on" IS NULL AND "valuation_source" = ?`,
			agentId, financeAccountId, pq.Array(heldSecurityIds), string(models.ValuationSourceFinanceSync)).Find(&soldAssets).Error; err != nil {
			return nil, err
		}
		for _, sold := range soldAssets {
			if _, err := self.upsertAssetValuation(&models.AssetValuation{
				AgentID: agentId, AssetID: sold.ID, ValuedOn: syncedOn, Value: "0", CurrencyCode: sold.CurrencyCode,
				ValuationSource: models.ValuationSourceFinanceSync, HeldQuantity: "0",
			}, now); err != nil {
				return nil, err
			}
			if err := self.tx.Model(&agentAssetModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, sold.ID).
				Updates(map[string]any{"closed_on": syncedOn, "modified_at": now}).Error; err != nil {
				return nil, err
			}
		}

		if isCashUnknown {
			holdingsApplied.isValuationSkipped[financeAccountId] = true
			continue
		}
		cashAccount := account
		if isBalanceKnown {
			cashAccount.CurrentBalance = finance.FormatAmount(cashValue)
		}
		holdingsApplied.accountByFinanceAccountId[financeAccountId] = cashAccount
	}
	return holdingsApplied, nil
}

// financeAccountOwnAsset is the asset a finance account's balance values,
// the one without a security, or nil when the person deleted it.
func (self *transaction) financeAccountOwnAsset(agentId, financeAccountId string) (*agentAssetModel, error) {
	var found []agentAssetModel
	if err := self.tx.Where(`"agent_id" = ? AND "finance_account_id" = ? AND "finance_security_id" IS NULL`, agentId, financeAccountId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// holdingAsset is the asset a holding of one security in one finance
// account is valued as, and the valuation source it takes: the one it
// already has, opened again if it was closed; else one a deleted finance
// source left behind for the same security, taken back; else a new one,
// named after the security and the account and of the account's own
// asset's kind (a holding in a retirement account is retirement).
func (self *transaction) holdingAsset(agentId, financeAccountId, securityId string, account finance.Account, accountAsset *agentAssetModel,
	currencyCode string, now time.Time) (assetId, valuationSource string, isCreated bool, err error) {
	var found []agentAssetModel
	if err := self.tx.Where(`"agent_id" = ? AND "finance_account_id" = ? AND "finance_security_id" = ?`, agentId, financeAccountId, securityId).
		Limit(1).Find(&found).Error; err != nil {
		return "", "", false, err
	}
	if len(found) == 1 {
		if found[0].ClosedOn != nil && found[0].ValuationSource == string(models.ValuationSourceFinanceSync) {
			if err := self.tx.Model(&agentAssetModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, found[0].ID).
				Updates(map[string]any{"closed_on": nil, "modified_at": now}).Error; err != nil {
				return "", "", false, err
			}
		}
		return found[0].ID, found[0].ValuationSource, false, nil
	}

	var security agentFinanceSecurityModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, securityId).First(&security).Error; err != nil {
		return "", "", false, err
	}
	assetKind := models.AssetKindInvestment
	accountName := strings.TrimSpace(account.AccountName)
	if accountAsset != nil {
		assetKind = models.AssetKind(accountAsset.AssetKind)
		accountName = accountAsset.AssetName
	}
	if assetKind != models.AssetKindRetirement {
		assetKind = models.AssetKindInvestment
	}
	assetName := firstNonEmptyText(security.TickerSymbol, security.SecurityName)
	if accountName != "" {
		assetName += " (" + accountName + ")"
	}
	// The same holding linked again, after its finance source was deleted:
	// the asset the old link made was kept, closed, with the security. It
	// is the same holding only when it is named for the same account as
	// well: another brokerage holding the same fund is another holding.
	var detachedIds []string
	if err := self.tx.Raw(`SELECT "id" FROM "agent_asset" AS "asset"
		WHERE "agent_id" = ? AND "finance_account_id" IS NULL AND "finance_security_id" = ? AND "valuation_source" = ?
		  AND "asset_name" = ?
		  AND EXISTS (SELECT 1 FROM "agent_asset_valuation" WHERE "asset_id" = "asset"."id" AND "valuation_source" = ?)
		LIMIT 2`, agentId, securityId, string(models.ValuationSourceManual), assetName, string(models.ValuationSourceFinanceSync)).
		Scan(&detachedIds).Error; err != nil {
		return "", "", false, err
	}
	if len(detachedIds) == 1 {
		if err := self.tx.Model(&agentAssetModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, detachedIds[0]).Updates(map[string]any{
			"finance_account_id": financeAccountId, "valuation_source": string(models.ValuationSourceFinanceSync), "closed_on": nil,
			"modified_at": now,
		}).Error; err != nil {
			return "", "", false, err
		}
		return detachedIds[0], string(models.ValuationSourceFinanceSync), false, nil
	}

	model := &agentAssetModel{
		ID: newID(), AgentID: agentId, AssetName: assetName, AssetKind: string(assetKind), IsLiability: false,
		CurrencyCode: currencyCode, FinanceAccountID: &financeAccountId, FinanceSecurityID: &securityId,
		ValuationSource: string(models.ValuationSourceFinanceSync), CreatedAt: now, ModifiedAt: now,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return "", "", false, err
	}
	return model.ID, model.ValuationSource, true, nil
}

// attachFinanceSecurities fills in each asset's security from its id.
func (self *transaction) attachFinanceSecurities(agentId string, assets []*models.Asset) error {
	securityIds := []string{}
	for _, asset := range assets {
		if asset.FinanceSecurityID != "" {
			securityIds = append(securityIds, asset.FinanceSecurityID)
		}
	}
	securityById, err := self.financeSecuritiesById(agentId, securityIds)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		asset.FinanceSecurity = securityById[asset.FinanceSecurityID]
	}
	return nil
}

func (self *transaction) financeSecuritiesById(agentId string, securityIds []string) (map[string]*models.FinanceSecurity, error) {
	securityById := map[string]*models.FinanceSecurity{}
	if len(securityIds) == 0 {
		return securityById, nil
	}
	var found []agentFinanceSecurityModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ANY(?::text[])`, agentId, pq.Array(securityIds)).Find(&found).Error; err != nil {
		return nil, err
	}
	for index := range found {
		securityById[found[index].ID] = found[index].toModel()
	}
	return securityById, nil
}

func financeTradeCursor(last *agentFinanceTradeModel) string {
	return formatDay(last.TradedOn) + "/" + last.ID
}

// financeTradeQuery is the trades a filter matches, the cursor, the
// offset and the limit aside: what a page is read from and what its
// total counts.
func (self *transaction) financeTradeQuery(agentId string, filter *FinanceTradeFilter) (*gorm.DB, error) {
	query := self.tx.Model(&agentFinanceTradeModel{}).Where(`"agent_id" = ?`, agentId)
	from, err := parseOptionalDay(filter.From)
	if err != nil {
		return nil, err
	}
	if from != "" {
		query = query.Where(`"traded_on" >= ?::date`, from)
	}
	to, err := parseOptionalDay(filter.To)
	if err != nil {
		return nil, err
	}
	if to != "" {
		query = query.Where(`"traded_on" <= ?::date`, to)
	}
	if filter.FinanceAccountID != "" {
		query = query.Where(`"finance_account_id" = ?`, filter.FinanceAccountID)
	}
	if filter.FinanceSecurityID != "" {
		query = query.Where(`"finance_security_id" = ?`, filter.FinanceSecurityID)
	}
	return query, nil
}

func (self *transaction) ListFinanceTrades(agentId string, filter *FinanceTradeFilter) (*FinanceTradePage, error) {
	if filter == nil {
		filter = &FinanceTradeFilter{}
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = FinanceTradeLimitDefault
	}
	if limit > FinanceTradeLimitMost {
		limit = FinanceTradeLimitMost
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: an offset cannot be negative", ErrInvalidArguments)
	}
	page := &FinanceTradePage{}
	if filter.ShouldCountTotal {
		counted, err := self.financeTradeQuery(agentId, filter)
		if err != nil {
			return nil, err
		}
		var totalCount int64
		if err := counted.Count(&totalCount).Error; err != nil {
			return nil, err
		}
		page.TotalCount = int(totalCount)
	}
	query, err := self.financeTradeQuery(agentId, filter)
	if err != nil {
		return nil, err
	}
	if filter.After != "" {
		tradedOn, tradeId, isCut := strings.Cut(filter.After, "/")
		tradedOn, err := parseDay(tradedOn)
		if !isCut || tradeId == "" || err != nil {
			return nil, fmt.Errorf("%w: %q is not a cursor from a page of trades", ErrInvalidArguments, filter.After)
		}
		query = query.Where(`("traded_on", "id") < (?::date, ?)`, tradedOn, tradeId)
	}
	var found []agentFinanceTradeModel
	// One more than the page, to know whether there is another.
	if err := query.Order(`"traded_on" DESC, "id" DESC`).Offset(filter.Offset).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, err
	}
	page.FinanceTrades = make([]*models.FinanceTrade, 0, len(found))
	if len(found) > limit {
		found = found[:limit]
		page.NextCursor = financeTradeCursor(&found[len(found)-1])
	}
	securityIds := []string{}
	for index := range found {
		trade := found[index].toModel()
		if trade.FinanceSecurityID != "" {
			securityIds = append(securityIds, trade.FinanceSecurityID)
		}
		page.FinanceTrades = append(page.FinanceTrades, trade)
	}
	securityById, err := self.financeSecuritiesById(agentId, securityIds)
	if err != nil {
		return nil, err
	}
	for _, trade := range page.FinanceTrades {
		trade.FinanceSecurity = securityById[trade.FinanceSecurityID]
	}
	return page, nil
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
