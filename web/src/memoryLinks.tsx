import { Link } from 'react-router-dom'

import { graphql } from './api'
import { Tag } from './components/common'
import {
  CalendarIcon,
  FlagIcon,
  MailIcon,
  MicrophoneIcon,
  QuoteIcon,
  ReceiptIcon,
  UserIcon,
  WalletIcon,
} from './components/icons'
import { useQuery } from './components/useQuery'
import { useTranslation } from './i18n/i18n'

// How the person's own records appear in the agent's memory.
//
// A memory page is joined to the records it is about by links of a kind and
// an identifier (a contact card, a mail message, an event, a reminder, a
// message of a conversation, a finance transaction or receipt, a passage of a
// recording). The records are not copied: a link names the record, and the
// server finds it again, with its title and where it opens, each time.
// This file is the one place the dashboard knows what a link looks like, so
// that a page of memory and every record view that says "In memory" agree.

// The kinds of record, in the order a page lists them. The order is the
// server's too (the first is the most personal).
export const RECORD_KINDS = [
  'contact',
  'mail',
  'calendar_event',
  'reminder',
  'conversation_message',
  'finance_transaction',
  'finance_receipt',
  'recording_passage',
] as const

export type RecordKind = (typeof RECORD_KINDS)[number]

export function isRecordKind(value: string): value is RecordKind {
  return (RECORD_KINDS as readonly string[]).includes(value)
}

// A link as the server returns it. The three fields about the record are read
// when the link is, never stored: a record that is gone has isUnavailable and
// nothing else to say.
export type RecordLink = {
  id: string
  nodePath: string
  nodeName: string
  nodeKind: string
  recordKind: string
  recordId: string
  recordRelation: string
  linkStatus: string
  passageStartMS: number
  passageEndMS: number
  transcriptRunId: string
  note: string
  happenedAt?: string | null
  createdAt: string
  recordTitle: string
  recordHappenedAt?: string | null
  dashboardPath: string
  isUnavailable: boolean
}

// RECORD_LINK_FIELDS is what a query selects of a link.
export const RECORD_LINK_FIELDS =
  'id nodePath nodeName nodeKind recordKind recordId recordRelation linkStatus passageStartMS passageEndMS transcriptRunId note happenedAt createdAt recordTitle recordHappenedAt dashboardPath isUnavailable'

const READ_RECORD_MEMORY_LINKS = `query ($recordKind: String!, $recordId: String!, $passageStartMS: Int64, $passageEndMS: Int64) {
  ReadRecordMemoryLinks(recordKind: $recordKind, recordId: $recordId, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS) { ${RECORD_LINK_FIELDS} }
}`

export const LIST_RECORD_LINKS = `query ($path: String!, $first: Int, $offset: Int) {
  ListAgentRecordLinks(path: $path, first: $first, offset: $offset) {
    recordLinks { ${RECORD_LINK_FIELDS} }
    recordLinkCount nextOffset
  }
}`

export const LINK_RECORD = `mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String!, $passageStartMS: Int64, $passageEndMS: Int64, $transcriptRunId: String) {
  LinkAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS, transcriptRunId: $transcriptRunId) { id }
}`

export const UNLINK_RECORD = `mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String, $passageStartMS: Int64, $passageEndMS: Int64) {
  UnlinkAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS)
}`

export const SET_NODE_CONTACT = `mutation ($path: String!, $contactId: String) {
  SetAgentNodeContact(path: $path, contactId: $contactId)
}`

// A link group: the links of one kind, in the order they came.
export type RecordLinkGroup = { recordKind: string; links: RecordLink[] }

// groupLinksByKind gathers links under their kind, kinds in RECORD_KINDS order
// and a kind this build does not know after them, so a newer server's kind is
// still listed rather than dropped.
export function groupLinksByKind(links: RecordLink[]): RecordLinkGroup[] {
  const groups = new Map<string, RecordLink[]>()
  for (const link of links) {
    groups.set(link.recordKind, [...(groups.get(link.recordKind) ?? []), link])
  }
  const known = RECORD_KINDS.filter((recordKind) => groups.has(recordKind))
  const unknown = [...groups.keys()].filter((recordKind) => !isRecordKind(recordKind))
  return [...known, ...unknown].map((recordKind) => ({ recordKind, links: groups.get(recordKind) ?? [] }))
}

// isProposed says the agent only guessed this link; the person confirms it or
// takes it away.
export function isProposed(link: RecordLink): boolean {
  return link.linkStatus === 'proposed'
}

// recordLinkDate is when the record happened, as the server found it, else
// when the link says it did.
export function recordLinkDate(link: RecordLink): Date | null {
  const written = link.recordHappenedAt || link.happenedAt
  if (!written) return null
  const date = new Date(written)
  return Number.isNaN(date.getTime()) ? null : date
}

// canOpen says the dashboard can open the record: there is a path in this
// dashboard and the record is still there.
export function canOpen(link: RecordLink): boolean {
  return !link.isUnavailable && link.dashboardPath.startsWith('/')
}

// referenceOf is the record's address as the command line writes it:
// kind:id, and for a passage of a recording the range in milliseconds.
export function referenceOf(link: Pick<RecordLink, 'recordKind' | 'recordId' | 'passageStartMS' | 'passageEndMS'>): string {
  const reference = `${link.recordKind}:${link.recordId}`
  return link.recordKind === 'recording_passage' ? `${reference}@${link.passageStartMS}-${link.passageEndMS}` : reference
}

// memoryPathOf is where a memory page opens.
export function memoryPathOf(nodePath: string): string {
  return `/knowledge/${nodePath}`
}

// RecordKindIcon is the mark of a kind of record, on a row and on a node of
// the graph's drawing.
export function RecordKindIcon({ recordKind, size = 16 }: { recordKind: string; size?: number }) {
  switch (recordKind) {
    case 'contact':
      return <UserIcon size={size} />
    case 'mail':
      return <MailIcon size={size} />
    case 'calendar_event':
      return <CalendarIcon size={size} />
    case 'reminder':
      return <FlagIcon size={size} />
    case 'conversation_message':
      return <QuoteIcon size={size} />
    case 'finance_transaction':
      return <WalletIcon size={size} />
    case 'finance_receipt':
      return <ReceiptIcon size={size} />
    case 'recording_passage':
      return <MicrophoneIcon size={size} />
    default:
      return <QuoteIcon size={size} />
  }
}

// useRecordKindLabel is the name of a kind of record.
export function useRecordKindLabel(): (recordKind: string) => string {
  const { t } = useTranslation()
  return (recordKind) =>
    isRecordKind(recordKind) ? t(`memoryLinks.kind.${recordKind}` as 'memoryLinks.kind.contact') : recordKind
}

// useRecordRelationLabel is what a page is to a record, in a word or two.
export function useRecordRelationLabel(): (recordRelation: string) => string {
  const { t } = useTranslation()
  return (recordRelation) =>
    (RECORD_RELATIONS as readonly string[]).includes(recordRelation)
      ? t(`memoryLinks.relation.${recordRelation}` as 'memoryLinks.relation.about')
      : recordRelation
}

export const RECORD_RELATIONS = [
  'is_contact',
  'wrote',
  'received',
  'attended',
  'organized',
  'spoke_in',
  'mentioned_in',
  'paid',
  'paid_by',
  'receipt_for',
  'about',
] as const

// useRecordMemoryLinks reads the pages linked to one record. For a passage of
// a recording the range narrows it to the passages that overlap it.
export function useRecordMemoryLinks(
  recordKind: string,
  recordId: string,
  passageStartMS?: number,
  passageEndMS?: number,
  isEnabled = true,
) {
  return useQuery(
    () =>
      isEnabled && recordId
        ? graphql<{ ReadRecordMemoryLinks: RecordLink[] }>(READ_RECORD_MEMORY_LINKS, {
            recordKind,
            recordId,
            passageStartMS: passageStartMS ?? null,
            passageEndMS: passageEndMS ?? null,
          })
        : Promise.resolve(null),
    [recordKind, recordId, passageStartMS, passageEndMS, isEnabled],
    { refresh: false },
  )
}

// pagesOf is the distinct pages that link to a record, the first link of each
// deciding what it is called and whether it is only a guess.
export function pagesOf(links: RecordLink[]): RecordLink[] {
  const seen = new Map<string, RecordLink>()
  for (const link of links) {
    const earlier = seen.get(link.nodePath)
    if (!earlier || (isProposed(earlier) && !isProposed(link))) seen.set(link.nodePath, link)
  }
  return [...seen.values()]
}

// MemoryPageLinks are the pages of memory a record is linked to, as links to
// them: what the page is to the record in the tooltip, and a guess marked as
// one.
export function MemoryPageLinks({ links }: { links: RecordLink[] }) {
  const { t } = useTranslation()
  const relationLabel = useRecordRelationLabel()
  return (
    <ul className="in-memory-pages">
      {pagesOf(links).map((link) => (
        <li key={link.nodePath}>
          <Link
            className="in-memory-page"
            to={memoryPathOf(link.nodePath)}
            title={`${relationLabel(link.recordRelation)}${isProposed(link) ? `, ${t('memoryLinks.perhaps')}` : ''}`}
          >
            {link.nodeName || link.nodePath}
          </Link>
          {isProposed(link) ? <Tag value={t('memoryLinks.perhaps')} tone="warn" /> : null}
        </li>
      ))}
    </ul>
  )
}

// InMemory says which pages of the agent's memory a record is linked to. It
// draws nothing while it reads, nothing when it fails (the record is still
// the thing being looked at), and nothing when no page is linked, so a record
// the agent has not met carries no empty heading.
export function InMemory({
  recordKind,
  recordId,
  passageStartMS,
  passageEndMS,
}: {
  recordKind: string
  recordId: string
  passageStartMS?: number
  passageEndMS?: number
}) {
  const { t } = useTranslation()
  const memory = useRecordMemoryLinks(recordKind, recordId, passageStartMS, passageEndMS)
  const links = memory.data?.ReadRecordMemoryLinks ?? []
  if (links.length === 0) return null
  return (
    <section className="in-memory" aria-label={t('memoryLinks.inMemory')}>
      <h4>{t('memoryLinks.inMemory')}</h4>
      <MemoryPageLinks links={links} />
    </section>
  )
}
