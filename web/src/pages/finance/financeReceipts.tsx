import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { graphql } from '../../api'
import { Loading, Tag, formatMoney } from '../../components/common'
import { ZoomablePicture } from '../../components/lightbox'
import { ConfirmDialog } from '../../components/dialog'
import { LinkIcon, MailIcon, PictureIcon, TrashIcon, UnlinkIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { uploadFiles } from '../../upload'
import {
  AGENT_ATTACHMENTS_PATH,
  ANNOTATE_TRANSACTION,
  DELETE_RECEIPT,
  FINANCE_RECEIPTS,
  FinanceReceipt,
  FinanceReceiptLine,
  FinanceReceiptPage,
  FinanceTransaction,
  MATCH_RECEIPT,
  MAXIMUM_LISTED_RECEIPT_COUNT,
  PROPOSE_RECEIPT_MATCHES,
  READ_RECEIPT,
  ReceiptMatchCandidate,
  UNMATCH_RECEIPT,
  amountOf,
  formatDay,
  hasAmount,
} from './financeApi'

// Receipts and the annotation in a finance transaction's details. The
// dialog keeps the state (useTransactionReceipts) and draws the section
// (FinanceReceiptsSection); choosing a receipt to match and confirming a
// delete are dialogs of their own that the details dialog shows in its
// place, since two dialogs open at once would both close on one Escape.

// The largest photo uploaded as a receipt: what a model takes as a picture,
// the same limit teanode finance read-receipt holds to.
export const MAXIMUM_RECEIPT_BYTES = 10 << 20

// How long the receipts are read again for after an upload, waiting for
// the receipt job to read it: every few seconds, for about two minutes.
// Kept in an object so a test can shorten it.
export const receiptPolling = { intervalMS: 4000, attemptCount: 30 }

// receiptQuantityText is a line's quantity as the receipt prints it,
// "2.53 lb @ 1.59", "3 @ 0.99" or "2", and nothing for a line that prints
// none or only a count of one.
export function receiptQuantityText(line: FinanceReceiptLine, locale?: string): string {
  const quantity = hasAmount(line.quantity)
    ? new Intl.NumberFormat(locale, { maximumFractionDigits: 3 }).format(Number(line.quantity))
    : ''
  const unit = (line.quantityUnit ?? '').trim()
  const unitPrice = hasAmount(line.unitPriceAmount)
    ? new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 4 }).format(
        Number(line.unitPriceAmount),
      )
    : ''
  const counted = [quantity, unit].filter(Boolean).join(' ')
  if (unitPrice) return `${counted || '1'} @ ${unitPrice}`
  if (!unit && hasAmount(line.quantity) && Number(line.quantity) === 1) return ''
  return counted
}

// In ten-thousandths, the places the server keeps, so sums are exact.
function minorUnitsOf(decimal?: string | null): number {
  return Math.round(Math.abs(amountOf(decimal)) * 10000)
}

// matchedAmountFor is how much of the finance transaction a receipt
// explains, empty when it is not matched to it.
export function matchedAmountFor(receipt: FinanceReceipt, financeTransactionId: string): string {
  return (
    receipt.receiptMatches.find((match) => match.financeTransactionId === financeTransactionId)?.matchedAmount ?? ''
  )
}

// unexplainedAmount is what of the charge its matched receipts do not
// explain, zero when they cover it.
export function unexplainedAmount(financeTransaction: FinanceTransaction, receipts: FinanceReceipt[]): number {
  const explained = receipts.reduce(
    (sum, receipt) => sum + minorUnitsOf(matchedAmountFor(receipt, financeTransaction.id)),
    0,
  )
  return Math.max(0, minorUnitsOf(financeTransaction.amount) - explained) / 10000
}

// The days a receipt explaining a charge was bought on: the matcher takes
// charges posted from three days before a purchase to seven days after, so
// from a week before the charge posted to three days after.
function purchaseWindow(postedOn: string): { from: string; to: string } {
  const posted = new Date(`${postedOn.slice(0, 10)}T00:00:00Z`)
  const shifted = (days: number) => new Date(posted.getTime() + days * 86_400_000).toISOString().slice(0, 10)
  return { from: shifted(-7), to: shifted(3) }
}

// receiptName is the merchant a receipt names, or the fallback for one
// that names none.
export function receiptName(receipt: FinanceReceipt, fallback: string): string {
  return receipt.merchantName.trim() || fallback
}

// readTransactionReceipts reads the receipts matched to one finance
// transaction, and how many there are. A charge explained by more receipts
// than one page holds is not one anybody has, so the first page is all of
// them in practice, and the count is the server's either way.
async function readTransactionReceipts(financeTransactionId: string): Promise<FinanceReceiptPage> {
  const answer = await graphql<{ FinanceReceipts?: FinanceReceiptPage | null }>(FINANCE_RECEIPTS, {
    financeTransactionId,
    limit: MAXIMUM_LISTED_RECEIPT_COUNT,
  })
  return answer?.FinanceReceipts ?? { financeReceipts: [], totalCount: 0 }
}

// findUnmatchedReceipt looks for the receipt read from an upload among the
// receipts matched to no charge: where the receipt job leaves it when the
// charge it was uploaded to refuses it (other receipts already explain the
// charge, another currency, money in). The server lists receipts newest
// purchase first and cannot be asked for one by its upload, so this is the
// first page of the unmatched ones, which is where a receipt just read and
// left unmatched is in practice.
async function findUnmatchedReceipt(agentAttachmentId: string): Promise<FinanceReceipt | undefined> {
  const answer = await graphql<{ FinanceReceipts?: FinanceReceiptPage | null }>(FINANCE_RECEIPTS, {
    isUnmatched: true,
    limit: MAXIMUM_LISTED_RECEIPT_COUNT,
  })
  return answer?.FinanceReceipts?.financeReceipts.find((receipt) => receipt.agentAttachmentId === agentAttachmentId)
}

// useTransactionReceipts reads the receipts matched to one finance
// transaction and does what can be done to them, each with a toast. After
// an upload it reads them again every few seconds until the receipt job
// has read the photo, or gives up after a couple of minutes and says so.
// A photo read but refused by the charge is left unmatched, and that is
// said too, rather than waiting for it to show on the charge.
// onTransactionChanged hands the list what changed on the transaction:
// its annotation, or how many receipts it has.
export function useTransactionReceipts(
  financeTransaction: FinanceTransaction,
  onTransactionChanged?: (changes: Partial<FinanceTransaction>) => void,
) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)
  const [readingAttachmentId, setReadingAttachmentId] = useState('')
  const changedRef = useRef(onTransactionChanged)
  changedRef.current = onTransactionChanged
  const financeTransactionId = financeTransaction.id
  const query = useQuery(() => readTransactionReceipts(financeTransactionId), [financeTransactionId], {
    refresh: false,
  })
  const receipts = query.data?.financeReceipts ?? []
  const fallbackName = t('finance.receiptWithoutMerchant')

  // A failed read is said once, in a toast; the section then says nothing
  // about what is matched, since it does not know.
  useEffect(() => {
    if (query.error) toast.failure(query.error, t('finance.failed'))
    // Once per failure, not on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.error])

  // Read again after a change, and the count handed to the list.
  const reload = async () => {
    const read = await readTransactionReceipts(financeTransactionId)
    changedRef.current?.({ receiptCount: read.totalCount })
    await query.reload(true)
    return read.financeReceipts
  }

  useEffect(() => {
    if (!readingAttachmentId) return
    let isStopped = false
    let attempt = 0
    let timer = 0
    const poll = async () => {
      attempt += 1
      try {
        const read = await readTransactionReceipts(financeTransactionId)
        if (isStopped) return
        const found = read.financeReceipts.find((receipt) => receipt.agentAttachmentId === readingAttachmentId)
        if (found) {
          setReadingAttachmentId('')
          changedRef.current?.({ receiptCount: read.totalCount })
          await query.reload(true)
          toast.done(t('finance.receiptRead', { merchant: receiptName(found, fallbackName) }))
          return
        }
        const unmatched = await findUnmatchedReceipt(readingAttachmentId)
        if (isStopped) return
        if (unmatched) {
          setReadingAttachmentId('')
          toast.failed(t('finance.receiptReadNotMatched', { merchant: receiptName(unmatched, fallbackName) }))
          return
        }
      } catch {
        // One failed read is not the end of waiting; the next one may work.
        if (isStopped) return
      }
      if (attempt >= receiptPolling.attemptCount) {
        setReadingAttachmentId('')
        toast.done(t('finance.receiptStillReading'))
        return
      }
      timer = window.setTimeout(() => void poll(), receiptPolling.intervalMS)
    }
    timer = window.setTimeout(() => void poll(), receiptPolling.intervalMS)
    return () => {
      isStopped = true
      window.clearTimeout(timer)
    }
    // Started once per upload; the rest is read when it fires.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readingAttachmentId, financeTransactionId])

  const upload = async (file: File) => {
    if (file.size > MAXIMUM_RECEIPT_BYTES) {
      toast.failed(t('finance.receiptTooLarge', { name: file.name, size: String(MAXIMUM_RECEIPT_BYTES >> 20) }))
      return
    }
    setIsBusy(true)
    try {
      const uploaded = (await uploadFiles('POST', AGENT_ATTACHMENTS_PATH, [file], () => undefined).promise) as {
        attachments?: { id: string }[]
      }
      const agentAttachmentId = uploaded?.attachments?.[0]?.id
      if (!agentAttachmentId) throw new Error(t('finance.receiptNotStored'))
      await graphql(READ_RECEIPT, { agentAttachmentId, financeTransactionId })
      toast.done(t('finance.receiptReading'))
      setReadingAttachmentId(agentAttachmentId)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const annotate = async (annotation: string) => {
    setIsBusy(true)
    try {
      const answer = await graphql<{ AnnotateTransaction: FinanceTransaction }>(ANNOTATE_TRANSACTION, {
        financeTransactionId,
        annotation,
      })
      changedRef.current?.(answer.AnnotateTransaction)
      toast.done(annotation ? t('finance.annotationSaved') : t('finance.annotationCleared'))
      return true
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  const unmatch = async (receipt: FinanceReceipt) => {
    setIsBusy(true)
    try {
      await graphql(UNMATCH_RECEIPT, { receiptId: receipt.id, financeTransactionId })
      toast.done(t('finance.receiptUnmatched', { merchant: receiptName(receipt, fallbackName) }))
      await reload()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const remove = async (receipt: FinanceReceipt) => {
    setIsBusy(true)
    try {
      await graphql(DELETE_RECEIPT, { receiptId: receipt.id })
      toast.done(t('finance.receiptDeleted', { merchant: receiptName(receipt, fallbackName) }))
      await reload()
      return true
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  const match = async (receipt: FinanceReceipt, matchedAmount: string) => {
    setIsBusy(true)
    try {
      await graphql(MATCH_RECEIPT, {
        receiptId: receipt.id,
        financeTransactionId,
        matchedAmount: matchedAmount || undefined,
      })
      toast.done(t('finance.receiptMatched', { merchant: receiptName(receipt, fallbackName) }))
      await reload()
      return true
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  return {
    receipts,
    isLoading: query.loading && !query.data,
    hasError: Boolean(query.error),
    isBusy,
    isReading: readingAttachmentId !== '',
    upload,
    annotate,
    unmatch,
    remove,
    match,
  }
}

// FinanceAnnotationEditor is the person's words on a transaction, edited
// where they are shown: Save writes what was typed, Clear takes it away.
// Save and Clear sit right under the box, and a line under them says when
// the agent wrote it.
export function FinanceAnnotationEditor({
  financeTransaction,
  isBusy,
  onSave,
}: {
  financeTransaction: FinanceTransaction
  isBusy: boolean
  onSave: (annotation: string) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const saved = financeTransaction.annotation ?? ''
  const [draft, setDraft] = useState(saved)
  // What was saved, or written by the agent meanwhile, replaces the draft.
  useEffect(() => {
    setDraft(saved)
  }, [financeTransaction.id, saved])
  const isChanged = draft.trim() !== saved.trim()
  return (
    <>
      <textarea
        className="finance-annotation-field"
        rows={2}
        value={draft}
        aria-label={t('finance.annotation')}
        placeholder={t('finance.annotationPlaceholder')}
        onChange={(event) => setDraft(event.target.value)}
      />
      <div className="finance-receipt-buttons finance-annotation-buttons">
        <button type="button" disabled={isBusy || !isChanged} onClick={() => void onSave(draft.trim())}>
          {t('common.save')}
        </button>
        {saved ? (
          <button type="button" disabled={isBusy} onClick={() => void onSave('')}>
            {t('finance.clearAnnotation')}
          </button>
        ) : null}
      </div>
      {saved && financeTransaction.annotatedBy === 'agent' ? (
        <span className="muted finance-detail-note">{t('finance.annotatedByAgent')}</span>
      ) : null}
    </>
  )
}

const KIND_LABELS: Partial<Record<FinanceReceiptLine['receiptLineKind'], Key>> = {
  discount: 'finance.receiptLineKindDiscount',
  tax: 'finance.receiptLineKindTax',
  fee: 'finance.receiptLineKindFee',
  tip: 'finance.receiptLineKindTip',
}

// The lines printed after the subtotal: it is the items and discounts
// added up, with the fees unless the receipt prints them after it
// (isFeeAfterSubtotal, as the balance check found), and taxes and tips
// come on top of it.
function isAfterSubtotal(line: FinanceReceiptLine, isFeeAfterSubtotal: boolean): boolean {
  return (
    line.receiptLineKind === 'tax' ||
    line.receiptLineKind === 'tip' ||
    (isFeeAfterSubtotal && line.receiptLineKind === 'fee')
  )
}

// ReceiptLines is a receipt's lines as a compact table that stays a table
// on a phone: what each says, how much of what at what price, and the
// amount. A discount sits indented under its item with its amount marked;
// fees are labeled, among the items or after the subtotal as the receipt
// prints them, and the subtotal comes before the taxes and tips it does
// not include.
export function ReceiptLines({ receipt }: { receipt: FinanceReceipt }) {
  const { t, language } = useTranslation()
  const lines = [...receipt.receiptLines].sort((first, second) => first.lineNumber - second.lineNumber)
  const subtotalLines = lines.filter((line) => !isAfterSubtotal(line, receipt.isFeeAfterSubtotal))
  const afterSubtotalLines = lines.filter((line) => isAfterSubtotal(line, receipt.isFeeAfterSubtotal))
  const lineRow = (line: FinanceReceiptLine) => {
    const kindLabel = KIND_LABELS[line.receiptLineKind]
    return (
      <tr key={line.id} className={`finance-receipt-line finance-receipt-${line.receiptLineKind}`}>
        {/* Cut to one line; the whole of it on hover. */}
        <td title={[line.description, line.taxClassCode].filter(Boolean).join(' ')}>
          {kindLabel ? <span className="finance-receipt-kind">{t(kindLabel)}</span> : null}
          {line.description}
          {line.taxClassCode ? <span className="muted finance-receipt-tax-mark"> {line.taxClassCode}</span> : null}
        </td>
        <td className="muted">{receiptQuantityText(line, language)}</td>
        <td className="numeric">{formatMoney(amountOf(line.lineAmount), receipt.currencyCode)}</td>
      </tr>
    )
  }
  return (
    <div className="finance-receipt-lines-scroll">
      <table className="finance-receipt-lines">
        <thead>
          <tr>
            <th scope="col">{t('finance.receiptItem')}</th>
            <th scope="col">{t('finance.receiptQuantity')}</th>
            <th scope="col" className="numeric">
              {t('finance.amount')}
            </th>
          </tr>
        </thead>
        <tbody>
          {subtotalLines.map(lineRow)}
          {hasAmount(receipt.subtotalAmount) ? (
            <tr className="finance-receipt-subtotal">
              <th scope="row" colSpan={2}>
                {t('finance.receiptSubtotal')}
              </th>
              <td className="numeric">{formatMoney(amountOf(receipt.subtotalAmount), receipt.currencyCode)}</td>
            </tr>
          ) : null}
          {afterSubtotalLines.map(lineRow)}
        </tbody>
        <tfoot>
          <tr>
            <th scope="row" colSpan={2}>
              {t('finance.receiptTotal')}
            </th>
            <td className="numeric">{formatMoney(amountOf(receipt.totalAmount), receipt.currencyCode)}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  )
}

// OpenMessageButton opens the message a receipt was read from in the
// mailbox. A message moves between folders, so where it is now is asked
// for when it is opened.
function OpenMessageButton({ mailboxItemId, name }: { mailboxItemId: string; name: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const openMessage = async () => {
    try {
      const answer = await graphql<{ GetMailboxItem: { id: string; folderId: string } }>(
        'query ($itemId: String!) { GetMailboxItem(itemId: $itemId) { id folderId } }',
        { itemId: mailboxItemId },
      )
      navigate(`/mailbox/${answer.GetMailboxItem.folderId}/${answer.GetMailboxItem.id}`)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    }
  }
  return (
    <Tooltip label={t('finance.openReceiptMessage')}>
      <button
        type="button"
        className="icon-action"
        aria-label={`${name}: ${t('finance.openReceiptMessage')}`}
        onClick={() => void openMessage()}
      >
        <MailIcon size={16} />
      </button>
    </Tooltip>
  )
}

// useReceiptCheckDirection says which way an unbalanced receipt's lines
// miss its printed totals, "the lines come to $2.20 less than printed" or
// "more", and nothing for a receipt that adds up. The difference is signed
// as what the lines come to less what is printed, as the server keeps it;
// a minus sign on a tag said the same thing less clearly.
export function useReceiptCheckDirection(): (receipt: FinanceReceipt) => string {
  const { t } = useTranslation()
  return (receipt) => {
    const difference = amountOf(receipt.checkDifferenceAmount)
    if (receipt.receiptCheckState === 'balanced' || difference === 0) return ''
    const amount = formatMoney(Math.abs(difference), receipt.currencyCode)
    return difference < 0 ? t('finance.receiptLinesLess', { amount }) : t('finance.receiptLinesMore', { amount })
  }
}

// ReceiptCheckTag says whether a receipt's lines add up to its printed
// totals, or by how much they miss; which way they miss is its tooltip.
export function ReceiptCheckTag({ receipt }: { receipt: FinanceReceipt }) {
  const { t } = useTranslation()
  const direction = useReceiptCheckDirection()
  if (receipt.receiptCheckState === 'balanced') return <Tag value={t('finance.receiptBalanced')} tone="good" />
  return (
    <span title={direction(receipt)}>
      <Tag
        value={t('finance.receiptUnbalanced', {
          amount: formatMoney(Math.abs(amountOf(receipt.checkDifferenceAmount)), receipt.currencyCode),
        })}
        tone="warn"
      />
    </span>
  )
}

// ReceiptSource opens where a receipt was read from: the uploaded photo or
// file, or the message in the mailbox. Nothing for a Gmail message or a
// message that is gone, which useReceiptSourceNote says in words.
export function ReceiptSource({ receipt, name }: { receipt: FinanceReceipt; name: string }) {
  const { t } = useTranslation()
  if (receipt.receiptSourceKind === 'attachment' && receipt.agentAttachmentId) {
    return (
      <Tooltip label={t('finance.openReceiptFile')}>
        <a
          className="icon-action"
          href={`${AGENT_ATTACHMENTS_PATH}/${encodeURIComponent(receipt.agentAttachmentId)}`}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={`${name}: ${t('finance.openReceiptFile')}`}
        >
          <PictureIcon size={16} />
        </a>
      </Tooltip>
    )
  }
  if (receipt.receiptSourceKind === 'mail' && receipt.mailboxItemId) {
    return <OpenMessageButton mailboxItemId={receipt.mailboxItemId} name={name} />
  }
  return null
}

// ReceiptPhoto is the photo a receipt was read from, shown above its lines
// so the two can be compared, and opened over the page to zoom and pan on a
// tap. A receipt does not say whether its file is a photo or a PDF, so the
// picture is tried and, when it does not load, nothing is shown here: the
// file is still a click away in ReceiptSource.
export function ReceiptPhoto({ receipt, name }: { receipt: FinanceReceipt; name: string }) {
  const { t } = useTranslation()
  const [isUnreadable, setIsUnreadable] = useState(false)
  if (receipt.receiptSourceKind !== 'attachment' || !receipt.agentAttachmentId || isUnreadable) return null
  return (
    <div className="finance-receipt-photo">
      <ZoomablePicture
        source={`${AGENT_ATTACHMENTS_PATH}/${encodeURIComponent(receipt.agentAttachmentId)}`}
        name={name}
        imageClassName="finance-receipt-photo-image"
        openTitle={t('finance.zoomReceiptPhoto')}
        onUnreadable={() => setIsUnreadable(true)}
      />
    </div>
  )
}

// useReceiptSourceNote is the words for a source that cannot be opened:
// a Gmail message, or a message no longer in the mailbox.
export function useReceiptSourceNote(): (receipt: FinanceReceipt) => string {
  const { t } = useTranslation()
  return (receipt) => {
    if (receipt.receiptSourceKind === 'gmail_message') return t('finance.receiptFromGmail')
    if (receipt.receiptSourceKind === 'mail' && !receipt.mailboxItemId) return t('finance.receiptMessageGone')
    return ''
  }
}

// useCandidateAgreement says in a line what agrees between a receipt and a
// charge it could explain: the amount, the card, the merchant, the days.
export function useCandidateAgreement(): (candidate: ReceiptMatchCandidate) => string {
  const { t, plural } = useTranslation()
  return (candidate) =>
    [
      candidate.isExactAmount ? t('finance.candidateExactAmount') : '',
      candidate.isSameAccount ? t('finance.candidateSameCard') : '',
      candidate.isMerchantNameShared ? t('finance.candidateSameMerchant') : '',
      candidate.dayDistanceCount === 0
        ? t('finance.candidateSameDay')
        : plural(
            Math.abs(candidate.dayDistanceCount),
            { one: 'finance.candidateDayDistanceOne', other: 'finance.candidateDayDistanceOther' },
            { count: String(Math.abs(candidate.dayDistanceCount)) },
          ),
    ]
      .filter(Boolean)
      .join(' · ')
}

// FinanceReceiptsSection is the receipts matched to a finance transaction,
// each with its lines, its check, where it came from and how much of the
// charge it explains; what none of them explain; and the buttons to
// upload a receipt and to match one already read.
export function FinanceReceiptsSection({
  financeTransaction,
  receipts,
  isLoading,
  hasError,
  isBusy,
  isReading,
  onUpload,
  onMatch,
  onUnmatch,
  onDelete,
}: {
  financeTransaction: FinanceTransaction
  receipts: FinanceReceipt[]
  isLoading: boolean
  hasError: boolean
  isBusy: boolean
  isReading: boolean
  onUpload: (file: File) => void
  onMatch: () => void
  onUnmatch: (receipt: FinanceReceipt) => void
  onDelete: (receipt: FinanceReceipt) => void
}) {
  const { t } = useTranslation()
  const picker = useRef<HTMLInputElement | null>(null)
  const unexplained = unexplainedAmount(financeTransaction, receipts)
  const fallbackName = t('finance.receiptWithoutMerchant')
  const sourceNote = useReceiptSourceNote()
  const checkDirection = useReceiptCheckDirection()

  return (
    <section className="finance-receipts" aria-label={t('finance.receipts')}>
      <div className="finance-metadata-head">
        <strong>{t('finance.receipts')}</strong>
      </div>
      <p className="muted field-hint">{t('finance.receiptsHint')}</p>
      {isLoading ? <Loading /> : null}
      {!isLoading && !hasError && receipts.length === 0 ? <p className="muted">{t('finance.noReceipts')}</p> : null}
      {receipts.map((receipt) => {
        const name = receiptName(receipt, fallbackName)
        const matchedAmount = matchedAmountFor(receipt, financeTransaction.id)
        const match = receipt.receiptMatches.find((each) => each.financeTransactionId === financeTransaction.id)
        const note = sourceNote(receipt)
        return (
          <article key={receipt.id} className="finance-receipt" aria-label={name}>
            <div className="finance-receipt-head">
              <div className="finance-receipt-title">
                <strong>{name}</strong>{' '}
                <span className="muted">
                  {[
                    receipt.purchasedOn ? formatDay(receipt.purchasedOn) : '',
                    receipt.merchantReceiptNumber
                      ? t('finance.receiptNumber', { number: receipt.merchantReceiptNumber })
                      : '',
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>{' '}
                <ReceiptCheckTag receipt={receipt} />
              </div>
              <div className="row-actions">
                <ReceiptSource receipt={receipt} name={name} />
                <Tooltip label={t('finance.unmatchReceipt')}>
                  <button
                    type="button"
                    className="icon-action"
                    disabled={isBusy}
                    aria-label={`${name}: ${t('finance.unmatchReceipt')}`}
                    onClick={() => onUnmatch(receipt)}
                  >
                    <UnlinkIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('finance.deleteReceipt')}>
                  <button
                    type="button"
                    className="icon-action danger"
                    disabled={isBusy}
                    aria-label={`${name}: ${t('finance.deleteReceipt')}`}
                    onClick={() => onDelete(receipt)}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </div>
            </div>
            <p className="muted finance-receipt-detail">
              {[
                matchedAmount
                  ? t('finance.receiptMatchedAmount', {
                      amount: formatMoney(amountOf(matchedAmount), financeTransaction.currencyCode),
                    })
                  : '',
                match?.receiptMatchSource === 'receipt_matcher' ? t('finance.receiptMatchedAutomatically') : '',
                checkDirection(receipt),
                note,
              ]
                .filter(Boolean)
                .join(' · ')}
            </p>
            <ReceiptPhoto receipt={receipt} name={name} />
            <ReceiptLines receipt={receipt} />
          </article>
        )
      })}
      {receipts.length > 0 && unexplained > 0 ? (
        <p className="finance-receipt-unexplained">
          <span>{t('finance.unexplained')}</span>
          <span className="numeric-value">{formatMoney(unexplained, financeTransaction.currencyCode)}</span>
        </p>
      ) : null}
      {isReading ? (
        <p className="muted finance-receipt-reading" role="status">
          {t('finance.receiptReadingNow')}
        </p>
      ) : null}
      <div className="finance-receipt-buttons">
        {/* What cannot be read is said where the file is chosen, not in a
            paragraph above every receipt. */}
        <Tooltip label={t('finance.uploadReceiptHint')}>
          <button type="button" disabled={isBusy || isReading} onClick={() => picker.current?.click()}>
            {t('finance.uploadReceipt')}
          </button>
        </Tooltip>
        <button type="button" disabled={isBusy} onClick={onMatch}>
          {t('finance.matchReceipt')}
        </button>
      </div>
      <input
        ref={picker}
        type="file"
        accept="image/*"
        hidden
        data-testid="receipt-file"
        onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ''
          if (file) onUpload(file)
        }}
      />
    </section>
  )
}

type Proposal = { receipt: FinanceReceipt; candidate: ReceiptMatchCandidate }

// ReceiptMatchChooser offers the receipts matched to no charge that could
// explain this one: those bought around the day it posted, each asked
// which charges it could explain and kept when this is one of them, with
// what agrees between them. Matching one closes the chooser.
export function ReceiptMatchChooser({
  financeTransaction,
  isBusy,
  onMatch,
  onClose,
}: {
  financeTransaction: FinanceTransaction
  isBusy: boolean
  onMatch: (receipt: FinanceReceipt, matchedAmount: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [proposals, setProposals] = useState<Proposal[] | null>(null)
  const fallbackName = t('finance.receiptWithoutMerchant')
  useEffect(() => {
    let isCancelled = false
    const days = purchaseWindow(financeTransaction.postedOn)
    void (async () => {
      try {
        const answer = await graphql<{ FinanceReceipts?: FinanceReceiptPage | null }>(FINANCE_RECEIPTS, {
          isUnmatched: true,
          from: days.from,
          to: days.to,
          limit: 50,
        })
        const unmatched = answer?.FinanceReceipts?.financeReceipts ?? []
        // One receipt that cannot be asked about leaves the others offered,
        // and one toast says some were left out.
        const settled = await Promise.allSettled(
          unmatched.map(async (receipt) => {
            const proposed = await graphql<{ ProposeReceiptMatches?: ReceiptMatchCandidate[] | null }>(
              PROPOSE_RECEIPT_MATCHES,
              { receiptId: receipt.id },
            )
            const candidate = (proposed?.ProposeReceiptMatches ?? []).find(
              (each) => each.financeTransactionId === financeTransaction.id,
            )
            return candidate ? { receipt, candidate } : null
          }),
        )
        if (isCancelled) return
        const proposed = settled.flatMap((outcome) =>
          outcome.status === 'fulfilled' && outcome.value ? [outcome.value] : [],
        )
        setProposals(proposed)
        const rejected = settled.find((outcome): outcome is PromiseRejectedResult => outcome.status === 'rejected')
        if (rejected) toast.failure(rejected.reason, t('finance.failed'))
      } catch (caught) {
        if (isCancelled) return
        setProposals([])
        toast.failure(caught, t('finance.failed'))
      }
    })()
    return () => {
      isCancelled = true
    }
    // Asked once, when the chooser opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [financeTransaction.id])

  const agreement = useCandidateAgreement()

  return (
    <ConfirmDialog
      wide
      title={t('finance.matchReceiptTitle')}
      onClose={onClose}
      body={
        <div className="finance-receipt-chooser">
          <p className="muted">{t('finance.matchReceiptHint')}</p>
          {proposals === null ? <Loading /> : null}
          {proposals !== null && proposals.length === 0 ? (
            <p className="muted">{t('finance.noReceiptCandidates')}</p>
          ) : null}
          {proposals && proposals.length > 0 ? (
            <ul className="finance-receipt-candidates">
              {proposals.map(({ receipt, candidate }) => {
                const name = receiptName(receipt, fallbackName)
                return (
                  <li key={receipt.id}>
                    <div>
                      <strong>{name}</strong>{' '}
                      <span className="muted">{receipt.purchasedOn ? formatDay(receipt.purchasedOn) : ''}</span>
                      <span className="muted finance-detail-note">
                        {[
                          t('finance.receiptTotalOf', {
                            amount: formatMoney(amountOf(receipt.totalAmount), receipt.currencyCode),
                          }),
                          t('finance.candidateExplains', {
                            amount: formatMoney(amountOf(candidate.matchedAmount), receipt.currencyCode),
                          }),
                          agreement(candidate),
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </div>
                    <Tooltip label={t('finance.matchThisReceipt')}>
                      <button
                        type="button"
                        className="icon-action"
                        disabled={isBusy}
                        aria-label={`${name}: ${t('finance.matchThisReceipt')}`}
                        onClick={() => onMatch(receipt, candidate.matchedAmount)}
                      >
                        <LinkIcon size={16} />
                      </button>
                    </Tooltip>
                  </li>
                )
              })}
            </ul>
          ) : null}
        </div>
      }
    />
  )
}

// DeleteReceiptConfirmation asks before a receipt is deleted, saying what
// goes with it and what stays.
export function DeleteReceiptConfirmation({
  receipt,
  isBusy,
  onConfirm,
  onClose,
}: {
  receipt: FinanceReceipt
  isBusy: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <ConfirmDialog
      title={t('finance.deleteReceiptTitle', {
        merchant: receiptName(receipt, t('finance.receiptWithoutMerchant')),
      })}
      body={t('finance.deleteReceiptBody')}
      confirmLabel={t('common.delete')}
      busy={isBusy}
      onConfirm={onConfirm}
      onClose={onClose}
    />
  )
}
