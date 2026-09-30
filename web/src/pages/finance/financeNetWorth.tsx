import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Field, Loading, Tag, formatMoney } from '../../components/common'
import { ArrowLeftIcon } from '../../components/icons'
import { Column, DataTable } from '../../components/dataTable'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SeriesChart, dayLabel } from '../../components/seriesChart'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useIsDesktop } from '../../components/sidebar'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import {
  ASSETS,
  ASSET_HISTORY,
  Asset,
  AssetHistory,
  AssetValuation,
  CLOSE_ASSET,
  CONVERT_CURRENCY,
  CREATE_ASSET,
  CurrencyConversion,
  DELETE_ASSET,
  DELETE_VALUATION,
  FINANCE_ACCOUNTS,
  FINANCE_TRADES,
  FinanceAccount,
  FinanceTrade,
  FinanceTradePage,
  NET_WORTH,
  NetWorth,
  RECORD_VALUATION,
  UPDATE_ASSET,
  amountOf,
  daysBefore,
  formatDay,
  formatQuantity,
  hasAmount,
  isDecimal,
  isHolding,
  personToday,
} from './financeApi'
import {
  CurrencyPicker,
  Money,
  accountLabel,
  UnconvertedNote,
  compactMoney,
  useAct,
  useFinanceWords,
  useReportingCurrency,
} from './financeCommon'
import { AssetFilters, assetFiltersFromSearch, groupAssets, writeAssetFilters } from './assetFilters'
import { RING_SLICE_COUNT, SpendingRing, foldIntoOther, ringSliceClass } from './spendingRing'

// The valuation sources a person gives an asset they add or change: their
// own values, or values their agent reads (from a connected server, say).
// Estimates are allowed separately, and finance_sync belongs to the assets
// finance sources make.
const CHOSEN_VALUATION_SOURCES = ['manual', 'agent_estimate', 'agent_reading']

// What each valuation source a person can choose means, said under the
// choice for the one chosen.
const VALUATION_SOURCE_HINTS: Record<string, Key> = {
  manual: 'finance.valuationSourceHint.manual',
  agent_estimate: 'finance.valuationSourceHint.agent_estimate',
  agent_reading: 'finance.valuationSourceHint.agent_reading',
}

const ASSET_KINDS = [
  'cash',
  'investment',
  'retirement',
  'property',
  'vehicle',
  'other_asset',
  'credit_card',
  'loan',
  'mortgage',
  'other_liability',
]

// How many trades one read brings, and one Load more adds.
const TRADE_PAGE_SIZE = 100

// How far back the chart reaches, in days; zero is from the first
// valuation there is.
const RANGES: {
  id: string
  days: number
  label: 'finance.range30' | 'finance.range90' | 'finance.range365' | 'finance.rangeAll'
}[] = [
  { id: '30', days: 30, label: 'finance.range30' },
  { id: '90', days: 90, label: 'finance.range90' },
  { id: '365', days: 365, label: 'finance.range365' },
  { id: 'all', days: 0, label: 'finance.rangeAll' },
]

// The Net worth section: the total over time in the reporting currency,
// then every asset with its latest value, the day of it and where it came
// from. An asset opens to a page of its own in the same place, named in
// the address so it can be linked to and gone back from.
export function FinanceNetWorthSection() {
  const [parameters, setParameters] = useSearchParams()
  const assetId = parameters.get('asset')
  const assets = useQuery(() => graphql<{ Assets: Asset[] }>(ASSETS), [], {
    refresh: false,
  })
  const chosen = assets.data?.Assets.find((asset) => asset.id === assetId) ?? null
  // The address of an asset's page: this one, naming the asset.
  const addressOf = (id: string | null) => {
    const written = new URLSearchParams(parameters)
    if (id) {
      written.set('asset', id)
    } else {
      written.delete('asset')
    }
    return written
  }
  if (assetId) {
    if (assets.loading && !assets.data) return <Loading />
    return <AssetPage asset={chosen} onBack={() => setParameters(addressOf(null))} onChanged={assets.reload} />
  }
  return (
    <>
      <NetWorthChart />
      <AssetsPanel assets={assets} linkTo={(id) => `?${addressOf(id).toString()}`} />
    </>
  )
}

function NetWorthChart() {
  const { t } = useTranslation()
  const [range, setRange] = useState('90')
  const days = RANGES.find((candidate) => candidate.id === range)?.days ?? 90
  const today = personToday()
  const from = days > 0 ? daysBefore(today, days) : undefined
  const { data, error, loading } = useQuery(
    () => graphql<{ NetWorth: NetWorth }>(NET_WORTH, { from, to: today }),
    [range],
    { refresh: false },
  )
  const worth = data?.NetWorth
  const points = worth?.convertedNetWorthPoints ?? []
  const currency = worth?.reportingCurrencyCode || 'USD'
  const latest = points[points.length - 1]
  return (
    <SettingsSection
      card
      title={t('finance.netWorthTitle')}
      description={t('finance.netWorthHint')}
      action={
        <div className="segmented" role="group" aria-label={t('finance.range')}>
          {RANGES.map((candidate) => (
            <button
              key={candidate.id}
              type="button"
              className={range === candidate.id ? 'active' : ''}
              aria-pressed={range === candidate.id}
              onClick={() => setRange(candidate.id)}
            >
              {t(candidate.label)}
            </button>
          ))}
        </div>
      }
    >
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {worth && points.length === 0 ? <SettingsEmpty>{t('finance.noNetWorth')}</SettingsEmpty> : null}
      {points.length > 0 ? (
        <SeriesChart
          label={t('finance.netWorthTitle')}
          keys={points.map((point) => point.netWorthOn)}
          keyLabel={dayLabel}
          format={(value) => formatMoney(value, currency)}
          axisFormat={(value) => compactMoney(value, currency)}
          headline={formatMoney(amountOf(latest?.netWorthAmount), currency)}
          caption={latest ? t('finance.netWorthOn', { day: formatDay(latest.netWorthOn) }) : undefined}
          series={[
            {
              id: 'netWorth',
              label: t('finance.netWorthTitle'),
              tone: 'output',
              shape: 'line',
              values: points.map((point) => amountOf(point.netWorthAmount)),
            },
          ]}
        />
      ) : null}
      <UnconvertedNote currencyCodes={worth?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}

// The assets, a group a kind: what share of what is owned each kind is, as
// a ring whose legend is the table under it, and what is owed beside it.
// A kind is one line until it is opened, so a brokerage's hundred holdings
// are one Investment line beside the house and the car; opened, its
// holdings sit under their finance account. Words search every name and
// open the kinds they match. Every row is one line: a holding's quantity
// and price are a column of their own rather than a line under its value.
function AssetsPanel({
  assets,
  linkTo,
}: {
  assets: { data: { Assets: Asset[] } | null; error: unknown; loading: boolean; reload: () => Promise<void> }
  linkTo: (id: string) => string
}) {
  const { t, plural } = useTranslation()
  const words = useFinanceWords()
  const isDesktop = useIsDesktop()
  const [adding, setAdding] = useState(false)
  const [search, setSearch] = useSearchParams()
  const filters = useMemo(() => assetFiltersFromSearch(search), [search])
  const setFilters = (change: (previous: AssetFilters) => AssetFilters) =>
    setSearch((previous) => writeAssetFilters(previous, change(assetFiltersFromSearch(previous))), { replace: true })
  const { reportingCurrencyCode } = useReportingCurrency()
  const accounts = useQuery(() => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS), [], {
    refresh: false,
  })
  const all = useMemo(() => assets.data?.Assets ?? [], [assets.data])
  // One rate a currency the assets are held in, to the reporting currency,
  // so the kinds can be added up; a currency without one is named under
  // the ring rather than added in.
  const foreignCurrencyCodes = [
    ...new Set(
      all
        .map((asset) => asset.latestValuation?.currencyCode ?? '')
        .filter((code) => code && code !== reportingCurrencyCode),
    ),
  ].sort()
  const rates = useQuery(
    async () => {
      const found: Record<string, number> = {}
      if (!reportingCurrencyCode) return found
      const answers = await Promise.allSettled(
        foreignCurrencyCodes.map((fromCurrencyCode) =>
          graphql<{ ConvertCurrency: CurrencyConversion }>(CONVERT_CURRENCY, {
            amount: '1',
            fromCurrencyCode,
            toCurrencyCode: reportingCurrencyCode,
          }),
        ),
      )
      answers.forEach((answer, index) => {
        if (answer.status === 'fulfilled') found[foreignCurrencyCodes[index]] = amountOf(answer.value.ConvertCurrency.convertedAmount)
      })
      return found
    },
    [reportingCurrencyCode, foreignCurrencyCodes.join(',')],
    { refresh: false },
  )
  const grouping = useMemo(
    () => groupAssets(all, filters, reportingCurrencyCode, rates.data ?? {}),
    [all, filters, reportingCurrencyCode, rates.data],
  )
  const currency = reportingCurrencyCode || 'USD'
  const slices = foldIntoOther(
    grouping.groups
      .filter((group) => !group.isLiability)
      .map((group) => ({ key: group.assetKind, label: words.assetKind(group.assetKind), amount: group.totalAmount })),
    RING_SLICE_COUNT,
  )
  const sliceIndexes = new Map(slices.map((slice, index) => [slice.key, index]))
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
  const accountName = (id?: string | null) => {
    const account = (accounts.data?.FinanceAccounts ?? []).find((candidate) => candidate.id === id)
    return account ? accountLabel(account) : ''
  }
  const isSearching = filters.text !== ''
  const isExpanded = (assetKind: string) => isSearching || filters.expandedAssetKinds.includes(assetKind)
  const toggle = (assetKind: string) =>
    setFilters((previous) => ({
      ...previous,
      expandedAssetKinds: previous.expandedAssetKinds.includes(assetKind)
        ? previous.expandedAssetKinds.filter((kind) => kind !== assetKind)
        : [...previous.expandedAssetKinds, assetKind],
    }))
  const filterWords = [
    filters.text ? t('finance.containingWords', { text: filters.text }) : '',
    filters.isClosedShown ? t('finance.closedIncluded') : '',
  ].filter(Boolean)

  const filterControls = (
    <>
      <div className="row finance-filters">
        <label className="finance-asset-search">
          <span>{t('finance.searchAssets')}</span>
          <input
            type="search"
            value={filters.text}
            onChange={(event) => {
              const text = event.target.value
              setFilters((previous) => ({ ...previous, text }))
            }}
          />
        </label>
      </div>
      {all.some((asset) => asset.closedOn) ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={filters.isClosedShown}
            onChange={(event) => {
              const isClosedShown = event.target.checked
              setFilters((previous) => ({ ...previous, isClosedShown }))
            }}
          />
          {t('finance.showClosed')}
        </label>
      ) : null}
    </>
  )

  // One row an asset: its name (a link to its page, one line, the whole
  // name its title), its value, a holding's quantity and price, the day of
  // the value and where it came from.
  const assetRow = (asset: Asset) => {
    const valuation = asset.latestValuation
    return (
      <tr key={asset.id} className="finance-asset-row">
        <td>
          <Link className="finance-asset-name" to={linkTo(asset.id)} title={asset.assetName}>
            {asset.assetName}
          </Link>
          {asset.closedOn ? (
            <>
              {' '}
              <Tag value={t('finance.closed')} />
            </>
          ) : null}
        </td>
        <td className="numeric">
          {valuation ? (
            <Money amount={signedValue(asset)} currency={valuation.currencyCode} />
          ) : (
            <span className="muted">—</span>
          )}
          {/* The room the kind's share takes on its line, so the values
              line up down the column. */}
          <span className="finance-share" aria-hidden="true" />
        </td>
        <td className="numeric optional muted">{valuation ? holdingText(valuation, t) : ''}</td>
        <td className="muted">{valuation ? formatDay(valuation.valuedOn) : '—'}</td>
        <td className="optional">{words.valuationSource(valuation?.valuationSource ?? asset.valuationSource)}</td>
      </tr>
    )
  }

  // A kind's assets, its holdings under a line naming their account.
  const groupRows = (list: Asset[]) => {
    const rows: React.ReactNode[] = []
    let accountShown: string | null = null
    for (const asset of list) {
      if (isHolding(asset) && asset.financeAccountId !== accountShown) {
        accountShown = asset.financeAccountId ?? ''
        const holdingCount = list.filter(
          (candidate) => isHolding(candidate) && candidate.financeAccountId === asset.financeAccountId,
        ).length
        rows.push(
          <tr key={`account-${accountShown}`} className="finance-asset-account">
            <td colSpan={5} className="muted">
              {plural(
                holdingCount,
                { one: 'finance.holdingsInOne', other: 'finance.holdingsInOther' },
                { account: accountName(asset.financeAccountId) || t('finance.deletedFinanceAccount') },
              )}
            </td>
          </tr>,
        )
      }
      rows.push(assetRow(asset))
    }
    return rows
  }

  return (
    <SettingsSection
      card
      title={t('finance.assetsTitle')}
      description={t('finance.assetsHint')}
      action={
        <button type="button" className="primary" onClick={() => setAdding(true)}>
          {t('finance.addAsset')}
        </button>
      }
    >
      <ErrorMessage error={assets.error} />
      {assets.loading && !assets.data ? <Loading /> : null}
      {assets.data && all.length === 0 ? <SettingsEmpty>{t('finance.noAssets')}</SettingsEmpty> : null}
      {slices.length > 0 ? (
        <SpendingRing
          slices={slices}
          currency={currency}
          label={t('finance.assetsRingLabel')}
          totalLabel={t('finance.owned')}
          highlightedKey={highlightedKey}
        >
          <dl className="finance-worth-line">
            <dt>{t('finance.owned')}</dt>
            <dd>{formatMoney(grouping.ownedAmount, currency)}</dd>
            <dt>{t('finance.owed')}</dt>
            <dd>{formatMoney(-grouping.owedAmount, currency)}</dd>
            <dt>{t('finance.netWorthLabel')}</dt>
            <dd>
              <strong>{formatMoney(grouping.ownedAmount - grouping.owedAmount, currency)}</strong>
            </dd>
          </dl>
        </SpendingRing>
      ) : null}
      <UnconvertedNote currencyCodes={grouping.unconvertedCurrencyCodes} />
      {all.length > 0 ? (
        <>
          {/* On a phone the filters fold into one line that says which
              hold, as the Transactions section's do. */}
          {isDesktop ? (
            filterControls
          ) : (
            <details className="finance-filter-disclosure">
              <summary>
                <strong>{t('finance.filtersLabel')}</strong>
                <span className="muted">
                  {filterWords.length > 0 ? filterWords.join(' · ') : t('finance.noAssetFilters')}
                </span>
              </summary>
              {filterControls}
            </details>
          )}
          <p className="muted finance-transaction-count">
            {t('finance.assetsShown', { count: String(grouping.matchingCount), total: String(all.length) })}
          </p>
          {grouping.groups.length === 0 ? <SettingsEmpty>{t('finance.noAssetsMatch')}</SettingsEmpty> : null}
          {grouping.groups.length > 0 ? (
            <div className="table-wrap">
              <table className="numbers-table finance-table finance-assets-table">
                <thead>
                  <tr>
                    <th>{t('finance.assetName')}</th>
                    <th className="numeric">{t('finance.latestValue')}</th>
                    <th className="numeric optional">{t('finance.holdingLabel')}</th>
                    <th>{t('finance.valuedOn')}</th>
                    <th className="optional">{t('finance.valuationSourceLabel')}</th>
                  </tr>
                </thead>
                <tbody>
                  {grouping.groups.flatMap((group) => {
                    const index = sliceIndexes.get(group.assetKind)
                    const otherIndex = slices.findIndex((slice) => slice.isOther)
                    const swatch = group.isLiability
                      ? null
                      : index !== undefined
                        ? ringSliceClass(slices[index], index)
                        : otherIndex >= 0
                          ? 'other'
                          : null
                    const sliceKey = index !== undefined ? group.assetKind : otherIndex >= 0 ? 'other' : null
                    const isOpen = isExpanded(group.assetKind)
                    const shown = isSearching ? group.matchingAssets : group.assets
                    const share =
                      !group.isLiability && grouping.ownedAmount > 0 ? group.totalAmount / grouping.ownedAmount : null
                    const name = words.assetKind(group.assetKind)
                    return [
                      // The whole row opens the kind; the button in it is
                      // what the keyboard and a screen reader reach.
                      <tr
                        key={`group-${group.assetKind}`}
                        className="finance-asset-group"
                        onClick={() => toggle(group.assetKind)}
                        onPointerEnter={sliceKey ? () => setHighlightedKey(sliceKey) : undefined}
                        onPointerLeave={sliceKey ? () => setHighlightedKey(null) : undefined}
                      >
                        <td>
                          <button
                            type="button"
                            className="finance-group-toggle"
                            aria-expanded={isOpen}
                            onFocus={sliceKey ? () => setHighlightedKey(sliceKey) : undefined}
                            onBlur={sliceKey ? () => setHighlightedKey(null) : undefined}
                          >
                            <span className={isOpen ? 'finance-group-chevron open' : 'finance-group-chevron'} />
                            {swatch ? <i className={`spending-ring-swatch ${swatch}`} aria-hidden="true" /> : null}
                            <span>{name}</span>
                            <span className="muted finance-group-count">
                              {isSearching && shown.length !== group.assets.length
                                ? t('finance.assetCountOf', {
                                    count: String(shown.length),
                                    total: String(group.assets.length),
                                  })
                                : plural(group.assets.length, { one: 'finance.assetCountOne', other: 'finance.assetCountOther' })}
                            </span>
                          </button>
                        </td>
                        <td className="numeric">
                          <strong>
                            {formatMoney(group.isLiability ? -group.totalAmount : group.totalAmount, currency)}
                          </strong>
                          {share !== null ? (
                            <span className="muted finance-share">
                              {share > 0 && share < 0.005 ? `<${percent.format(0.01)}` : percent.format(share)}
                            </span>
                          ) : null}
                        </td>
                        <td className="optional" />
                        <td />
                        <td className="optional" />
                      </tr>,
                      ...(isOpen ? groupRows(shown) : []),
                    ]
                  })}
                </tbody>
              </table>
            </div>
          ) : null}
        </>
      ) : null}
      {adding ? <AssetDialog onClose={() => setAdding(false)} onSaved={assets.reload} /> : null}
    </SettingsSection>
  )
}

// signedValue is an asset's latest value as it counts toward net worth: a
// liability's value is what is owed, so it counts against it.
function signedValue(asset: Asset): number {
  const value = amountOf(asset.latestValuation?.value)
  return asset.isLiability ? -value : value
}

// holdingText is a holding's quantity and the price of one, "12.1235 at
// $150.50"; nothing for anything that is not a holding.
function holdingText(valuation: AssetValuation, t: (key: Key, values?: Record<string, string>) => string): string {
  if (!hasAmount(valuation.heldQuantity)) return ''
  const quantity = formatQuantity(valuation.heldQuantity)
  return hasAmount(valuation.unitPrice)
    ? t('finance.holdingLine', {
        quantity,
        price: formatMoney(amountOf(valuation.unitPrice), valuation.currencyCode),
      })
    : quantity
}

// AssetDialog makes an asset, with its first value if there is one, in one
// operation, or changes one. An asset a finance source made is valued by
// its syncs, in its account's currency, so neither can be changed on it;
// on any other the currency can, since each value keeps its own.
function AssetDialog({
  asset,
  onClose,
  onSaved,
}: {
  asset?: Asset
  onClose: () => void
  onSaved: () => Promise<void>
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const { reportingCurrencyCode } = useReportingCurrency()
  const { busy, act } = useAct(onSaved)
  const [assetName, setAssetName] = useState(asset?.assetName ?? '')
  const [assetKind, setAssetKind] = useState(asset?.assetKind ?? 'vehicle')
  const [currencyCode, setCurrencyCode] = useState(asset?.currencyCode ?? '')
  const [valuationSource, setValuationSource] = useState(asset?.valuationSource ?? 'manual')
  const [value, setValue] = useState('')
  const [valuedOn, setValuedOn] = useState(() => personToday())
  const [estimateDescription, setEstimateDescription] = useState(asset?.estimateDescription ?? '')
  const currency = currencyCode || reportingCurrencyCode || 'USD'
  // Choosing estimates is the permission: asking again in a checkbox was
  // the same question twice. The description is what the agent may search
  // the web with, so an estimate needs one.
  const isEstimatedByAgent = valuationSource === 'agent_estimate'
  const isFromSync = asset?.valuationSource === 'finance_sync'
  const valuationSources = CHOSEN_VALUATION_SOURCES.includes(valuationSource)
    ? CHOSEN_VALUATION_SOURCES
    : [valuationSource, ...CHOSEN_VALUATION_SOURCES]

  return (
    <FormDialog
      title={asset ? t('finance.editAsset') : t('finance.addAsset')}
      submitLabel={asset ? t('common.save') : t('finance.addAsset')}
      busy={busy}
      canSubmit={
        assetName.trim() !== '' &&
        (value.trim() === '' || isDecimal(value)) &&
        (!isEstimatedByAgent || estimateDescription.trim() !== '')
      }
      onClose={onClose}
      onSubmit={() => {
        const shared = {
          assetName: assetName.trim(),
          assetKind,
          estimateDescription: isEstimatedByAgent ? estimateDescription.trim() : (asset?.estimateDescription ?? ''),
          isEstimateAllowed: isFromSync ? (asset?.isEstimateAllowed ?? false) : isEstimatedByAgent,
        }
        // What a finance source values keeps its currency and valuation
        // source; the server refuses a change to either.
        const chosen = isFromSync ? {} : { currencyCode: currency, valuationSource }
        void act(
          () =>
            asset
              ? graphql(UPDATE_ASSET, { ...shared, ...chosen, assetId: asset.id })
              : graphql(CREATE_ASSET, {
                  ...shared,
                  ...chosen,
                  value: value.trim() || undefined,
                  valuedOn: value.trim() ? valuedOn : undefined,
                }),
          asset ? t('finance.assetSaved') : t('finance.assetAdded'),
        ).then((isDone) => {
          if (isDone) onClose()
        })
      }}
    >
      <label>
        <span>{t('finance.assetName')}</span>
        <input value={assetName} onChange={(event) => setAssetName(event.target.value)} />
      </label>
      <label>
        <span>{t('finance.assetKindLabel')}</span>
        <Select
          block
          value={assetKind}
          label={t('finance.assetKindLabel')}
          options={ASSET_KINDS.map((kind) => ({ value: kind, label: words.assetKind(kind) }))}
          onChange={setAssetKind}
        />
      </label>
      {!isFromSync ? (
        <>
          <label>
            <span>{t('finance.currency')}</span>
            <CurrencyPicker value={currency} label={t('finance.currency')} onChange={setCurrencyCode} />
          </label>
          <label>
            <span>{t('finance.valuationSourceLabel')}</span>
            <Select
              block
              value={valuationSource}
              label={t('finance.valuationSourceLabel')}
              options={valuationSources.map((source) => ({ value: source, label: words.valuationSource(source) }))}
              onChange={setValuationSource}
            />
          </label>
          {/* What the chosen one means, rather than all three at once. */}
          {VALUATION_SOURCE_HINTS[valuationSource] ? (
            <p className="muted field-hint">{t(VALUATION_SOURCE_HINTS[valuationSource])}</p>
          ) : null}
          {isEstimatedByAgent ? (
            <>
              <label>
                <span>{t('finance.estimateDescription')}</span>
                <textarea
                  rows={3}
                  value={estimateDescription}
                  onChange={(event) => setEstimateDescription(event.target.value)}
                />
              </label>
              <p className="muted field-hint">{t('finance.estimateHint')}</p>
            </>
          ) : null}
        </>
      ) : null}
      {!asset ? (
        <>
          <label>
            <span>{t('finance.firstValue')}</span>
            <input inputMode="decimal" value={value} onChange={(event) => setValue(event.target.value)} />
          </label>
          <p className="muted field-hint">{t('finance.valueHint')}</p>
          {/* The day belongs to a value; without one there is nothing to date. */}
          {value.trim() !== '' ? (
            <label>
              <span>{t('finance.valuedOn')}</span>
              <input type="date" value={valuedOn} max={personToday()} onChange={(event) => setValuedOn(event.target.value)} />
            </label>
          ) : null}
        </>
      ) : null}
    </FormDialog>
  )
}

function AssetPage({
  asset,
  onBack,
  onChanged,
}: {
  asset: Asset | null
  onBack: () => void
  onChanged: () => Promise<void>
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const history = useQuery(
    () =>
      asset ? graphql<{ AssetHistory: AssetHistory }>(ASSET_HISTORY, { assetId: asset.id }) : Promise.resolve(null),
    [asset?.id],
    { refresh: false },
  )
  const reloadBoth = async () => {
    await Promise.all([history.reload(), onChanged()])
  }
  const { busy, act, run } = useAct(reloadBoth)
  const [editing, setEditing] = useState(false)
  const [recording, setRecording] = useState(false)
  const [closing, setClosing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deletingValuation, setDeletingValuation] = useState<AssetValuation | null>(null)
  const [value, setValue] = useState('')
  const [valuedOn, setValuedOn] = useState(() => personToday())
  const [valuationNote, setValuationNote] = useState('')
  const [closedOn, setClosedOn] = useState(() => personToday())

  const back = (
    <div className="page-actions">
      {/* A button with the way back drawn on it, not a text link: the
          dashboard keeps text buttons for a row's one action. */}
      <button type="button" className="with-icon" onClick={onBack}>
        <ArrowLeftIcon size={16} />
        {t('finance.backToNetWorth')}
      </button>
    </div>
  )

  if (!asset) {
    return (
      <SettingsSection card title={t('finance.assetsTitle')}>
        {back}
        <SettingsEmpty>{t('finance.assetMissing')}</SettingsEmpty>
      </SettingsSection>
    )
  }

  const isFromSync = asset.valuationSource === 'finance_sync'
  const isAssetHolding = isHolding(asset)
  const security = asset.financeSecurity
  const valuations = history.data?.AssetHistory.assetValuations ?? []

  return (
    <>
      <SettingsSection
        card
        title={asset.assetName}
        description={asset.isLiability ? t('finance.liabilityHint') : undefined}
      >
        {back}
        <div className="page-actions">
          {!asset.closedOn && !isFromSync ? (
            <button
              type="button"
              className="primary"
              onClick={() => {
                setValue('')
                setValuedOn(personToday())
                setValuationNote('')
                setRecording(true)
              }}
            >
              {t('finance.addValue')}
            </button>
          ) : null}
          <button type="button" onClick={() => setEditing(true)}>
            {t('common.edit')}
          </button>
          {!asset.closedOn ? (
            <button type="button" onClick={() => setClosing(true)}>
              {t('finance.closeAsset')}
            </button>
          ) : (
            <button
              type="button"
              disabled={busy}
              onClick={() => void run(CLOSE_ASSET, { assetId: asset.id, shouldReopen: true }, t('finance.assetReopened'))}
            >
              {t('finance.reopen')}
            </button>
          )}
          <button type="button" className="danger" onClick={() => setDeleting(true)}>
            {t('common.delete')}
          </button>
        </div>
        <table className="detail">
          <tbody>
            {/* A holding is one security: what it is comes first. */}
            <Field label={t('finance.security')}>
              {security ? [security.tickerSymbol, security.securityName].filter(Boolean).join(' · ') : undefined}
            </Field>
            <Field label={t('finance.securityKindLabel')}>
              {security ? words.securityKind(security.securityKind) : undefined}
            </Field>
            <Field label={t('finance.closePrice')}>
              {security && hasAmount(security.closePrice)
                ? t('finance.closePriceOn', {
                    price: formatMoney(amountOf(security.closePrice), security.currencyCode || asset.currencyCode),
                    day: formatDay(security.closePriceOn),
                  })
                : undefined}
            </Field>
            <Field label={t('finance.assetKindLabel')}>{words.assetKind(asset.assetKind)}</Field>
            <Field label={t('finance.currency')}>{asset.currencyCode}</Field>
            <Field label={t('finance.valuationSourceLabel')}>{words.valuationSource(asset.valuationSource)}</Field>
            <Field label={t('finance.latestValue')}>
              {asset.latestValuation ? (
                <>
                  <Money amount={asset.latestValuation.value} currency={asset.latestValuation.currencyCode} />{' '}
                  <span className="muted">{formatDay(asset.latestValuation.valuedOn)}</span>
                  <HoldingLine valuation={asset.latestValuation} />
                </>
              ) : undefined}
            </Field>
            <Field label={t('finance.estimateDescription')}>{asset.estimateDescription || undefined}</Field>
            <Field label={t('finance.estimates')}>
              {asset.isEstimateAllowed ? t('finance.estimatesAllowed') : t('finance.estimatesNotAllowed')}
            </Field>
            <Field label={t('finance.closedOn')}>{asset.closedOn ? formatDay(asset.closedOn) : undefined}</Field>
          </tbody>
        </table>
      </SettingsSection>
      <SettingsSection card title={t('finance.historyTitle')} description={t('finance.historyHint')}>
        <ErrorMessage error={history.error} />
        {history.loading && !history.data ? <Loading /> : null}
        {history.data && valuations.length === 0 ? <SettingsEmpty>{t('finance.noValuations')}</SettingsEmpty> : null}
        {valuations.length > 0 ? (
          <div className="table-wrap">
            <table className="numbers-table finance-table">
              <thead>
                <tr>
                  <th>{t('finance.valuedOn')}</th>
                  <th className="numeric">{t('finance.value')}</th>
                  {isAssetHolding ? (
                    <>
                      <th className="numeric">{t('finance.heldQuantity')}</th>
                      <th className="numeric">{t('finance.unitPrice')}</th>
                      <th className="numeric">{t('finance.costBasis')}</th>
                    </>
                  ) : null}
                  <th>{t('finance.valuationSourceLabel')}</th>
                  {/* A holding is valued by its syncs and never estimated. */}
                  {!isAssetHolding ? <th className="numeric">{t('finance.estimateRange')}</th> : null}
                  <th>{t('finance.valuationNote')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {valuations.map((valuation) => (
                  <tr key={valuation.id}>
                    <td>{formatDay(valuation.valuedOn)}</td>
                    <td className="numeric">
                      <Money amount={valuation.value} currency={valuation.currencyCode} />
                    </td>
                    {isAssetHolding ? (
                      <>
                        <td className="numeric">{formatQuantity(valuation.heldQuantity)}</td>
                        <td className="numeric">
                          <Money amount={valuation.unitPrice} currency={valuation.currencyCode} />
                        </td>
                        <td className="numeric">
                          <Money amount={valuation.costBasis} currency={valuation.currencyCode} />
                        </td>
                      </>
                    ) : null}
                    <td>{words.valuationSource(valuation.valuationSource)}</td>
                    {!isAssetHolding ? (
                      <td className="numeric">
                        {valuation.estimateLow && valuation.estimateHigh ? (
                          `${formatMoney(amountOf(valuation.estimateLow), valuation.currencyCode)} – ${formatMoney(
                            amountOf(valuation.estimateHigh),
                            valuation.currencyCode,
                          )}`
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                    ) : null}
                    <td className="wrap">
                      {valuation.valuationNote || ''}
                      {valuation.evidenceUrls.map((address) => (
                        <span key={address} className="finance-evidence">
                          <a href={address} target="_blank" rel="noopener noreferrer nofollow">
                            {hostOf(address)}
                          </a>
                        </span>
                      ))}
                    </td>
                    <td>
                      <button
                        type="button"
                        className="link danger"
                        aria-label={`${formatDay(valuation.valuedOn)}: ${t('finance.deleteValuation')}`}
                        onClick={() => setDeletingValuation(valuation)}
                      >
                        {t('common.delete')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </SettingsSection>
      {isAssetHolding && asset.financeAccountId && asset.financeSecurityId ? (
        <HoldingTrades financeAccountId={asset.financeAccountId} financeSecurityId={asset.financeSecurityId} />
      ) : null}
      {editing ? <AssetDialog asset={asset} onClose={() => setEditing(false)} onSaved={reloadBoth} /> : null}
      {recording ? (
        <FormDialog
          title={t('finance.addValue')}
          submitLabel={t('finance.addValue')}
          busy={busy}
          canSubmit={isDecimal(value) && valuedOn !== ''}
          onClose={() => setRecording(false)}
          onSubmit={() => {
            void act(
              () =>
                graphql(RECORD_VALUATION, {
                  assetId: asset.id,
                  value: value.trim(),
                  valuedOn,
                  valuationNote: valuationNote.trim() || undefined,
                }),
              t('finance.valueAdded'),
            ).then((isDone) => {
              if (isDone) setRecording(false)
            })
          }}
        >
          <div className="row">
            <label>
              <span>{t('finance.value')}</span>
              <input inputMode="decimal" value={value} onChange={(event) => setValue(event.target.value)} />
            </label>
            <label>
              <span>{t('finance.valuedOn')}</span>
              <input
                type="date"
                value={valuedOn}
                max={personToday()}
                onChange={(event) => setValuedOn(event.target.value)}
              />
            </label>
          </div>
          <p className="muted field-hint">
            {asset.isLiability ? t('finance.valueLiabilityHint') : t('finance.valueHint')}
          </p>
          <label>
            <span>{t('finance.valuationNote')}</span>
            <input value={valuationNote} onChange={(event) => setValuationNote(event.target.value)} />
          </label>
        </FormDialog>
      ) : null}
      {closing ? (
        <FormDialog
          title={t('finance.closeAsset')}
          submitLabel={t('finance.closeAsset')}
          busy={busy}
          canSubmit={closedOn !== ''}
          onClose={() => setClosing(false)}
          onSubmit={() => {
            void run(CLOSE_ASSET, { assetId: asset.id, closedOn }, t('finance.assetClosed')).then((isDone) => {
              if (isDone) setClosing(false)
            })
          }}
        >
          <p className="muted">{t('finance.closeAssetHint')}</p>
          <label>
            <span>{t('finance.closedOn')}</span>
            <input type="date" value={closedOn} onChange={(event) => setClosedOn(event.target.value)} />
          </label>
        </FormDialog>
      ) : null}
      {deleting ? (
        <ConfirmDialog
          title={t('finance.deleteAsset')}
          body={t('finance.deleteAssetBody', { name: asset.assetName, count: valuations.length })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onClose={() => setDeleting(false)}
          onConfirm={() => {
            void act(() => graphql(DELETE_ASSET, { assetId: asset.id }), t('finance.assetDeleted')).then((isDone) => {
              setDeleting(false)
              if (isDone) onBack()
            })
          }}
        />
      ) : null}
      {deletingValuation ? (
        <ConfirmDialog
          title={t('finance.deleteValuation')}
          body={t('finance.deleteValuationBody', {
            value: formatMoney(amountOf(deletingValuation.value), deletingValuation.currencyCode),
            day: formatDay(deletingValuation.valuedOn),
          })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onClose={() => setDeletingValuation(null)}
          onConfirm={() => {
            void run(DELETE_VALUATION, { valuationId: deletingValuation.id }, t('finance.valuationDeleted')).then(() =>
              setDeletingValuation(null),
            )
          }}
        />
      ) : null}
    </>
  )
}

// HoldingLine is a holding's quantity and the price of one under its
// value, "12.1235 at $150.50"; nothing for anything that is not a holding.
function HoldingLine({ valuation }: { valuation: AssetValuation }) {
  const { t } = useTranslation()
  const said = holdingText(valuation, t)
  return said ? <span className="muted finance-cell-detail">{said}</span> : null
}

// HoldingTrades is a holding's trades, newest first: every buy, sell,
// cancel and transfer of its security in its finance account, read a page
// at a time from where the last page ended, as the finance transactions
// are.
function HoldingTrades({
  financeAccountId,
  financeSecurityId,
}: {
  financeAccountId: string
  financeSecurityId: string
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const words = useFinanceWords()
  const variables = { financeAccountId, financeSecurityId, limit: TRADE_PAGE_SIZE }
  const filterKey = JSON.stringify(variables)
  const first = useQuery(() => graphql<{ FinanceTrades: FinanceTradePage }>(FINANCE_TRADES, variables), [filterKey], {
    refresh: false,
  })
  // The pages read after the first, and where the next one starts.
  const [more, setMore] = useState<{ rows: FinanceTrade[]; after: string | null; isLoaded: boolean }>({
    rows: [],
    after: null,
    isLoaded: false,
  })
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  useEffect(() => {
    setMore({ rows: [], after: null, isLoaded: false })
  }, [filterKey])

  const firstPage = first.data?.FinanceTrades
  const after = more.isLoaded ? more.after : (firstPage?.nextCursor ?? null)
  const rows = useMemo(() => [...(firstPage?.financeTrades ?? []), ...more.rows], [firstPage, more.rows])

  const loadMore = async () => {
    if (!after) return
    setIsLoadingMore(true)
    try {
      const answer = await graphql<{ FinanceTrades: FinanceTradePage }>(FINANCE_TRADES, { ...variables, after })
      setMore((previous) => ({
        rows: [...previous.rows, ...answer.FinanceTrades.financeTrades],
        after: answer.FinanceTrades.nextCursor ?? null,
        isLoaded: true,
      }))
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsLoadingMore(false)
    }
  }

  const columns: Column<FinanceTrade>[] = [
    {
      key: 'tradedOn',
      header: t('finance.tradedOn'),
      value: (row) => row.tradedOn,
      render: (row) => formatDay(row.tradedOn),
      sort: (left, right) => left.tradedOn.localeCompare(right.tradedOn),
    },
    {
      key: 'tradeKind',
      header: t('finance.tradeKindLabel'),
      value: (row) => words.tradeKind(row.tradeKind),
    },
    {
      key: 'tradedQuantity',
      header: t('finance.tradedQuantity'),
      numeric: true,
      value: (row) => row.tradedQuantity ?? '',
      render: (row) => formatQuantity(row.tradedQuantity),
      sort: (left, right) => amountOf(left.tradedQuantity) - amountOf(right.tradedQuantity),
    },
    {
      key: 'unitPrice',
      header: t('finance.unitPrice'),
      numeric: true,
      value: (row) => row.unitPrice ?? '',
      render: (row) => <Money amount={row.unitPrice} currency={row.currencyCode} />,
      sort: (left, right) => amountOf(left.unitPrice) - amountOf(right.unitPrice),
    },
    {
      key: 'tradeAmount',
      header: t('finance.amount'),
      numeric: true,
      value: (row) => row.tradeAmount,
      render: (row) => <Money amount={row.tradeAmount} currency={row.currencyCode} />,
      sort: (left, right) => amountOf(left.tradeAmount) - amountOf(right.tradeAmount),
    },
    {
      key: 'feeAmount',
      header: t('finance.feeAmount'),
      numeric: true,
      value: (row) => row.feeAmount ?? '',
      render: (row) => <Money amount={row.feeAmount} currency={row.currencyCode} />,
      sort: (left, right) => amountOf(left.feeAmount) - amountOf(right.feeAmount),
    },
    // The institution's own words for the trade: context, so dropped on a
    // phone.
    {
      key: 'description',
      header: t('finance.tradeDescription'),
      optional: true,
      truncate: true,
      value: (row) => row.description,
    },
  ]

  return (
    <SettingsSection card title={t('finance.tradesTitle')} description={t('finance.tradesHint')}>
      <ErrorMessage error={first.error} />
      {first.loading && !first.data ? <Loading /> : null}
      {first.data ? (
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(row) => row.id}
          loading={first.loading}
          emptyMessage={t('finance.noTrades')}
          countLabel={(count) => t('finance.transactionsLoaded', { count: String(count) })}
        />
      ) : null}
      {after ? (
        <div className="page-actions page-actions-end">
          <button type="button" disabled={isLoadingMore} onClick={() => void loadMore()}>
            {t('finance.loadMore')}
          </button>
        </div>
      ) : null}
    </SettingsSection>
  )
}

function hostOf(address: string): string {
  try {
    return new URL(address).hostname
  } catch {
    return address
  }
}
