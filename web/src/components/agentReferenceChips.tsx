import type { AgentReference } from '../api'
import { useTranslation } from '../i18n/i18n'
import { formatMoney } from './common'
import { CloseIcon, SparkIcon } from './icons'
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

// ReferenceChips are what a turn points at, above the box while it is
// being written (each removable) and on the person's line once it is sent.
export function ReferenceChips({
  references,
  onRemove,
}: {
  references: AgentReference[]
  onRemove?: (index: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="agent-references">
      {references.map((reference, index) => {
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
                  onClick={() => onRemove(index)}
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
