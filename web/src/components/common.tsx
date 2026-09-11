import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { CheckIcon, CopyIcon, ShieldIcon } from './icons'
import { Tooltip } from './tooltip'
import { useToast } from './toast'
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
    <Tooltip label={verdict.detail}>
      <span className={verdict.tone ? `verdict-mark ${verdict.tone}` : 'verdict-mark'} aria-label={verdict.detail}>
        <ShieldIcon size={13} />
      </span>
    </Tooltip>
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

  const said = state === 'failed' ? t('common.copyFailed') : state === 'copied' ? t('common.copied') : name

  return (
    <Tooltip label={said}>
      <button
        type="button"
        className="icon-button copy-button"
        aria-label={state === 'copied' ? t('common.copied') : name}
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
    </Tooltip>
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
// ErrorMessage says what went wrong through the toasts at the foot of the
// window — the one place every failure is said — and draws nothing where
// it sits. It keeps its old name and shape so a page can still put it
// where the thing that failed is; what changed is where the words appear.
export function ErrorMessage({ error }: { error: unknown }) {
  const toast = useToast()
  const message =
    error === null || error === undefined || error === '' || error === false
      ? ''
      : error instanceof Error
        ? error.message
        : String(error)
  // Once per error, not per wording: a form renders on every keystroke
  // with the same error object, and a second failure with the same words
  // is a new object and a new toast.
  const shown = useRef<unknown>(null)
  useEffect(() => {
    if (message && error !== shown.current) toast.failed(message)
    shown.current = error
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [error])
  return null
}

// SaveRow is the foot of a settings form: what went wrong, the button, and
// what it did. Written once because it was written ten times — with the note
// above the button in one file and beside it in another, so two settings
// pages disagreed about where "saved" appears.
//
// The note is a prop because it is not always the same sentence: some
// settings take effect as soon as they are saved and some wait for a
// restart, and that is the one thing the reader needs to be told here.
// useSaySaved says a save happened, once, at the moment it happens.
//
// That it saved belongs at the foot of the window with everything else that
// has just happened, rather than being a word beside the button that stays
// there until something takes it back. What the note says beyond "saved" —
// "this needs a restart" — travels with it.
//
// Only on the change from not-saved to saved, because the form above this
// renders again for every keystroke, and a toast per keystroke is not a thing
// anybody wants.
export function useSaySaved(saved: boolean, note: string) {
  const toast = useToast()
  const said = useRef(false)
  useEffect(() => {
    if (saved && !said.current) {
      toast.done(note)
    }
    said.current = saved
  }, [saved, note, toast])
}

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
  useSaySaved(saved, note)

  return (
    <>
      <ErrorMessage error={problem} />
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy || !canSave}>
          {busy ? t('common.saving') : t('common.save')}
        </button>
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

// formatCount says a count the way a person reads one: 59, 13.5k, 1.2M.
// For tokens and anything else that runs into the thousands and is read
// for its size rather than its exact value.
export function formatCount(count?: number | null): string {
  const value = count ?? 0
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(value >= 10_000_000 ? 0 : 1)}M`
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10_000 ? 0 : 1)}k`
  return String(value)
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

// A moment, written out, with the zone it is in.
//
// The zone is not decoration. A timestamp without one is ambiguous the moment
// it is read anywhere but the machine that rendered it — and these are read
// in support threads, pasted into tickets and compared against logs from a
// server in another country.
export function formatTime(value?: string): string {
  if (!value) {
    return '—'
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString(undefined, { timeZoneName: 'short' })
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
