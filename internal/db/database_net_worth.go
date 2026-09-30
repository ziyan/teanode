package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// NetWorthOperation is what a person owns and owes and what each was worth
// on each day. Every call takes the agent id and filters on it.
type NetWorthOperation interface {
	// CreateAsset adds an asset. Its IsLiability follows its kind; its
	// valuation source is manual unless it says otherwise.
	CreateAsset(asset *models.Asset) (*models.Asset, error)

	// UpdateAsset changes an asset of the agent through a function given
	// a copy. Its id, owner, finance account and creation stay as they
	// were, and IsLiability follows the kind. ErrNotFound when the agent
	// has no such asset.
	UpdateAsset(agentId, assetId string, modify func(*models.Asset) error) (*models.Asset, error)

	// CloseAsset records the day an asset was sold or paid off; it counts
	// until that day and not after. An empty day opens it again.
	CloseAsset(agentId, assetId, closedOn string) (*models.Asset, error)

	// DeleteAsset removes an asset of the agent and its whole history.
	DeleteAsset(agentId, assetId string) error

	// GetAsset is one asset of the agent, or nil.
	GetAsset(agentId, assetId string) (*models.Asset, error)

	// ListAssets is the agent's assets, open ones first, each with the
	// valuation that counts now as its LatestValuation.
	ListAssets(agentId string) ([]*models.Asset, error)

	// RecordValuation keeps one value of an asset of the agent for a day
	// and a valuation source, replacing the one already kept for the same
	// three. Its currency is the asset's unless it says otherwise.
	RecordValuation(valuation *models.AssetValuation) (*models.AssetValuation, error)

	// DeleteValuation removes one valuation of the agent.
	DeleteValuation(agentId, valuationId string) error

	// ListAssetValuations is one asset's history, newest day first, and
	// on one day the winning valuation first.
	ListAssetValuations(agentId, assetId string) ([]*models.AssetValuation, error)

	// NetWorthSeries is, for each day from one to the other, both
	// included, the sum per currency of the winning valuation of every
	// asset open that day, liabilities subtracted. The winning valuation
	// is the latest on or before the day; among one day's, manual wins,
	// then finance_sync and agent_reading, then agent_estimate. A day and
	// currency with nothing to sum has no row. Converting is the caller's.
	NetWorthSeries(agentId, from, to string) ([]*models.NetWorthPoint, error)

	// DetachAssetsOfSource turns the assets valued by one finance source's
	// finance accounts into manual assets closed on closedOn, "2006-01-02",
	// before the source is deleted: their history stays, they stop counting
	// toward net worth after that day, and the finance account link is
	// cleared by the delete. An asset already closed keeps its day. It
	// answers how many it turned.
	DetachAssetsOfSource(agentId, sourceId, closedOn string) (int, error)
}

// netWorthSeriesDaysMost is the longest series one call builds: ten years
// of days, which is more than the chart shows and bounds the query.
const netWorthSeriesDaysMost = 3660

// valuationPrecedence orders one day's valuations of one asset, winner
// first: the person knows what they sold the car for, a balance read from
// the institution or through a connected server comes next, and an
// estimate from the web last. Two of the same rank on one day are both
// readings of the account, and the one written last wins.
func valuationPrecedence(table string) string {
	return fmt.Sprintf(`CASE "%[1]s"."valuation_source" WHEN 'manual' THEN 0 WHEN 'finance_sync' THEN 1 WHEN 'agent_reading' THEN 1 ELSE 2 END ASC, "%[1]s"."modified_at" DESC, "%[1]s"."id" DESC`, table)
}

type agentAssetModel struct {
	ID                  string     `gorm:"column:id;primaryKey"`
	AgentID             string     `gorm:"column:agent_id"`
	AssetName           string     `gorm:"column:asset_name"`
	AssetKind           string     `gorm:"column:asset_kind"`
	IsLiability         bool       `gorm:"column:is_liability"`
	CurrencyCode        string     `gorm:"column:currency_code"`
	FinanceAccountID    *string    `gorm:"column:finance_account_id"`
	ValuationSource     string     `gorm:"column:valuation_source"`
	EstimateDescription string     `gorm:"column:estimate_description"`
	IsEstimateAllowed   bool       `gorm:"column:is_estimate_allowed"`
	ClosedOn            *time.Time `gorm:"column:closed_on"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	ModifiedAt          time.Time  `gorm:"column:modified_at"`
}

func (agentAssetModel) TableName() string { return "agent_asset" }

func (self *agentAssetModel) toModel() *models.Asset {
	return &models.Asset{
		ID: self.ID, AgentID: self.AgentID, AssetName: self.AssetName, AssetKind: models.AssetKind(self.AssetKind),
		IsLiability: self.IsLiability, CurrencyCode: self.CurrencyCode, FinanceAccountID: optionalString(self.FinanceAccountID),
		ValuationSource: models.ValuationSource(self.ValuationSource), EstimateDescription: self.EstimateDescription,
		IsEstimateAllowed: self.IsEstimateAllowed, ClosedOn: formatOptionalDay(self.ClosedOn),
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

type agentAssetValuationModel struct {
	ID              string         `gorm:"column:id;primaryKey"`
	AgentID         string         `gorm:"column:agent_id"`
	AssetID         string         `gorm:"column:asset_id"`
	ValuedOn        time.Time      `gorm:"column:valued_on"`
	Value           string         `gorm:"column:value"`
	CurrencyCode    string         `gorm:"column:currency_code"`
	ValuationSource string         `gorm:"column:valuation_source"`
	EstimateLow     *string        `gorm:"column:estimate_low"`
	EstimateHigh    *string        `gorm:"column:estimate_high"`
	ValuationNote   string         `gorm:"column:valuation_note"`
	EvidenceURLs    pq.StringArray `gorm:"column:evidence_urls;type:text[]"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	ModifiedAt      time.Time      `gorm:"column:modified_at"`
}

func (agentAssetValuationModel) TableName() string { return "agent_asset_valuation" }

func (self *agentAssetValuationModel) toModel() *models.AssetValuation {
	evidenceUrls := []string(self.EvidenceURLs)
	if evidenceUrls == nil {
		evidenceUrls = []string{}
	}
	return &models.AssetValuation{
		ID: self.ID, AgentID: self.AgentID, AssetID: self.AssetID, ValuedOn: formatDay(self.ValuedOn),
		Value: self.Value, CurrencyCode: self.CurrencyCode, ValuationSource: models.ValuationSource(self.ValuationSource),
		EstimateLow: optionalString(self.EstimateLow), EstimateHigh: optionalString(self.EstimateHigh),
		ValuationNote: self.ValuationNote, EvidenceURLs: evidenceUrls,
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

// validateAsset checks an asset before it is written and fills in what
// follows from the rest.
func (self *transaction) validateAsset(asset *models.Asset) error {
	asset.AssetName = strings.TrimSpace(asset.AssetName)
	asset.CurrencyCode = strings.TrimSpace(asset.CurrencyCode)
	if asset.AgentID == "" || asset.AssetName == "" || asset.CurrencyCode == "" {
		return fmt.Errorf("%w: an asset needs an agent, a name and a currency", ErrInvalidArguments)
	}
	if !asset.AssetKind.IsValid() {
		return fmt.Errorf("%w: %q is not a kind of asset", ErrInvalidArguments, asset.AssetKind)
	}
	if asset.ValuationSource == "" {
		asset.ValuationSource = models.ValuationSourceManual
	}
	if !asset.ValuationSource.IsValid() {
		return fmt.Errorf("%w: %q is not a valuation source", ErrInvalidArguments, asset.ValuationSource)
	}
	closedOn, err := parseOptionalDay(asset.ClosedOn)
	if err != nil {
		return err
	}
	asset.ClosedOn = closedOn
	asset.IsLiability = asset.AssetKind.IsLiability()
	return nil
}

func assetToModel(asset *models.Asset) *agentAssetModel {
	model := &agentAssetModel{
		ID: asset.ID, AgentID: asset.AgentID, AssetName: asset.AssetName, AssetKind: string(asset.AssetKind),
		IsLiability: asset.IsLiability, CurrencyCode: asset.CurrencyCode, FinanceAccountID: optionalID(asset.FinanceAccountID),
		ValuationSource: string(asset.ValuationSource), EstimateDescription: asset.EstimateDescription,
		IsEstimateAllowed: asset.IsEstimateAllowed, CreatedAt: asset.CreatedAt, ModifiedAt: asset.ModifiedAt,
	}
	if asset.ClosedOn != "" {
		closedOn, _ := time.Parse(time.DateOnly, asset.ClosedOn)
		model.ClosedOn = &closedOn
	}
	return model
}

func (self *transaction) CreateAsset(asset *models.Asset) (*models.Asset, error) {
	created := *asset
	created.LatestValuation = nil
	if err := self.validateAsset(&created); err != nil {
		return nil, err
	}
	if created.FinanceAccountID != "" {
		account, err := self.GetFinanceAccount(created.AgentID, created.FinanceAccountID)
		if err != nil {
			return nil, err
		}
		if account == nil {
			return nil, ErrNotFound
		}
	}
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	if err := self.applyMutation(models.AuditResourceAsset, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(assetToModel(&created)).Error
	}); err != nil {
		return nil, err
	}
	return self.GetAsset(created.AgentID, created.ID)
}

func (self *transaction) GetAsset(agentId, assetId string) (*models.Asset, error) {
	var found []agentAssetModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, assetId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) UpdateAsset(agentId, assetId string, modify func(*models.Asset) error) (*models.Asset, error) {
	var found []agentAssetModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" = ?`, agentId, assetId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	before := found[0].toModel()
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	after.ID, after.AgentID, after.FinanceAccountID, after.CreatedAt = before.ID, before.AgentID, before.FinanceAccountID, before.CreatedAt
	after.LatestValuation = nil
	if err := self.validateAsset(&after); err != nil {
		return nil, err
	}
	after.ModifiedAt = time.Now()
	model := assetToModel(&after)
	if err := self.applyMutation(models.AuditResourceAsset, assetId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Model(&agentAssetModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, assetId).Updates(map[string]any{
			"asset_name": model.AssetName, "asset_kind": model.AssetKind, "is_liability": model.IsLiability,
			"currency_code": model.CurrencyCode, "valuation_source": model.ValuationSource,
			"estimate_description": model.EstimateDescription, "is_estimate_allowed": model.IsEstimateAllowed,
			"closed_on": model.ClosedOn, "modified_at": model.ModifiedAt,
		}).Error
	}); err != nil {
		return nil, err
	}
	return self.GetAsset(agentId, assetId)
}

func (self *transaction) CloseAsset(agentId, assetId, closedOn string) (*models.Asset, error) {
	closedOn, err := parseOptionalDay(closedOn)
	if err != nil {
		return nil, err
	}
	return self.UpdateAsset(agentId, assetId, func(asset *models.Asset) error {
		asset.ClosedOn = closedOn
		return nil
	})
}

func (self *transaction) DeleteAsset(agentId, assetId string) error {
	before, err := self.GetAsset(agentId, assetId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceAsset, assetId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, assetId).Delete(&agentAssetModel{}).Error
	})
}

func (self *transaction) ListAssets(agentId string) ([]*models.Asset, error) {
	var found []agentAssetModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).
		Order(`("closed_on" IS NOT NULL) ASC, "created_at" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	var latest []agentAssetValuationModel
	if err := self.tx.Raw(`SELECT DISTINCT ON ("valuation"."asset_id") "valuation".* FROM "agent_asset_valuation" AS "valuation"
		WHERE "valuation"."agent_id" = ?
		ORDER BY "valuation"."asset_id", "valuation"."valued_on" DESC, `+valuationPrecedence("valuation"), agentId).
		Scan(&latest).Error; err != nil {
		return nil, err
	}
	latestByAssetId := make(map[string]*models.AssetValuation, len(latest))
	for index := range latest {
		latestByAssetId[latest[index].AssetID] = latest[index].toModel()
	}
	assets := make([]*models.Asset, 0, len(found))
	for index := range found {
		asset := found[index].toModel()
		asset.LatestValuation = latestByAssetId[asset.ID]
		assets = append(assets, asset)
	}
	return assets, nil
}

// --- valuations ----------------------------------------------------------

func (self *transaction) RecordValuation(valuation *models.AssetValuation) (*models.AssetValuation, error) {
	if valuation == nil || valuation.AgentID == "" || valuation.AssetID == "" {
		return nil, fmt.Errorf("%w: a valuation needs an agent and an asset", ErrInvalidArguments)
	}
	asset, err := self.GetAsset(valuation.AgentID, valuation.AssetID)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, ErrNotFound
	}
	recorded := *valuation
	if recorded.CurrencyCode == "" {
		recorded.CurrencyCode = asset.CurrencyCode
	}
	if recorded.ValuationSource != models.ValuationSourceManual {
		return self.upsertAssetValuation(&recorded, time.Now())
	}
	// A manual valuation is the person's change, so it is audited, with
	// what it replaced.
	var before *models.AssetValuation
	valuedOn, err := parseDay(recorded.ValuedOn)
	if err != nil {
		return nil, err
	}
	var existing []agentAssetValuationModel
	if err := self.tx.Where(`"agent_id" = ? AND "asset_id" = ? AND "valued_on" = ?::date AND "valuation_source" = ?`,
		recorded.AgentID, recorded.AssetID, valuedOn, string(recorded.ValuationSource)).Limit(1).Find(&existing).Error; err != nil {
		return nil, err
	}
	auditAction := models.AuditActionCreate
	recorded.ID = newID()
	if len(existing) > 0 {
		before = existing[0].toModel()
		auditAction = models.AuditActionUpdate
		recorded.ID = before.ID
	}
	// The audit row is written after the write, so it records the
	// valuation as kept.
	if err := self.applyMutation(models.AuditResourceAssetValuation, recorded.ID, auditAction, before, &recorded, func(*gorm.DB) error {
		written, err := self.upsertAssetValuation(&recorded, time.Now())
		if err != nil {
			return err
		}
		recorded = *written
		return nil
	}); err != nil {
		return nil, err
	}
	return &recorded, nil
}

// upsertAssetValuation writes one valuation on its asset, day and
// valuation source, replacing the one there, whose id it keeps; a new one
// takes the valuation's id when it carries one. The asset is the caller's
// to have checked belongs to the agent.
func (self *transaction) upsertAssetValuation(valuation *models.AssetValuation, now time.Time) (*models.AssetValuation, error) {
	if !valuation.ValuationSource.IsValid() {
		return nil, fmt.Errorf("%w: %q is not a valuation source", ErrInvalidArguments, valuation.ValuationSource)
	}
	valuedOn, err := parseDay(valuation.ValuedOn)
	if err != nil {
		return nil, err
	}
	valuationValue, err := canonicalAmount("value", valuation.Value)
	if err != nil {
		return nil, err
	}
	estimateLow, err := canonicalOptionalAmount("estimate low", valuation.EstimateLow)
	if err != nil {
		return nil, err
	}
	estimateHigh, err := canonicalOptionalAmount("estimate high", valuation.EstimateHigh)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(valuation.CurrencyCode) == "" {
		return nil, fmt.Errorf("%w: a valuation needs a currency", ErrInvalidArguments)
	}
	evidenceUrls := valuation.EvidenceURLs
	if evidenceUrls == nil {
		evidenceUrls = []string{}
	}
	valuationId := valuation.ID
	if valuationId == "" {
		valuationId = newID()
	}
	var written []agentAssetValuationModel
	if err := self.tx.Raw(`INSERT INTO "agent_asset_valuation" ("id", "agent_id", "asset_id", "valued_on", "value", "currency_code",
			"valuation_source", "estimate_low", "estimate_high", "valuation_note", "evidence_urls", "created_at", "modified_at")
		VALUES (?, ?, ?, ?::date, ?::numeric, ?, ?, ?::numeric, ?::numeric, ?, ?::text[], ?, ?)
		ON CONFLICT ("asset_id", "valued_on", "valuation_source") DO UPDATE SET
			"value" = EXCLUDED."value", "currency_code" = EXCLUDED."currency_code",
			"estimate_low" = EXCLUDED."estimate_low", "estimate_high" = EXCLUDED."estimate_high",
			"valuation_note" = EXCLUDED."valuation_note", "evidence_urls" = EXCLUDED."evidence_urls",
			"modified_at" = EXCLUDED."modified_at"
		RETURNING *`,
		valuationId, valuation.AgentID, valuation.AssetID, valuedOn, valuationValue, strings.TrimSpace(valuation.CurrencyCode),
		string(valuation.ValuationSource), estimateLow, estimateHigh, valuation.ValuationNote, pq.Array(evidenceUrls), now, now).
		Scan(&written).Error; err != nil {
		return nil, err
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("db: the valuation was not kept")
	}
	return written[0].toModel(), nil
}

func (self *transaction) DeleteValuation(agentId, valuationId string) error {
	var found []agentAssetValuationModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, valuationId).Limit(1).Find(&found).Error; err != nil {
		return err
	}
	if len(found) == 0 {
		return ErrNotFound
	}
	remove := func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, valuationId).Delete(&agentAssetValuationModel{}).Error
	}
	if found[0].ValuationSource != string(models.ValuationSourceManual) {
		return remove(self.tx)
	}
	return self.applyMutation(models.AuditResourceAssetValuation, valuationId, models.AuditActionDelete, found[0].toModel(), nil, remove)
}

func (self *transaction) ListAssetValuations(agentId, assetId string) ([]*models.AssetValuation, error) {
	var found []agentAssetValuationModel
	if err := self.tx.Raw(`SELECT "valuation".* FROM "agent_asset_valuation" AS "valuation"
		WHERE "valuation"."agent_id" = ? AND "valuation"."asset_id" = ?
		ORDER BY "valuation"."valued_on" DESC, `+valuationPrecedence("valuation"), agentId, assetId).Scan(&found).Error; err != nil {
		return nil, err
	}
	valuations := make([]*models.AssetValuation, 0, len(found))
	for index := range found {
		valuations = append(valuations, found[index].toModel())
	}
	return valuations, nil
}

// --- net worth -------------------------------------------------------------

func (self *transaction) NetWorthSeries(agentId, from, to string) ([]*models.NetWorthPoint, error) {
	from, err := parseDay(from)
	if err != nil {
		return nil, err
	}
	to, err = parseDay(to)
	if err != nil {
		return nil, err
	}
	fromDay, _ := time.Parse(time.DateOnly, from)
	toDay, _ := time.Parse(time.DateOnly, to)
	if toDay.Before(fromDay) {
		return nil, fmt.Errorf("%w: the series ends before it starts", ErrInvalidArguments)
	}
	if toDay.Sub(fromDay) > netWorthSeriesDaysMost*24*time.Hour {
		return nil, ErrTooMuchAsked
	}
	// For each day and each asset open that day, the one valuation that
	// wins: the latest on or before the day, and among that day's the
	// first by precedence. Then per day and currency, the sum with
	// liabilities subtracted.
	var rows []struct {
		NetWorthOn     string `gorm:"column:net_worth_on"`
		CurrencyCode   string `gorm:"column:currency_code"`
		NetWorthAmount string `gorm:"column:net_worth_amount"`
	}
	if err := self.tx.Raw(`SELECT to_char("series"."day", 'YYYY-MM-DD') AS "net_worth_on", "winning"."currency_code",
			SUM(CASE WHEN "asset"."is_liability" THEN -"winning"."value" ELSE "winning"."value" END)::text AS "net_worth_amount"
		FROM generate_series(?::date, ?::date, interval '1 day') AS "series" ("day")
		JOIN "agent_asset" AS "asset"
		  ON "asset"."agent_id" = ? AND ("asset"."closed_on" IS NULL OR "series"."day"::date <= "asset"."closed_on")
		CROSS JOIN LATERAL (
			SELECT "valuation"."value", "valuation"."currency_code"
			FROM "agent_asset_valuation" AS "valuation"
			WHERE "valuation"."asset_id" = "asset"."id" AND "valuation"."agent_id" = "asset"."agent_id"
			  AND "valuation"."valued_on" <= "series"."day"::date
			ORDER BY "valuation"."valued_on" DESC, `+valuationPrecedence("valuation")+`
			LIMIT 1
		) AS "winning"
		GROUP BY "series"."day", "winning"."currency_code"
		ORDER BY "series"."day" ASC, "winning"."currency_code" ASC`, from, to, agentId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	points := make([]*models.NetWorthPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, &models.NetWorthPoint{NetWorthOn: row.NetWorthOn, CurrencyCode: row.CurrencyCode, NetWorthAmount: row.NetWorthAmount})
	}
	return points, nil
}

func (self *transaction) DetachAssetsOfSource(agentId, sourceId, closedOn string) (int, error) {
	var found []agentAssetModel
	if err := self.tx.Where(`"agent_id" = ? AND "valuation_source" = ? AND "finance_account_id" IN
			(SELECT "id" FROM "agent_finance_account" WHERE "agent_id" = ? AND "source_id" = ?)`,
		agentId, string(models.ValuationSourceFinanceSync), agentId, sourceId).Find(&found).Error; err != nil {
		return 0, err
	}
	for index := range found {
		if _, err := self.UpdateAsset(agentId, found[index].ID, func(asset *models.Asset) error {
			asset.ValuationSource = models.ValuationSourceManual
			if asset.ClosedOn == "" {
				asset.ClosedOn = closedOn
			}
			return nil
		}); err != nil {
			return 0, err
		}
	}
	return len(found), nil
}
