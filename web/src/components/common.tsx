import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { CheckIcon, CopyIcon, ShieldIcon } from './icons'
import { Mail } from '../api'
import { Key, useTranslation } from '../i18n/i18n'

export function Tag({ value, tone }: { value: string; tone?: 'good' | 'bad' | 'warn' }) {
  return <span className={tone ? `tag ${tone}` : 'tag'}>{value}</span>
}

// --- what a message's checks said ---------------------------------------------
//
// Every message arrives with verdicts on it — whether the domain authorised
// the server that sent it (SPF), whether the signature holds (DKIM), whether
// the domain's own policy is satisfied (DMARC), and what the spam filter made
// of it. All of it was on the audit page and nowhere a person reading their
// mail would look, which is the one place it answers a question somebody
// actually has: is this really from who it says?

export type MailVerdict = {
  tone: 'good' | 'bad' | 'warn' | undefined
  // The one-line summary, for a tooltip or a line under a message.
  detail: string
  // How the spam filter scored it, when it scored it at all.
  score?: number
}

// verdictOf reduces a message's checks to one answer.
//
// Authenticated means DMARC passed, or — for a domain that publishes no DMARC
// policy, which is still most of them — SPF passed and a signature held.
// Failed means one of them actively said no, which is different from having
// nothing to say: a domain with no SPF record has not failed anything.
export function verdictOf(
  mail: Mail | null | undefined,
  t: (key: Key, values?: Record<string, string | number>) => string,
): MailVerdict | null {
  const results = mail?.authenticationResults
  if (!results) {
    return null
  }
  const spf = (results.spf?.result ?? '').toLowerCase()
  const dmarc = (results.dmarc?.result ?? '').toLowerCase()
  const dkim = (results.dkims ?? []).map((each) => (each.result ?? '').toLowerCase())
  const score = results.spamFilter?.score

  const parts: string[] = []
  if (spf) {
    parts.push(`SPF ${spf}`)
  }
  if (dkim.length > 0) {
    parts.push(`DKIM ${dkim.includes('pass') ? 'pass' : dkim[0]}`)
  }
  if (dmarc) {
    parts.push(`DMARC ${dmarc}`)
  }
  if (score !== undefined && score !== null) {
    parts.push(t('mailbox.spamScore', { score: score.toFixed(1) }))
  }
  if (parts.length === 0) {
    return null
  }

  const failed =
    dmarc === 'fail' ||
    spf === 'fail' ||
    spf === 'hardfail' ||
    (dkim.length > 0 && !dkim.includes('pass') && dkim.includes('fail'))
  const authenticated = dmarc === 'pass' || (spf === 'pass' && dkim.includes('pass'))
  const spammy = score !== undefined && score !== null && score >= 5

  let tone: MailVerdict['tone']
  if (failed) {
    tone = 'bad'
  } else if (spammy) {
    tone = 'warn'
  } else if (authenticated) {
    tone = 'good'
  }
  return { tone, detail: parts.join(' · '), score: score ?? undefined }
}

// VerdictMark is that answer in the width of one character: a shield, colored
// by what the checks said, with the whole of it in the tooltip. A list of
// fifty messages cannot spend a line each on this, and a list that showed
// nothing for a message that failed its checks would be worse than one that
// never mentioned them.
export function VerdictMark({ mail }: { mail?: Mail | null }) {
  const { t } = useTranslation()
  const verdict = verdictOf(mail, t)
  if (!verdict) {
    return null
  }
  return (
    <span
      className={verdict.tone ? `verdict-mark ${verdict.tone}` : 'verdict-mark'}
      title={verdict.detail}
      aria-label={verdict.detail}
    >
      <ShieldIcon size={13} />
    </span>
  )
}

// Copy, as an icon at the end of a value rather than a word beside it.
//
// A DKIM public key is four hundred characters, and printing all of them says
// nothing a reader can check by eye while taking four lines of a table to say
// it. The value is clamped to its line and this takes the whole of it,
// unclamped, to wherever it is going.
//
// The tick lasts a moment and goes back. A button that stays changed is a
// button that has forgotten what it does.
export function CopyIconButton({ value, label }: { value: string; label?: string }) {
  const { t } = useTranslation()
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')

  useEffect(() => {
    if (state === 'idle') {
      return
    }
    const timer = window.setTimeout(() => setState('idle'), 1500)
    return () => window.clearTimeout(timer)
  }, [state])

  const name = label ?? t('common.copy')

  return (
    <button
      type="button"
      className="icon-button copy-button"
      aria-label={state === 'copied' ? t('common.copied') : name}
      title={state === 'failed' ? t('common.copyFailed') : state === 'copied' ? t('common.copied') : name}
      onClick={() => {
        // The clipboard API is absent on an insecure origin, which a
        // deployment behind plain HTTP is. Say so rather than doing nothing:
        // the value is on the screen either way.
        if (!navigator.clipboard) {
          setState('failed')
          return
        }
        void navigator.clipboard.writeText(value).then(
          () => setState('copied'),
          () => setState('failed'),
        )
      }}
    >
      {state === 'copied' ? <CheckIcon size={15} /> : <CopyIcon size={15} />}
    </button>
  )
}

// toneFor maps a verdict onto a color. Everything an authentication check can
// say ends up here, so a reader can scan a list without reading each word.
export function toneFor(value?: string): 'good' | 'bad' | 'warn' | undefined {
  switch ((value ?? '').toLowerCase()) {
    case 'pass':
    case 'delivered':
    case 'accepted':
    case 'none':
      return 'good'
    case 'fail':
    case 'rejected':
    case 'dropped':
    case 'hardfail':
    case 'permerror':
      return 'bad'
    case 'softfail':
    case 'neutral':
    case 'temperror':
    case 'queued':
    case 'attempted':
    case 'delayed':
      return 'warn'
    default:
      return undefined
  }
}

// A query on a local network answers in a few milliseconds, and a word that
// appears and disappears in that time is a flicker rather than information —
// it was on screen on every navigation. So nothing is drawn until waiting has
// actually become waiting.
//
// A quarter of a second: long enough that a fast answer never flashes, short
// enough that a slow one does not look like a page that failed to load.
const PATIENCE = 250

export function Loading() {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const timer = window.setTimeout(() => setVisible(true), PATIENCE)
    return () => window.clearTimeout(timer)
  }, [])

  if (!visible) {
    return null
  }
  return <p className="muted">{t('common.loading')}</p>
}

// Whatever went wrong, said in the one place the eye already looks for it.
// Nothing is drawn when there is nothing wrong, so a caller can render it
// unconditionally instead of writing the same guard on every page.
export function ErrorMessage({ error }: { error: unknown }) {
  if (error === null || error === undefined || error === '' || error === false) {
    return null
  }
  const message = error instanceof Error ? error.message : String(error)
  return <p className="error">{message}</p>
}

// SaveRow is the foot of a settings form: what went wrong, the button, and
// what it did. Written once because it was written ten times — with the note
// above the button in one file and beside it in another, so two settings
// pages disagreed about where "saved" appears.
//
// The note is a prop because it is not always the same sentence: some
// settings take effect as soon as they are saved and some wait for a
// restart, and that is the one thing the reader needs to be told here.
export function SaveRow({
  busy,
  saved,
  problem,
  note,
  canSave = true,
}: {
  busy: boolean
  saved: boolean
  problem?: unknown
  note: string
  // False while there is nothing to save — nothing changed, or a field is
  // still empty.
  canSave?: boolean
}) {
  const { t } = useTranslation()

  return (
    <>
      <ErrorMessage error={problem} />
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy || !canSave}>
          {busy ? t('common.saving') : t('common.save')}
        </button>
        {saved && <span className="muted">{note}</span>}
      </div>
    </>
  )
}

// Field is a row of a key/value table, skipped when there is nothing to put
// in it: a label with an em dash beside it costs a line and says nothing.
export function Field({ label, mono, children }: { label: string; mono?: boolean; children?: React.ReactNode }) {
  if (children === undefined || children === null || children === '' || children === false) {
    return null
  }
  return (
    <tr>
      <td className="shrink muted">{label}</td>
      <td className={mono ? 'mono wrap' : 'wrap'}>{children}</td>
    </tr>
  )
}

export function formatBytes(size?: number): string {
  if (!size) {
    return '—'
  }
  if (size < 1024) {
    return `${size} B`
  }
  if (size < 1024 * 1024) {
    return `${Math.round(size / 1024)} KB`
  }
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

export function formatTime(value?: string): string {
  if (!value) {
    return '—'
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString()
}

// DomainLink names a domain and goes to it.
//
// A domain is a place in this dashboard, and it is named in four lists. Every
// one of them showed it as gray text, which is exactly what the domain's own
// page is one click away from answering.
//
// A domain that has since been deleted is still named in the mail it received,
// and there is nowhere for it to go: that is said rather than linked.
export function DomainLink({ domainId, names }: { domainId?: string; names: Map<string, string> }) {
  const { t } = useTranslation()
  const name = names.get(domainId ?? '')

  if (!name || !domainId) {
    return <span className="muted">{t('mail.deletedDomain')}</span>
  }
  return <Link to={`/domains/${domainId}`}>{name}</Link>
}

// The enums the server speaks, in the reader's language.
//
// A column showing "rua" or "dsn" is showing an identifier: those are the
// words the protocol uses, and nobody outside this codebase has to know them.
// Every one is translated, and anything unrecognized falls through as itself
// rather than disappearing — a new kind added to the server should show up as
// a word nobody translated, not as an empty cell.
const MAIL_KINDS: Record<string, Key> = {
  incoming: 'kind.incoming',
  outgoing: 'kind.outgoing',
  exchange: 'kind.exchange',
  dsn: 'kind.dsn',
  rua: 'kind.rua',
  ruf: 'kind.ruf',
  forward: 'kind.forward',
  internal: 'kind.internal',
  external: 'kind.external',
}

const STATUSES: Record<string, Key> = {
  received: 'status.received',
  accepted: 'status.accepted',
  rejected: 'status.rejected',
  queued: 'status.queued',
  dropped: 'status.dropped',
  delivered: 'status.delivered',
  attempted: 'status.attempted',
  failed: 'status.failed',
  delayed: 'status.delayed',
  relayed: 'status.relayed',
  expanded: 'status.expanded',
}

// useEnumLabel returns the words for a kind and a status. A hook rather than
// a function because the language it answers in is the reader's, and that can
// change without the page reloading.
export function useEnumLabel() {
  const { t } = useTranslation()
  return {
    kind: (value?: string) => (value && MAIL_KINDS[value] ? t(MAIL_KINDS[value]) : (value ?? '')),
    status: (value?: string) => (value && STATUSES[value] ? t(STATUSES[value]) : (value ?? '')),
  }
}

// KindTag is a kind as a chip, so the column reads as a set of categories
// rather than as a column of lowercase words.
export function KindTag({ value }: { value?: string }) {
  const { kind } = useEnumLabel()
  if (!value) {
    return null
  }
  return <Tag value={kind(value)} />
}
