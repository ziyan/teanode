import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { PageHeadingActions } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import { AlbumIcon, PlusIcon } from '../../components/icons'
import { SettingsEmpty } from '../../components/settingsList'
import { useToast } from '../../components/toast'
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
import { AlbumTitleDialog, PhotoSections, type PhotoSection } from './albumControls'

// The Albums page: a grid of the person's albums by their covers, and above
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

export function PhotoAlbumsPage() {
  const session = useSession()
  return <AccountAlbumsPage key={session.userId ?? ''} />
}

function AccountAlbumsPage() {
  const { t, plural, language } = useTranslation()
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

  async function create(albumTitle: string, source: PhotoAlbumSuggestion | null) {
    setIsBusy(true)
    try {
      const photoIds = source ? await listSuggestionPhotoIds(source) : []
      const created = await createPhotoAlbum(albumTitle, photoIds)
      toast.done(t('photos.albumCreated', { title: created.albumTitle }))
      navigate(albumAddress(created.id))
    } catch (caught) {
      toast.failure(caught, t('photos.actionFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  const list = albums.data ?? []
  const suggested = suggestions.data ?? []
  return (
    <div className="photos-page">
      <ErrorMessage error={albums.error} />
      <PageHeadingActions>
        <button type="button" className="primary" onClick={() => setIsNaming(true)}>
          <PlusIcon size={16} /> {t('photos.newAlbum')}
        </button>
      </PageHeadingActions>
      <div className="photo-toolbar">
        <PhotoSections current="albums" onSelect={(section) => navigate(sectionAddress(section))} />
      </div>

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
                <button type="button" onClick={() => setSuggestion(entry)}>
                  {t('photos.createAlbum')}
                </button>
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
        <div className="photo-album-grid">
          {list.map((album) => (
            <AlbumCell key={album.id} album={album} />
          ))}
        </div>
      )}

      {isNaming && (
        <AlbumTitleDialog
          title={t('photos.newAlbum')}
          submitLabel={t('common.create')}
          isBusy={isBusy}
          onSubmit={(albumTitle) => void create(albumTitle, null)}
          onClose={() => setIsNaming(false)}
        />
      )}
      {suggestion && (
        <AlbumTitleDialog
          title={t('photos.createAlbum')}
          submitLabel={t('common.create')}
          initialTitle={suggestion.albumTitle}
          isBusy={isBusy}
          onSubmit={(albumTitle) => void create(albumTitle, suggestion)}
          onClose={() => setSuggestion(null)}
        />
      )}
    </div>
  )
}

// One album: its cover, square, its title under it, and in muted text how
// many photos it holds and the dates they span.
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
    </Link>
  )
}
