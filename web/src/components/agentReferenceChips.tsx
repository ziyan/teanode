import type { AgentReference } from '../api'
import { useTranslation } from '../i18n/i18n'
import { formatMoney } from './common'
import { CloseIcon, ReplyIcon, SparkIcon } from './icons'
import { Tooltip } from './tooltip'

// referenceDay is a finance transaction's posted day as its chip says it:
// "Jun 9", with the year only when it is not this one.
function referenceDay(postedOn: string): string {
  const parsed = new Date(`${postedOn.slice(0, 10)}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return postedOn
  const isThisYear = parsed.getFullYear() === new Date().getFullYear()
  return parsed.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    ...(isThisYear ? {} : { year: 'numeric' }),
  })
}

// referenceLabel is what a chip says about what the person pointed at: a
// thread by its subject, a page of memory by its name, a finance
// transaction by its day, merchant (or description) and amount.
export function referenceLabel(reference: AgentReference, t: ReturnType<typeof useTranslation>['t']): string {
  if (reference.financeTransactionId) {
    const amount = Number(reference.amount)
    return t('agentDrawer.financeTransactionReference', {
      day: reference.postedOn ? referenceDay(reference.postedOn) : '',
      name: reference.merchantName || reference.description || '',
      amount: Number.isFinite(amount) ? formatMoney(amount, reference.currencyCode) : (reference.amount ?? ''),
    })
  }
  return reference.subject || reference.name || reference.path || reference.itemId || reference.threadId || ''
}

// referenceHint is the line on hover: who a thread is from, a page's
// path, a finance transaction's full description.
function referenceHint(reference: AgentReference): string {
  if (reference.financeTransactionId) return reference.description ?? ''
  return reference.from ?? reference.path ?? ''
}

// isReplyReference says the reference is a message of the conversation
// the turn answers, drawn as a quote rather than a chip.
export function isReplyReference(reference: AgentReference): boolean {
  return Boolean(reference.agentMessageId || reference.quotedText)
}

// ReplyQuote is the message a turn answers, above the box while it is
// being written: who said it, the start of it, and a way to drop it. On
// the person's line once sent it is the quote above their words.
export function ReplyQuote({ reference, title, onRemove }: { reference: AgentReference; title: string; onRemove: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="agent-reply-quote">
      <ReplyIcon size={14} />
      <div className="agent-reply-quote-body">
        <div className="agent-reply-quote-title">{title}</div>
        <div className="agent-reply-quote-text">{reference.quotedText}</div>
      </div>
      <button
        type="button"
        className="icon-action"
        title={t('agentDrawer.cancelReply')}
        aria-label={t('agentDrawer.cancelReply')}
        onClick={onRemove}
      >
        <CloseIcon size={12} />
      </button>
    </div>
  )
}

// ReferenceChips are what a turn points at, above the box while it is
// being written (each removable) and on the person's line once it is sent.
// A message replied to is not among them: it is drawn as a quote.
export function ReferenceChips({
  references,
  onRemove,
}: {
  references: AgentReference[]
  onRemove?: (reference: AgentReference) => void
}) {
  const { t } = useTranslation()
  const chips = references.filter((reference) => !isReplyReference(reference))
  if (chips.length === 0) return null
  return (
    <div className="agent-references">
      {chips.map((reference, index) => {
        const label = referenceLabel(reference, t)
        return (
          <Tooltip
            key={`${reference.itemId ?? reference.path ?? reference.financeTransactionId ?? ''}-${index}`}
            label={referenceHint(reference)}
          >
            <span className="agent-reference-chip">
              <SparkIcon size={11} /> {label}
              {onRemove && (
                <button
                  type="button"
                  className="icon-action"
                  title={t('agentDrawer.remove')}
                  aria-label={`${label}: ${t('agentDrawer.remove')}`}
                  onClick={() => onRemove(reference)}
                >
                  <CloseIcon size={12} />
                </button>
              )}
            </span>
          </Tooltip>
        )
      })}
    </div>
  )
}
