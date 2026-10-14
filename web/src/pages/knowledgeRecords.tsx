import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../api'
import { Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { CheckIcon, CloseIcon, UnlinkIcon } from '../components/icons'
import { Pager, usePageInAddress } from '../components/pager'
import { useDebounced } from '../components/preview'
import { SettingsRow, SettingsSection } from '../components/settingsList'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import {
  LINK_RECORD,
  LIST_RECORD_LINKS,
  RecordKindIcon,
  SET_NODE_CONTACT,
  UNLINK_RECORD,
  canOpen,
  groupLinksByKind,
  isProposed,
  recordLinkDate,
  useRecordKindLabel,
  useRecordRelationLabel,
  type RecordLink,
} from '../memoryLinks'

// The records a memory page is about, on the page.
//
// A page is the agent's writing; the person's own records (their contact
// card, mail, events, transactions, recordings) are joined to it by links the
// agent made, or only guessed, or the person made. Here they are grouped by
// kind, each opening the record. A guess says so and can be confirmed or
// taken away; a link the person took away is not made again.

// LINKS_PER_PAGE is how many links the page read carries, and so how many a
// part of the list holds.
const LINKS_PER_PAGE = 50

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

export function LinkedRecords({
  path,
  kind,
  contactId,
  initialLinks,
  linkCount,
  onChanged,
  onDone,
  onFailed,
}: {
  path: string
  // The page's own kind: only a page about a person is offered a contact card.
  kind: string
  contactId?: string
  // The first LINKS_PER_PAGE links, as the page read them; linkCount is how many there are.
  initialLinks: RecordLink[]
  linkCount: number
  onChanged: () => void
  onDone: (said: string) => void
  onFailed: (caught: unknown) => void
}) {
  const { t } = useTranslation()
  const kindLabel = useRecordKindLabel()
  const relationLabel = useRecordRelationLabel()
  const listing = usePageInAddress({
    pageParameter: 'links',
    pageSizeParameter: 'linkRows',
    defaultPageSize: LINKS_PER_PAGE,
  })
  const [removing, setRemoving] = useState<RecordLink | null>(null)
  const [isLinkingContact, setIsLinkingContact] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState('')

  // The first part comes with the page. A later part is asked for, and asked
  // again when the page was read again after a change.
  const isLaterPart = linkCount > LINKS_PER_PAGE && listing.offset > 0
  const laterPart = useQuery(
    () =>
      isLaterPart
        ? graphql<{ ListAgentRecordLinks: { recordLinks: RecordLink[] } }>(LIST_RECORD_LINKS, {
            path,
            first: LINKS_PER_PAGE,
            offset: listing.offset,
          })
        : Promise.resolve(null),
    [path, isLaterPart, listing.offset, linkCount, initialLinks.map((link) => link.id).join(',')],
    { refresh: false },
  )
  const laterLinks = laterPart.data?.ListAgentRecordLinks.recordLinks
  const links = useMemo(
    () => (isLaterPart ? (laterLinks ?? []) : initialLinks),
    [isLaterPart, laterLinks, initialLinks],
  )
  const groups = useMemo(() => groupLinksByKind(links), [links])
  const canLinkContact = kind === 'person'

  async function act(run: () => Promise<unknown>, said: string): Promise<boolean> {
    setIsBusy(true)
    setProblem('')
    try {
      await run()
      onDone(said)
      onChanged()
      return true
    } catch (caught) {
      setProblem(messageOf(caught))
      onFailed(caught)
      return false
    } finally {
      setIsBusy(false)
    }
  }

  const reference = (link: RecordLink) => ({
    path,
    recordKind: link.recordKind,
    recordId: link.recordId,
    passageStartMS: link.recordKind === 'recording_passage' ? link.passageStartMS : null,
    passageEndMS: link.recordKind === 'recording_passage' ? link.passageEndMS : null,
  })
  const confirm = (link: RecordLink) =>
    act(
      () =>
        graphql(LINK_RECORD, {
          ...reference(link),
          recordRelation: link.recordRelation,
          transcriptRunId: link.transcriptRunId || null,
        }),
      t('memoryLinks.confirmed'),
    )
  const remove = (link: RecordLink) =>
    act(
      () => graphql(UNLINK_RECORD, { ...reference(link), recordRelation: link.recordRelation }),
      t('memoryLinks.removed'),
    )

  // A page with nothing linked and no card to name has nothing to say here.
  if (linkCount === 0 && !canLinkContact) return null

  return (
    <SettingsSection
      card
      title={t('knowledge.linkedRecords')}
      description={t('knowledge.linkedRecordsHint')}
      action={
        canLinkContact ? (
          <button type="button" onClick={() => setIsLinkingContact(true)}>
            {t('memoryLinks.linkContact')}
          </button>
        ) : undefined
      }
    >
      {linkCount === 0 ? <p className="muted">{t('knowledge.noLinkedRecords')}</p> : null}
      {groups.map((group) => (
        <div key={group.recordKind} className="linked-records-group">
          <h4 className="linked-records-kind">
            <RecordKindIcon recordKind={group.recordKind} size={16} />
            {kindLabel(group.recordKind)}
            <span className="muted">{group.links.length}</span>
          </h4>
          {group.links.map((link) => {
            const date = recordLinkDate(link)
            const title = link.isUnavailable
              ? t('memoryLinks.unavailable')
              : link.recordTitle || t('memoryLinks.untitled')
            return (
              <SettingsRow
                key={link.id}
                title={
                  canOpen(link) ? (
                    <Link className="linked-record-title" to={link.dashboardPath}>
                      {title}
                    </Link>
                  ) : (
                    <span className={link.isUnavailable ? 'muted' : undefined}>{title}</span>
                  )
                }
                badge={isProposed(link) ? <Tag value={t('memoryLinks.perhaps')} tone="warn" /> : null}
                subtitle={[relationLabel(link.recordRelation), date ? date.toLocaleDateString() : '']
                  .filter(Boolean)
                  .join(' · ')}
                actions={
                  <div className="row-actions">
                    {isProposed(link) ? (
                      <Tooltip label={t('memoryLinks.confirm')}>
                        <button
                          type="button"
                          className="icon-action"
                          disabled={isBusy}
                          aria-label={`${title}: ${t('memoryLinks.confirm')}`}
                          onClick={() => void confirm(link)}
                        >
                          <CheckIcon size={16} />
                        </button>
                      </Tooltip>
                    ) : null}
                    <Tooltip label={t('memoryLinks.remove')}>
                      <button
                        type="button"
                        className="icon-action danger"
                        disabled={isBusy}
                        aria-label={`${title}: ${t('memoryLinks.remove')}`}
                        // A guess is taken away at once; a link somebody stated asks first.
                        onClick={() => (isProposed(link) ? void remove(link) : setRemoving(link))}
                      >
                        {isProposed(link) ? <CloseIcon size={16} /> : <UnlinkIcon size={16} />}
                      </button>
                    </Tooltip>
                  </div>
                }
              />
            )
          })}
        </div>
      ))}
      {linkCount > LINKS_PER_PAGE ? (
        <div className="table-bar">
          <Pager
            pageIndex={listing.pageIndex}
            pageSize={listing.pageSize}
            shownCount={links.length}
            totalCount={linkCount}
            onPageIndex={listing.setPageIndex}
          />
        </div>
      ) : null}

      {removing ? (
        <ConfirmDialog
          title={t('memoryLinks.remove')}
          body={t('memoryLinks.removeBody', { title: removing.recordTitle || kindLabel(removing.recordKind) })}
          confirmLabel={t('memoryLinks.remove')}
          busy={isBusy}
          error={problem}
          onClose={() => setRemoving(null)}
          onConfirm={async () => {
            await remove(removing)
            setRemoving(null)
          }}
        />
      ) : null}

      {isLinkingContact ? (
        <LinkContactDialog
          path={path}
          currentContactId={contactId ?? ''}
          busy={isBusy}
          error={problem}
          onClose={() => setIsLinkingContact(false)}
          onSubmit={async (chosenContactId) => {
            const isDone = await act(
              () => graphql(SET_NODE_CONTACT, { path, contactId: chosenContactId || null }),
              chosenContactId ? t('memoryLinks.contactLinked') : t('memoryLinks.contactCleared'),
            )
            if (isDone) setIsLinkingContact(false)
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

type Book = { id: string; name: string }
type ContactChoice = { id: string; name?: string; organization?: string; emails: string[] }

const BOOKS = `query { ListAddressBooks { id name } }`
const CONTACTS = `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) { id name organization emails }
}`

// CONTACT_CHOICES is how many cards the picker lists per address book: more
// than fits without scrolling, few enough that the search is how a card is
// found in a big book.
const CONTACT_CHOICES = 20

// contactLabel is a card as the picker names it: the name, else the first address.
export function contactLabel(contact: ContactChoice): string {
  return contact.name || contact.emails[0] || contact.organization || contact.id
}

// LinkContactDialog names the card a person's page is about, from the cards
// in the person's own address books. It never makes a card, and the card is
// read by the agent only if the book is shared with it.
function LinkContactDialog({
  path,
  currentContactId,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  path: string
  currentContactId: string
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (contactId: string) => void
}) {
  const { t } = useTranslation()
  const [typed, setTyped] = useState('')
  const [chosen, setChosen] = useState(currentContactId)
  const searchText = useDebounced(typed.trim(), 250)
  const choices = useQuery(
    async () => {
      const books = (await graphql<{ ListAddressBooks: Book[] }>(BOOKS)).ListAddressBooks
      const found = await Promise.all(
        books.map((book) =>
          graphql<{ ListContacts: ContactChoice[] }>(CONTACTS, {
            addressBookId: book.id,
            query: searchText || null,
            first: CONTACT_CHOICES,
          }),
        ),
      )
      return found.flatMap((answer) => answer.ListContacts)
    },
    [path, searchText],
    { refresh: false },
  )
  const contacts = choices.data ?? []

  return (
    <FormDialog
      title={t('memoryLinks.linkContactTitle')}
      submitLabel={t('memoryLinks.linkContact')}
      busy={busy}
      error={error || (choices.error ? messageOf(choices.error) : null)}
      canSubmit={chosen !== '' && chosen !== currentContactId}
      otherAction={
        currentContactId ? (
          <button type="button" className="danger" disabled={busy} onClick={() => onSubmit('')}>
            {t('memoryLinks.clearContact')}
          </button>
        ) : undefined
      }
      onClose={onClose}
      onSubmit={() => onSubmit(chosen)}
    >
      <p className="muted">{t('memoryLinks.linkContactHint')}</p>
      <label>
        {t('memoryLinks.contactSearch')}
        <input
          type="search"
          value={typed}
          placeholder={t('memoryLinks.contactSearchPlaceholder')}
          onChange={(event) => setTyped(event.target.value)}
        />
      </label>
      <div className="contact-picker" role="radiogroup" aria-label={t('memoryLinks.contactSearch')}>
        {contacts.length === 0 && !choices.loading ? <p className="muted">{t('memoryLinks.noContacts')}</p> : null}
        {contacts.map((contact) => (
          <label key={contact.id} className="checkbox contact-picker-row">
            <input
              type="radio"
              name="linked-contact"
              checked={chosen === contact.id}
              onChange={() => setChosen(contact.id)}
            />
            <span>
              {contactLabel(contact)}
              {contact.emails[0] && contactLabel(contact) !== contact.emails[0] ? (
                <span className="muted"> {contact.emails[0]}</span>
              ) : null}
            </span>
          </label>
        ))}
      </div>
    </FormDialog>
  )
}
