import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Field, Loading, Tag, formatMoney } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SeriesChart, dayLabel } from '../../components/seriesChart'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  ASSETS,
  ASSET_HISTORY,
  Asset,
  AssetHistory,
  AssetValuation,
  CLOSE_ASSET,
  CREATE_ASSET,
  DELETE_ASSET,
  DELETE_VALUATION,
  NET_WORTH,
  NetWorth,
  RECORD_VALUATION,
  UPDATE_ASSET,
  amountOf,
  formatDay,
  isDecimal,
  isoDay,
} from './financeApi'
import { CurrencyPicker, Money, UnconvertedNote, useAct, useFinanceWords, useReportingCurrency } from './financeCommon'

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
  const choose = (id: string | null) => {
    const written = new URLSearchParams(parameters)
    if (id) {
      written.set('asset', id)
    } else {
      written.delete('asset')
    }
    setParameters(written)
  }
  if (assetId) {
    if (assets.loading && !assets.data) return <Loading />
    return <AssetPage asset={chosen} onBack={() => choose(null)} onChanged={assets.reload} />
  }
  return (
    <>
      <NetWorthChart />
      <AssetsPanel assets={assets} onChoose={choose} />
    </>
  )
}

function NetWorthChart() {
  const { t } = useTranslation()
  const [range, setRange] = useState('90')
  const days = RANGES.find((candidate) => candidate.id === range)?.days ?? 90
  const from = days > 0 ? isoDay(new Date(Date.now() - days * 86_400_000)) : undefined
  const { data, error, loading } = useQuery(
    () => graphql<{ NetWorth: NetWorth }>(NET_WORTH, { from, to: isoDay(new Date()) }),
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

function AssetsPanel({
  assets,
  onChoose,
}: {
  assets: { data: { Assets: Asset[] } | null; error: unknown; loading: boolean; reload: () => Promise<void> }
  onChoose: (id: string) => void
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const [isClosedShown, setIsClosedShown] = useState(false)
  const [adding, setAdding] = useState(false)
  const list = (assets.data?.Assets ?? []).filter((asset) => isClosedShown || !asset.closedOn)
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
      {assets.data && list.length === 0 ? <SettingsEmpty>{t('finance.noAssets')}</SettingsEmpty> : null}
      {list.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table">
            <thead>
              <tr>
                <th>{t('finance.assetName')}</th>
                <th>{t('finance.assetKindLabel')}</th>
                <th className="numeric">{t('finance.latestValue')}</th>
                <th>{t('finance.valuedOn')}</th>
                <th>{t('finance.valuationSourceLabel')}</th>
              </tr>
            </thead>
            <tbody>
              {list.map((asset) => {
                const valuation = asset.latestValuation
                return (
                  <tr key={asset.id}>
                    <td>
                      <button type="button" className="link" onClick={() => onChoose(asset.id)}>
                        {asset.assetName}
                      </button>
                      {asset.closedOn ? (
                        <>
                          {' '}
                          <Tag value={t('finance.closed')} />
                        </>
                      ) : null}
                    </td>
                    <td>{words.assetKind(asset.assetKind)}</td>
                    <td className="numeric">
                      {valuation ? (
                        <Money
                          amount={asset.isLiability ? -amountOf(valuation.value) : amountOf(valuation.value)}
                          currency={valuation.currencyCode}
                        />
                      ) : (
                        <span className="muted">—</span>
                      )}
                    </td>
                    <td className="muted">{valuation ? formatDay(valuation.valuedOn) : '—'}</td>
                    <td>{words.valuationSource(valuation?.valuationSource ?? asset.valuationSource)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : null}
      {(assets.data?.Assets ?? []).some((asset) => asset.closedOn) ? (
        <label className="checkbox">
          <input type="checkbox" checked={isClosedShown} onChange={(event) => setIsClosedShown(event.target.checked)} />
          {t('finance.showClosed')}
        </label>
      ) : null}
      {adding ? <AssetDialog onClose={() => setAdding(false)} onSaved={assets.reload} /> : null}
    </SettingsSection>
  )
}

// AssetDialog makes an asset, with its first value if there is one, or
// changes one. What an asset is valued in cannot change after it exists:
// its history is in that currency.
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
  const [value, setValue] = useState('')
  const [valuedOn, setValuedOn] = useState(() => isoDay(new Date()))
  const [estimateDescription, setEstimateDescription] = useState(asset?.estimateDescription ?? '')
  const [isEstimateAllowed, setIsEstimateAllowed] = useState(asset?.isEstimateAllowed ?? false)
  const currency = currencyCode || reportingCurrencyCode || 'USD'
  const isFromSync = asset?.valuationSource === 'finance_sync'

  return (
    <FormDialog
      title={asset ? t('finance.editAsset') : t('finance.addAsset')}
      submitLabel={asset ? t('common.save') : t('finance.addAsset')}
      busy={busy}
      canSubmit={assetName.trim() !== '' && (value.trim() === '' || isDecimal(value))}
      onClose={onClose}
      onSubmit={() => {
        const shared = {
          assetName: assetName.trim(),
          assetKind,
          estimateDescription: estimateDescription.trim(),
          isEstimateAllowed,
        }
        void act(
          () =>
            asset
              ? graphql(UPDATE_ASSET, { ...shared, assetId: asset.id })
              : createWithValue({ ...shared, currencyCode: currency }, value.trim(), valuedOn),
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
      {!asset ? (
        <>
          <label>
            <span>{t('finance.currency')}</span>
            <CurrencyPicker value={currency} label={t('finance.currency')} onChange={setCurrencyCode} />
          </label>
          <div className="row">
            <label>
              <span>{t('finance.firstValue')}</span>
              <input inputMode="decimal" value={value} onChange={(event) => setValue(event.target.value)} />
            </label>
            <label>
              <span>{t('finance.valuedOn')}</span>
              <input
                type="date"
                value={valuedOn}
                max={isoDay(new Date())}
                onChange={(event) => setValuedOn(event.target.value)}
              />
            </label>
          </div>
          <p className="muted field-hint">{t('finance.valueHint')}</p>
        </>
      ) : null}
      {!isFromSync ? (
        <>
          <label>
            <span>{t('finance.estimateDescription')}</span>
            <textarea
              rows={3}
              value={estimateDescription}
              onChange={(event) => setEstimateDescription(event.target.value)}
            />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={isEstimateAllowed}
              onChange={(event) => setIsEstimateAllowed(event.target.checked)}
            />
            {t('finance.isEstimateAllowed')}
          </label>
          <p className="muted field-hint">{t('finance.estimateHint')}</p>
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
  const [valuedOn, setValuedOn] = useState(() => isoDay(new Date()))
  const [valuationNote, setValuationNote] = useState('')
  const [closedOn, setClosedOn] = useState(() => isoDay(new Date()))

  const back = (
    <div className="page-actions">
      <button type="button" className="link" onClick={onBack}>
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
                setValuedOn(isoDay(new Date()))
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
          ) : null}
          <button type="button" className="danger" onClick={() => setDeleting(true)}>
            {t('common.delete')}
          </button>
        </div>
        <table className="detail">
          <tbody>
            <Field label={t('finance.assetKindLabel')}>{words.assetKind(asset.assetKind)}</Field>
            <Field label={t('finance.currency')}>{asset.currencyCode}</Field>
            <Field label={t('finance.valuationSourceLabel')}>{words.valuationSource(asset.valuationSource)}</Field>
            <Field label={t('finance.latestValue')}>
              {asset.latestValuation ? (
                <>
                  <Money amount={asset.latestValuation.value} currency={asset.latestValuation.currencyCode} />{' '}
                  <span className="muted">{formatDay(asset.latestValuation.valuedOn)}</span>
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
                  <th>{t('finance.valuationSourceLabel')}</th>
                  <th className="numeric">{t('finance.estimateRange')}</th>
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
                    <td>{words.valuationSource(valuation.valuationSource)}</td>
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
                max={isoDay(new Date())}
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

// createWithValue makes an asset and, when a value was given, records it
// as its first valuation: two operations, the same two the command line's
// create-asset and record-valuation are.
async function createWithValue(asset: Record<string, unknown>, value: string, valuedOn: string): Promise<void> {
  const created = await graphql<{ CreateAsset: { id: string } }>(CREATE_ASSET, asset)
  if (value) {
    await graphql(RECORD_VALUATION, { assetId: created.CreateAsset.id, value, valuedOn })
  }
}

function hostOf(address: string): string {
  try {
    return new URL(address).hostname
  } catch {
    return address
  }
}
