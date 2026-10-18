import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { CopyIconButton, ErrorMessage, Loading, Tag, formatMoney } from '../../components/common'
import { Column, DataTable, Range } from '../../components/dataTable'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PictureIcon, RefreshIcon } from '../../components/icons'
import { ZoomablePicture } from '../../components/lightbox'
import { usePageInAddress } from '../../components/pager'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { uploadFiles } from '../../upload'
import {
  AGENT_ATTACHMENTS_PATH,
  DELETE_PAYSTUB,
  DELETE_TAX_FORM,
  FINANCE_PAYSTUB,
  FINANCE_PAYSTUBS,
  FINANCE_TAX_FORMS,
  FinancePaystub,
  FinancePaystubLine,
  FinancePaystubPage,
  FinanceTaxFormEntry,
  MAXIMUM_LISTED_TAX_DOCUMENT_COUNT,
  READ_TAX_DOCUMENT,
  REGENERATE_TAX_DOCUMENT_ADDRESS,
  SET_TAX_FILING_STATUS,
  SET_TAX_YEAR_SETTING,
  TAX_DOCUMENT_UPLOAD_PATH,
  TAX_DOCUMENT_ADDRESS,
  TAX_FILING_STATUSES,
  TAX_YEAR,
  TaxDocumentAddress,
  TaxDocumentReading,
  TaxFilingStatus,
  TaxFormPage,
  TaxFormView,
  TaxYearComparison,
  TaxYearEmployment,
  TaxYearEstimate,
  TaxYearInvestment,
  TaxYearInvestmentPreview,
  TaxYearSetting,
  TaxYearFigures,
  TaxYearSummary,
  amountOf,
  formatDay,
  hasAmount,
  personToday,
  taxFormKindName,
} from './financeApi'
import { PaystubEntryDialog } from './financePaystubEntry'
import { TaxFormEntryDialog } from './financeTaxFormEntry'
import { RingSlice, SpendingRing, ringSliceClass } from './spendingRing'

// The Taxes section: one tax year (in the address as ?year=), put
// together from the paystubs paid in it and the W-2s for it, as teanode
// finance tax-year puts it together. Each employment's figures come from
// its W-2 when there is one and from the latest paystub's year to date
// when there is not, with a projection to December 31; where both exist
// each W-2 box is set beside the paystubs'. Under the year are its
// findings, then the W-2s and the paystubs themselves, each opening a
// dialog with its rows, its file and Delete. Paystubs are paged on the
// server with the page in the address, as the receipts are.
//
// One dialog is open at a time (every dialog closes on Escape from a
// document listener), so confirming a delete takes the document's place
// and the document comes back when it is called off.

// How long the paystubs and W-2s are read again for after an upload,
// waiting for the reading job: every few seconds, for about two minutes,
// as receipts wait. Kept in an object so a test can shorten it.
export const taxDocumentPolling = { intervalMS: 4000, attemptCount: 30 }

// How many years the year menu offers: this one and the four before it,
// enough for a return being filed late and the years a refund can still
// be claimed for.
const OFFERED_YEAR_COUNT = 5

// Summary amounts are dollars: the year is a US tax year.
const TAX_CURRENCY_CODE = 'USD'

type Opened =
  | { dialogKind: 'paystub'; paystub: FinancePaystub }
  | { dialogKind: 'deletePaystub'; paystub: FinancePaystub }
  | { dialogKind: 'taxForm'; taxFormView: TaxFormView }
  | { dialogKind: 'deleteTaxForm'; taxFormView: TaxFormView }
  | { dialogKind: 'addPaystub' }
  | { dialogKind: 'addTaxForm' }

function formatDollars(decimal?: string | null): string {
  return formatMoney(amountOf(decimal), TAX_CURRENCY_CODE)
}

function isZero(decimal?: string | null): boolean {
  return amountOf(decimal) === 0
}

// offeredYears is the year menu, newest first: this year, the four
// before it, and the year shown when it is none of them (an address from a
// link to an older year).
export function offeredYears(currentYear: number, shownYear: number): string[] {
  const years = new Set<number>()
  for (let index = 0; index < OFFERED_YEAR_COUNT; index++) years.add(currentYear - index)
  years.add(shownYear)
  return [...years].sort((first, second) => second - first).map(String)
}

function yearFromSearch(search: URLSearchParams, currentYear: number): number {
  const year = Number(search.get('year'))
  return Number.isInteger(year) && year >= 1900 && year <= 9999 ? year : currentYear
}

// --- the figures ------------------------------------------------------------

type FigureRow = {
  key: string
  label: string
  amountOf: (figures: TaxYearFigures) => string
  // Wages and federal withholding are the two lines every return starts
  // from, so they are shown even at zero; every other line only when it
  // has an amount.
  isAlwaysShown: boolean
}

function useFigureRows(): (figuresList: TaxYearFigures[]) => FigureRow[] {
  const { t } = useTranslation()
  return (figuresList) => {
    const stateCodes = [
      ...new Set(figuresList.flatMap((figures) => figures.taxYearStateFigures.map((state) => state.stateCode))),
    ].sort()
    const stateFigure = (figures: TaxYearFigures, stateCode: string) =>
      figures.taxYearStateFigures.find((state) => state.stateCode === stateCode)
    const rows: FigureRow[] = [
      {
        key: 'wages',
        label: t('finance.taxWages'),
        amountOf: (figures) => figures.wagesAmount,
        isAlwaysShown: true,
      },
      {
        key: 'federalIncomeTax',
        label: t('finance.taxFederalIncomeTax'),
        amountOf: (figures) => figures.federalIncomeTaxAmount,
        isAlwaysShown: true,
      },
      {
        key: 'socialSecurityWages',
        label: t('finance.taxSocialSecurityWages'),
        amountOf: (figures) => figures.socialSecurityWagesAmount,
        isAlwaysShown: false,
      },
      {
        key: 'socialSecurityTax',
        label: t('finance.taxSocialSecurityTax'),
        amountOf: (figures) => figures.socialSecurityTaxAmount,
        isAlwaysShown: false,
      },
      {
        key: 'medicareWages',
        label: t('finance.taxMedicareWages'),
        amountOf: (figures) => figures.medicareWagesAmount,
        isAlwaysShown: false,
      },
      {
        key: 'medicareTax',
        label: t('finance.taxMedicareTax'),
        amountOf: (figures) => figures.medicareTaxAmount,
        isAlwaysShown: false,
      },
      {
        key: 'retirementTraditional',
        label: t('finance.taxRetirementTraditional'),
        amountOf: (figures) => figures.retirementTraditionalAmount,
        isAlwaysShown: false,
      },
      {
        key: 'retirementRoth',
        label: t('finance.taxRetirementRoth'),
        amountOf: (figures) => figures.retirementRothAmount,
        isAlwaysShown: false,
      },
      {
        key: 'hsa',
        label: t('finance.taxHsa'),
        amountOf: (figures) => figures.hsaAmount,
        isAlwaysShown: false,
      },
      {
        key: 'dependentCare',
        label: t('finance.taxDependentCare'),
        amountOf: (figures) => figures.dependentCareAmount,
        isAlwaysShown: false,
      },
    ]
    for (const stateCode of stateCodes) {
      rows.push(
        {
          key: `stateWages-${stateCode}`,
          label: t('finance.taxStateWages', { state: stateCode }),
          amountOf: (figures) => stateFigure(figures, stateCode)?.stateWagesAmount ?? '',
          isAlwaysShown: false,
        },
        {
          key: `stateIncomeTax-${stateCode}`,
          label: t('finance.taxStateIncomeTax', { state: stateCode }),
          amountOf: (figures) => stateFigure(figures, stateCode)?.stateIncomeTaxAmount ?? '',
          isAlwaysShown: false,
        },
      )
    }
    rows.push({
      key: 'localIncomeTax',
      label: t('finance.taxLocalIncomeTax'),
      amountOf: (figures) => figures.localIncomeTaxAmount,
      isAlwaysShown: false,
    })
    return rows.filter((row) => row.isAlwaysShown || figuresList.some((figures) => !isZero(row.amountOf(figures))))
  }
}

// TaxFiguresTable is a year's figures as a compact table that stays a
// table on a phone, with a second column to December 31 when they are
// projected and a line under it saying from how many paychecks.
function TaxFiguresTable({
  figures,
  projectedFigures,
  remainingPayCount,
  retirementDeferralLimitAmount,
}: {
  figures: TaxYearFigures
  projectedFigures?: TaxYearFigures | null
  remainingPayCount?: number
  // The year's limit, when the projection stops contributions at it.
  retirementDeferralLimitAmount?: string | null
}) {
  const { t, plural, language } = useTranslation()
  const figureRows = useFigureRows()
  const rows = figureRows(projectedFigures ? [figures, projectedFigures] : [figures])
  return (
    <>
      <div className="finance-receipt-lines-scroll">
        <table className="finance-receipt-lines finance-tax-figures">
          <thead>
            <tr>
              <th scope="col">{t('finance.taxFigure')}</th>
              <th scope="col" className="numeric">
                {projectedFigures ? t('finance.taxSoFar') : t('finance.amount')}
              </th>
              {projectedFigures ? (
                <th scope="col" className="numeric">
                  {t('finance.taxByDecember31')}
                </th>
              ) : null}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key}>
                <td title={row.label}>{row.label}</td>
                <td className="numeric">{formatDollars(row.amountOf(figures))}</td>
                {projectedFigures ? <td className="numeric">{formatDollars(row.amountOf(projectedFigures))}</td> : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {projectedFigures && remainingPayCount ? (
        <p className="muted finance-receipt-detail finance-tax-note">
          {plural(
            remainingPayCount,
            { one: 'finance.taxProjectedPayCountOne', other: 'finance.taxProjectedPayCountOther' },
            { count: remainingPayCount.toLocaleString(language) },
          )}
          {retirementDeferralLimitAmount
            ? ` ${t('finance.taxRetirementCapped', { amount: formatDollars(retirementDeferralLimitAmount) })}`
            : null}
        </p>
      ) : null}
    </>
  )
}

// boxName is a W-2 box as it is printed, "1", "12 D" or "16 CA".
function boxName(boxCode: string, boxQualifier?: string | null): string {
  return [boxCode, boxQualifier ?? ''].filter(Boolean).join(' ')
}

// TaxComparisonCheckTag says whether a W-2 box and the paystubs agree, or
// by how much they differ.
function TaxComparisonCheckTag({ comparison }: { comparison: TaxYearComparison }) {
  const { t } = useTranslation()
  if (comparison.isMatching) return <Tag value={t('finance.taxMatches')} tone="good" />
  return (
    <Tag
      value={t('finance.taxDiffersBy', {
        amount: formatDollars(String(Math.abs(amountOf(comparison.differenceAmount)))),
      })}
      tone="warn"
    />
  )
}

// TaxComparisonTable sets each W-2 box beside the latest paystub's year
// to date for it, box by box.
function TaxComparisonTable({ employment }: { employment: TaxYearEmployment }) {
  const { t } = useTranslation()
  return (
    <>
      <div className="finance-metadata-head">
        <strong>{t('finance.taxComparisonTitle')}</strong>
      </div>
      <div className="finance-receipt-lines-scroll">
        <table className="finance-receipt-lines finance-tax-comparison">
          <thead>
            <tr>
              <th scope="col">{t('finance.taxBox')}</th>
              <th scope="col" className="numeric">
                {t('finance.taxW2')}
              </th>
              <th scope="col" className="numeric">
                {employment.latestPayDate
                  ? t('finance.taxPaystubsThrough', { day: formatDay(employment.latestPayDate) })
                  : t('finance.taxPaystubs')}
              </th>
              <th scope="col">{t('finance.receiptCheck')}</th>
            </tr>
          </thead>
          <tbody>
            {employment.taxYearComparisons.map((comparison) => (
              <tr key={boxName(comparison.boxCode, comparison.boxQualifier)}>
                <td title={`${boxName(comparison.boxCode, comparison.boxQualifier)} ${comparison.boxLabel}`}>
                  <span className="finance-receipt-kind">{boxName(comparison.boxCode, comparison.boxQualifier)}</span>
                  {comparison.boxLabel}
                </td>
                <td className="numeric">{formatDollars(comparison.w2Amount)}</td>
                <td className="numeric">{formatDollars(comparison.paystubAmount)}</td>
                <td>
                  <TaxComparisonCheckTag comparison={comparison} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}

// EmploymentPanel is one person's work for one employer: where the
// figures come from, the figures, and the W-2 against the paystubs.
function EmploymentPanel({
  employment,
  retirementDeferralLimitAmount,
}: {
  employment: TaxYearEmployment
  retirementDeferralLimitAmount?: string | null
}) {
  const { t, plural, language } = useTranslation()
  const source =
    employment.taxYearFigureSource === 'w2'
      ? t('finance.taxFromW2')
      : plural(
          employment.paystubCount,
          { one: 'finance.taxFromPaystubsOne', other: 'finance.taxFromPaystubsOther' },
          {
            count: employment.paystubCount.toLocaleString(language),
            day: formatDay(employment.latestPayDate),
          },
        )
  return (
    <SettingsSection
      card
      title={t('finance.taxEmploymentTitle', { employee: employment.employeeName, employer: employment.employerName })}
      description={source}
    >
      <TaxFiguresTable
        figures={employment.figures}
        projectedFigures={employment.projectedFigures}
        remainingPayCount={employment.remainingPayCount}
        retirementDeferralLimitAmount={employment.isRetirementDeferralCapped ? retirementDeferralLimitAmount : null}
      />
      {employment.taxYearComparisons.length > 0 ? <TaxComparisonTable employment={employment} /> : null}
    </SettingsSection>
  )
}

// HouseholdPanel adds up the year's employments, and each person's when
// more than one person earned, with the Social Security a person had
// withheld beyond the year's maximum across their employers.
function HouseholdPanel({ summary }: { summary: TaxYearSummary }) {
  const { t } = useTranslation()
  const isSeveralPeople = summary.taxYearPeople.length > 1
  return (
    <SettingsSection card title={t('finance.taxHouseholdTitle')} description={t('finance.taxHouseholdHint')}>
      <TaxFiguresTable figures={summary.figures} projectedFigures={summary.projectedFigures} />
      {summary.taxYearPeople.map((person) => (
        <div key={person.employeeName}>
          {isSeveralPeople ? (
            <>
              <div className="finance-metadata-head">
                <strong>{person.employeeName}</strong>
              </div>
              <TaxFiguresTable figures={person.figures} projectedFigures={person.projectedFigures} />
            </>
          ) : null}
          {!isZero(person.excessSocialSecurityTaxAmount) ? (
            <p className="muted finance-receipt-detail finance-tax-note">
              {t('finance.taxExcessSocialSecurity', {
                employee: person.employeeName,
                amount: formatDollars(person.excessSocialSecurityTaxAmount),
              })}
            </p>
          ) : null}
        </div>
      ))}
    </SettingsSection>
  )
}

// TaxDocumentAddressRow is the address paystubs and W-2s are mailed to:
// the person's own mailbox address with a token after a plus. Each PDF or
// picture mailed there, or forwarded from a payroll email, is read as a
// paystub or a W-2 on its own. Made the first time it is shown; a new one
// ends the old address, after asking.
function TaxDocumentAddressRow() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(
    async () => (await graphql<{ TaxDocumentAddress: TaxDocumentAddress }>(TAX_DOCUMENT_ADDRESS)).TaxDocumentAddress,
    [],
    { refresh: false },
  )
  const [isBusy, setIsBusy] = useState(false)
  const [isRegenerating, setIsRegenerating] = useState(false)
  const regenerate = async () => {
    setIsBusy(true)
    try {
      await graphql(REGENERATE_TAX_DOCUMENT_ADDRESS)
      toast.done(t('finance.taxAddressRegenerated'))
      setIsRegenerating(false)
      await reload(true)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }
  if (!data) return <ErrorMessage error={error} />
  return (
    <>
      <SettingsRow
        title={t('finance.taxAddress')}
        subtitle={
          data.taxDocumentAddress ? (
            <>
              <code className="statement-address">{data.taxDocumentAddress}</code>
              <span className="muted finance-tax-address-hint">
                {t(data.canReadPdfs ? 'finance.taxAddressHint' : 'finance.taxAddressHintPictures')}
              </span>
            </>
          ) : (
            t('finance.taxAddressNone')
          )
        }
        actions={
          <div className="row-actions">
            {data.taxDocumentAddress ? (
              <CopyIconButton value={data.taxDocumentAddress} label={t('finance.taxAddressCopy')} />
            ) : null}
            <Tooltip label={t('finance.taxAddressRegenerate')}>
              <button
                type="button"
                className="icon-action"
                disabled={isBusy}
                aria-label={`${t('finance.taxAddress')}: ${t('finance.taxAddressRegenerate')}`}
                onClick={() => setIsRegenerating(true)}
              >
                <RefreshIcon size={16} />
              </button>
            </Tooltip>
          </div>
        }
      />
      {isRegenerating ? (
        <ConfirmDialog
          title={t('finance.taxAddressRegenerate')}
          body={t('finance.taxAddressRegenerateBody')}
          confirmLabel={t('finance.taxAddressRegenerateConfirm')}
          busy={isBusy}
          onClose={() => setIsRegenerating(false)}
          onConfirm={() => void regenerate()}
        />
      ) : null}
    </>
  )
}

// FILING_STATUS_LABELS name each filing status for the menu.
const FILING_STATUS_LABELS: Record<TaxFilingStatus, Key> = {
  single: 'finance.taxFilingSingle',
  married_filing_jointly: 'finance.taxFilingJointly',
  married_filing_separately: 'finance.taxFilingSeparately',
  head_of_household: 'finance.taxFilingHeadOfHousehold',
}

// TaxEstimatePanel is the year's pay as a ring: the tax already withheld,
// what the rest of the year's paychecks will withhold, what will be owed
// at filing, and what is left, the year's tax in the middle; beside it the
// same in figures, each tax on its own, every figure with its share of
// pay (the year's tax's is the effective rate). The filing status is kept
// on the server; until the person picks one it is assumed, and says so.
function TaxEstimatePanel({
  estimate,
  year,
  isSaving,
  onFilingStatus,
  onEditSettings,
}: {
  estimate: TaxYearEstimate
  year: number
  isSaving: boolean
  onFilingStatus: (status: TaxFilingStatus) => void
  onEditSettings: () => void
}) {
  const { t } = useTranslation()
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
  const payAmount = amountOf(estimate.payAmount)
  const incomeAmount = amountOf(estimate.incomeAmount)
  const hasInvestmentIncome = !isZero(estimate.investmentIncomeAmount)
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 1 })
  // Every share is of income: pay and investment income together.
  const shareOfPay = (amount: number) => (incomeAmount > 0 ? percent.format(amount / incomeAmount) : '')
  const slices: RingSlice[] = [
    {
      key: 'paid',
      label: t(estimate.isProjected ? 'finance.taxPaid' : 'finance.taxWithheld'),
      amount: amountOf(estimate.paidTaxAmount),
      sliceClass: 'tone-1',
    },
    {
      key: 'upcoming',
      label: t('finance.taxUpcomingWithheld'),
      amount: amountOf(estimate.upcomingWithheldTaxAmount),
      sliceClass: 'tone-4',
    },
    { key: 'owing', label: t('finance.taxOwing'), amount: amountOf(estimate.owingTaxAmount), sliceClass: 'owing' },
    {
      key: 'remaining',
      label: t('finance.taxRemainingIncome'),
      amount: amountOf(estimate.remainingIncomeAmount),
      sliceClass: 'tone-0',
    },
  ].map((slice) => ({ ...slice, isOther: false, foldedCount: 0 }))
  const shown = slices.filter((slice) => slice.amount > 0)
  // A figure's row: its name, its amount and its share of pay. A slice's
  // row has its swatch, and the row under the pointer picks out its slice,
  // as the tables under the other rings do.
  const figureLine = ({
    key,
    label,
    amount,
    slice,
    className,
    isTotal = false,
  }: {
    key: string
    label: string
    amount: number
    slice?: RingSlice
    className?: string
    isTotal?: boolean
  }) => {
    const pointing = slice
      ? { onPointerEnter: () => setHighlightedKey(slice.key), onPointerLeave: () => setHighlightedKey(null) }
      : {}
    const lineClass = isTotal ? 'finance-tax-estimate-total' : undefined
    return (
      <Fragment key={key}>
        <dt className={lineClass} {...pointing}>
          {slice ? <i className={`spending-ring-swatch ${ringSliceClass(slice, 0)}`} aria-hidden="true" /> : null}
          {label}
        </dt>
        <dd className={[lineClass, className].filter(Boolean).join(' ') || undefined} {...pointing}>
          {isTotal ? <strong>{formatDollars(String(amount))}</strong> : formatDollars(String(amount))}
        </dd>
        <dd className={['muted finance-tax-share', lineClass].filter(Boolean).join(' ')} {...pointing}>
          {shareOfPay(amount)}
        </dd>
      </Fragment>
    )
  }
  const taxLines: { key: string; label: string; amount: string }[] = [
    { key: 'federal', label: t('finance.taxEstimateFederal'), amount: estimate.federalIncomeTaxAmount },
    {
      key: 'preferential',
      label: t('finance.taxEstimatePreferential'),
      amount: estimate.preferentialIncomeTaxAmount,
    },
    {
      key: 'foreignTaxCredit',
      label: t('finance.taxEstimateForeignTaxCredit'),
      amount: `-${estimate.foreignTaxCreditAmount}`,
    },
    { key: 'childCredit', label: t('finance.taxEstimateChildCredit'), amount: `-${estimate.childTaxCreditAmount}` },
    { key: 'careCredit', label: t('finance.taxEstimateCareCredit'), amount: `-${estimate.dependentCareCreditAmount}` },
    { key: 'socialSecurity', label: t('finance.taxEstimateSocialSecurity'), amount: estimate.socialSecurityTaxAmount },
    { key: 'medicare', label: t('finance.taxEstimateMedicare'), amount: estimate.medicareTaxAmount },
    {
      key: 'netInvestment',
      label: t('finance.taxEstimateNetInvestment'),
      amount: estimate.netInvestmentIncomeTaxAmount,
    },
    { key: 'state', label: t('finance.taxEstimateState'), amount: estimate.stateIncomeTaxAmount },
    { key: 'local', label: t('finance.taxEstimateLocal'), amount: estimate.localIncomeTaxAmount },
  ].filter((line) => line.key === 'federal' || !isZero(line.amount))
  return (
    <SettingsSection
      card
      title={t('finance.taxEstimateTitle', { year: String(year) })}
      description={t(estimate.isProjected ? 'finance.taxEstimateHint' : 'finance.taxEstimateHintOver')}
      action={
        <div className="finance-taxes-actions">
          <Select
            className="finance-tax-filing-select"
            value={estimate.taxFilingStatus}
            label={t('finance.taxFilingStatus')}
            disabled={isSaving}
            options={TAX_FILING_STATUSES.map((status) => ({ value: status, label: t(FILING_STATUS_LABELS[status]) }))}
            onChange={(value) => onFilingStatus(value as TaxFilingStatus)}
          />
          <button type="button" onClick={onEditSettings}>
            {t('finance.taxYearSettings')}
          </button>
        </div>
      }
    >
      <div className="finance-worth-summary finance-tax-estimate">
        <SpendingRing
          slices={slices}
          currency={TAX_CURRENCY_CODE}
          label={t('finance.taxEstimateRingLabel', { year: String(year) })}
          totalLabel={t('finance.taxForTheYear')}
          totalAmount={amountOf(estimate.totalTaxAmount)}
          highlightedKey={highlightedKey}
        />
        <dl className="finance-worth-line">
          {shown.map((slice) => figureLine({ key: slice.key, label: slice.label, amount: slice.amount, slice }))}
          {!isZero(estimate.refundAmount)
            ? figureLine({
                key: 'refund',
                label: t('finance.taxRefund'),
                amount: amountOf(estimate.refundAmount),
                className: 'finance-tax-refund',
              })
            : null}
          {hasInvestmentIncome ? (
            <>
              {figureLine({ key: 'pay', label: t('finance.taxPay'), amount: payAmount, isTotal: true })}
              {figureLine({
                key: 'investment',
                label: t(estimate.isInvestmentIncomePreviewed ? 'finance.taxEstimateInvestmentPreviewed' : 'finance.taxEstimateInvestmentIncome'),
                amount: amountOf(estimate.investmentIncomeAmount),
              })}
              {figureLine({ key: 'income', label: t('finance.taxIncome'), amount: incomeAmount, isTotal: true })}
            </>
          ) : (
            figureLine({ key: 'pay', label: t('finance.taxPay'), amount: payAmount, isTotal: true })
          )}
        </dl>
        <dl className="finance-worth-line">
          {taxLines.map((line) => figureLine({ key: line.key, label: line.label, amount: amountOf(line.amount) }))}
          {figureLine({
            key: 'total',
            label: t('finance.taxForTheYear'),
            amount: amountOf(estimate.totalTaxAmount),
            isTotal: true,
          })}
        </dl>
      </div>
      <p className="muted finance-receipt-detail finance-tax-note">
        {estimate.isTaxFilingStatusAssumed ? `${t('finance.taxFilingStatusAssumed')} ` : null}
        {!isZero(estimate.capitalLossDeductionAmount)
          ? `${t('finance.taxEstimateCapitalLoss', { amount: formatDollars(estimate.capitalLossDeductionAmount) })} `
          : null}
        {!isZero(estimate.otherIncomeAmount)
          ? `${t('finance.taxEstimateOtherIncome', { amount: formatDollars(estimate.otherIncomeAmount) })} `
          : null}
        {estimate.isItemized
          ? t('finance.taxEstimateItemizedNote', {
              deduction: formatDollars(estimate.deductionAmount),
              mortgage: formatDollars(estimate.mortgageInterestAmount),
              taxes: formatDollars(estimate.stateAndLocalTaxDeductionAmount),
              gifts: formatDollars(estimate.charitableContributionAmount),
              taxable: formatDollars(estimate.taxableIncomeAmount),
            })
          : isZero(estimate.charitableContributionAmount)
            ? t('finance.taxEstimateNote', {
                deduction: formatDollars(estimate.standardDeductionAmount),
                taxable: formatDollars(estimate.taxableIncomeAmount),
              })
            : t('finance.taxEstimateNoteGifts', {
                deduction: formatDollars(estimate.standardDeductionAmount),
                gifts: formatDollars(estimate.charitableContributionAmount),
                taxable: formatDollars(estimate.taxableIncomeAmount),
              })}
      </p>
    </SettingsSection>
  )
}

// TaxInvestmentPreviewPanel is the year's interest and dividends as the
// linked accounts were paid them, an account a line, those whose income is
// not taxed marked; what the estimate counts of it, projected to December
// 31, until the 1099s come.
function TaxInvestmentPreviewPanel({ preview }: { preview: TaxYearInvestmentPreview }) {
  const { t } = useTranslation()
  const countedWords = (isCounted: boolean) =>
    t(isCounted ? 'finance.taxPreviewCounted' : 'finance.taxPreviewReplaced')
  return (
    <SettingsSection card title={t('finance.taxPreviewTitle')} description={t('finance.taxPreviewHint')}>
      <div className="finance-receipt-lines-scroll">
        <table className="finance-receipt-lines finance-tax-figures">
          <thead>
            <tr>
              <th scope="col">{t('finance.taxPreviewAccount')}</th>
              <th scope="col">{t('finance.taxPreviewKind')}</th>
              <th scope="col" className="numeric">
                {t('finance.taxSoFar')}
              </th>
            </tr>
          </thead>
          <tbody>
            {preview.taxYearAccountIncomes.map((income) => (
              <tr key={`${income.financeAccountId}|${income.incomeKind}`}>
                <td title={income.accountName}>
                  <span className="finance-tax-preview-account">
                    <span>{income.accountName}</span>
                    {income.isTaxAdvantaged ? <Tag value={t('finance.taxPreviewNotTaxed')} /> : null}
                  </span>
                </td>
                <td className="muted">
                  {t(income.incomeKind === 'dividend' ? 'finance.taxPreviewDividend' : 'finance.taxPreviewInterest')}
                </td>
                <td className="numeric">{formatDollars(income.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="muted finance-receipt-detail finance-tax-note">
        {t('finance.taxPreviewTotals', {
          interest: formatDollars(preview.interestAmount),
          projectedInterest: formatDollars(preview.projectedInterestAmount),
          interestCounted: countedWords(preview.isInterestCounted),
          dividends: formatDollars(preview.dividendsAmount),
          projectedDividends: formatDollars(preview.projectedDividendsAmount),
          dividendsCounted: countedWords(preview.isDividendsCounted),
        })}
      </p>
    </SettingsSection>
  )
}

// TaxYearSettingDialog is what a year's estimate takes that no document
// says: children and the care paid for them, a spouse's or other household
// income, gifts and taxes for itemizing, and the gains realized so far in
// a year whose 1099-Bs have not come. Saving sends only the fields
// changed.
function TaxYearSettingDialog({
  year,
  setting,
  onClose,
  onSaved,
}: {
  year: number
  setting?: TaxYearSetting | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const plain = (amount?: string | null) => (amount && amountOf(amount) !== 0 ? String(amountOf(amount)) : '')
  const initial = {
    childCount: String(setting?.childCount ?? 0),
    otherDependentCount: String(setting?.otherDependentCount ?? 0),
    dependentCareExpenseAmount: plain(setting?.dependentCareExpenseAmount),
    otherIncomeAmount: plain(setting?.otherIncomeAmount),
    charitableContributionAmount: plain(setting?.charitableContributionAmount),
    propertyTaxAmount: plain(setting?.propertyTaxAmount),
    stateIncomeTaxPaidAmount: plain(setting?.stateIncomeTaxPaidAmount),
    estimatedShortTermGainAmount: setting?.estimatedShortTermGainAmount ? String(amountOf(setting.estimatedShortTermGainAmount)) : '',
    estimatedLongTermGainAmount: setting?.estimatedLongTermGainAmount ? String(amountOf(setting.estimatedLongTermGainAmount)) : '',
    estimatedGainNote: setting?.estimatedGainNote ?? '',
  }
  const [fields, setFields] = useState(initial)
  const [isBusy, setIsBusy] = useState(false)
  const field = (key: keyof typeof initial, label: Key, isCount = false) => (
    <label>
      <span>{t(label)}</span>
      <input
        inputMode={isCount ? 'numeric' : 'decimal'}
        value={fields[key]}
        onChange={(event) => setFields({ ...fields, [key]: event.target.value })}
      />
    </label>
  )
  const save = async () => {
    const variables: Record<string, unknown> = { taxYear: year }
    for (const key of Object.keys(initial) as (keyof typeof initial)[]) {
      if (fields[key] === initial[key]) continue
      if (key === 'childCount' || key === 'otherDependentCount') variables[key] = Number(fields[key] || 0)
      else if ((key === 'estimatedShortTermGainAmount' || key === 'estimatedLongTermGainAmount') && fields[key].trim() === '')
        variables.isEstimatedGainCleared = true
      else variables[key] = fields[key].trim() || (key === 'estimatedGainNote' ? '' : '0')
    }
    setIsBusy(true)
    try {
      await graphql(SET_TAX_YEAR_SETTING, variables)
      toast.done(t('finance.taxYearSettingsSaved', { year: String(year) }))
      onSaved()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }
  return (
    <FormDialog
      title={t('finance.taxYearSettingsTitle', { year: String(year) })}
      submitLabel={t('common.save')}
      busy={isBusy}
      canSubmit={!isBusy}
      onClose={onClose}
      onSubmit={() => void save()}
    >
      <p className="muted">{t('finance.taxYearSettingsHint')}</p>
      {field('childCount', 'finance.taxSettingChildren', true)}
      {field('otherDependentCount', 'finance.taxSettingOtherDependents', true)}
      {field('dependentCareExpenseAmount', 'finance.taxSettingDependentCare')}
      {field('otherIncomeAmount', 'finance.taxSettingOtherIncome')}
      {field('propertyTaxAmount', 'finance.taxSettingPropertyTax')}
      {field('stateIncomeTaxPaidAmount', 'finance.taxSettingStateTaxPaid')}
      {field('charitableContributionAmount', 'finance.taxSettingCharitable')}
      {field('estimatedShortTermGainAmount', 'finance.taxSettingShortTermGain')}
      {field('estimatedLongTermGainAmount', 'finance.taxSettingLongTermGain')}
      <label>
        <span>{t('finance.taxSettingGainNote')}</span>
        <input
          value={fields.estimatedGainNote}
          onChange={(event) => setFields({ ...fields, estimatedGainNote: event.target.value })}
        />
      </label>
      <p className="muted field-hint">{t('finance.taxSettingGainHint')}</p>
    </FormDialog>
  )
}

const FINDING_KIND_LABELS: Record<string, Key> = {
  broken_chain: 'finance.taxFindingBrokenChain',
  pay_date_gap: 'finance.taxFindingPayDateGap',
  duplicate_paystub: 'finance.taxFindingDuplicatePaystub',
  w2_missing: 'finance.taxFindingW2Missing',
  w2_differs_from_paystubs: 'finance.taxFindingW2Differs',
  several_w2s: 'finance.taxFindingSeveralW2s',
  excess_social_security: 'finance.taxFindingExcessSocialSecurity',
  hsa_over_limit: 'finance.taxFindingHSAOverLimit',
  cost_basis_missing: 'finance.taxFindingCostBasisMissing',
  investment_recipient_assumed: 'finance.taxFindingRecipientAssumed',
  mortgage_over_limit: 'finance.taxFindingMortgageOverLimit',
}

// TaxInvestmentPanel is the year's investment income from its 1099s: each
// figure the estimate taxes, a line apiece, those of nothing left out
// except interest and dividends.
function TaxInvestmentPanel({ investment }: { investment: TaxYearInvestment }) {
  const { t } = useTranslation()
  const lines: { key: string; label: string; amount: string; isAlwaysShown?: boolean }[] = [
    { key: 'interest', label: t('finance.taxInvestmentInterest'), amount: investment.interestAmount, isAlwaysShown: true },
    { key: 'taxExempt', label: t('finance.taxInvestmentTaxExemptInterest'), amount: investment.taxExemptInterestAmount },
    {
      key: 'ordinary',
      label: t('finance.taxInvestmentOrdinaryDividends'),
      amount: investment.ordinaryDividendsAmount,
      isAlwaysShown: true,
    },
    { key: 'qualified', label: t('finance.taxInvestmentQualifiedDividends'), amount: investment.qualifiedDividendsAmount },
    {
      key: 'distributions',
      label: t('finance.taxInvestmentCapitalGainDistributions'),
      amount: investment.capitalGainDistributionsAmount,
    },
    { key: 'shortTerm', label: t('finance.taxInvestmentShortTerm'), amount: investment.shortTermGainAmount },
    { key: 'longTerm', label: t('finance.taxInvestmentLongTerm'), amount: investment.longTermGainAmount },
    { key: 'withheld', label: t('finance.taxInvestmentWithheld'), amount: investment.federalIncomeTaxWithheldAmount },
    { key: 'foreign', label: t('finance.taxInvestmentForeignTax'), amount: investment.foreignTaxPaidAmount },
  ].filter((line) => line.isAlwaysShown || !isZero(line.amount))
  return (
    <SettingsSection card title={t('finance.taxInvestmentTitle')} description={t('finance.taxInvestmentHint')}>
      <div className="finance-receipt-lines-scroll">
        <table className="finance-receipt-lines finance-tax-figures">
          <tbody>
            {lines.map((line) => (
              <tr key={line.key}>
                <td title={line.label}>{line.label}</td>
                <td className="numeric">{formatDollars(line.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </SettingsSection>
  )
}

// --- the documents ----------------------------------------------------------

// TaxDocumentSource opens the file a paystub or W-2 was read from: a PDF
// in a new tab, a photo the same way.
function TaxDocumentSource({ agentAttachmentId, name }: { agentAttachmentId: string; name: string }) {
  const { t } = useTranslation()
  return (
    <Tooltip label={t('finance.openTaxDocumentFile')}>
      <a
        className="icon-action"
        href={`${AGENT_ATTACHMENTS_PATH}/${encodeURIComponent(agentAttachmentId)}`}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={`${name}: ${t('finance.openTaxDocumentFile')}`}
      >
        <PictureIcon size={16} />
      </a>
    </Tooltip>
  )
}

// TaxDocumentPicture is the photo a document was read from, above its rows
// so the two can be compared, zoomed over the page on a tap. A PDF does not
// load as a picture and shows nothing here; TaxDocumentSource opens it.
function TaxDocumentPicture({ agentAttachmentId, name }: { agentAttachmentId: string; name: string }) {
  const { t } = useTranslation()
  const [isUnreadable, setIsUnreadable] = useState(false)
  if (!agentAttachmentId || isUnreadable) return null
  return (
    <div className="finance-receipt-photo">
      <ZoomablePicture
        source={`${AGENT_ATTACHMENTS_PATH}/${encodeURIComponent(agentAttachmentId)}`}
        name={name}
        imageClassName="finance-receipt-photo-image"
        openTitle={t('finance.zoomReceiptPhoto')}
        onUnreadable={() => setIsUnreadable(true)}
      />
    </div>
  )
}

// paystubLinesTotal adds up the lines of one kind this period, for a
// paystub that prints no total of its own.
function paystubLinesTotal(paystub: FinancePaystub, kind: FinancePaystubLine['paystubLineKind']): number {
  return (
    paystub.paystubLines
      .filter((line) => line.paystubLineKind === kind)
      .reduce((sum, line) => sum + Math.round(amountOf(line.currentAmount) * 10000), 0) / 10000
  )
}

function paystubTaxTotal(paystub: FinancePaystub): number {
  return hasAmount(paystub.totalTaxAmount) ? amountOf(paystub.totalTaxAmount) : paystubLinesTotal(paystub, 'tax')
}

function paystubDeductionTotal(paystub: FinancePaystub): number {
  return hasAmount(paystub.totalDeductionAmount)
    ? amountOf(paystub.totalDeductionAmount)
    : paystubLinesTotal(paystub, 'deduction')
}

// usePaystubCheckDirection says which way an unbalanced paystub misses:
// gross pay less its taxes and deductions comes to more or less than the
// take-home pay printed. The difference is signed as the server keeps it,
// worked out less printed.
function usePaystubCheckDirection(): (paystub: FinancePaystub) => string {
  const { t } = useTranslation()
  return (paystub) => {
    const difference = amountOf(paystub.checkDifferenceAmount)
    if (paystub.paystubCheckState === 'balanced' || difference === 0) return ''
    const amount = formatMoney(Math.abs(difference), paystub.currencyCode)
    return difference < 0 ? t('finance.paystubLinesLess', { amount }) : t('finance.paystubLinesMore', { amount })
  }
}

// PaystubCheckTag says whether a paystub adds up, or by how much it
// misses, in the receipts' words; which way it misses is its tooltip.
export function PaystubCheckTag({ paystub }: { paystub: FinancePaystub }) {
  const { t } = useTranslation()
  const direction = usePaystubCheckDirection()
  if (paystub.paystubCheckState === 'balanced') return <Tag value={t('finance.receiptBalanced')} tone="good" />
  return (
    <span title={direction(paystub)}>
      <Tag
        value={t('finance.receiptUnbalanced', {
          amount: formatMoney(Math.abs(amountOf(paystub.checkDifferenceAmount)), paystub.currencyCode),
        })}
        tone="warn"
      />
    </span>
  )
}

// TaxFormCheckTag says whether a W-2 agrees with itself, or how many of
// its checks disagree; nothing when nothing could be checked.
function TaxFormCheckTag({ taxFormView }: { taxFormView: TaxFormView }) {
  const { plural, t, language } = useTranslation()
  if (taxFormView.taxFormFindings.length === 0) return null
  const disagreeingCount = taxFormView.taxFormFindings.filter((finding) => !finding.isAgreeing).length
  if (disagreeingCount === 0) return <Tag value={t('finance.taxFormAgrees')} tone="good" />
  return (
    <Tag
      value={plural(
        disagreeingCount,
        { one: 'finance.taxFormDisagreesOne', other: 'finance.taxFormDisagreesOther' },
        { count: disagreeingCount.toLocaleString(language) },
      )}
      tone="warn"
    />
  )
}

function taxFormName(taxFormView: TaxFormView): string {
  return taxFormView.financeTaxForm.payerName.trim() || taxFormView.financeTaxForm.recipientName
}

// --- reading what is uploaded ------------------------------------------------

type FoundDocument = { agentAttachmentId: string; message: string; taxYear: number }

// findReadDocuments looks for the paystubs and W-2s read from the uploads
// being waited for. The server cannot be asked for one by its upload, so
// this is the first page of each, every year, which is where a document
// just read is in practice.
async function findReadDocuments(
  agentAttachmentIds: string[],
  describe: {
    paystub: (paystub: FinancePaystub) => string
    taxForm: (taxFormView: TaxFormView) => string
  },
): Promise<FoundDocument[]> {
  const [paystubAnswer, taxFormAnswer] = await Promise.all([
    graphql<{ FinancePaystubs?: FinancePaystubPage | null }>(FINANCE_PAYSTUBS, {
      limit: MAXIMUM_LISTED_TAX_DOCUMENT_COUNT,
    }),
    graphql<{ FinanceTaxForms?: TaxFormPage | null }>(FINANCE_TAX_FORMS, { limit: MAXIMUM_LISTED_TAX_DOCUMENT_COUNT }),
  ])
  const waitedFor = new Set(agentAttachmentIds)
  const found: FoundDocument[] = []
  for (const paystub of paystubAnswer?.FinancePaystubs?.financePaystubs ?? []) {
    if (waitedFor.has(paystub.agentAttachmentId))
      found.push({
        agentAttachmentId: paystub.agentAttachmentId,
        message: describe.paystub(paystub),
        taxYear: Number(paystub.payDate.slice(0, 4)),
      })
  }
  for (const taxFormView of taxFormAnswer?.FinanceTaxForms?.taxForms ?? []) {
    if (waitedFor.has(taxFormView.financeTaxForm.agentAttachmentId))
      found.push({
        agentAttachmentId: taxFormView.financeTaxForm.agentAttachmentId,
        message: describe.taxForm(taxFormView),
        taxYear: taxFormView.financeTaxForm.taxYear,
      })
  }
  return found
}

// useTaxDocumentUpload uploads paystubs and W-2s, marked as tax documents
// so the server keeps none of their text, has the agent read each, and
// then reads the documents again every few seconds until each is read,
// saying so for each one, or gives up after a couple of minutes and says
// that. onRead is told when something new was read, and showYear opens
// the year a document read belongs to when it is not the one shown.
function useTaxDocumentUpload({
  shownYear,
  onRead,
  showYear,
}: {
  shownYear: number
  onRead: () => void
  showYear: (year: number) => void
}) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)
  const [readingAttachmentIds, setReadingAttachmentIds] = useState<string[]>([])
  const latest = useRef({ shownYear, onRead, showYear })
  latest.current = { shownYear, onRead, showYear }
  const readingKey = readingAttachmentIds.join(',')

  useEffect(() => {
    if (readingAttachmentIds.length === 0) return
    let isStopped = false
    let attempt = 0
    let timer = 0
    const describe = {
      paystub: (paystub: FinancePaystub) =>
        t('finance.paystubRead', { employer: paystub.employerName, day: formatDay(paystub.payDate) }),
      taxForm: (taxFormView: TaxFormView) =>
        t('finance.taxFormRead', {
          year: String(taxFormView.financeTaxForm.taxYear),
          form: taxFormKindName(taxFormView.financeTaxForm.taxFormKind),
          employer: taxFormName(taxFormView),
        }),
    }
    const poll = async () => {
      attempt += 1
      try {
        const found = await findReadDocuments(readingAttachmentIds, describe)
        if (isStopped) return
        if (found.length > 0) {
          for (const document of found) {
            const isOtherYear = document.taxYear !== latest.current.shownYear
            toast.done(
              document.message,
              isOtherYear
                ? {
                    label: t('finance.taxShowYear', { year: String(document.taxYear) }),
                    run: () => latest.current.showYear(document.taxYear),
                  }
                : undefined,
            )
          }
          latest.current.onRead()
          const foundIds = new Set(found.map((document) => document.agentAttachmentId))
          // Those still being read are waited for afresh, by the effect
          // that runs for the shorter list.
          setReadingAttachmentIds((previous) => previous.filter((id) => !foundIds.has(id)))
          return
        }
      } catch {
        // One failed read is not the end of waiting; the next one may work.
        if (isStopped) return
      }
      if (attempt >= taxDocumentPolling.attemptCount) {
        setReadingAttachmentIds([])
        toast.done(t('finance.taxDocumentsStillReading'))
        return
      }
      timer = window.setTimeout(() => void poll(), taxDocumentPolling.intervalMS)
    }
    timer = window.setTimeout(() => void poll(), taxDocumentPolling.intervalMS)
    return () => {
      isStopped = true
      window.clearTimeout(timer)
    }
    // Started once per change in what is waited for; the rest is read when
    // it fires.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readingKey])

  const upload = async (files: File[]) => {
    if (files.length === 0) return
    setIsBusy(true)
    try {
      const uploaded = (await uploadFiles('POST', TAX_DOCUMENT_UPLOAD_PATH, files, () => undefined).promise) as {
        attachments?: { id: string }[]
      }
      const agentAttachmentIds = (uploaded?.attachments ?? []).map((attachment) => attachment.id).filter(Boolean)
      if (agentAttachmentIds.length === 0) throw new Error(t('finance.taxDocumentNotStored'))
      // One file the server refuses (a PDF on a server that cannot read
      // them) leaves the others read, and says what it said.
      const queued: string[] = []
      for (const agentAttachmentId of agentAttachmentIds) {
        try {
          const answer = await graphql<{ ReadTaxDocument?: TaxDocumentReading | null }>(READ_TAX_DOCUMENT, {
            agentAttachmentId,
          })
          queued.push(...(answer?.ReadTaxDocument?.agentAttachmentIds ?? [agentAttachmentId]))
        } catch (caught) {
          toast.failure(caught, t('finance.failed'))
        }
      }
      if (queued.length > 0) {
        toast.done(
          plural(
            queued.length,
            { one: 'finance.taxDocumentReadingOne', other: 'finance.taxDocumentReadingOther' },
            { count: queued.length.toLocaleString(language) },
          ),
        )
        setReadingAttachmentIds((previous) => [...previous, ...queued.filter((id) => !previous.includes(id))])
      }
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  return { isBusy, isReading: readingAttachmentIds.length > 0, upload }
}

// --- the section ------------------------------------------------------------

export function FinanceTaxesSection() {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const [search, setSearch] = useSearchParams()
  const currentYear = Number(personToday().slice(0, 4))
  const year = yearFromSearch(search, currentYear)
  const picker = useRef<HTMLInputElement | null>(null)

  // Another year is another list of paystubs, so the page goes back to
  // the first; the address is rewritten in place rather than adding a
  // step to Back, as the receipts' filters are.
  const showYear = useCallback(
    (nextYear: number) =>
      setSearch(
        (previous) => {
          const next = new URLSearchParams(previous)
          next.set('year', String(nextYear))
          next.delete('page')
          return next
        },
        { replace: true },
      ),
    [setSearch],
  )

  const taxYearQuery = useQuery(
    async () =>
      (await graphql<{ TaxYear: TaxYearSummary }>(TAX_YEAR, { taxYear: year })).TaxYear,
    [year],
    { refresh: false },
  )
  const taxFormsQuery = useQuery(
    async () => {
      const answer = await graphql<{ FinanceTaxForms?: TaxFormPage | null }>(FINANCE_TAX_FORMS, {
        taxYear: year,
        limit: MAXIMUM_LISTED_TAX_DOCUMENT_COUNT,
      })
      return answer?.FinanceTaxForms ?? { taxForms: [], hasMore: false, totalCount: 0 }
    },
    [year],
    { refresh: false },
  )

  // The page asked for follows the Receipts section: nothing before the
  // table says its range, and until it says one for this year the page
  // is the address's, which another year has sent back to the first.
  const yearKey = String(year)
  const yearKeyNow = useRef(yearKey)
  yearKeyNow.current = yearKey
  const [range, setRange] = useState<(Range & { yearKey: string }) | null>(null)
  const onRange = useCallback((next: Range) => setRange({ ...next, yearKey: yearKeyNow.current }), [])
  const { pageIndex } = usePageInAddress()
  const offset = range === null ? null : range.yearKey === yearKey ? range.offset : pageIndex * range.limit
  const paystubsQuery = useQuery(
    async () => {
      if (!range || offset === null) return null
      const answer = await graphql<{ FinancePaystubs?: FinancePaystubPage | null }>(FINANCE_PAYSTUBS, {
        taxYear: year,
        limit: range.limit,
        offset,
      })
      return answer?.FinancePaystubs ?? { financePaystubs: [], hasMore: false, totalCount: 0 }
    },
    [yearKey, offset, range?.limit],
    { refresh: false },
  )

  const reload = async () => {
    await Promise.all([taxYearQuery.reload(true), taxFormsQuery.reload(true), paystubsQuery.reload(true)])
  }
  const reloadRef = useRef(reload)
  reloadRef.current = reload
  const uploading = useTaxDocumentUpload({
    shownYear: year,
    onRead: () => void reloadRef.current(),
    showYear,
  })

  const [opened, setOpened] = useState<Opened | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  useEffect(() => setOpened(null), [year])

  const openPaystub = async (paystubId: string) => {
    const onPage = paystubsQuery.data?.financePaystubs.find((paystub) => paystub.id === paystubId)
    if (onPage) {
      setOpened({ dialogKind: 'paystub', paystub: onPage })
      return
    }
    try {
      const answer = await graphql<{ FinancePaystub?: FinancePaystub | null }>(FINANCE_PAYSTUB, { paystubId })
      if (!answer?.FinancePaystub) {
        toast.failed(t('finance.paystubGone'))
        return
      }
      setOpened({ dialogKind: 'paystub', paystub: answer.FinancePaystub })
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    }
  }

  // The year's settings, in a dialog of their own.
  const [isEditingSettings, setIsEditingSettings] = useState(false)

  // The filing status is the person's setting: kept on the server, the
  // year read again for it.
  const [isSavingFilingStatus, setIsSavingFilingStatus] = useState(false)
  const saveFilingStatus = async (status: TaxFilingStatus) => {
    setIsSavingFilingStatus(true)
    try {
      await graphql(SET_TAX_FILING_STATUS, { taxFilingStatus: status })
      await taxYearQuery.reload(true)
      toast.done(t('finance.taxFilingStatusSaved', { status: t(FILING_STATUS_LABELS[status]) }))
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsSavingFilingStatus(false)
    }
  }

  const removePaystub = async (paystub: FinancePaystub) => {
    setIsBusy(true)
    try {
      await graphql(DELETE_PAYSTUB, { paystubId: paystub.id })
      toast.done(t('finance.paystubDeleted', { employer: paystub.employerName, day: formatDay(paystub.payDate) }))
      setOpened(null)
      await reload()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const removeTaxForm = async (taxFormView: TaxFormView) => {
    setIsBusy(true)
    try {
      await graphql(DELETE_TAX_FORM, { taxFormId: taxFormView.financeTaxForm.id })
      toast.done(
        t('finance.taxFormDeleted', {
          year: String(taxFormView.financeTaxForm.taxYear),
          form: taxFormKindName(taxFormView.financeTaxForm.taxFormKind),
          employer: taxFormName(taxFormView),
        }),
      )
      setOpened(null)
      await reload()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const summary = taxYearQuery.data?.taxYear === year ? taxYearQuery.data : null
  const isEmptyYear = summary !== null && summary.paystubCount === 0 && summary.taxFormCount === 0
  const taxForms = taxFormsQuery.data?.taxForms ?? []
  const shownPage = paystubsQuery.data ?? null
  const totalCount = shownPage?.totalCount ?? 0
  const paystubs = shownPage?.financePaystubs ?? []

  const paystubCountWords = (count: number) =>
    plural(
      count,
      { one: 'finance.paystubCountOne', other: 'finance.paystubCountOther' },
      { count: count.toLocaleString(language) },
    )

  const columns: Column<FinancePaystub>[] = [
    {
      key: 'payDate',
      header: t('finance.paystubPayDate'),
      value: (paystub) => paystub.payDate,
      render: (paystub) => formatDay(paystub.payDate),
    },
    {
      key: 'employer',
      header: t('finance.paystubEmployer'),
      value: (paystub) => paystub.employerName,
      // One line high, cut with an ellipsis, the whole name on hover, so
      // the amounts and the check beside it stay where the eye expects.
      render: (paystub) => (
        <span className="finance-tax-name" title={paystub.employerName}>
          {paystub.employerName}
        </span>
      ),
    },
    {
      key: 'gross',
      header: t('finance.paystubGross'),
      numeric: true,
      value: (paystub) => paystub.grossAmount,
      render: (paystub) => formatMoney(amountOf(paystub.grossAmount), paystub.currencyCode),
    },
    {
      key: 'taxes',
      header: t('finance.paystubTaxes'),
      numeric: true,
      render: (paystub) => formatMoney(paystubTaxTotal(paystub), paystub.currencyCode),
    },
    {
      key: 'deductions',
      header: t('finance.paystubDeductions'),
      numeric: true,
      render: (paystub) => formatMoney(paystubDeductionTotal(paystub), paystub.currencyCode),
    },
    {
      key: 'net',
      header: t('finance.paystubNet'),
      numeric: true,
      value: (paystub) => paystub.netAmount,
      render: (paystub) => formatMoney(amountOf(paystub.netAmount), paystub.currencyCode),
    },
    {
      key: 'check',
      header: t('finance.receiptCheck'),
      render: (paystub) => <PaystubCheckTag paystub={paystub} />,
    },
  ]

  // The dialog open now, one at a time.
  let dialog: React.ReactNode = null
  if (opened?.dialogKind === 'paystub') {
    const paystub = opened.paystub
    dialog = (
      <PaystubDialog
        paystub={paystub}
        isBusy={isBusy}
        onDelete={() => setOpened({ dialogKind: 'deletePaystub', paystub })}
        onClose={() => setOpened(null)}
      />
    )
  } else if (opened?.dialogKind === 'deletePaystub') {
    const paystub = opened.paystub
    dialog = (
      <ConfirmDialog
        title={t('finance.deletePaystubTitle', { employer: paystub.employerName, day: formatDay(paystub.payDate) })}
        body={t('finance.deletePaystubBody')}
        confirmLabel={t('common.delete')}
        busy={isBusy}
        onConfirm={() => void removePaystub(paystub)}
        onClose={() => setOpened({ dialogKind: 'paystub', paystub })}
      />
    )
  } else if (opened?.dialogKind === 'taxForm') {
    const taxFormView = opened.taxFormView
    dialog = (
      <TaxFormDialog
        taxFormView={taxFormView}
        isBusy={isBusy}
        onDelete={() => setOpened({ dialogKind: 'deleteTaxForm', taxFormView })}
        onClose={() => setOpened(null)}
      />
    )
  } else if (opened?.dialogKind === 'addPaystub') {
    dialog = <PaystubEntryDialog onRecorded={() => void reload()} onClose={() => setOpened(null)} />
  } else if (opened?.dialogKind === 'addTaxForm') {
    dialog = <TaxFormEntryDialog taxYear={year} onRecorded={() => void reload()} onClose={() => setOpened(null)} />
  } else if (opened?.dialogKind === 'deleteTaxForm') {
    const taxFormView = opened.taxFormView
    dialog = (
      <ConfirmDialog
        title={t('finance.deleteTaxFormTitle', {
          year: String(taxFormView.financeTaxForm.taxYear),
          form: taxFormKindName(taxFormView.financeTaxForm.taxFormKind),
          employer: taxFormName(taxFormView),
        })}
        body={t('finance.deleteTaxFormBody')}
        confirmLabel={t('common.delete')}
        busy={isBusy}
        onConfirm={() => void removeTaxForm(taxFormView)}
        onClose={() => setOpened({ dialogKind: 'taxForm', taxFormView })}
      />
    )
  }

  return (
    <>
      <SettingsSection
        card
        title={t('finance.taxesTitle')}
        action={
          <div className="finance-taxes-actions">
            <Select
              className="finance-tax-year-select"
              value={String(year)}
              label={t('finance.taxYear')}
              options={offeredYears(currentYear, year).map((option) => ({ value: option, label: option }))}
              onChange={(value) => showYear(Number(value))}
            />
            {/* What can be read is said where the files are chosen. */}
            <Tooltip label={t('finance.addTaxDocumentsHint')}>
              <button type="button" disabled={uploading.isBusy} onClick={() => picker.current?.click()}>
                {t('finance.addTaxDocuments')}
              </button>
            </Tooltip>
            <button type="button" onClick={() => setOpened({ dialogKind: 'addPaystub' })}>
              {t('finance.addPaystub')}
            </button>
            <button type="button" onClick={() => setOpened({ dialogKind: 'addTaxForm' })}>
              {t('finance.addTaxForm')}
            </button>
            <input
              ref={picker}
              type="file"
              accept="application/pdf,image/*"
              multiple
              hidden
              data-testid="tax-document-files"
              onChange={(event) => {
                const files = Array.from(event.target.files ?? [])
                event.target.value = ''
                void uploading.upload(files)
              }}
            />
          </div>
        }
      >
        <ErrorMessage error={taxYearQuery.error} />
        {!summary && !taxYearQuery.error ? <Loading /> : null}
        {isEmptyYear ? <SettingsEmpty>{t('finance.noTaxDocuments', { year: String(year) })}</SettingsEmpty> : null}
        <TaxDocumentAddressRow />
      </SettingsSection>
      {summary && !isEmptyYear ? (
        <>
          {summary.taxYearEstimate ? (
            <TaxEstimatePanel
              estimate={summary.taxYearEstimate}
              year={year}
              isSaving={isSavingFilingStatus}
              onFilingStatus={(status) => void saveFilingStatus(status)}
              onEditSettings={() => setIsEditingSettings(true)}
            />
          ) : null}
          {summary.taxYearInvestmentPreview ? (
            <TaxInvestmentPreviewPanel preview={summary.taxYearInvestmentPreview} />
          ) : null}
          {summary.taxYearEmployments.map((employment) => (
            <EmploymentPanel
              key={`${employment.employeeName}|${employment.employerName}`}
              employment={employment}
              retirementDeferralLimitAmount={summary.retirementDeferralLimitAmount}
            />
          ))}
          {summary.taxYearEmployments.length > 1 ? <HouseholdPanel summary={summary} /> : null}
          {summary.taxYearInvestment ? <TaxInvestmentPanel investment={summary.taxYearInvestment} /> : null}
          {summary.taxYearFindings.length > 0 ? (
            <SettingsSection card title={t('finance.taxFindingsTitle')} description={t('finance.taxFindingsHint')}>
              <ul className="finance-receipt-candidates finance-tax-findings">
                {summary.taxYearFindings.map((finding, index) => {
                  const kindLabel = FINDING_KIND_LABELS[finding.taxYearFindingCode]
                  const paystubId = finding.paystubIds[0]
                  return (
                    <li key={`${finding.taxYearFindingCode}-${index}`}>
                      <div>
                        {kindLabel ? <Tag value={t(kindLabel)} /> : null}{' '}
                        {paystubId ? (
                          <button /* link-button: opens the paystub the finding is about in place, inline in a list of them */
                            type="button"
                            className="link"
                            onClick={() => void openPaystub(paystubId)}
                          >
                            {finding.findingText}
                          </button>
                        ) : (
                          <span>{finding.findingText}</span>
                        )}
                      </div>
                    </li>
                  )
                })}
              </ul>
            </SettingsSection>
          ) : null}
          <SettingsSection card title={t('finance.taxFormsTitle')} description={t('finance.taxFormsHint')}>
            <ErrorMessage error={taxFormsQuery.error} />
            {taxFormsQuery.data && taxForms.length === 0 ? (
              <SettingsEmpty>{t('finance.noTaxForms', { year: String(year) })}</SettingsEmpty>
            ) : null}
            {taxForms.length > 0 ? (
              <div className="table-surface finance-tax-forms-table">
                <table>
                  <thead>
                    <tr>
                      <th>{t('finance.taxFormYear')}</th>
                      <th>{t('finance.taxFormKind')}</th>
                      <th>{t('finance.taxFormPayer')}</th>
                      <th>{t('finance.taxFormEmployee')}</th>
                      <th>{t('finance.receiptCheck')}</th>
                      <th>{t('finance.taxFormFile')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {taxForms.map((taxFormView) => {
                      const name = taxFormName(taxFormView)
                      return (
                        <tr key={taxFormView.financeTaxForm.id}>
                          <td>{taxFormView.financeTaxForm.taxYear}</td>
                          <td>{taxFormKindName(taxFormView.financeTaxForm.taxFormKind)}</td>
                          <td>
                            <button /* link-button: opens the form's boxes in place, inline in a list of them */
                              type="button"
                              className="link finance-tax-name"
                              title={name}
                              aria-label={t('finance.taxFormDetailsOf', {
                                year: String(taxFormView.financeTaxForm.taxYear),
                                form: taxFormKindName(taxFormView.financeTaxForm.taxFormKind),
                                employer: name,
                              })}
                              onClick={() => setOpened({ dialogKind: 'taxForm', taxFormView })}
                            >
                              {name}
                            </button>
                          </td>
                          <td>{taxFormView.financeTaxForm.recipientName}</td>
                          <td>
                            <TaxFormCheckTag taxFormView={taxFormView} />
                          </td>
                          <td>
                            <TaxDocumentSource
                              agentAttachmentId={taxFormView.financeTaxForm.agentAttachmentId}
                              name={name}
                            />
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            ) : null}
          </SettingsSection>
          <SettingsSection card title={t('finance.paystubsTitle')} description={t('finance.paystubsHint')}>
            {shownPage && totalCount > 0 ? (
              <p className="muted finance-transaction-count">{paystubCountWords(totalCount)}</p>
            ) : null}
            <ErrorMessage error={paystubsQuery.error} />
            {/* Drawn before the first answer, since the table is what says
                which page to ask for. */}
            <div className="finance-transactions-table finance-paystubs-table">
              <DataTable
                columns={columns}
                rows={paystubs}
                rowKey={(paystub) => paystub.id}
                loading={!shownPage}
                remote={{ total: totalCount, onRange }}
                emptyMessage={t('finance.noPaystubs', { year: String(year) })}
                countLabel={paystubCountWords}
                onRowOpen={(paystub) => setOpened({ dialogKind: 'paystub', paystub })}
                rowOpenLabel={(paystub) =>
                  t('finance.paystubDetailsOf', { employer: paystub.employerName, day: formatDay(paystub.payDate) })
                }
              />
            </div>
          </SettingsSection>
        </>
      ) : null}
      {dialog}
      {isEditingSettings ? (
        <TaxYearSettingDialog
          year={year}
          setting={summary?.taxYearSetting}
          onClose={() => setIsEditingSettings(false)}
          onSaved={() => {
            setIsEditingSettings(false)
            void taxYearQuery.reload(true)
          }}
        />
      ) : null}
    </>
  )
}

// --- a paystub ----------------------------------------------------------------

type LineColumn = {
  key: string
  header: string
  amountOf: (line: FinancePaystubLine) => string | null | undefined
  isMoney: boolean
}

// PaystubLineGroup is one kind of a paystub's lines (its earnings, taxes
// or deductions) as a compact table: what each says, then its amounts,
// a column left out when no line prints it, and the group's total.
function PaystubLineGroup({
  heading,
  lines,
  columns,
  currencyCode,
  totalLabel,
  totalAmount,
  yearToDateTotalAmount,
}: {
  heading: string
  lines: FinancePaystubLine[]
  columns: LineColumn[]
  currencyCode: string
  totalLabel: string
  totalAmount: number
  yearToDateTotalAmount?: number | null
}) {
  const { t, language } = useTranslation()
  if (lines.length === 0) return null
  const shownColumns = columns.filter((column) => lines.some((line) => hasAmount(column.amountOf(line))))
  const cellText = (column: LineColumn, line: FinancePaystubLine) => {
    const value = column.amountOf(line)
    if (!hasAmount(value)) return ''
    return column.isMoney
      ? formatMoney(amountOf(value), currencyCode)
      : new Intl.NumberFormat(language, { maximumFractionDigits: 2 }).format(Number(value))
  }
  const periodIndex = shownColumns.findIndex((column) => column.key === 'current')
  const yearToDateIndex = shownColumns.findIndex((column) => column.key === 'yearToDate')
  return (
    <>
      <div className="finance-metadata-head">
        <strong>{heading}</strong>
      </div>
      <div className="finance-receipt-lines-scroll">
        <table className="finance-receipt-lines finance-paystub-lines">
          <thead>
            <tr>
              <th scope="col">{t('finance.paystubLineDescription')}</th>
              {shownColumns.map((column) => (
                <th key={column.key} scope="col" className="numeric">
                  {column.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {lines.map((line) => {
              const description = [line.description, line.taxJurisdictionCode ?? ''].filter(Boolean).join(' ')
              return (
                <tr key={line.id}>
                  {/* Cut to one line; the whole of it on hover. */}
                  <td title={description}>
                    {line.description}
                    {/* The state's code, unless the line's own words already say it. */}
                    {line.taxJurisdictionCode && !line.description.includes(line.taxJurisdictionCode) ? (
                      <span className="muted finance-receipt-tax-mark"> {line.taxJurisdictionCode}</span>
                    ) : null}
                  </td>
                  {shownColumns.map((column) => (
                    <td key={column.key} className="numeric">
                      {cellText(column, line)}
                    </td>
                  ))}
                </tr>
              )
            })}
          </tbody>
          <tfoot>
            <tr>
              <th scope="row">{totalLabel}</th>
              {shownColumns.map((column, index) => (
                <td key={column.key} className="numeric">
                  {index === periodIndex
                    ? formatMoney(totalAmount, currencyCode)
                    : index === yearToDateIndex && yearToDateTotalAmount !== undefined && yearToDateTotalAmount !== null
                      ? formatMoney(yearToDateTotalAmount, currencyCode)
                      : ''}
                </td>
              ))}
            </tr>
          </tfoot>
        </table>
      </div>
    </>
  )
}

// PaystubDialog is one paystub: who paid it, when and for which days, its
// check, its file, its lines grouped as earnings, taxes and deductions with
// their totals, the take-home pay, and Delete.
function PaystubDialog({
  paystub,
  isBusy,
  onDelete,
  onClose,
}: {
  paystub: FinancePaystub
  isBusy: boolean
  onDelete: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const checkDirection = usePaystubCheckDirection()
  const direction = checkDirection(paystub)
  const lines = [...paystub.paystubLines].sort((first, second) => first.lineNumber - second.lineNumber)
  const linesOf = (kind: FinancePaystubLine['paystubLineKind']) => lines.filter((line) => line.paystubLineKind === kind)
  const current: LineColumn = {
    key: 'current',
    header: t('finance.paystubThisPeriod'),
    amountOf: (line) => line.currentAmount,
    isMoney: true,
  }
  const yearToDate: LineColumn = {
    key: 'yearToDate',
    header: t('finance.paystubYearToDate'),
    amountOf: (line) => line.yearToDateAmount,
    isMoney: true,
  }
  const name = paystub.employerName
  const period =
    paystub.periodStartOn && paystub.periodEndOn
      ? t('finance.paystubPeriod', { from: formatDay(paystub.periodStartOn), to: formatDay(paystub.periodEndOn) })
      : ''
  return (
    <ConfirmDialog
      wide
      title={name}
      onClose={onClose}
      body={
        <div className="finance-receipt-details">
          <div className="finance-receipt-head">
            <div className="finance-receipt-title">
              <span className="muted">
                {[t('finance.paystubPaidOn', { day: formatDay(paystub.payDate) }), period, paystub.employeeName]
                  .filter(Boolean)
                  .join(' · ')}
              </span>{' '}
              <PaystubCheckTag paystub={paystub} />
            </div>
            <div className="row-actions">
              <TaxDocumentSource agentAttachmentId={paystub.agentAttachmentId} name={name} />
            </div>
          </div>
          {direction ? <p className="muted finance-receipt-detail">{direction}</p> : null}
          <TaxDocumentPicture agentAttachmentId={paystub.agentAttachmentId} name={name} />
          <PaystubLineGroup
            heading={t('finance.paystubEarnings')}
            lines={linesOf('earning')}
            columns={[
              { key: 'hours', header: t('finance.paystubHours'), amountOf: (line) => line.hours, isMoney: false },
              { key: 'rate', header: t('finance.paystubRate'), amountOf: (line) => line.rateAmount, isMoney: true },
              current,
              yearToDate,
            ]}
            currencyCode={paystub.currencyCode}
            totalLabel={t('finance.paystubGross')}
            totalAmount={amountOf(paystub.grossAmount)}
            yearToDateTotalAmount={
              hasAmount(paystub.yearToDateGrossAmount) ? amountOf(paystub.yearToDateGrossAmount) : null
            }
          />
          <PaystubLineGroup
            heading={t('finance.paystubTaxes')}
            lines={linesOf('tax')}
            columns={[
              {
                key: 'currentWages',
                header: t('finance.paystubWagesThisPeriod'),
                amountOf: (line) => line.currentWagesAmount,
                isMoney: true,
              },
              {
                key: 'yearToDateWages',
                header: t('finance.paystubWagesYearToDate'),
                amountOf: (line) => line.yearToDateWagesAmount,
                isMoney: true,
              },
              current,
              yearToDate,
            ]}
            currencyCode={paystub.currencyCode}
            totalLabel={t('finance.paystubTaxes')}
            totalAmount={paystubTaxTotal(paystub)}
          />
          <PaystubLineGroup
            heading={t('finance.paystubDeductions')}
            lines={linesOf('deduction')}
            columns={[
              current,
              yearToDate,
              {
                key: 'employerCurrent',
                header: t('finance.paystubEmployerPart'),
                amountOf: (line) => line.employerCurrentAmount,
                isMoney: true,
              },
              {
                key: 'employerYearToDate',
                header: t('finance.paystubEmployerPartYearToDate'),
                amountOf: (line) => line.employerYearToDateAmount,
                isMoney: true,
              },
            ]}
            currencyCode={paystub.currencyCode}
            totalLabel={t('finance.paystubDeductions')}
            totalAmount={paystubDeductionTotal(paystub)}
          />
          <PaystubLineGroup
            heading={t('finance.paystubReimbursements')}
            lines={linesOf('reimbursement')}
            columns={[current, yearToDate]}
            currencyCode={paystub.currencyCode}
            totalLabel={t('finance.paystubReimbursements')}
            totalAmount={paystubLinesTotal(paystub, 'reimbursement')}
          />
          <p className="finance-paystub-net">
            <span>{t('finance.paystubNet')}</span>
            <span className="numeric-value">{formatMoney(amountOf(paystub.netAmount), paystub.currencyCode)}</span>
          </p>
          <div className="finance-receipt-buttons">
            <button type="button" className="danger" disabled={isBusy} onClick={onDelete}>
              {t('finance.deletePaystub')}
            </button>
          </div>
        </div>
      }
    />
  )
}

// --- a W-2 or a 1099 -------------------------------------------------------

// BOX_LABELS name each kind's boxes, by box as printed.
const BOX_LABELS: Record<string, Record<string, Key>> = {
  w2: {
    '1': 'finance.w2Box1',
    '2': 'finance.w2Box2',
    '3': 'finance.w2Box3',
    '4': 'finance.w2Box4',
    '5': 'finance.w2Box5',
    '6': 'finance.w2Box6',
    '7': 'finance.w2Box7',
    '8': 'finance.w2Box8',
    '10': 'finance.w2Box10',
    '11': 'finance.w2Box11',
    '12': 'finance.w2Box12',
    '13': 'finance.w2Box13',
    '14': 'finance.w2Box14',
    '15': 'finance.w2Box15',
    '16': 'finance.w2Box16',
    '17': 'finance.w2Box17',
    '18': 'finance.w2Box18',
    '19': 'finance.w2Box19',
    '20': 'finance.w2Box20',
  },
  '1099_int': {
    '1': 'finance.int1099Box1',
    '2': 'finance.int1099Box2',
    '3': 'finance.int1099Box3',
    '4': 'finance.int1099Box4',
    '5': 'finance.int1099Box5',
    '6': 'finance.int1099Box6',
    '8': 'finance.int1099Box8',
    '9': 'finance.int1099Box9',
    '10': 'finance.int1099Box10',
    '11': 'finance.int1099Box11',
    '12': 'finance.int1099Box12',
    '13': 'finance.int1099Box13',
    '16': 'finance.int1099Box16',
    '17': 'finance.int1099Box17',
  },
  '1099_div': {
    '1a': 'finance.div1099Box1a',
    '1b': 'finance.div1099Box1b',
    '2a': 'finance.div1099Box2a',
    '2b': 'finance.div1099Box2b',
    '2c': 'finance.div1099Box2c',
    '2d': 'finance.div1099Box2d',
    '2e': 'finance.div1099Box2e',
    '2f': 'finance.div1099Box2f',
    '3': 'finance.div1099Box3',
    '4': 'finance.div1099Box4',
    '5': 'finance.div1099Box5',
    '6': 'finance.div1099Box6',
    '7': 'finance.div1099Box7',
    '9': 'finance.div1099Box9',
    '10': 'finance.div1099Box10',
    '12': 'finance.div1099Box12',
    '13': 'finance.div1099Box13',
    '15': 'finance.div1099Box15',
    '16': 'finance.div1099Box16',
  },
  '1098': {
    '1': 'finance.mortgage1098Box1',
    '2': 'finance.mortgage1098Box2',
    '3': 'finance.mortgage1098Box3',
    '4': 'finance.mortgage1098Box4',
    '5': 'finance.mortgage1098Box5',
    '6': 'finance.mortgage1098Box6',
    '8': 'finance.mortgage1098Box8',
    '9': 'finance.mortgage1098Box9',
    '10': 'finance.mortgage1098Box10',
    '11': 'finance.mortgage1098Box11',
  },
  '1099_b': {
    '1d': 'finance.b1099Box1d',
    '1e': 'finance.b1099Box1e',
    '1f': 'finance.b1099Box1f',
    '1g': 'finance.b1099Box1g',
    '4': 'finance.b1099Box4',
    '15': 'finance.b1099Box15',
    '16': 'finance.b1099Box16',
  },
}

// A 1099-B's totals, which the server names as its boxes' qualifiers.
const BROKER_TERM_LABELS: Record<string, Key> = {
  short_covered: 'finance.brokerTermShortCovered',
  short_noncovered: 'finance.brokerTermShortNoncovered',
  long_covered: 'finance.brokerTermLongCovered',
  long_noncovered: 'finance.brokerTermLongNoncovered',
  unknown_term: 'finance.brokerTermUnknown',
}

// Box 13's three checkboxes, which the server names as its qualifiers.
const W2_CHECKBOX_LABELS: Record<string, Key> = {
  statutory_employee: 'finance.w2StatutoryEmployee',
  retirement_plan: 'finance.w2RetirementPlan',
  third_party_sick_pay: 'finance.w2ThirdPartySickPay',
}

// TaxFormDialog is one W-2, 1099 or 1098: whose and from whom, its check, its file, its
// boxes as printed, what its checks of itself found, and Delete.
function TaxFormDialog({
  taxFormView,
  isBusy,
  onDelete,
  onClose,
}: {
  taxFormView: TaxFormView
  isBusy: boolean
  onDelete: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const taxForm = taxFormView.financeTaxForm
  const name = taxFormName(taxFormView)
  const entries = [...taxForm.taxFormEntries].sort((first, second) => first.entryNumber - second.entryNumber)
  const qualifierText = (entry: FinanceTaxFormEntry) => {
    const qualifier = entry.boxQualifier ?? ''
    const qualifierLabel =
      taxForm.taxFormKind === '1099_b'
        ? BROKER_TERM_LABELS[qualifier]
        : entry.boxCode === '13'
          ? W2_CHECKBOX_LABELS[qualifier]
          : undefined
    return qualifierLabel ? t(qualifierLabel) : qualifier
  }
  const valueText = (entry: FinanceTaxFormEntry) => {
    if (hasAmount(entry.amount)) return formatDollars(entry.amount)
    if (entry.textValue) return entry.textValue
    if ((taxForm.taxFormKind === 'w2' && entry.boxCode === '13') || entry.isChecked)
      return entry.isChecked ? t('finance.w2Checked') : t('finance.w2NotChecked')
    return ''
  }
  return (
    <ConfirmDialog
      wide
      title={t('finance.taxFormTitle', {
        year: String(taxForm.taxYear),
        form: taxFormKindName(taxForm.taxFormKind),
        employer: name,
      })}
      onClose={onClose}
      body={
        <div className="finance-receipt-details">
          <div className="finance-receipt-head">
            <div className="finance-receipt-title">
              <span className="muted">
                {[
                  t('finance.taxFormFor', { employee: taxForm.recipientName }),
                  taxForm.isCorrected ? t('finance.taxFormCorrected') : '',
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </span>{' '}
              <TaxFormCheckTag taxFormView={taxFormView} />
            </div>
            <div className="row-actions">
              <TaxDocumentSource agentAttachmentId={taxForm.agentAttachmentId} name={name} />
            </div>
          </div>
          {taxFormView.taxFormFindings.length > 0 ? (
            <ul className="finance-receipt-candidates finance-tax-form-findings">
              {taxFormView.taxFormFindings.map((finding) => (
                <li key={finding.taxFormFindingCode}>
                  <span>{finding.findingText}</span>
                  <Tag
                    value={finding.isAgreeing ? t('finance.taxFindingAgrees') : t('finance.taxFindingDisagrees')}
                    tone={finding.isAgreeing ? 'good' : 'warn'}
                  />
                </li>
              ))}
            </ul>
          ) : null}
          <TaxDocumentPicture agentAttachmentId={taxForm.agentAttachmentId} name={name} />
          <div className="finance-receipt-lines-scroll">
            <table className="finance-receipt-lines finance-tax-form-boxes">
              <thead>
                <tr>
                  <th scope="col">{t('finance.taxBox')}</th>
                  <th scope="col">{t('finance.taxBoxOf')}</th>
                  <th scope="col" className="numeric">
                    {t('finance.taxBoxValue')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => {
                  const boxLabelKey = BOX_LABELS[taxForm.taxFormKind]?.[entry.boxCode]
                  const boxLabel = boxLabelKey ? t(boxLabelKey) : ''
                  return (
                    <tr key={entry.id}>
                      <td title={[entry.boxCode, boxLabel].filter(Boolean).join(' ')}>
                        <span className="finance-receipt-kind">{entry.boxCode}</span>
                        {boxLabel}
                      </td>
                      <td className="muted">{qualifierText(entry)}</td>
                      <td className="numeric">{valueText(entry)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          <div className="finance-receipt-buttons">
            <button type="button" className="danger" disabled={isBusy} onClick={onDelete}>
              {t('finance.deleteTaxForm', { form: taxFormKindName(taxForm.taxFormKind) })}
            </button>
          </div>
        </div>
      }
    />
  )
}
