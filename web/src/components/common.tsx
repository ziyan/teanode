import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { Key, useTranslation } from '../i18n/i18n'


export function Tag({ value, tone }: { value: string; tone?: 'good' | 'bad' | 'warn' }) {
  return <span className={tone ? `tag ${tone}` : 'tag'}>{value}</span>
}

// toneFor maps a verdict onto a colour. Everything an authentication check can
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

export function ErrorMessage({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error)
  return <p className="error">{message}</p>
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
// one of them showed it as grey text, which is exactly what the domain's own
// page is one click away from answering.
//
// A domain that has since been deleted is still named in the mail it received,
// and there is nowhere for it to go: that is said rather than linked.
export function DomainLink({
  domainId,
  names,
}: {
  domainId?: string
  names: Map<string, string>
}) {
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
// Every one is translated, and anything unrecognised falls through as itself
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
