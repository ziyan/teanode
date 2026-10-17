import { useState } from 'react'
import { FormDialog } from '../../components/dialog'
import { AlbumAddIcon } from '../../components/icons'
import { MenuButton } from '../../components/menuButton'
import { Select } from '../../components/select'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { listPhotoAlbums, readPhotoLibrary, type PhotoAlbum } from '../../photos/api'
import { listPhotoFaceGroups } from '../../photos/people'
import { listCircleChoices, type CircleChoice } from '../../photos/sharing'

// The pieces the library, the albums page and the viewer share: the choice
// between the library's views, the question that asks for an album's title,
// and the menu that puts photos in an album.

export type PhotoSection = 'library' | 'favorites' | 'albums' | 'people' | 'trash'

const SECTIONS: {
  section: PhotoSection
  label: 'photos.listLibrary' | 'photos.listFavorites' | 'photos.listAlbums' | 'photos.listPeople' | 'photos.listTrash'
}[] = [
  { section: 'library', label: 'photos.listLibrary' },
  { section: 'favorites', label: 'photos.listFavorites' },
  { section: 'albums', label: 'photos.listAlbums' },
  { section: 'people', label: 'photos.listPeople' },
  { section: 'trash', label: 'photos.listTrash' },
]

// Whether People is offered: when faces can be found on this server, or when
// the person has people from before (the server may have been changed since).
// Read once when the choice is first drawn; until then it is left out, so that
// the other choices do not shift when it arrives on a server that has faces.
function useIsPeopleShown(): boolean {
  const library = useQuery(() => readPhotoLibrary(), [], { refresh: false })
  const isAvailable = library.data?.isFaceRecognitionAvailable === true
  const hasAnswered = library.data !== null
  const groups = useQuery(
    async () => (hasAnswered && !isAvailable ? await listPhotoFaceGroups(true) : []),
    [hasAnswered, isAvailable],
    { refresh: false },
  )
  return isAvailable || (groups.data?.length ?? 0) > 0
}

// The segmented choice at the head of Photos. Choosing leaves the page's own
// state to the caller: the library keeps its search when it changes list,
// and Albums is a page of its own.
export function PhotoSections({
  current,
  onSelect,
}: {
  current: PhotoSection
  onSelect: (section: PhotoSection) => void
}) {
  const { t } = useTranslation()
  const isPeopleShown = useIsPeopleShown()
  return (
    <div className="segmented" role="group" aria-label={t('photos.show')}>
      {SECTIONS.filter((entry) => entry.section !== 'people' || isPeopleShown || current === 'people').map((entry) => (
        <button
          key={entry.section}
          type="button"
          className={entry.section === current ? 'active' : ''}
          aria-pressed={entry.section === current}
          onClick={() => onSelect(entry.section)}
        >
          {t(entry.label)}
        </button>
      ))}
    </div>
  )
}

// The most characters an album's title may have, as the server counts them.
const MOST_ALBUM_TITLE_CHARACTERS = 255

// The circles a new album can be made for, read when a dialog asks for them:
// none for a person who is in no circle, and then nothing is offered.
export function useCircleChoices(): CircleChoice[] {
  const circles = useQuery(() => listCircleChoices(), [], { refresh: false })
  return circles.data ?? []
}

// AlbumTitleDialog asks for an album's title: for a new album, a suggestion
// made into one, and a rename. The caller says what the button does. Given
// circles, it also asks whose the album is: the person's own, or one of the
// circles', where every member adds their photos.
export function AlbumTitleDialog({
  title,
  submitLabel,
  initialTitle = '',
  circles = [],
  isBusy,
  error,
  onSubmit,
  onClose,
}: {
  title: string
  submitLabel: string
  initialTitle?: string
  circles?: CircleChoice[]
  isBusy?: boolean
  error?: string | null
  onSubmit: (albumTitle: string, circleId: string | null) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [albumTitle, setAlbumTitle] = useState(initialTitle)
  const [circleId, setCircleId] = useState('')
  const trimmed = albumTitle.trim()
  const circle = circles.find((each) => each.id === circleId)
  return (
    <FormDialog
      title={title}
      submitLabel={submitLabel}
      busy={isBusy}
      error={error}
      canSubmit={trimmed !== ''}
      onSubmit={() => onSubmit(trimmed, circle ? circle.id : null)}
      onClose={onClose}
    >
      <label>
        <span>{t('photos.albumTitleLabel')}</span>
        <input
          type="text"
          value={albumTitle}
          maxLength={MOST_ALBUM_TITLE_CHARACTERS}
          autoComplete="off"
          onChange={(event) => setAlbumTitle(event.target.value)}
          onFocus={(event) => event.target.select()}
        />
      </label>
      {circles.length > 0 && (
        <label>
          <span>{t('photos.albumFor')}</span>
          <Select
            block
            value={circleId}
            label={t('photos.albumFor')}
            options={[
              { value: '', label: t('photos.albumForMe') },
              ...circles.map((each) => ({ value: each.id, label: t('photos.albumForCircle', { name: each.name }) })),
            ]}
            onChange={setCircleId}
          />
          {circle && <span className="muted">{t('photos.albumForCircleHint', { name: circle.name })}</span>}
        </label>
      )}
    </FormDialog>
  )
}

// AddToAlbumMenu is the button that lists the person's albums, most recently
// changed first, with "New album..." at the head. What choosing does is the
// caller's, so the menu works the same on a selection and in the viewer.
export function AddToAlbumMenu({
  onChooseAlbum,
  onNewAlbum,
  hiddenAlbumId,
}: {
  onChooseAlbum: (album: PhotoAlbum) => void
  onNewAlbum: () => void
  // The album the photos are being looked at in: they are in it already.
  hiddenAlbumId?: string
}) {
  const { t } = useTranslation()
  return (
    <Tooltip label={t('photos.addToAlbum')}>
      <MenuButton
        label={t('photos.addToAlbum')}
        className="icon-button"
        icon={<AlbumAddIcon size={18} />}
        render={(close) => (
          <AlbumChoices
            hiddenAlbumId={hiddenAlbumId}
            onChooseAlbum={(album) => {
              close()
              onChooseAlbum(album)
            }}
            onNewAlbum={() => {
              close()
              onNewAlbum()
            }}
          />
        )}
      />
    </Tooltip>
  )
}

// Read when the menu opens, so that an album made a moment ago is in it.
function AlbumChoices({
  hiddenAlbumId,
  onChooseAlbum,
  onNewAlbum,
}: {
  hiddenAlbumId?: string
  onChooseAlbum: (album: PhotoAlbum) => void
  onNewAlbum: () => void
}) {
  const { t, plural } = useTranslation()
  const albums = useQuery(() => listPhotoAlbums(), [], { refresh: false })
  // Only the albums the person adds their own photos to: not the ones shared
  // with them to view.
  const choices = (albums.data ?? []).filter((album) => album.id !== hiddenAlbumId && album.canCallerContribute)
  return (
    <>
      <button type="button" role="menuitem" onClick={onNewAlbum}>
        {t('photos.newAlbumMenu')}
      </button>
      {albums.loading && !albums.data ? (
        <div className="menu-header">{t('common.loading')}</div>
      ) : (
        choices.length > 0 && (
          <>
            <div className="menu-separator" />
            {choices.map((album) => (
              <button key={album.id} type="button" role="menuitem" onClick={() => onChooseAlbum(album)}>
                <span className="photo-album-choice">
                  <span className="photo-album-choice-title">{album.albumTitle}</span>
                  <span className="muted">
                    {[
                      plural(album.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' }),
                      album.isOwnedByCaller ? '' : album.circleId ? album.circleName : album.ownerName,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                </span>
              </button>
            ))}
          </>
        )
      )}
    </>
  )
}
