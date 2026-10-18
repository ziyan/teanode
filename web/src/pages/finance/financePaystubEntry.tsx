import { useEffect, useState } from 'react'

import { graphql } from '../../api'
import { FormDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'
import { TAX_DOCUMENT_UPLOAD_PATH, formatDay, isDecimal } from './financeApi'
import { CurrencyPicker, useReportingCurrency } from './financeCommon'
import {
  PAYSTUB_DEDUCTION_KINDS,
  PAYSTUB_LINE_KINDS,
  PAYSTUB_TAX_KINDS,
  PaystubLineInput,
  PaystubLineKind,
  PAYSTUB_UNBALANCED_REFUSAL,
  RECORD_PAYSTUB,
} from './financeEntryApi'
import {
  ExchangeRateHint,
  SourceFile,
  SourceFileField,
  blankToUndefined,
  isDay,
  isOptionalDecimal,
} from './financeEntryCommon'

// The dialog that records a paystub by hand: the Add paystub button on the
// Taxes page opens it. It asks for the paystub as RecordPaystub takes it
// (who paid whom and when, the totals as printed, and each line with an
// amount: an earning with its hours and rate, a tax with the wages it was
// figured on, a deduction with the employer's part) and the PDF or picture
// it was typed from, which the paystub is kept against, marked as a tax
// document so its text is never kept. The server refuses a paystub whose
// lines do not add up unless the person says to record it anyway.

type LineDraft = {
  key: number
  paystubLineKind: PaystubLineKind
  description: string
  hours: string
  rateAmount: string
  currentAmount: string
  yearToDateAmount: string
  paystubTaxKind: string
  taxJurisdictionCode: string
  currentWagesAmount: string
  yearToDateWagesAmount: string
  paystubDeductionKind: string
  employerCurrentAmount: string
  employerYearToDateAmount: string
}

const LINE_KIND_LABELS: Record<PaystubLineKind, Key> = {
  earning: 'finance.entryPaystubKindEarning',
  tax: 'finance.entryPaystubKindTax',
  deduction: 'finance.entryPaystubKindDeduction',
  reimbursement: 'finance.entryPaystubKindReimbursement',
}

const TAX_KIND_LABELS: Record<string, Key> = {
  federal_income: 'finance.entryPaystubTaxFederalIncome',
  social_security: 'finance.entryPaystubTaxSocialSecurity',
  medicare: 'finance.entryPaystubTaxMedicare',
  medicare_additional: 'finance.entryPaystubTaxMedicareAdditional',
  state_income: 'finance.entryPaystubTaxStateIncome',
  local_income: 'finance.entryPaystubTaxLocalIncome',
  state_disability: 'finance.entryPaystubTaxStateDisability',
  other: 'finance.entryPaystubOther',
}

const DEDUCTION_KIND_LABELS: Record<string, Key> = {
  retirement_traditional: 'finance.entryPaystubDeductionRetirementTraditional',
  retirement_roth: 'finance.entryPaystubDeductionRetirementRoth',
  hsa: 'finance.entryPaystubDeductionHSA',
  health_premium: 'finance.entryPaystubDeductionHealthPremium',
  fsa_health: 'finance.entryPaystubDeductionFSAHealth',
  fsa_dependent_care: 'finance.entryPaystubDeductionFSADependentCare',
  commuter: 'finance.entryPaystubDeductionCommuter',
  insurance: 'finance.entryPaystubDeductionInsurance',
  other: 'finance.entryPaystubOther',
}

function emptyLine(key: number): LineDraft {
  return {
    key,
    paystubLineKind: 'earning',
    description: '',
    hours: '',
    rateAmount: '',
    currentAmount: '',
    yearToDateAmount: '',
    paystubTaxKind: '',
    taxJurisdictionCode: '',
    currentWagesAmount: '',
    yearToDateWagesAmount: '',
    paystubDeductionKind: '',
    employerCurrentAmount: '',
    employerYearToDateAmount: '',
  }
}

// paystubLineOf is the line as the server takes it: only the fields its
// kind has, so a field typed and then left behind by changing the kind is
// not sent.
function paystubLineOf(line: LineDraft): PaystubLineInput {
  const common = {
    paystubLineKind: line.paystubLineKind,
    description: blankToUndefined(line.description),
    currentAmount: blankToUndefined(line.currentAmount),
    yearToDateAmount: blankToUndefined(line.yearToDateAmount),
  }
  if (line.paystubLineKind === 'earning') {
    return { ...common, hours: blankToUndefined(line.hours), rateAmount: blankToUndefined(line.rateAmount) }
  }
  if (line.paystubLineKind === 'tax') {
    return {
      ...common,
      paystubTaxKind: blankToUndefined(line.paystubTaxKind),
      taxJurisdictionCode: blankToUndefined(line.taxJurisdictionCode),
      currentWagesAmount: blankToUndefined(line.currentWagesAmount),
      yearToDateWagesAmount: blankToUndefined(line.yearToDateWagesAmount),
    }
  }
  if (line.paystubLineKind === 'deduction') {
    return {
      ...common,
      paystubDeductionKind: blankToUndefined(line.paystubDeductionKind),
      employerCurrentAmount: blankToUndefined(line.employerCurrentAmount),
      employerYearToDateAmount: blankToUndefined(line.employerYearToDateAmount),
    }
  }
  return common
}

export function PaystubEntryDialog({ onRecorded, onClose }: { onRecorded: () => void; onClose: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const { reportingCurrencyCode } = useReportingCurrency()
  const [employerName, setEmployerName] = useState('')
  const [employerTaxId, setEmployerTaxId] = useState('')
  const [employeeName, setEmployeeName] = useState('')
  const [employeeTaxIdMask, setEmployeeTaxIdMask] = useState('')
  const [payDate, setPayDate] = useState('')
  const [periodStartOn, setPeriodStartOn] = useState('')
  const [periodEndOn, setPeriodEndOn] = useState('')
  const [currencyCode, setCurrencyCode] = useState('')
  const [grossAmount, setGrossAmount] = useState('')
  const [yearToDateGrossAmount, setYearToDateGrossAmount] = useState('')
  const [totalTaxAmount, setTotalTaxAmount] = useState('')
  const [totalDeductionAmount, setTotalDeductionAmount] = useState('')
  const [netAmount, setNetAmount] = useState('')
  const [lines, setLines] = useState<LineDraft[]>([emptyLine(1)])
  const [nextLineKey, setNextLineKey] = useState(2)
  const [sourceFile, setSourceFile] = useState<SourceFile | null>(null)
  const [isUnbalancedAccepted, setIsUnbalancedAccepted] = useState(false)
  const [unbalancedRefusedSignature, setUnbalancedRefusedSignature] = useState<string | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  useEffect(() => {
    if (reportingCurrencyCode) setCurrencyCode((previous) => previous || reportingCurrencyCode)
  }, [reportingCurrencyCode])

  const isValid =
    sourceFile !== null &&
    employerName.trim() !== '' &&
    employeeName.trim() !== '' &&
    isDay(payDate) &&
    (periodStartOn.trim() === '' || isDay(periodStartOn)) &&
    (periodEndOn.trim() === '' || isDay(periodEndOn)) &&
    (currencyCode.trim() === '' || /^[A-Za-z]{3}$/.test(currencyCode.trim())) &&
    (employeeTaxIdMask.trim() === '' || /^\d{4}$/.test(employeeTaxIdMask.trim())) &&
    isDecimal(grossAmount) &&
    isDecimal(netAmount) &&
    isOptionalDecimal(yearToDateGrossAmount) &&
    isOptionalDecimal(totalTaxAmount) &&
    isOptionalDecimal(totalDeductionAmount) &&
    lines.length > 0 &&
    lines.every((line) =>
      [
        line.hours,
        line.rateAmount,
        line.currentAmount,
        line.yearToDateAmount,
        line.currentWagesAmount,
        line.yearToDateWagesAmount,
        line.employerCurrentAmount,
        line.employerYearToDateAmount,
      ].every(isOptionalDecimal),
    )

  const changeLine = (key: number, change: Partial<LineDraft>) =>
    setLines((previous) => previous.map((line) => (line.key === key ? { ...line, ...change } : line)))
  const addLine = () => {
    setLines((previous) => [...previous, emptyLine(nextLineKey)])
    setNextLineKey(nextLineKey + 1)
  }
  const removeLine = (key: number) => setLines((previous) => previous.filter((line) => line.key !== key))

  const variablesWithoutAcceptance = {
    agentAttachmentId: sourceFile?.agentAttachmentId,
    employerName: employerName.trim(),
    employerTaxId: blankToUndefined(employerTaxId),
    employeeName: employeeName.trim(),
    employeeTaxIdMask: blankToUndefined(employeeTaxIdMask),
    payDate: payDate.trim(),
    periodStartOn: blankToUndefined(periodStartOn),
    periodEndOn: blankToUndefined(periodEndOn),
    currencyCode: blankToUndefined(currencyCode)?.toUpperCase(),
    grossAmount: grossAmount.trim(),
    yearToDateGrossAmount: blankToUndefined(yearToDateGrossAmount),
    totalTaxAmount: blankToUndefined(totalTaxAmount),
    totalDeductionAmount: blankToUndefined(totalDeductionAmount),
    netAmount: netAmount.trim(),
    paystubLines: lines.map(paystubLineOf),
  }
  const signature = JSON.stringify(variablesWithoutAcceptance)
  // The checkbox is offered only for the form the server refused as
  // unbalanced; changing anything puts it away again.
  const isUnbalancedRefusedShown = unbalancedRefusedSignature === signature

  useEffect(() => {
    setIsUnbalancedAccepted(false)
  }, [signature])

  const submit = async () => {
    setIsBusy(true)
    try {
      await graphql(RECORD_PAYSTUB, {
        ...variablesWithoutAcceptance,
        isUnbalancedAccepted: isUnbalancedRefusedShown && isUnbalancedAccepted,
      })
      toast.done(t('finance.entryPaystubRecorded', { employer: employerName.trim(), day: formatDay(payDate.trim()) }))
      onRecorded()
      onClose()
    } catch (caught) {
      if (caught instanceof Error && caught.message.includes(PAYSTUB_UNBALANCED_REFUSAL)) {
        setUnbalancedRefusedSignature(signature)
      }
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const amountField = (label: Key, value: string, onChange: (typed: string) => void) => (
    <label>
      <span>{t(label)}</span>
      <input inputMode="decimal" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
  const textField = (label: Key, value: string, onChange: (typed: string) => void, maxLength?: number) => (
    <label>
      <span>{t(label)}</span>
      <input value={value} maxLength={maxLength} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
  const dayField = (label: Key, value: string, onChange: (typed: string) => void) => (
    <label>
      <span>{t(label)}</span>
      <input type="date" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  )

  return (
    <FormDialog
      wide
      title={t('finance.entryPaystubTitle')}
      submitLabel={t('finance.entryRecordPaystub')}
      busy={isBusy}
      canSubmit={isValid && !isBusy}
      onSubmit={() => void submit()}
      onClose={() => {
        if (!isBusy) onClose()
      }}
    >
      <p className="muted">{t('finance.entryPaystubHint')}</p>
      <div className="finance-entry-fields">
        {textField('finance.entryEmployerName', employerName, setEmployerName)}
        {textField('finance.entryEmployerTaxId', employerTaxId, setEmployerTaxId)}
        {textField('finance.entryEmployeeName', employeeName, setEmployeeName)}
        {textField('finance.entryEmployeeTaxIdMask', employeeTaxIdMask, setEmployeeTaxIdMask, 4)}
        {dayField('finance.entryPayDate', payDate, setPayDate)}
        {dayField('finance.entryPeriodStartOn', periodStartOn, setPeriodStartOn)}
        {dayField('finance.entryPeriodEndOn', periodEndOn, setPeriodEndOn)}
        <div>
          <span className="finance-entry-label">{t('finance.currency')}</span>
          <CurrencyPicker value={currencyCode} label={t('finance.currency')} onChange={setCurrencyCode} />
          <ExchangeRateHint currencyCode={currencyCode} />
        </div>
        {amountField('finance.entryGrossAmount', grossAmount, setGrossAmount)}
        {amountField('finance.entryYearToDateGrossAmount', yearToDateGrossAmount, setYearToDateGrossAmount)}
        {amountField('finance.entryTotalTaxAmount', totalTaxAmount, setTotalTaxAmount)}
        {amountField('finance.entryTotalDeductionAmount', totalDeductionAmount, setTotalDeductionAmount)}
        {amountField('finance.entryNetAmount', netAmount, setNetAmount)}
      </div>
      <fieldset className="finance-entry-lines">
        <legend>{t('finance.entryPaystubLines')}</legend>
        <p className="muted field-hint">{t('finance.entryPaystubLinesHint')}</p>
        {lines.map((line, index) => (
          <div
            key={line.key}
            className="finance-entry-line"
            role="group"
            aria-label={t('finance.entryLineNumber', { number: index + 1 })}
          >
            <strong className="finance-entry-line-number muted">
              {t('finance.entryLineNumber', { number: index + 1 })}
            </strong>
            <label>
              <span>{t('finance.entryLineKind')}</span>
              <select
                value={line.paystubLineKind}
                onChange={(event) => changeLine(line.key, { paystubLineKind: event.target.value as PaystubLineKind })}
              >
                {PAYSTUB_LINE_KINDS.map((kind) => (
                  <option key={kind} value={kind}>
                    {t(LINE_KIND_LABELS[kind])}
                  </option>
                ))}
              </select>
            </label>
            <label className="finance-entry-wide">
              <span>{t('finance.entryLineDescription')}</span>
              <input
                value={line.description}
                onChange={(event) => changeLine(line.key, { description: event.target.value })}
              />
            </label>
            {line.paystubLineKind === 'earning' ? (
              <>
                {amountField('finance.entryPaystubHours', line.hours, (typed) =>
                  changeLine(line.key, { hours: typed }),
                )}
                {amountField('finance.entryPaystubRateAmount', line.rateAmount, (typed) =>
                  changeLine(line.key, { rateAmount: typed }),
                )}
              </>
            ) : null}
            {amountField('finance.entryPaystubCurrentAmount', line.currentAmount, (typed) =>
              changeLine(line.key, { currentAmount: typed }),
            )}
            {amountField('finance.entryPaystubYearToDateAmount', line.yearToDateAmount, (typed) =>
              changeLine(line.key, { yearToDateAmount: typed }),
            )}
            {line.paystubLineKind === 'tax' ? (
              <>
                <label>
                  <span>{t('finance.entryPaystubTaxKind')}</span>
                  <select
                    value={line.paystubTaxKind}
                    onChange={(event) => changeLine(line.key, { paystubTaxKind: event.target.value })}
                  >
                    <option value="" />
                    {PAYSTUB_TAX_KINDS.map((kind) => (
                      <option key={kind} value={kind}>
                        {t(TAX_KIND_LABELS[kind])}
                      </option>
                    ))}
                  </select>
                </label>
                {textField(
                  'finance.entryPaystubTaxJurisdictionCode',
                  line.taxJurisdictionCode,
                  (typed) => changeLine(line.key, { taxJurisdictionCode: typed }),
                  8,
                )}
                {amountField('finance.entryPaystubCurrentWagesAmount', line.currentWagesAmount, (typed) =>
                  changeLine(line.key, { currentWagesAmount: typed }),
                )}
                {amountField('finance.entryPaystubYearToDateWagesAmount', line.yearToDateWagesAmount, (typed) =>
                  changeLine(line.key, { yearToDateWagesAmount: typed }),
                )}
              </>
            ) : null}
            {line.paystubLineKind === 'deduction' ? (
              <>
                <label>
                  <span>{t('finance.entryPaystubDeductionKind')}</span>
                  <select
                    value={line.paystubDeductionKind}
                    onChange={(event) => changeLine(line.key, { paystubDeductionKind: event.target.value })}
                  >
                    <option value="" />
                    {PAYSTUB_DEDUCTION_KINDS.map((kind) => (
                      <option key={kind} value={kind}>
                        {t(DEDUCTION_KIND_LABELS[kind])}
                      </option>
                    ))}
                  </select>
                </label>
                {amountField('finance.entryPaystubEmployerCurrentAmount', line.employerCurrentAmount, (typed) =>
                  changeLine(line.key, { employerCurrentAmount: typed }),
                )}
                {amountField('finance.entryPaystubEmployerYearToDateAmount', line.employerYearToDateAmount, (typed) =>
                  changeLine(line.key, { employerYearToDateAmount: typed }),
                )}
              </>
            ) : null}
            <Tooltip label={t('common.remove')}>
              <button
                type="button"
                className="icon-action danger finance-entry-line-remove"
                disabled={lines.length === 1}
                aria-label={`${t('finance.entryLineNumber', { number: index + 1 })}: ${t('common.remove')}`}
                onClick={() => removeLine(line.key)}
              >
                <CloseIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ))}
        <button type="button" onClick={addLine}>
          {t('finance.entryAddLine')}
        </button>
      </fieldset>
      <SourceFileField
        label="finance.entryPaystubFile"
        hint="finance.entryTaxDocumentFileHint"
        accept="application/pdf,image/*"
        uploadPath={TAX_DOCUMENT_UPLOAD_PATH}
        notStoredKey="finance.entryFileNotStored"
        isDisabled={isBusy}
        onChange={setSourceFile}
      />
      {isUnbalancedRefusedShown ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={isUnbalancedAccepted}
            onChange={(event) => setIsUnbalancedAccepted(event.target.checked)}
          />
          {t('finance.entryUnbalancedAccepted')}
        </label>
      ) : null}
    </FormDialog>
  )
}
