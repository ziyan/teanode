import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ErrorMessage, Loading } from '../../components/common'
import { AlbumIcon, CloseIcon, PlusIcon } from '../../components/icons'
import { SettingsEmpty } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  createPhotoAlbum,
  listPhotoAlbums,
  listSuggestionPhotoIds,
  readSamplePreviews,
  suggestPhotoAlbums,
  type PhotoAlbum,
  type PhotoAlbumSuggestion,
} from '../../photos/api'
import { titleNamesPlaces } from '../../photos/albumSuggestion'
import { formatDateRange, personTimeZone } from '../../photos/days'
import { useSession } from '../../session'
import { AlbumTitleDialog, PhotoSections, useCircleChoices, type PhotoSection } from './albumControls'
import { AlbumOwnerMark } from './albumSharing'
import { PhotoToolbar } from './toolbar'

// The Albums page: a grid of the person's albums by their covers, then the
// albums of the circles they are in, then the ones shared with them, and above
// it the trips and events the server found in photos that are in no album.
// Nothing is made from a suggestion until the person chooses it and names it.

export function albumAddress(photoAlbumId: string) {
  return `/photos/albums/${encodeURIComponent(photoAlbumId)}`
}

// Where choosing a view of the library goes. Albums is this page.
export function sectionAddress(section: PhotoSection): string {
  return section === 'library'
    ? '/photos'
    : section === 'albums'
      ? '/photos/albums'
      : section === 'people'
        ? '/photos/people'
        : `/photos?view=${section}`
}

// The suggestions a person has said "not now" to, kept in this browser under
// the person's id: the server keeps none, and a suggestion that comes back
// every visit after it was refused is a list nobody trusts. A suggestion is
// told by the dates it spans, which is all that is the same when the server
// finds it again. Where the browser keeps nothing, they come back.
const DISMISSED_KEY = 'teanode.photos.dismissedSuggestions.'

function suggestionKey(suggestion: Pick<PhotoAlbumSuggestion, 'takenAfter' | 'takenBefore'>): string {
  return `${suggestion.takenAfter}|${suggestion.takenBefore}`
}

function readDismissed(userId: string): string[] {
  try {
    const stored: unknown = JSON.parse(window.localStorage.getItem(DISMISSED_KEY + userId) ?? '[]')
    return Array.isArray(stored) ? stored.filter((entry): entry is string => typeof entry === 'string') : []
  } catch {
    return []
  }
}

function useDismissedSuggestions(userId: string): [ReadonlySet<string>, (suggestion: PhotoAlbumSuggestion) => void] {
  const [dismissed, setDismissed] = useState<string[]>(() => readDismissed(userId))
  const dismiss = (suggestion: PhotoAlbumSuggestion) => {
    const next = [...dismissed, suggestionKey(suggestion)]
    setDismissed(next)
    try {
      window.localStorage.setItem(DISMISSED_KEY + userId, JSON.stringify(next))
    } catch {
      // Not kept; it is gone from the page until the next visit.
    }
  }
  return [useMemo(() => new Set(dismissed), [dismissed]), dismiss]
}

export function PhotoAlbumsPage() {
  const session = useSession()
  return <AccountAlbumsPage key={session.userId ?? ''} />
}

function AccountAlbumsPage() {
  const { t, plural, language } = useTranslation()
  const session = useSession()
  const [dismissedKeys, dismissSuggestion] = useDismissedSuggestions(session.userId ?? '')
  const toast = useToast()
  const navigate = useNavigate()
  const albums = useQuery(() => listPhotoAlbums(), [])
  const suggestions = useQuery(() => suggestPhotoAlbums(), [], { refresh: false })
  const sampleIds = useMemo(
    () => (suggestions.data ?? []).flatMap((suggestion) => suggestion.samplePhotoIds),
    [suggestions.data],
  )
  const samples = useQuery(() => readSamplePreviews(sampleIds), [sampleIds.join(',')], { refresh: false })
  const [isNaming, setIsNaming] = useState(false)
  const [suggestion, setSuggestion] = useState<PhotoAlbumSuggestion | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const circles = useCircleChoices()

  async function create(albumTitle: string, circleId: string | null, source: PhotoAlbumSuggestion | null) {
    setIsBusy(true)
    try {
      const photoIds = source ? await listSuggestionPhotoIds(source) : []
      const created = await createPhotoAlbum(albumTitle, photoIds, circleId)
      toast.done(t('photos.albumCreated', { title: created.albumTitle }))
      navigate(albumAddress(created.id))
    } catch (caught) {
      toast.failure(caught, t('photos.actionFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  const list = albums.data ?? []
  const ownAlbums = list.filter((album) => album.isOwnedByCaller)
  const circleAlbums = list.filter((album) => album.circleId !== null)
  const sharedAlbums = list.filter((album) => !album.isOwnedByCaller && album.circleId === null)
  const suggested = (suggestions.data ?? []).filter((entry) => !dismissedKeys.has(suggestionKey(entry)))
  // The sections are named only when there is more than one kind of album.
  const hasOtherSections = circleAlbums.length > 0 || sharedAlbums.length > 0
  return (
    <div className="photos-page">
      <ErrorMessage error={albums.error} />
      <PhotoSections current="albums" onSelect={(section) => navigate(sectionAddress(section))} />
      <PhotoToolbar>
        <button type="button" className="primary" onClick={() => setIsNaming(true)}>
          <PlusIcon size={16} /> {t('photos.newAlbum')}
        </button>
      </PhotoToolbar>

      {suggested.length > 0 && (
        <section className="photo-suggestions" aria-labelledby="photo-suggestions-heading">
          <h2 id="photo-suggestions-heading" className="photo-section-heading">
            {t('photos.suggested')}
          </h2>
          <p className="muted photo-section-hint">{t('photos.suggestedHint')}</p>
          <ul className="photo-suggestion-list">
            {suggested.map((entry) => (
              <li className="photo-suggestion" key={`${entry.takenAfter}-${entry.takenBefore}`}>
                <div className="photo-suggestion-samples" aria-hidden="true">
                  {entry.samplePhotoIds.map((photoId) =>
                    samples.data?.[photoId] ? (
                      <img key={photoId} src={samples.data[photoId]} alt="" loading="lazy" draggable={false} />
                    ) : (
                      <span key={photoId} className="photo-suggestion-blank" />
                    ),
                  )}
                </div>
                <div className="photo-suggestion-body">
                  <strong>{entry.albumTitle}</strong>
                  <span className="muted">
                    {[
                      formatDateRange(entry.takenAfter, entry.takenBefore, language, personTimeZone()),
                      plural(entry.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' }),
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                  {entry.placeNames.length > 0 && !titleNamesPlaces(entry.albumTitle, entry.placeNames) && (
                    <span className="muted">{entry.placeNames.join(', ')}</span>
                  )}
                </div>
                <div className="photo-suggestion-actions">
                  <button type="button" onClick={() => setSuggestion(entry)}>
                    {t('photos.createAlbum')}
                  </button>
                  <Tooltip label={t('photos.notNow')}>
                    <button
                      type="button"
                      className="icon-action"
                      aria-label={`${entry.albumTitle}: ${t('photos.notNow')}`}
                      onClick={() => dismissSuggestion(entry)}
                    >
                      <CloseIcon size={16} />
                    </button>
                  </Tooltip>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}

      {albums.loading && !albums.data ? (
        <Loading />
      ) : list.length === 0 ? (
        <SettingsEmpty>{t('photos.emptyAlbums')}</SettingsEmpty>
      ) : (
        <>
          <AlbumSection
            headingId="photo-albums-own"
            heading={hasOtherSections ? t('photos.albumsOwn') : null}
            albums={ownAlbums}
            emptyText={hasOtherSections ? t('photos.albumsOwnEmpty') : null}
          />
          <AlbumSection
            headingId="photo-albums-circles"
            heading={t('photos.albumsCircles')}
            hint={t('photos.albumsCirclesHint')}
            albums={circleAlbums}
          />
          <AlbumSection
            headingId="photo-albums-shared"
            heading={t('photos.albumsShared')}
            hint={t('photos.albumsSharedHint')}
            albums={sharedAlbums}
          />
        </>
      )}

      {isNaming && (
        <AlbumTitleDialog
          title={t('photos.newAlbum')}
          submitLabel={t('common.create')}
          circles={circles}
          isBusy={isBusy}
          onSubmit={(albumTitle, circleId) => void create(albumTitle, circleId, null)}
          onClose={() => setIsNaming(false)}
        />
      )}
      {suggestion && (
        <AlbumTitleDialog
          title={t('photos.createAlbum')}
          submitLabel={t('common.create')}
          initialTitle={suggestion.albumTitle}
          circles={circles}
          isBusy={isBusy}
          onSubmit={(albumTitle, circleId) => void create(albumTitle, circleId, suggestion)}
          onClose={() => setSuggestion(null)}
        />
      )}
    </div>
  )
}

// One kind of album: a heading when the page has more than one kind, and the
// grid. A kind with no album is left out, except the person's own, which says
// so when the others are there.
function AlbumSection({
  headingId,
  heading,
  hint,
  albums,
  emptyText,
}: {
  headingId: string
  heading: string | null
  hint?: string
  albums: PhotoAlbum[]
  emptyText?: string | null
}) {
  if (albums.length === 0 && !emptyText) return null
  return (
    <section className="photo-album-section" aria-labelledby={heading ? headingId : undefined}>
      {heading && (
        <h2 id={headingId} className="photo-section-heading">
          {heading}
        </h2>
      )}
      {hint && <p className="muted photo-section-hint">{hint}</p>}
      {albums.length === 0 ? (
        <SettingsEmpty>{emptyText}</SettingsEmpty>
      ) : (
        <div className="photo-album-grid">
          {albums.map((album) => (
            <AlbumCell key={album.id} album={album} />
          ))}
        </div>
      )}
    </section>
  )
}

// One album: its cover, square, its title under it, and in muted text how
// many photos it holds and the dates they span. An album that is not the
// person's own says whose it is, by the owner's face and name or the circle's.
function AlbumCell({ album }: { album: PhotoAlbum }) {
  const { plural, language } = useTranslation()
  const details = [
    plural(album.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' }),
    formatDateRange(album.firstTakenAt, album.lastTakenAt, language, personTimeZone()),
  ]
    .filter(Boolean)
    .join(' · ')
  return (
    <Link className="photo-album-cell" to={albumAddress(album.id)}>
      <span className="photo-album-cover">
        {album.coverPreviewUrl ? (
          <img src={album.coverPreviewUrl} alt="" loading="lazy" draggable={false} />
        ) : (
          <span className="photo-album-cover-empty" aria-hidden="true">
            <AlbumIcon size={32} />
          </span>
        )}
      </span>
      <span className="photo-album-title">{album.albumTitle}</span>
      <span className="photo-album-details muted">{details}</span>
      {!album.isOwnedByCaller && <AlbumOwnerMark album={album} />}
    </Link>
  )
}
