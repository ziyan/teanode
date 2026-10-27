import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { formatAbsolute, formatRelative, hasTime } from '../components/relativeTime'
import { SettingsEmpty } from '../components/settingsList'
import { useIsDesktop } from '../components/sidebar'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { photoSmallPreviewPath } from '../photos/api'
import {
  FIND_MAIL_FOLDER,
  isSearchArea,
  resultPath,
  SEARCH_AREAS,
  SEARCH_EVERYTHING,
  SEARCH_PER_AREA_LIMIT,
  searchPath,
  showAllPath,
  type SearchArea,
  type SearchEverythingAnswer,
  type SearchResult,
} from '../searchEverything'

const AREA_NAMES: Record<SearchArea, Key> = {
  chats: 'search.area.chats',
  mail: 'search.area.mail',
  contacts: 'search.area.contacts',
  photos: 'search.area.photos',
  memory: 'search.area.memory',
  documents: 'search.area.documents',
  recordings: 'search.area.recordings',
  places: 'search.area.places',
  finance: 'search.area.finance',
}

// SearchPage is what the search box at the top of the rail found: one group
// for each area that had something, in a fixed order, each result a row that
// opens the thing where it lives, and a button to the area's own page with
// the same words when there is more. The words are in the address (?q=).
export function SearchPage() {
  const { t, language } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const isDesktop = useIsDesktop()
  const [parameters] = useSearchParams()
  const words = (parameters.get('q') ?? '').trim()
  const [typed, setTyped] = useState(words)
  useEffect(() => setTyped(words), [words])
  const [openingKey, setOpeningKey] = useState('')

  const found = useQuery(
    () =>
      words === ''
        ? Promise.resolve(null)
        : graphql<{ SearchEverything: SearchEverythingAnswer }>(SEARCH_EVERYTHING, {
            words,
            perAreaLimit: SEARCH_PER_AREA_LIMIT,
          }),
    [words],
    { refresh: false },
  )
  const answer = found.data?.SearchEverything
  const groups = (answer?.areas ?? [])
    .filter((group) => isSearchArea(group.searchArea) && group.results.length > 0)
    .sort(
      (first, second) =>
        SEARCH_AREAS.indexOf(first.searchArea as SearchArea) - SEARCH_AREAS.indexOf(second.searchArea as SearchArea),
    )

  // An area that failed or timed out is said once, as a failure is anywhere
  // on the dashboard; the groups that did answer stay.
  useEffect(() => {
    const failedAreas = (answer?.failedAreas ?? [])
      .filter(isSearchArea)
      .sort((first, second) => SEARCH_AREAS.indexOf(first) - SEARCH_AREAS.indexOf(second))
    if (failedAreas.length === 0) return
    toast.failed(t('search.failedAreas', { areas: failedAreas.map((area) => t(AREA_NAMES[area])).join(', ') }))
    // The toast and the translation are made again on every render; an answer
    // is told once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [answer])

  const open = async (area: SearchArea, result: SearchResult) => {
    const path = resultPath(area, result.targetId, words)
    if (path !== null) {
      void navigate(path)
      return
    }
    // A mail result names the message, not its folder, and a message is
    // opened in the folder it is in.
    const key = `${area}:${result.targetId}`
    setOpeningKey(key)
    try {
      const opened = await graphql<{ GetMailboxItem: { id: string; folderId: string } }>(FIND_MAIL_FOLDER, {
        itemId: result.targetId,
      })
      void navigate(
        `/mailbox/${encodeURIComponent(opened.GetMailboxItem.folderId)}/${encodeURIComponent(opened.GetMailboxItem.id)}`,
      )
    } catch (failure) {
      toast.failure(failure, t('search.openFailed'))
      void navigate(showAllPath(area, words))
    } finally {
      setOpeningKey('')
    }
  }

  return (
    <div className="search-page">
      {/* On a phone the rail's box is behind the menu button; the page has
          its own so the words can be changed where they are read. */}
      {!isDesktop && (
        <form
          className="search-page-form"
          role="search"
          onSubmit={(event) => {
            event.preventDefault()
            const next = typed.trim()
            if (next !== '' && next !== words) void navigate(searchPath(next))
          }}
        >
          <input
            type="search"
            value={typed}
            placeholder={t('search.placeholder')}
            aria-label={t('search.title')}
            onChange={(event) => setTyped(event.target.value)}
          />
        </form>
      )}
      {words === '' ? (
        <SettingsEmpty>{t('search.hint')}</SettingsEmpty>
      ) : found.loading && answer?.words !== words ? (
        // The answer kept from the words before is not shown under new ones,
        // whose Show all and documents rows would open the new words.
        <Loading />
      ) : found.error ? (
        <ErrorMessage error={found.error} />
      ) : groups.length === 0 ? (
        <SettingsEmpty>{t('search.nothingFound', { words })}</SettingsEmpty>
      ) : (
        groups.map((group) => {
          const area = group.searchArea as SearchArea
          const name = t(AREA_NAMES[area])
          return (
            <section key={area} className="card search-group" aria-label={name}>
              <h3>{name}</h3>
              <div className="search-results">
                {group.results.map((result) => {
                  const key = `${area}:${result.targetId}`
                  return (
                    <button
                      key={key}
                      type="button"
                      className={area === 'photos' ? 'search-result search-result-with-photo' : 'search-result'}
                      disabled={openingKey === key}
                      onClick={() => void open(area, result)}
                    >
                      {area === 'photos' && <SearchPhotoThumbnail photoId={result.targetId} />}
                      <span className="search-result-text">
                        <span className="search-result-title">{result.titleText || t('search.untitled')}</span>
                        {result.detailText && <span className="search-result-detail muted">{result.detailText}</span>}
                      </span>
                      {hasTime(result.happenedAt) && (
                        <time
                          className="search-result-time muted"
                          dateTime={new Date(result.happenedAt!).toISOString()}
                          title={formatAbsolute(new Date(result.happenedAt!))}
                        >
                          {formatRelative(new Date(result.happenedAt!), language)}
                        </time>
                      )}
                    </button>
                  )
                })}
              </div>
              {group.hasMore && (
                <div className="page-actions page-actions-end">
                  <Link className="button" to={showAllPath(area, words)}>
                    {t('search.showAll', { area: name })}
                  </Link>
                </div>
              )}
            </section>
          )
        })
      )}
    </div>
  )
}

// SearchPhotoThumbnail is the picture of a photo found, before its name: a
// file name and a date say little about which photo it is. A photo with no
// preview yet keeps the square, empty.
function SearchPhotoThumbnail({ photoId }: { photoId: string }) {
  const [isBroken, setIsBroken] = useState(false)
  if (isBroken) return <span className="search-result-photo" aria-hidden="true" />
  return (
    <img
      className="search-result-photo"
      src={photoSmallPreviewPath(photoId)}
      alt=""
      loading="lazy"
      draggable={false}
      onError={() => setIsBroken(true)}
    />
  )
}
