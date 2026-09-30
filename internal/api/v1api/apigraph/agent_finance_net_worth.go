package apigraph

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// Net worth: assets, their valuations, and the sum of them over time. Part
// of the finance area (FinanceQuery, FinanceMutation).

// NetWorthView is net worth per day over a range.
type NetWorthView struct {
	// From and To are the range, both included, "2006-01-02".
	From string `json:"from"`
	To   string `json:"to"`

	// NetWorthPoints is each day's net worth per currency, never added
	// across currencies.
	NetWorthPoints []*models.NetWorthPoint `json:"netWorthPoints"`

	// ReportingCurrencyCode is what ConvertedNetWorthPoints are in: each
	// currency's total converted at that day's rate. A currency with no
	// rate is left out of every day and named in UnconvertedCurrencyCodes.
	ReportingCurrencyCode    string                    `json:"reportingCurrencyCode,omitempty" graphapi:"nullable"`
	ConvertedNetWorthPoints  []*ConvertedNetWorthPoint `json:"convertedNetWorthPoints"`
	UnconvertedCurrencyCodes []string                  `json:"unconvertedCurrencyCodes"`
}

// ConvertedNetWorthPoint is one day's net worth in the reporting currency.
type ConvertedNetWorthPoint struct {
	NetWorthOn     string `json:"netWorthOn"`
	NetWorthAmount string `json:"netWorthAmount"`
}

// AssetHistoryView is one asset and its valuations, newest day first.
type AssetHistoryView struct {
	Asset           *models.Asset            `json:"asset"`
	AssetValuations []*models.AssetValuation `json:"assetValuations"`
}

// NetWorthArguments are a range of days ("2006-01-02"; the last thirty
// days to today when left out) and the currency to convert into instead of
// the reporting currency.
type NetWorthArguments struct {
	From         string `json:"from" graphapi:"nullable"`
	To           string `json:"to" graphapi:"nullable"`
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

// AssetArguments name one of the caller's assets.
type AssetArguments struct {
	AssetID string `json:"assetId"`
}

// CreateAssetArguments describe a new asset.
type CreateAssetArguments struct {
	AssetName string `json:"assetName"`

	// AssetKind is cash, investment, retirement, property, vehicle,
	// other_asset, credit_card, loan, mortgage or other_liability; the
	// last four subtract from net worth.
	AssetKind    string `json:"assetKind"`
	CurrencyCode string `json:"currencyCode"`

	// ValuationSource is where its values normally come from: manual (the
	// default), agent_reading or agent_estimate. finance_sync belongs to
	// the assets finance sources make.
	ValuationSource string `json:"valuationSource" graphapi:"nullable"`

	// EstimateDescription is what the agent may search the web with, and
	// IsEstimateAllowed whether it may estimate this asset at all.
	EstimateDescription string `json:"estimateDescription" graphapi:"nullable"`
	IsEstimateAllowed   *bool  `json:"isEstimateAllowed" graphapi:"nullable"`

	// Value, when given, is the asset's first valuation, recorded with
	// the asset's valuation source on ValuedOn ("2006-01-02", today when
	// left out), in the same change as the asset.
	Value    string `json:"value" graphapi:"nullable"`
	ValuedOn string `json:"valuedOn" graphapi:"nullable"`
}

// UpdateAssetArguments change what is given and leave the rest.
type UpdateAssetArguments struct {
	AssetID             string  `json:"assetId"`
	AssetName           *string `json:"assetName" graphapi:"nullable"`
	AssetKind           *string `json:"assetKind" graphapi:"nullable"`
	CurrencyCode        *string `json:"currencyCode" graphapi:"nullable"`
	ValuationSource     *string `json:"valuationSource" graphapi:"nullable"`
	EstimateDescription *string `json:"estimateDescription" graphapi:"nullable"`
	IsEstimateAllowed   *bool   `json:"isEstimateAllowed" graphapi:"nullable"`
}

// CloseAssetArguments give the day an asset was sold or paid off (today
// when left out), or ask to open it again.
type CloseAssetArguments struct {
	AssetID      string `json:"assetId"`
	ClosedOn     string `json:"closedOn" graphapi:"nullable"`
	ShouldReopen *bool  `json:"shouldReopen" graphapi:"nullable"`
}

// RecordValuationArguments are one value of an asset for a day.
type RecordValuationArguments struct {
	AssetID string `json:"assetId"`

	// Value is the size of the thing, positive for what is owed too: the
	// asset's kind gives the sign.
	Value string `json:"value"`

	// ValuedOn is the day, "2006-01-02", today when left out.
	ValuedOn string `json:"valuedOn" graphapi:"nullable"`

	// ValuationSource is manual (the default, and what the dashboard and
	// the command line record), or agent_reading or agent_estimate, which
	// the agent records. finance_sync is refused: those come from syncs.
	// An estimate needs an asset whose person allowed estimates.
	ValuationSource string `json:"valuationSource" graphapi:"nullable"`

	// An estimate's range, what it rests on, and the pages it read.
	EstimateLow   string   `json:"estimateLow" graphapi:"nullable"`
	EstimateHigh  string   `json:"estimateHigh" graphapi:"nullable"`
	ValuationNote string   `json:"valuationNote" graphapi:"nullable"`
	EvidenceURLs  []string `json:"evidenceUrls" graphapi:"nullable"`
}

// ValuationArguments name one of the caller's valuations.
type ValuationArguments struct {
	ValuationID string `json:"valuationId"`
}

// netWorthDaysMost is the longest range a net worth series covers: ten
// years of days.
const netWorthDaysMost = 3660

func (self *graph) NetWorth(ctx context.Context, arguments NetWorthArguments) (*NetWorthView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	to, err := dayArgument("to", arguments.To, personToday(principal))
	if err != nil {
		return nil, err
	}
	toDay, _ := time.Parse(time.DateOnly, to)
	from, err := dayArgument("from", arguments.From, toDay.AddDate(0, 0, -30).Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	fromDay, _ := time.Parse(time.DateOnly, from)
	if fromDay.After(toDay) {
		return nil, fmt.Errorf("%w: from %s is after to %s", api.ErrInvalidArguments, from, to)
	}
	if toDay.Sub(fromDay) > netWorthDaysMost*24*time.Hour {
		return nil, fmt.Errorf("%w: a net worth series covers at most %d days", api.ErrInvalidArguments, netWorthDaysMost)
	}
	tx := self.transaction(ctx)
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	points, err := tx.NetWorthSeries(found.ID, from, to)
	if err != nil {
		return nil, financeError(err)
	}
	view := &NetWorthView{
		From: from, To: to, NetWorthPoints: points, ReportingCurrencyCode: currencyCode,
		ConvertedNetWorthPoints: []*ConvertedNetWorthPoint{}, UnconvertedCurrencyCodes: []string{},
	}
	if view.NetWorthPoints == nil {
		view.NetWorthPoints = []*models.NetWorthPoint{}
	}
	if currencyCode == "" {
		return view, nil
	}
	converter := rates.NewConverter(ctx, self.exchangeRateFetcher(), tx)
	unconverted := map[string]bool{}
	type dayCurrency struct {
		day          string
		currencyCode string
	}
	convertedByDayCurrency := map[dayCurrency]*big.Rat{}
	for _, point := range points {
		if unconverted[point.CurrencyCode] {
			continue
		}
		converted, isConverted, err := convertOrSkip(converter, point.NetWorthAmount, point.CurrencyCode, currencyCode, point.NetWorthOn)
		if err != nil {
			return nil, financeError(err)
		}
		if !isConverted {
			unconverted[point.CurrencyCode] = true
			continue
		}
		convertedByDayCurrency[dayCurrency{day: point.NetWorthOn, currencyCode: point.CurrencyCode}] = converted
	}
	totalByDay := map[string]*big.Rat{}
	for key, converted := range convertedByDayCurrency {
		if unconverted[key.currencyCode] {
			continue
		}
		if totalByDay[key.day] == nil {
			totalByDay[key.day] = new(big.Rat)
		}
		totalByDay[key.day].Add(totalByDay[key.day], converted)
	}
	days := make([]string, 0, len(totalByDay))
	for day := range totalByDay {
		days = append(days, day)
	}
	sort.Strings(days)
	for _, day := range days {
		view.ConvertedNetWorthPoints = append(view.ConvertedNetWorthPoints, &ConvertedNetWorthPoint{NetWorthOn: day, NetWorthAmount: finance.FormatAmount(totalByDay[day])})
	}
	view.UnconvertedCurrencyCodes = sortedCurrencyCodes(unconverted)
	return view, nil
}

func (self *graph) Assets(ctx context.Context) ([]*models.Asset, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	assets, err := self.transaction(ctx).ListAssets(found.ID)
	if err != nil {
		return nil, err
	}
	if assets == nil {
		assets = []*models.Asset{}
	}
	return assets, nil
}

// ownAsset is one of the caller's assets, or not found.
func ownAsset(ctx context.Context, self *graph, agentId, assetId string) (*models.Asset, error) {
	if strings.TrimSpace(assetId) == "" {
		return nil, fmt.Errorf("%w: which asset", api.ErrInvalidArguments)
	}
	asset, err := self.transaction(ctx).GetAsset(agentId, strings.TrimSpace(assetId))
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, api.ErrNotFound
	}
	return asset, nil
}

func (self *graph) AssetHistory(ctx context.Context, arguments AssetArguments) (*AssetHistoryView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	asset, err := ownAsset(ctx, self, found.ID, arguments.AssetID)
	if err != nil {
		return nil, err
	}
	valuations, err := self.transaction(ctx).ListAssetValuations(found.ID, asset.ID)
	if err != nil {
		return nil, err
	}
	if valuations == nil {
		valuations = []*models.AssetValuation{}
	}
	return &AssetHistoryView{Asset: asset, AssetValuations: valuations}, nil
}

// assetValuationSourceArgument is a valuation source a person may give an
// asset: anything but finance_sync, which belongs to the assets finance
// sources make.
func assetValuationSourceArgument(value string) (models.ValuationSource, error) {
	valuationSource := models.ValuationSource(strings.TrimSpace(value))
	if valuationSource == "" {
		return models.ValuationSourceManual, nil
	}
	if !valuationSource.IsValid() {
		return "", fmt.Errorf("%w: %q is not manual, agent_reading or agent_estimate", api.ErrInvalidArguments, valuationSource)
	}
	if valuationSource == models.ValuationSourceFinanceSync {
		return "", fmt.Errorf("%w: finance_sync assets are made by finance sources, one for each finance account", api.ErrInvalidArguments)
	}
	return valuationSource, nil
}

func (self *graph) CreateAsset(ctx context.Context, arguments CreateAssetArguments) (*models.Asset, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	currencyCode, err := currencyArgument("currencyCode", arguments.CurrencyCode, "")
	if err != nil {
		return nil, err
	}
	if currencyCode == "" {
		return nil, fmt.Errorf("%w: an asset needs a currency, a code like USD", api.ErrInvalidArguments)
	}
	valuationSource, err := assetValuationSourceArgument(arguments.ValuationSource)
	if err != nil {
		return nil, err
	}
	asset := &models.Asset{
		AgentID: found.ID, AssetName: strings.TrimSpace(arguments.AssetName), AssetKind: models.AssetKind(strings.TrimSpace(arguments.AssetKind)),
		CurrencyCode: currencyCode, ValuationSource: valuationSource, EstimateDescription: strings.TrimSpace(arguments.EstimateDescription),
	}
	if arguments.IsEstimateAllowed != nil {
		asset.IsEstimateAllowed = *arguments.IsEstimateAllowed
	}
	// The first value is read before anything is written: the request
	// commits what was written even when the resolver then fails.
	var firstValuation *models.AssetValuation
	if strings.TrimSpace(arguments.Value) != "" {
		value, err := amountArgument("value", arguments.Value)
		if err != nil {
			return nil, err
		}
		valuedOn, err := dayArgument("valuedOn", arguments.ValuedOn, personToday(principal))
		if err != nil {
			return nil, err
		}
		if valuationSource == models.ValuationSourceAgentEstimate && !asset.IsEstimateAllowed {
			return nil, fmt.Errorf("%w: estimates are not allowed for %q; the person can allow them on the asset", api.ErrInvalidArguments, asset.AssetName)
		}
		firstValuation = &models.AssetValuation{AgentID: found.ID, ValuedOn: valuedOn, Value: value, CurrencyCode: currencyCode, ValuationSource: valuationSource}
	} else if strings.TrimSpace(arguments.ValuedOn) != "" {
		return nil, fmt.Errorf("%w: valuedOn is the day of the first value, which was not given", api.ErrInvalidArguments)
	}
	var created *models.Asset
	err = self.writing(ctx).TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		if created, err = tx.CreateAsset(asset); err != nil {
			return err
		}
		if firstValuation == nil {
			return nil
		}
		firstValuation.AssetID = created.ID
		// The one value it has is the one that counts.
		created.LatestValuation, err = tx.RecordValuation(firstValuation)
		return err
	})
	if err != nil {
		return nil, financeError(err)
	}
	if isEstimatedByAgent(created) {
		self.startAssetEstimates(ctx, self.writing(ctx), principal, found)
	}
	return created, nil
}

func (self *graph) UpdateAsset(ctx context.Context, arguments UpdateAssetArguments) (*models.Asset, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	var currencyCode string
	if arguments.CurrencyCode != nil {
		if currencyCode, err = currencyArgument("currencyCode", *arguments.CurrencyCode, ""); err != nil {
			return nil, err
		}
	}
	var valuationSource models.ValuationSource
	if arguments.ValuationSource != nil {
		if valuationSource, err = assetValuationSourceArgument(*arguments.ValuationSource); err != nil {
			return nil, err
		}
	}
	// Whether the agent estimated it before this change, so allowing it
	// starts the estimates once and a rename does not.
	wasEstimatedByAgent := false
	updated, err := self.writing(ctx).UpdateAsset(found.ID, strings.TrimSpace(arguments.AssetID), func(asset *models.Asset) error {
		wasEstimatedByAgent = isEstimatedByAgent(asset)
		// A finance account's asset is valued by its syncs, in the
		// account's currency.
		isFromSync := asset.FinanceAccountID != "" && asset.ValuationSource == models.ValuationSourceFinanceSync
		if arguments.AssetName != nil {
			asset.AssetName = strings.TrimSpace(*arguments.AssetName)
		}
		if arguments.AssetKind != nil {
			asset.AssetKind = models.AssetKind(strings.TrimSpace(*arguments.AssetKind))
		}
		if arguments.CurrencyCode != nil && currencyCode != asset.CurrencyCode {
			if isFromSync {
				return fmt.Errorf("%w: this asset is valued by its finance account's syncs, in the account's currency", api.ErrInvalidArguments)
			}
			if currencyCode == "" {
				return fmt.Errorf("%w: an asset needs a currency", api.ErrInvalidArguments)
			}
			asset.CurrencyCode = currencyCode
		}
		if arguments.ValuationSource != nil && valuationSource != asset.ValuationSource {
			if isFromSync {
				return fmt.Errorf("%w: this asset is valued by its finance account's syncs", api.ErrInvalidArguments)
			}
			asset.ValuationSource = valuationSource
		}
		if arguments.EstimateDescription != nil {
			asset.EstimateDescription = strings.TrimSpace(*arguments.EstimateDescription)
		}
		if arguments.IsEstimateAllowed != nil {
			asset.IsEstimateAllowed = *arguments.IsEstimateAllowed
		}
		return nil
	})
	if err == nil && !wasEstimatedByAgent && isEstimatedByAgent(updated) {
		self.startAssetEstimates(ctx, self.writing(ctx), principal, found)
	}
	return updated, financeError(err)
}

func (self *graph) CloseAsset(ctx context.Context, arguments CloseAssetArguments) (*models.Asset, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	closedOn, err := dayArgument("closedOn", arguments.ClosedOn, personToday(principal))
	if err != nil {
		return nil, err
	}
	if arguments.ShouldReopen != nil && *arguments.ShouldReopen {
		closedOn = ""
	}
	closed, err := self.writing(ctx).CloseAsset(found.ID, strings.TrimSpace(arguments.AssetID), closedOn)
	return closed, financeError(err)
}

// DeleteAsset is allowed on a server that no longer offers finance: what
// is the person's to remove stays theirs to remove.
func (self *graph) DeleteAsset(ctx context.Context, arguments AssetArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.writing(ctx).DeleteAsset(found.ID, strings.TrimSpace(arguments.AssetID)); err != nil {
		return false, financeError(err)
	}
	return true, nil
}

func (self *graph) RecordValuation(ctx context.Context, arguments RecordValuationArguments) (*models.AssetValuation, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	asset, err := ownAsset(ctx, self, found.ID, arguments.AssetID)
	if err != nil {
		return nil, err
	}
	valuationSource := models.ValuationSource(strings.TrimSpace(arguments.ValuationSource))
	if valuationSource == "" {
		valuationSource = models.ValuationSourceManual
	}
	switch valuationSource {
	case models.ValuationSourceManual:
	case models.ValuationSourceAgentReading, models.ValuationSourceAgentEstimate:
		// The agent does not record over a finance account's syncs: its
		// balance is what the provider reports.
		if asset.ValuationSource == models.ValuationSourceFinanceSync {
			return nil, fmt.Errorf("%w: %q is valued by its finance account's syncs", api.ErrInvalidArguments, asset.AssetName)
		}
		if valuationSource == models.ValuationSourceAgentEstimate && !asset.IsEstimateAllowed {
			return nil, fmt.Errorf("%w: estimates are not allowed for %q; the person can allow them on the asset", api.ErrInvalidArguments, asset.AssetName)
		}
	case models.ValuationSourceFinanceSync:
		return nil, fmt.Errorf("%w: finance_sync valuations come from syncs only", api.ErrInvalidArguments)
	default:
		return nil, fmt.Errorf("%w: %q is not manual, agent_reading or agent_estimate", api.ErrInvalidArguments, valuationSource)
	}
	value, err := amountArgument("value", arguments.Value)
	if err != nil {
		return nil, err
	}
	valuedOn, err := dayArgument("valuedOn", arguments.ValuedOn, personToday(principal))
	if err != nil {
		return nil, err
	}
	estimateLow, err := optionalAmountArgument("estimateLow", arguments.EstimateLow)
	if err != nil {
		return nil, err
	}
	estimateHigh, err := optionalAmountArgument("estimateHigh", arguments.EstimateHigh)
	if err != nil {
		return nil, err
	}
	evidenceUrls := []string{}
	for _, evidenceUrl := range arguments.EvidenceURLs {
		evidenceUrl = strings.TrimSpace(evidenceUrl)
		if evidenceUrl == "" {
			continue
		}
		if !strings.HasPrefix(evidenceUrl, "https://") && !strings.HasPrefix(evidenceUrl, "http://") {
			return nil, fmt.Errorf("%w: evidence %q is not a web address", api.ErrInvalidArguments, evidenceUrl)
		}
		evidenceUrls = append(evidenceUrls, evidenceUrl)
	}
	recorded, err := self.writing(ctx).RecordValuation(&models.AssetValuation{
		AgentID: found.ID, AssetID: asset.ID, ValuedOn: valuedOn, Value: value, CurrencyCode: asset.CurrencyCode,
		ValuationSource: valuationSource, EstimateLow: estimateLow, EstimateHigh: estimateHigh,
		ValuationNote: strings.TrimSpace(arguments.ValuationNote), EvidenceURLs: evidenceUrls,
	})
	return recorded, financeError(err)
}

// DeleteValuation is allowed on a server that no longer offers finance,
// as DeleteAsset is.
func (self *graph) DeleteValuation(ctx context.Context, arguments ValuationArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.writing(ctx).DeleteValuation(found.ID, strings.TrimSpace(arguments.ValuationID)); err != nil {
		return false, financeError(err)
	}
	return true, nil
}
