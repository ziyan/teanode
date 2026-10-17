import { useEffect, useState } from 'react'

import { failureText, graphql, isNotFound } from '../api'
import { Avatar } from './avatar'
import { Tag, formatTime } from './common'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { useTranslation, type Key, type Values } from '../i18n/i18n'

// Skills shared with other people, and the calls made through them.
// docs/decisions/20261016-a-payment-skill-is-shared-call-by-call.md says why a
// payment is shared call by call, and docs/subsystems/skills.md ("Sharing a
// skill") how it works. What is here is the part both the agent page and the
// drawer draw: the share and the call as the server describes them, money in
// the currency's own digits, and the card a call that waits is answered on.

export type SkillShare = {
  id: string
  skillName: string
  toolNames: string[]
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  ownerAvatarUrl: string | null
  ownerAvatarColorIndex: number
  granteeUserId: string
  granteeUsername: string
  granteeName: string
  granteeAvatarUrl: string | null
  granteeAvatarColorIndex: number
  paymentLimitMinor: number | null
  monthlyLimitMinor: number | null
  currencyCode: string
  isApprovalRequired: boolean
  isOwnedByCaller: boolean
  waitingCallCount: number
  monthTotalMinor: number
}

export type SkillShareCall = {
  id: string
  skillShareId: string
  isShareEnded: boolean
  skillName: string
  granteeUserId: string
  granteeUsername: string
  granteeName: string
  granteeAvatarUrl: string | null
  granteeAvatarColorIndex: number
  ownerName: string
  toolName: string
  toolArguments: string
  requestReason: string
  amountMinor: number | null
  currencyCode: string
  callState: string
  toolOutput: string
  answeredAt: string | null
  answerNote: string
  expiresAt: string | null
  createdAt: string
}

export const SKILL_SHARE_FIELDS = `id skillName toolNames ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
  granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex
  paymentLimitMinor monthlyLimitMinor currencyCode isApprovalRequired isOwnedByCaller waitingCallCount monthTotalMinor`

export const SKILL_SHARE_CALL_FIELDS = `id skillShareId isShareEnded skillName granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex
  ownerName toolName toolArguments requestReason amountMinor currencyCode callState toolOutput answeredAt answerNote expiresAt createdAt`

export const LIST_SKILL_SHARE_CALLS = `query ($shareId: String!, $callState: String) {
  ListSkillShareCalls(shareId: $shareId, callState: $callState) { ${SKILL_SHARE_CALL_FIELDS} }
}`

// The calls of shares that have ended: the owner's record, kept when a share is
// taken back or the two stop sharing a circle.
export const LIST_ENDED_SKILL_SHARE_CALLS = `query {
  ListSkillShareCalls(isShareEnded: true) { ${SKILL_SHARE_CALL_FIELDS} }
}`

const ANSWER_SKILL_SHARE_CALL = `mutation ($callId: String!, $isApproved: Boolean!, $answerNote: String) {
  AnswerSkillShareCall(callId: $callId, isApproved: $isApproved, answerNote: $answerNote) { ${SKILL_SHARE_CALL_FIELDS} }
}`

// currencyExponent is how many digits follow the point in a currency's own
// units: 2 for most, 0 for those with no minor unit, 3 for dinars. The same
// table the server, the command line and the app carry.
const NO_MINOR_UNIT = new Set('bif clp djf gnf jpy kmf krw mga pyg rwf ugx vnd vuv xaf xof xpf'.split(' '))
const THOUSANDTHS = new Set('bhd iqd jod kwd lyd omr tnd'.split(' '))

export function currencyExponent(currencyCode: string): number {
  const code = currencyCode.toLowerCase()
  if (NO_MINOR_UNIT.has(code)) return 0
  if (THOUSANDTHS.has(code)) return 3
  return 2
}

// formatMinor writes an amount in minor units as people write money: 14.00 USD.
export function formatMinor(minor: number, currencyCode: string): string {
  const exponent = currencyExponent(currencyCode)
  const divisor = 10 ** exponent
  const sign = minor < 0 ? '-' : ''
  const absolute = Math.abs(minor)
  const whole = Math.floor(absolute / divisor)
  const fraction = exponent > 0 ? '.' + String(absolute % divisor).padStart(exponent, '0') : ''
  return `${sign}${whole}${fraction}${currencyCode ? ' ' + currencyCode.toUpperCase() : ''}`
}

// parseMinor reads an amount somebody typed in the currency's own units into
// minor units, or null when it is not an amount above zero in that currency:
// no sign, no thousands separator, no more digits after the point than the
// currency has. A limit read wrongly is a limit that does not hold.
export function parseMinor(typed: string, currencyCode: string): number | null {
  const text = typed.trim()
  const exponent = currencyExponent(currencyCode)
  const match = /^(\d{1,15})(?:\.(\d*))?$/.exec(text)
  if (!match) return null
  const fraction = match[2] ?? ''
  if (fraction.length > exponent || (text.endsWith('.') && exponent > 0)) return null
  const minor = Number(match[1] + fraction.padEnd(exponent, '0'))
  return Number.isSafeInteger(minor) && minor > 0 ? minor : null
}

// limitsLine says in words what a share allows without asking.
export function limitsLine(share: SkillShare, t: (key: Key, values?: Values) => string): string {
  if (share.isApprovalRequired) return t('skillShare.limitsAskEveryTime')
  const parts: string[] = []
  if (share.paymentLimitMinor !== null)
    parts.push(t('skillShare.limitsPayment', { amount: formatMinor(share.paymentLimitMinor, share.currencyCode) }))
  if (share.monthlyLimitMinor !== null)
    parts.push(t('skillShare.limitsMonthly', { amount: formatMinor(share.monthlyLimitMinor, share.currencyCode) }))
  return parts.length > 0 ? parts.join(', ') : t('skillShare.limitsNone')
}

// argumentLines are the arguments a call was made with, one line each, less
// the amount and currency, which the card shows apart. A value is kept whole:
// the owner is approving exactly this call, and a cut value could hide the part
// that matters. The card wraps it.
export function argumentLines(toolArguments: string, hide: string[]): [string, string][] {
  let parsed: unknown
  try {
    parsed = JSON.parse(toolArguments)
  } catch {
    return []
  }
  if (!parsed || typeof parsed !== 'object') return []
  const lines: [string, string][] = []
  for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
    if (hide.includes(key) || value === null || value === undefined || value === '') continue
    const text = typeof value === 'string' ? value : JSON.stringify(value)
    lines.push([key.replace(/_/g, ' '), text])
  }
  return lines
}

// SkillShareCallCard is one call of a shared skill, with who asked, what, and for how
// much; and, while it waits, the two buttons that answer it. Approving runs
// exactly this call on the owner's computer. What the other person's agent
// wrote as its reason is drawn as their words, quoted, and never as part of
// the page.
export function SkillShareCallCard({
  call,
  isOwner = true,
  onAnswered,
}: {
  call: SkillShareCall
  // The owner answers; the person it is shared with only sees how it stands.
  isOwner?: boolean
  onAnswered?: (answered: SkillShareCall) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const name = call.granteeName || call.granteeUsername
  const isWaiting = call.callState === 'waiting_for_approval'
  const amount = call.amountMinor !== null ? formatMinor(call.amountMinor, call.currencyCode) : ''
  const lines = argumentLines(call.toolArguments, ['amount_minor', 'amountMinor', 'currency', 'request_reason'])

  async function answer(isApproved: boolean) {
    setBusy(true)
    try {
      const done = await graphql<{ AnswerSkillShareCall: SkillShareCall }>(ANSWER_SKILL_SHARE_CALL, {
        callId: call.id,
        isApproved,
        answerNote: note.trim() || null,
      })
      onAnswered?.(done.AnswerSkillShareCall)
      toast.done(isApproved ? t('skillShare.callToastApproved', { name }) : t('skillShare.callToastDeclined', { name }))
    } catch (caught) {
      toast.failed(failureText(caught, t(isApproved ? 'skillShare.callApproveFailed' : 'skillShare.callDeclineFailed')))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      className="skill-share-call-card"
      role="group"
      aria-label={t('skillShare.callAsks', { name, tool: call.toolName, skill: call.skillName })}
    >
      <div className="skill-share-call-card-head">
        <Avatar name={name} avatarUrl={call.granteeAvatarUrl} colorIndex={call.granteeAvatarColorIndex} size={28} />
        <strong className="skill-share-call-card-title">
          {isOwner
            ? t('skillShare.callAsks', { name, tool: call.toolName, skill: call.skillName })
            : t('skillShare.callYouAsked', { name: call.ownerName, tool: call.toolName, skill: call.skillName })}
        </strong>
        <Tag
          value={t(`skillShare.callState.${call.callState}` as Parameters<typeof t>[0])}
          tone={isWaiting ? 'warn' : call.callState === 'ran' ? 'good' : 'bad'}
        />
      </div>
      <dl className="skill-share-call-card-facts">
        <dt>{t('skillShare.callTool')}</dt>
        <dd>{call.toolName}</dd>
        <dt>{t('skillShare.callAmount')}</dt>
        <dd className={amount ? 'skill-share-call-card-amount' : 'muted'}>
          {amount || t('skillShare.callAmountUnknown')}
        </dd>
        {lines.map(([key, value]) => (
          <div className="skill-share-call-card-line" key={key}>
            <dt>{key}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      {call.requestReason ? (
        <figure className="skill-share-call-card-reason">
          <figcaption className="muted">{t('skillShare.callReasonFrom', { name })}</figcaption>
          <blockquote>{call.requestReason}</blockquote>
        </figure>
      ) : null}
      {isWaiting && isOwner ? (
        <>
          <input
            className="skill-share-call-card-note"
            type="text"
            value={note}
            maxLength={400}
            placeholder={t('skillShare.callNotePlaceholder', { name })}
            aria-label={t('skillShare.callNotePlaceholder', { name })}
            disabled={busy}
            onChange={(event) => setNote(event.target.value)}
          />
          <div className="skill-share-call-card-actions">
            <button type="button" className="primary" disabled={busy} onClick={() => void answer(true)}>
              {t('skillShare.callApprove')}
            </button>
            <button type="button" disabled={busy} onClick={() => void answer(false)}>
              {t('skillShare.callDecline')}
            </button>
          </div>
          {call.expiresAt ? (
            <p className="muted">{t('skillShare.callExpires', { time: formatTime(call.expiresAt) })}</p>
          ) : null}
        </>
      ) : isWaiting ? (
        <p className="muted">{t('skillShare.callWaitingForOwner', { name: call.ownerName })}</p>
      ) : (
        <p className="muted">
          {t(`skillShare.callOutcome.${call.callState}` as Parameters<typeof t>[0])}
          {call.answerNote ? ` ${t('skillShare.callTheirNote', { note: call.answerNote })}` : ''}
        </p>
      )}
    </div>
  )
}

// SkillShareCallWaitingNote is the card for a skill_share_call_waiting note of the main
// conversation: the note names the call, the call is read for how it stands now
// (it may have been answered on the phone since), and the card is drawn from
// that. A call that is gone, because the share was taken back or the two no
// longer share a circle, says so.
export function SkillShareCallWaitingNote({ detail }: { detail: string }) {
  const { t } = useTranslation()
  const { skillShareId: shareId, callId } = waitingNoteOf(detail)
  // Read again every few seconds while the call waits, so that an answer given
  // on the phone shows here; once it is settled, or gone, it is read no more,
  // since a transcript can hold many of these notes. A read that failed is not
  // an answer: it keeps being tried, and can be tried now.
  const [isSettled, setIsSettled] = useState(false)
  const calls = useQuery(
    async () => {
      if (!shareId || !callId) return null
      try {
        const answer = await graphql<{ ListSkillShareCalls: SkillShareCall[] }>(LIST_SKILL_SHARE_CALLS, {
          shareId,
          callState: null,
        })
        return answer.ListSkillShareCalls.find((each) => each.id === callId) ?? null
      } catch (caught) {
        // Only an answer of not found says the share is gone. Anything else
        // is a read that did not get through.
        if (isNotFound(caught)) return null
        throw caught
      }
    },
    [shareId, callId],
    { refresh: !isSettled },
  )
  useEffect(() => {
    if (calls.loading || calls.error) return
    if (calls.data === null || calls.data.callState !== 'waiting_for_approval') setIsSettled(true)
  }, [calls.loading, calls.error, calls.data])
  const [answered, setAnswered] = useState<SkillShareCall | null>(null)
  const call = answered ?? calls.data
  return (
    <div className="agent-line skill-share-call-card-wrap">
      {call ? (
        <SkillShareCallCard call={call} onAnswered={setAnswered} />
      ) : calls.error ? (
        <p className="muted">
          {t('skillShare.callLoadFailed')}{' '}
          <button type="button" onClick={() => void calls.reload()}>
            {t('common.tryAgain')}
          </button>
        </p>
      ) : (
        <p className="muted">{calls.loading ? t('skillShare.callLoading') : t('skillShare.callGone')}</p>
      )}
    </div>
  )
}

// waitingNoteOf reads the share and the call a skill_share_call_waiting note
// names, from its detail. A detail that is not that names nothing.
function waitingNoteOf(detail: string): { skillShareId: string; callId: string } {
  try {
    const note = JSON.parse(detail) as { skillShareId?: string; callId?: string } | null
    return { skillShareId: note?.skillShareId ?? '', callId: note?.callId ?? '' }
  } catch {
    return { skillShareId: '', callId: '' }
  }
}
