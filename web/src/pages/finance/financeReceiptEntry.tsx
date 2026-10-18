import { useEffect, useState } from 'react'

import { graphql } from '../../api'
import { formatMoney } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'
import { AGENT_ATTACHMENTS_PATH, amountOf, formatDay, isDecimal } from './financeApi'
import { CurrencyPicker, useReportingCurrency } from './financeCommon'
import {
  PREVIEW_RECORD_RECEIPT,
  RECEIPT_LINE_KINDS,
  RECORD_RECEIPT,
  ReceiptEntryPreview,
  ReceiptLineInput,
  ReceiptLineKind,
  RecordedReceipt,
} from './financeEntryApi'
import {
  ExchangeRateHint,
  SourceFile,
  SourceFileField,
  blankToUndefined,
  isDay,
  isOptionalDecimal,
} from './financeEntryCommon'
import { useCandidateAgreement } from './financeReceipts'

// The dialog that records a receipt by hand: the Add receipt button on the
// Receipts page opens it. It asks for the receipt as RecordReceipt takes it
// (the merchant, the day, the currency, the lines as printed with their
// taxes, fees and tips among them, the printed subtotal and total, the last
// digits of the card paid with) and the photo or PDF it was typed from,
// which the receipt is kept against. Preview asks the server what recording
// would do, writing nothing: whether the lines add up, which charge it
// would be matched to, whether it takes the place of one recorded from the
// same file. Changing anything after a preview asks for a new one.

type LineDraft = {
  key: number
  receiptLineKind: ReceiptLineKind
  description: string
  quantity: string
  quantityUnit: string
  unitPriceAmount: string
  lineAmount: string
  taxClassCode: string
  discountedLineNumber: string
}

const LINE_KIND_LABELS: Record<ReceiptLineKind, Key> = {
  item: 'finance.entryLineKindItem',
  discount: 'finance.entryLineKindDiscount',
  tax: 'finance.entryLineKindTax',
  fee: 'finance.entryLineKindFee',
  tip: 'finance.entryLineKindTip',
}

function emptyLine(key: number): LineDraft {
  return {
    key,
    receiptLineKind: 'item',
    description: '',
    quantity: '',
    quantityUnit: '',
    unitPriceAmount: '',
    lineAmount: '',
    taxClassCode: '',
    discountedLineNumber: '',
  }
}

// purchasedAtOf is the day and the time of day typed as RFC 3339 in the
// browser's zone, which is the zone the receipt's time was read in; nothing
// when either is missing.
export function purchasedAtOf(purchasedOn: string, purchasedTime: string): string | undefined {
  if (!isDay(purchasedOn) || !/^\d{2}:\d{2}$/.test(purchasedTime.trim())) return undefined
  const local = new Date(`${purchasedOn.trim()}T${purchasedTime.trim()}:00`)
  if (Number.isNaN(local.getTime())) return undefined
  const offsetMinutes = -local.getTimezoneOffset()
  const sign = offsetMinutes < 0 ? '-' : '+'
  const hours = String(Math.floor(Math.abs(offsetMinutes) / 60)).padStart(2, '0')
  const minutes = String(Math.abs(offsetMinutes) % 60).padStart(2, '0')
  return `${purchasedOn.trim()}T${purchasedTime.trim()}:00${sign}${hours}:${minutes}`
}

function receiptLineOf(line: LineDraft, index: number): ReceiptLineInput {
  return {
    lineNumber: index + 1,
    receiptLineKind: line.receiptLineKind,
    description: blankToUndefined(line.description),
    quantity: blankToUndefined(line.quantity),
    quantityUnit: blankToUndefined(line.quantityUnit),
    unitPriceAmount: blankToUndefined(line.unitPriceAmount),
    lineAmount: line.lineAmount.trim(),
    taxClassCode: blankToUndefined(line.taxClassCode),
    discountedLineNumber:
      line.receiptLineKind === 'discount' && /^\d+$/.test(line.discountedLineNumber.trim())
        ? Number(line.discountedLineNumber.trim())
        : undefined,
  }
}

export function ReceiptEntryDialog({
  financeTransactionId,
  onRecorded,
  onClose,
}: {
  // The charge the receipt was handed in for, matched by hand; without it
  // the server matches the one charge it is sure of.
  financeTransactionId?: string
  onRecorded: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const agreement = useCandidateAgreement()
  const { reportingCurrencyCode } = useReportingCurrency()
  const [merchantName, setMerchantName] = useState('')
  const [merchantReceiptNumber, setMerchantReceiptNumber] = useState('')
  const [purchasedOn, setPurchasedOn] = useState('')
  const [purchasedTime, setPurchasedTime] = useState('')
  const [currencyCode, setCurrencyCode] = useState('')
  const [subtotalAmount, setSubtotalAmount] = useState('')
  const [totalAmount, setTotalAmount] = useState('')
  const [paymentAccountMask, setPaymentAccountMask] = useState('')
  const [lines, setLines] = useState<LineDraft[]>([emptyLine(1)])
  const [nextLineKey, setNextLineKey] = useState(2)
  const [sourceFile, setSourceFile] = useState<SourceFile | null>(null)
  const [isUnbalancedAccepted, setIsUnbalancedAccepted] = useState(false)
  const [preview, setPreview] = useState<{ answer: ReceiptEntryPreview; signature: string } | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  // The person's own currency until another is chosen.
  useEffect(() => {
    if (reportingCurrencyCode) setCurrencyCode((previous) => previous || reportingCurrencyCode)
  }, [reportingCurrencyCode])

  const variablesWithoutAcceptance = {
    agentAttachmentId: sourceFile?.agentAttachmentId,
    merchantName: merchantName.trim(),
    merchantReceiptNumber: blankToUndefined(merchantReceiptNumber),
    purchasedOn: blankToUndefined(purchasedOn),
    purchasedAt: purchasedAtOf(purchasedOn, purchasedTime),
    currencyCode: currencyCode.trim().toUpperCase(),
    subtotalAmount: blankToUndefined(subtotalAmount),
    totalAmount: totalAmount.trim(),
    paymentAccountMask: blankToUndefined(paymentAccountMask),
    receiptLines: lines.map(receiptLineOf),
    financeTransactionId,
  }
  const signature = JSON.stringify(variablesWithoutAcceptance)
  const shownPreview = preview && preview.signature === signature ? preview.answer : null
  const variables = {
    ...variablesWithoutAcceptance,
    isUnbalancedAccepted: shownPreview?.receiptCheckState === 'unbalanced' ? isUnbalancedAccepted : undefined,
  }

  const isValid =
    sourceFile !== null &&
    merchantName.trim() !== '' &&
    /^[A-Za-z]{3}$/.test(currencyCode.trim()) &&
    isDecimal(totalAmount) &&
    isOptionalDecimal(subtotalAmount) &&
    (purchasedOn.trim() === '' || isDay(purchasedOn)) &&
    lines.length > 0 &&
    lines.every(
      (line) =>
        isDecimal(line.lineAmount) &&
        isOptionalDecimal(line.quantity) &&
        isOptionalDecimal(line.unitPriceAmount) &&
        (line.receiptLineKind !== 'discount' ||
          line.discountedLineNumber.trim() === '' ||
          /^\d+$/.test(line.discountedLineNumber.trim())),
    )

  const changeLine = (key: number, change: Partial<LineDraft>) =>
    setLines((previous) => previous.map((line) => (line.key === key ? { ...line, ...change } : line)))
  const addLine = () => {
    setLines((previous) => [...previous, emptyLine(nextLineKey)])
    setNextLineKey(nextLineKey + 1)
  }
  const removeLine = (key: number) => setLines((previous) => previous.filter((line) => line.key !== key))

  const submit = async () => {
    setIsBusy(true)
    try {
      if (!shownPreview) {
        const answer = await graphql<{ PreviewRecordReceipt: ReceiptEntryPreview }>(PREVIEW_RECORD_RECEIPT, variables)
        setPreview({ answer: answer.PreviewRecordReceipt, signature })
        setIsUnbalancedAccepted(false)
        return
      }
      const answer = await graphql<{ RecordReceipt: RecordedReceipt }>(RECORD_RECEIPT, variables)
      const recorded = answer.RecordReceipt
      toast.done(
        [
          t('finance.entryReceiptRecorded', { merchant: recorded.financeReceipt.merchantName }),
          recorded.receiptCheckSummary,
          recorded.isReplaced ? t('finance.entryReceiptReplaced') : '',
          ...recorded.droppedReceiptMatchReasons,
        ]
          .filter(Boolean)
          .join(' '),
      )
      onRecorded()
      onClose()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const candidates = shownPreview?.receiptMatchCandidates ?? []

  return (
    <FormDialog
      wide
      title={t('finance.entryReceiptTitle')}
      submitLabel={shownPreview ? t('finance.entryRecordReceipt') : t('finance.entryPreview')}
      busy={isBusy}
      canSubmit={isValid && !isBusy && (shownPreview?.receiptCheckState !== 'unbalanced' || isUnbalancedAccepted)}
      onSubmit={() => void submit()}
      onClose={() => {
        if (!isBusy) onClose()
      }}
    >
      <p className="muted">{t('finance.entryReceiptHint')}</p>
      <div className="finance-entry-fields">
        <label>
          <span>{t('finance.entryMerchantName')}</span>
          <input value={merchantName} onChange={(event) => setMerchantName(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.entryMerchantReceiptNumber')}</span>
          <input value={merchantReceiptNumber} onChange={(event) => setMerchantReceiptNumber(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.entryPurchasedOn')}</span>
          <input type="date" value={purchasedOn} onChange={(event) => setPurchasedOn(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.entryPurchasedTime')}</span>
          <input
            type="time"
            value={purchasedTime}
            disabled={!isDay(purchasedOn)}
            onChange={(event) => setPurchasedTime(event.target.value)}
          />
        </label>
        <div>
          <span className="finance-entry-label">{t('finance.currency')}</span>
          <CurrencyPicker value={currencyCode} label={t('finance.currency')} onChange={setCurrencyCode} />
          <ExchangeRateHint currencyCode={currencyCode} />
        </div>
        <label>
          <span>{t('finance.entryPaymentAccountMask')}</span>
          <input
            value={paymentAccountMask}
            maxLength={8}
            inputMode="numeric"
            onChange={(event) => setPaymentAccountMask(event.target.value)}
          />
        </label>
      </div>
      <fieldset className="finance-entry-lines">
        <legend>{t('finance.entryReceiptLines')}</legend>
        <p className="muted field-hint">{t('finance.entryReceiptLinesHint')}</p>
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
                value={line.receiptLineKind}
                onChange={(event) => changeLine(line.key, { receiptLineKind: event.target.value as ReceiptLineKind })}
              >
                {RECEIPT_LINE_KINDS.map((kind) => (
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
            <label>
              <span>{t('finance.entryLineQuantity')}</span>
              <input
                inputMode="decimal"
                value={line.quantity}
                onChange={(event) => changeLine(line.key, { quantity: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.entryLineQuantityUnit')}</span>
              <input
                value={line.quantityUnit}
                onChange={(event) => changeLine(line.key, { quantityUnit: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.entryLineUnitPriceAmount')}</span>
              <input
                inputMode="decimal"
                value={line.unitPriceAmount}
                onChange={(event) => changeLine(line.key, { unitPriceAmount: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.entryLineAmount')}</span>
              <input
                inputMode="decimal"
                value={line.lineAmount}
                onChange={(event) => changeLine(line.key, { lineAmount: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.entryLineTaxClassCode')}</span>
              <input
                value={line.taxClassCode}
                maxLength={4}
                onChange={(event) => changeLine(line.key, { taxClassCode: event.target.value })}
              />
            </label>
            {line.receiptLineKind === 'discount' ? (
              <label>
                <span>{t('finance.entryLineDiscountedLineNumber')}</span>
                <input
                  inputMode="numeric"
                  value={line.discountedLineNumber}
                  onChange={(event) => changeLine(line.key, { discountedLineNumber: event.target.value })}
                />
              </label>
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
      <div className="finance-entry-fields">
        <label>
          <span>{t('finance.entrySubtotalAmount')}</span>
          <input
            inputMode="decimal"
            value={subtotalAmount}
            onChange={(event) => setSubtotalAmount(event.target.value)}
          />
        </label>
        <label>
          <span>{t('finance.entryTotalAmount')}</span>
          <input inputMode="decimal" value={totalAmount} onChange={(event) => setTotalAmount(event.target.value)} />
        </label>
      </div>
      <SourceFileField
        label="finance.entryReceiptFile"
        hint="finance.entryReceiptFileHint"
        accept="image/*"
        uploadPath={AGENT_ATTACHMENTS_PATH}
        notStoredKey="finance.receiptNotStored"
        isDisabled={isBusy}
        onChange={setSourceFile}
      />
      {shownPreview ? (
        <section className="finance-entry-preview" aria-label={t('finance.entryPreviewTitle')}>
          <strong>{t('finance.entryPreviewTitle')}</strong>
          <p>{shownPreview.receiptCheckSummary}</p>
          {shownPreview.isReplacing ? <p className="muted">{t('finance.entryReceiptReplacing')}</p> : null}
          {candidates.length === 0 ? (
            <p className="muted">{t('finance.entryNoCandidates')}</p>
          ) : (
            <>
              <p className="muted">{t('finance.entryCandidates')}</p>
              <ul className="finance-entry-candidates">
                {candidates.map((candidate) => (
                  <li key={candidate.financeTransactionId}>
                    <span>
                      {candidate.financeTransaction
                        ? `${formatDay(candidate.financeTransaction.postedOn)} · ${
                            candidate.financeTransaction.merchantName || candidate.financeTransaction.description
                          } · ${formatMoney(amountOf(candidate.financeTransaction.amount), candidate.financeTransaction.currencyCode)}`
                        : candidate.financeTransactionId}
                    </span>
                    <span className="muted">
                      {[
                        t('finance.candidateExplains', {
                          amount: formatMoney(
                            amountOf(candidate.matchedAmount),
                            candidate.financeTransaction?.currencyCode ?? currencyCode,
                          ),
                        }),
                        agreement(candidate),
                        candidate.isAutomatic ? t('finance.entryCandidateAutomatic') : '',
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </li>
                ))}
              </ul>
            </>
          )}
          {shownPreview.receiptCheckState === 'unbalanced' ? (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={isUnbalancedAccepted}
                onChange={(event) => setIsUnbalancedAccepted(event.target.checked)}
              />
              {t('finance.entryUnbalancedAccepted')}
            </label>
          ) : null}
        </section>
      ) : null}
    </FormDialog>
  )
}
