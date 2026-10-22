import { useMemo, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { graphql, isNotFound, openAgentConversation } from '../api'
import { OPEN_DIRECT_CHAT } from '../components/agentChat'
import { Avatar } from '../components/avatar'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { contactPhotoAddress, useContactEditor } from '../components/contactEditor'
import { LinkContactDialog } from '../components/contactLinkDialog'
import { ErrorMessage, Loading, formatMoney } from '../components/common'
import { PencilIcon } from '../components/icons'
import { dayKeyOf, placeTitle, visitDurationText, type PlaceVisit } from '../components/locationData'
import { PlaceLabelIcon } from '../components/placeLabelIcon'
import { personName } from '../components/personLabel'
import { formatAbsolute, formatRelative, hasTime, RelativeTime } from '../components/relativeTime'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { personPhotosAddress } from '../lib/circlePeople'
import {
  composeToAddressPath,
  isSectionDrawn,
  mailFromAddressPath,
  personFinancePath,
  personHealthPath,
  personLocationPath,
  READ_PERSON_CARD,
  type PersonCard,
  type PersonCardMail,
  type PersonCardSectionState,
} from '../lib/personCard'
import { relationshipLabel, relationshipWord } from '../lib/relationships'
import type { Photo } from '../photos/api'
import { FIND_MAIL_FOLDER } from '../searchEverything'
import { hasPermission, useSession } from '../session'
import { PhotoImage, isPhotoPreviewPending } from './photos/grid'
import { usePhotoAltText } from './photos/altText'
import { amountText, dayText, parseDay, summaryValue, typeName } from './health/healthApi'
import { GET_CURRENT_PRONOUN, RelationshipDialog, useAct } from './circles'

// A person's card: who somebody is to the signed-in person, and a few items of
// everything of theirs that person can see elsewhere, each with a button into
// the part of the app it is taken from. It is of an account that shares a
// circle with the signed-in person (/people/<userId>), or of one of their own
// contacts (/people/contact/<addressBookId>/<contactId>), which may be the same
// person. One query fills it; each section says whether it was read, is not
// shared, needs a contact linked, or failed, and the rest still show.

const CIRCLE_KIND_LABELS: Record<string, Key> = {
  family: 'circles.kind.family',
  friends: 'circles.kind.friends',
  colleagues: 'circles.kind.colleagues',
}

export function PersonPage() {
  const { t } = useTranslation()
  const parameters = useParams()
  const userId = parameters.userId ?? ''
  const addressBookId = parameters.addressBookId ?? ''
  const contactId = parameters.contactId ?? ''

  const card = useQuery(
    async () =>
      (
        await graphql<{ ReadPersonCard: PersonCard }>(READ_PERSON_CARD, {
          userId: userId || null,
          addressBookId: addressBookId || null,
          contactId: contactId || null,
          itemLimit: null,
        })
      ).ReadPersonCard,
    [userId, addressBookId, contactId],
    { refresh: false },
  )

  const person = card.data?.person ?? null
  const contact = card.data?.contact ?? null
  const displayName = person ? personName(person) : contact?.name?.trim() || contact?.emails[0] || ''
  useBreadcrumbDetail(displayName)

  if (card.loading && !card.data) {
    return <Loading />
  }
  if (!card.data) {
    // A person who shares no circle with the caller is answered not found, as
    // a circle somebody is not in is, and said as such rather than as a failure.
    return (
      <>
        {card.error && !isNotFound(card.error) ? <ErrorMessage error={card.error} /> : null}
        <SettingsEmpty>{t(card.error && !isNotFound(card.error) ? 'person.failed' : 'person.notFound')}</SettingsEmpty>
        <p>
          <Link className="button" to="/circles">
            {t('circles.title')}
          </Link>
        </p>
      </>
    )
  }
  return (
    <PersonCardView
      card={card.data}
      displayName={displayName}
      addressBookId={addressBookId}
      reload={async () => {
        await card.reload()
      }}
    />
  )
}

function PersonCardView({
  card,
  displayName,
  addressBookId,
  reload,
}: {
  card: PersonCard
  displayName: string
  // The book the route names, for the contact's form; empty on an account's page.
  addressBookId: string
  reload: () => Promise<void>
}) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const session = useSession()
  const { person, contact } = card
  const [isOpeningChat, setOpeningChat] = useState(false)
  const [isLinkingContact, setLinkingContact] = useState(false)
  const [isRelating, setRelating] = useState(false)
  const [openingMailId, setOpeningMailId] = useState('')

  const canUseContacts =
    session.services?.isContactsEnabled === true && hasPermission(session.permissions, 'contacts:use')
  const canWriteMail = session.services?.isMailboxEnabled !== false && hasPermission(session.permissions, 'mail:read')
  const firstEmail = contact?.emails[0] ?? ''
  const isOwnCard = person !== null && person.userId === session.userId

  // The book the contact is in, which on an account's card is whichever of
  // the person's books holds the contact linked to it.
  const editor = useContactEditor({
    addressBookId: contact?.addressBookId || addressBookId || undefined,
    onChanged: reload,
  })
  const { act, isBusy, problem, clearProblem } = useAct(reload)

  const timeFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { hour: 'numeric', minute: '2-digit' }),
    [language],
  )
  const dayTimeFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }),
    [language],
  )
  const dayFormat = useMemo(() => new Intl.DateTimeFormat(language, { month: 'short', day: 'numeric' }), [language])
  const timeOf = (moment: string) => timeFormat.format(new Date(moment))
  // An arrival with its day when it was not today, so a visit begun days ago
  // does not read as this evening's.
  const arrivalOf = (moment: string) =>
    dayKeyOf(moment) === dayKeyOf(new Date()) ? timeOf(moment) : dayTimeFormat.format(new Date(moment))
  const rangeOf = (visit: PlaceVisit): string => {
    if (visit.arrivedAt && visit.departedAt) {
      return t('location.timeRange', { from: timeOf(visit.arrivedAt), until: timeOf(visit.departedAt) })
    }
    if (visit.arrivedAt) return t('location.timeSince', { from: timeOf(visit.arrivedAt) })
    if (visit.departedAt) return t('location.timeUntil', { until: timeOf(visit.departedAt) })
    return ''
  }

  const openChat = async () => {
    if (!person) return
    setOpeningChat(true)
    try {
      const answer = await graphql<{ OpenDirectChat: { id: string } }>(OPEN_DIRECT_CHAT, { userId: person.userId })
      if (!openAgentConversation(answer.OpenDirectChat.id)) toast.failed(t('person.messageFailed'))
    } catch (failure) {
      toast.failure(failure, t('person.messageFailed'))
    } finally {
      setOpeningChat(false)
    }
  }

  // A mail is opened in the folder it is in, which the card names only as a
  // path, so the folder is asked for by the message, as a search result is.
  const openMail = async (mail: PersonCardMail) => {
    setOpeningMailId(mail.mailboxItemId)
    try {
      const opened = await graphql<{ GetMailboxItem: { id: string; folderId: string } }>(FIND_MAIL_FOLDER, {
        itemId: mail.mailboxItemId,
      })
      void navigate(
        `/mailbox/${encodeURIComponent(opened.GetMailboxItem.folderId)}/${encodeURIComponent(opened.GetMailboxItem.id)}`,
      )
    } catch (failure) {
      toast.failure(failure, t('search.openFailed'))
    } finally {
      setOpeningMailId('')
    }
  }

  const relationship =
    person && person.relationshipKind
      ? t('person.yourRelationship', {
          word: relationshipWord(t, person.relationshipKind, person.relationshipTermGender, person.relationshipWord),
        })
      : ''
  const canLinkContact = person !== null && contact === null && canUseContacts

  // What a section says in the place of its rows when it has none to show.
  const stateText = (state: PersonCardSectionState, notSharedText: string): React.ReactNode => {
    switch (state) {
      case 'notShared':
        return <PersonEmpty>{notSharedText}</PersonEmpty>
      case 'needsContact':
        return (
          <>
            <PersonEmpty>{t('person.needsContact', { name: displayName })}</PersonEmpty>
            {canLinkContact && (
              <div className="page-actions page-actions-end">
                <button type="button" onClick={() => setLinkingContact(true)}>
                  {t('person.linkContact')}
                </button>
              </div>
            )}
          </>
        )
      case 'failed':
        return <PersonEmpty>{t('person.sectionFailed')}</PersonEmpty>
      default:
        return null
    }
  }

  const showAll = (to: string, label = t('person.showAll')) => (
    <div className="page-actions page-actions-end">
      <Link className="button" to={to}>
        {label}
      </Link>
    </div>
  )

  const { location, health, finance, photos, mail, recordings, memory } = card
  // A circle's kind, unless its name already says it ("Family" of kind family).
  const circleKindText = (circle: (typeof card.sharedCircles)[number]) => {
    const kindText = CIRCLE_KIND_LABELS[circle.circleKind]
      ? t(CIRCLE_KIND_LABELS[circle.circleKind])
      : circle.circleKind
    return kindText.trim().toLowerCase() === circle.circleName.trim().toLowerCase() ? '' : kindText
  }
  const todayDay = dayText(new Date())
  const locationView = location?.location ?? null
  const openVisit = locationView?.openVisit ?? null
  // The visit they are in is the first row, so it is not listed again.
  const otherVisits = (location?.todayVisits ?? []).filter((visit) => visit.id !== openVisit?.id)

  return (
    <div className="person-page">
      <section className="card person-head" aria-label={displayName}>
        <div className="person-head-who">
          <Avatar
            name={displayName || '?'}
            avatarUrl={person ? person.avatarUrl : contact?.hasPhoto ? contactPhotoAddress(contact.id) : null}
            colorIndex={person?.avatarColorIndex}
            size={64}
          />
          <div className="person-head-body">
            <p className="person-head-name">{displayName}</p>
            {relationship && <p className="muted">{relationship}</p>}
            {(person?.name?.trim() || contact?.organization) && (
              <p className="muted">
                {[person?.name?.trim() ? person.username : '', contact?.organization ?? ''].filter(Boolean).join(' · ')}
              </p>
            )}
          </div>
        </div>
        <div className="person-head-actions">
          {card.canMessage && !isOwnCard && (
            <button type="button" className="primary" disabled={isOpeningChat} onClick={() => void openChat()}>
              {t('chat.message')}
            </button>
          )}
          {canWriteMail && firstEmail && (
            <Link className="button" to={composeToAddressPath(firstEmail)}>
              {t('person.sendMail')}
            </Link>
          )}
          {contact && canUseContacts && (
            <button
              type="button"
              disabled={editor.isBusy || editor.openingContactId === contact.id}
              onClick={() => void editor.open(contact.id)}
            >
              {t('addressBook.edit')}
            </button>
          )}
          {canLinkContact && (
            <button type="button" onClick={() => setLinkingContact(true)}>
              {t('person.linkContact')}
            </button>
          )}
        </div>
      </section>

      {isSectionDrawn(location) && (
        <section className="card person-section" aria-label={t('location.title')}>
          <h3>{t('location.title')}</h3>
          {location.sectionState === 'shown' ? (
            <>
              <div className="person-row">
                {openVisit && (
                  <span className="person-row-icon">
                    <PlaceLabelIcon placeLabel={openVisit.place.placeLabel} />
                  </span>
                )}
                <span className="person-row-text">
                  <span className="person-row-title">
                    {locationView?.pause && locationView.shareLevel !== 'owner' ? (
                      locationView.pause.endedAt ? (
                        t('location.pausedOtherUntil', { time: arrivalOf(locationView.pause.endedAt) })
                      ) : (
                        t('location.pausedOther')
                      )
                    ) : openVisit ? (
                      t('location.atSince', {
                        place: placeTitle(openVisit.place, t),
                        time: openVisit.arrivedAt ? arrivalOf(openVisit.arrivedAt) : '',
                      })
                    ) : locationView?.latestPoint ? (
                      <>
                        {t('location.lastSeen')} <RelativeTime value={locationView.latestPoint.measuredAt} />
                      </>
                    ) : (
                      t('location.nowhereOther')
                    )}
                  </span>
                  {openVisit && locationView?.latestPoint && (
                    <span className="person-row-detail muted">
                      {t('location.lastSeen')} <RelativeTime value={locationView.latestPoint.measuredAt} />
                    </span>
                  )}
                </span>
              </div>
              {locationView?.shareLevel === 'current' ? (
                <PersonEmpty>{t('location.personSharesNowOnly', { name: displayName })}</PersonEmpty>
              ) : (
                <>
                  {otherVisits.length > 0 && <h4 className="person-subheading">{t('person.visitsToday')}</h4>}
                  {otherVisits.length === 0
                    ? // The visit they are in, begun on an earlier day, is today's
                      // whole story, and is the row above.
                      !openVisit && <PersonEmpty>{t('person.noVisitsToday')}</PersonEmpty>
                    : otherVisits.map((visit) => (
                        <div className="person-row" key={visit.id}>
                          <span className="person-row-icon">
                            <PlaceLabelIcon placeLabel={visit.place.placeLabel} />
                          </span>
                          <span className="person-row-text">
                            <span className="person-row-title">{placeTitle(visit.place, t)}</span>
                            <span className="person-row-detail muted">{rangeOf(visit)}</span>
                          </span>
                          <span className="person-row-meta muted">{visitDurationText(visit, t)}</span>
                        </div>
                      ))}
                </>
              )}
              {person && showAll(personLocationPath(person.userId))}
            </>
          ) : (
            stateText(location.sectionState, t('person.locationNotShared', { name: displayName }))
          )}
        </section>
      )}

      {isSectionDrawn(health) && (
        <section className="card person-section" aria-label={t('health.title')}>
          <h3>{t('health.title')}</h3>
          {health.sectionState === 'shown' ? (
            <>
              {health.healthTypes.length === 0 ? (
                <PersonEmpty>{t('person.noHealth', { name: displayName })}</PersonEmpty>
              ) : (
                health.healthTypes.map((healthType) => {
                  const latest = healthType.latestDaySummary
                  return (
                    <div className="person-row" key={healthType.healthType}>
                      <span className="person-row-text">
                        <span className="person-row-title">
                          {typeName(t, healthType.healthType, healthType.typeName)}
                        </span>
                        {latest && (
                          <span className="person-row-detail muted">
                            {latest.summaryDay === todayDay
                              ? t('person.today')
                              : dayFormat.format(parseDay(latest.summaryDay))}
                          </span>
                        )}
                      </span>
                      <span className="person-row-meta">
                        {latest
                          ? amountText(
                              summaryValue(latest, healthType.summaryKind),
                              healthType.summaryKind,
                              healthType.quantityUnit,
                              healthType.healthType,
                              language,
                              t,
                            )
                          : ''}
                      </span>
                    </div>
                  )
                })
              )}
              {person && health.healthTypes.length > 0 && showAll(personHealthPath(person.userId))}
            </>
          ) : (
            stateText(health.sectionState, t('person.healthNotShared', { name: displayName }))
          )}
        </section>
      )}

      {isSectionDrawn(finance) && (
        <section className="card person-section" aria-label={t('finance.title')}>
          <h3>{t('finance.title')}</h3>
          {finance.sectionState === 'shown' ? (
            <>
              {finance.financeAccounts.map((account) => {
                const usageShare = account.creditUsage?.usageShare
                return (
                  <div className="person-row" key={account.financeAccountId}>
                    <span className="person-row-text">
                      <span className="person-row-title">
                        {account.accountMask ? `${account.accountName} ··${account.accountMask}` : account.accountName}
                      </span>
                      <span className="person-row-detail muted">
                        {t(`finance.shareLevelName.${account.financeShareLevel}` as Key)}
                        {usageShare !== undefined && usageShare !== null
                          ? ` · ${t('person.financeUsed', { percent: `${Math.round(usageShare * 100)}%` })}`
                          : ''}
                      </span>
                    </span>
                    <span className="person-row-meta">
                      {account.currentBalance ? formatMoney(Number(account.currentBalance), account.currencyCode) : ''}
                    </span>
                  </div>
                )
              })}
              {person && finance.hasMore && showAll(personFinancePath(person.userId))}
            </>
          ) : (
            stateText(finance.sectionState, t('person.financeNotShared', { name: displayName }))
          )}
        </section>
      )}

      {photos && (photos.photosState !== 'unavailable' || photos.albumsState !== 'unavailable') && (
        <section className="card person-section" aria-label={t('nav.photos')}>
          <h3>{t('nav.photos')}</h3>
          {photos.photosState !== 'unavailable' && (
            <>
              <h4 className="person-subheading">{t('photos.accountPhotosOf', { name: displayName })}</h4>
              {photos.photosState === 'shown' ? (
                photos.photos.length === 0 ? (
                  <PersonEmpty>{t('person.noPhotos', { name: displayName })}</PersonEmpty>
                ) : (
                  <PersonPhotos photos={photos.photos} />
                )
              ) : (
                stateText(photos.photosState, t('person.notShared', { name: displayName }))
              )}
              {photos.photosState === 'shown' &&
                person &&
                photos.photos.length > 0 &&
                showAll(personPhotosAddress(person.userId))}
            </>
          )}
          {photos.albumsState !== 'unavailable' && (
            <>
              <h4 className="person-subheading">{t('person.albumsShared', { name: displayName })}</h4>
              {photos.albumsState === 'shown' ? (
                photos.albums.length === 0 ? (
                  <PersonEmpty>{t('person.noAlbums', { name: displayName })}</PersonEmpty>
                ) : (
                  photos.albums.map((album) => (
                    <Link className="person-row" key={album.id} to={`/photos/albums/${encodeURIComponent(album.id)}`}>
                      <span className="person-row-cover">
                        {album.coverPreviewUrl && <img src={album.coverPreviewUrl} alt="" loading="lazy" />}
                      </span>
                      <span className="person-row-text">
                        <span className="person-row-title">{album.albumTitle}</span>
                        <span className="person-row-detail muted">
                          {plural(album.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' })}
                        </span>
                      </span>
                    </Link>
                  ))
                )
              ) : (
                stateText(photos.albumsState, t('person.notShared', { name: displayName }))
              )}
              {photos.albumsState === 'shown' &&
                photos.hasMoreAlbums &&
                showAll('/photos/albums', t('person.showAllAlbums'))}
            </>
          )}
        </section>
      )}

      {isSectionDrawn(mail) && (
        <section className="card person-section" aria-label={t('nav.mail')}>
          <h3>{t('nav.mail')}</h3>
          {mail.sectionState === 'shown' ? (
            <>
              {mail.mails.length === 0 ? (
                <PersonEmpty>{t('person.noMail', { name: displayName })}</PersonEmpty>
              ) : (
                mail.mails.map((item) => (
                  <button
                    key={item.mailboxItemId}
                    type="button"
                    className="person-row"
                    disabled={openingMailId === item.mailboxItemId}
                    onClick={() => void openMail(item)}
                  >
                    <span className="person-row-text">
                      <span className="person-row-title">{item.subject || t('mail.noSubject')}</span>
                      <span className="person-row-detail muted">
                        {item.isFromPerson
                          ? t('person.mailFrom', { name: item.fromName || item.fromAddress })
                          : t('person.mailFromYou')}
                      </span>
                      {item.snippet && <span className="person-row-detail muted">{item.snippet}</span>}
                    </span>
                    {hasTime(item.receivedAt) && (
                      <time
                        className="person-row-meta muted"
                        dateTime={new Date(item.receivedAt).toISOString()}
                        title={formatAbsolute(new Date(item.receivedAt))}
                      >
                        {formatRelative(new Date(item.receivedAt), language)}
                      </time>
                    )}
                  </button>
                ))
              )}
              {firstEmail && mail.mails.length > 0 && showAll(mailFromAddressPath(firstEmail))}
            </>
          ) : (
            stateText(mail.sectionState, t('person.notShared', { name: displayName }))
          )}
        </section>
      )}

      {isSectionDrawn(recordings) && (
        <section className="card person-section" aria-label={t('recordings.title')}>
          <h3>{t('recordings.title')}</h3>
          {recordings.sectionState === 'shown' ? (
            recordings.recordings.length === 0 ? (
              <PersonEmpty>{t('person.noRecordings', { name: displayName })}</PersonEmpty>
            ) : (
              recordings.recordings.map((recording) => (
                <Link
                  className="person-row"
                  key={recording.recordingId}
                  to={`/recordings/${encodeURIComponent(recording.recordingId)}`}
                >
                  <span className="person-row-text">
                    <span className="person-row-title">{recording.title || t('recordings.untitled')}</span>
                    <span className="person-row-detail muted">
                      {plural(recording.spokenPassageCount, {
                        one: 'person.speaksInOne',
                        other: 'person.speaksInOther',
                      })}
                    </span>
                  </span>
                  {hasTime(recording.recordedAt) && (
                    <time
                      className="person-row-meta muted"
                      dateTime={new Date(recording.recordedAt!).toISOString()}
                      title={formatAbsolute(new Date(recording.recordedAt!))}
                    >
                      {formatRelative(new Date(recording.recordedAt!), language)}
                    </time>
                  )}
                </Link>
              ))
            )
          ) : (
            stateText(recordings.sectionState, t('person.notShared', { name: displayName }))
          )}
        </section>
      )}

      {isSectionDrawn(memory) && (
        <section className="card person-section" aria-label={t('nav.knowledge')}>
          <h3>{t('nav.knowledge')}</h3>
          {memory.sectionState === 'shown' ? (
            memory.pages.length === 0 ? (
              <PersonEmpty>{t('person.noMemory', { name: displayName })}</PersonEmpty>
            ) : (
              memory.pages.map((page) => (
                <Link
                  className="person-row"
                  key={page.nodePath}
                  to={`/knowledge/${page.nodePath.split('/').map(encodeURIComponent).join('/')}`}
                >
                  <span className="person-row-text">
                    <span className="person-row-title">{page.nodeName}</span>
                    {page.summary && <span className="person-row-detail muted">{page.summary}</span>}
                    {page.isMatchedByName && (
                      <span className="person-row-detail muted">{t('person.matchedByName')}</span>
                    )}
                  </span>
                </Link>
              ))
            )
          ) : (
            stateText(memory.sectionState, t('person.notShared', { name: displayName }))
          )}
        </section>
      )}

      {contact && (
        <section className="card person-section" aria-label={t('person.sectionContact')}>
          <h3>{t('person.sectionContact')}</h3>
          {!contact.organization &&
          contact.emails.length === 0 &&
          contact.phones.length === 0 &&
          contact.addresses.length === 0 ? (
            <PersonEmpty>{t('person.noContactDetails')}</PersonEmpty>
          ) : (
            <dl className="person-details">
              {contact.organization && (
                <div>
                  <dt>{t('addressBook.organization')}</dt>
                  <dd>{contact.organization}</dd>
                </div>
              )}
              {contact.emails.map((email) => (
                <div key={`email-${email}`}>
                  <dt>{t('addressBook.email')}</dt>
                  <dd>
                    <a href={`mailto:${email}`}>{email}</a>
                  </dd>
                </div>
              ))}
              {contact.phones.map((phone) => (
                <div key={`phone-${phone}`}>
                  <dt>{t('addressBook.phone')}</dt>
                  <dd>
                    <a href={`tel:${phone}`}>{phone}</a>
                  </dd>
                </div>
              ))}
              {contact.addresses.map((address) => (
                <div key={`address-${address.written}`}>
                  <dt>{t('addressBook.postal')}</dt>
                  <dd>{address.written}</dd>
                </div>
              ))}
            </dl>
          )}
        </section>
      )}

      {card.sharedCircles.length > 0 && (
        <section className="card person-section" aria-label={t('circles.title')}>
          <h3>{t('circles.title')}</h3>
          {card.sharedCircles.map((circle) => (
            <Link className="person-row" key={circle.circleId} to={`/circles/${encodeURIComponent(circle.circleId)}`}>
              <span className="person-row-text">
                <span className="person-row-title">{circle.circleName}</span>
                {circleKindText(circle) && <span className="person-row-detail muted">{circleKindText(circle)}</span>}
              </span>
            </Link>
          ))}
        </section>
      )}

      {person && !isOwnCard && (
        <SettingsSection card title={t('circles.relationship.action')}>
          <SettingsRow
            title={
              person.relationshipKind
                ? relationshipLabel(t, person.relationshipKind, person.relationshipTermGender, person.relationshipWord)
                : t('person.relationshipUnset')
            }
            actions={
              <Tooltip label={t('circles.relationship.action')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${displayName}: ${t('circles.relationship.action')}`}
                  onClick={() => {
                    clearProblem()
                    setRelating(true)
                  }}
                >
                  <PencilIcon size={16} />
                </button>
              </Tooltip>
            }
          />
        </SettingsSection>
      )}

      {isRelating && person && (
        <PersonRelationshipDialog
          person={person}
          ownUserId={session.userId ?? ''}
          act={act}
          isBusy={isBusy}
          problem={problem}
          onClose={() => setRelating(false)}
        />
      )}

      {isLinkingContact && person && (
        <LinkContactDialog account={person} onLinked={() => void reload()} onClose={() => setLinkingContact(false)} />
      )}

      {editor.dialogs}
    </div>
  )
}

// PersonRelationshipDialog is the Circles page's relationship dialog, with the
// signed-in person's own pronoun asked for here: the dialog asks how they are
// described only when they have not said, and keeps its rule that only the
// pronoun is saved when only the pronoun changed.
function PersonRelationshipDialog({
  person,
  ownUserId,
  act,
  isBusy,
  problem,
  onClose,
}: {
  person: NonNullable<PersonCard['person']>
  ownUserId: string
  act: Parameters<typeof RelationshipDialog>[0]['act']
  isBusy: boolean
  problem: string | null
  onClose: () => void
}) {
  // Null when it could not be read, which asks nothing.
  const ownPerson = useQuery(
    async () =>
      (await graphql<{ GetCurrentUser: { pronoun?: string | null } | null }>(GET_CURRENT_PRONOUN)).GetCurrentUser,
    [],
    { refresh: false },
  )
  const ownPronoun = ownPerson.data ? (ownPerson.data.pronoun ?? '') : null
  return (
    <RelationshipDialog
      member={person}
      ownPronoun={ownPronoun}
      ownUserId={ownUserId}
      act={act}
      isBusy={isBusy}
      problem={problem}
      onPronounSaved={() => void ownPerson.reload()}
      onClose={onClose}
    />
  )
}

// PersonPhotos is the few photos of somebody as thumbnails, each opening in the
// library as a photo in the grid does.
function PersonPhotos({ photos }: { photos: Photo[] }) {
  const { t } = useTranslation()
  const altText = usePhotoAltText()
  return (
    <div className="photo-grid person-photos">
      {photos.map((photo) => (
        <div className="photo-cell" key={photo.id}>
          <Link
            className="photo-open"
            to={`/photos/${encodeURIComponent(photo.id)}`}
            aria-label={t('photos.openPhoto', { name: photo.originalFilename || photo.placeName || t('nav.photos') })}
          >
            {isPhotoPreviewPending(photo) ? (
              <span className="photo-state" title={t('photos.waiting')} role="img" aria-label={t('photos.waiting')}>
                <span className="photo-spinner" />
              </span>
            ) : (
              <PhotoImage src={photo.smallPreviewUrl} alt={altText(photo)} />
            )}
          </Link>
        </div>
      ))}
    </div>
  )
}

/** PersonEmpty is a section with nothing in it, said in one quiet line: the
 * card has many sections, and a box for each empty one would bury the rest. */
function PersonEmpty({ children }: { children: ReactNode }) {
  return <p className="person-empty muted">{children}</p>
}
