import { useEffect, useRef, useState } from 'react'
import { CheckIcon, HeartIcon, WarningIcon } from '../../components/icons'
import { useTranslation } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { dayHeading, formatDuration, groupByDay, groupByTrashDay, personTimeZone } from '../../photos/days'
import { usePhotoAltText } from './altText'

// The timeline's grid: day headings, and under each the day's photos as
// square cells. A cell opens the photo; its checkbox selects it. The checkbox
// shows on hover, and on every cell once anything is selected or the page is in
// selection mode (the way a touch screen, which has no hover, selects).
//
// What the photos are under depends on what the list is. A timeline is under
// the days they were taken. The Trash is under the days they were moved there,
// with how long each group has left, since that is what somebody looking at
// the Trash asks. The results of a search are under nothing: they are in the
// order of how well they match, and day headings over them would be out of
// order.

export type PhotoGrouping = 'day' | 'trash' | 'none'

export function PhotoGrid({
  photos,
  grouping = 'day',
  trashRetentionDays = 0,
  selectedIds,
  isSelectionMode,
  onOpen,
  onToggle,
}: {
  photos: Photo[]
  grouping?: PhotoGrouping
  // How long the server keeps a photo in the Trash; 0 when it does not say.
  trashRetentionDays?: number
  selectedIds: ReadonlySet<string>
  // True from the Select button until Cancel, even while nothing is chosen yet.
  isSelectionMode: boolean
  onOpen: (photo: Photo) => void
  // The index is into photos, for the range a shift-click selects.
  onToggle: (index: number, isRange: boolean) => void
}) {
  const { t, plural, language } = useTranslation()
  const timeZone = personTimeZone()
  const groups: { key: string; heading: string | null; note: string | null; photos: Photo[] }[] =
    grouping === 'none'
      ? photos.length > 0
        ? [{ key: 'results', heading: null, note: null, photos }]
        : []
      : grouping === 'trash'
        ? groupByTrashDay(photos, trashRetentionDays, new Date(), timeZone).map((day) => ({
            key: day.key,
            heading: t('photos.trashedOn', { date: dayHeading(day.heading, language, timeZone) }),
            note:
              day.daysLeft === null
                ? null
                : day.daysLeft === 0
                  ? t('photos.trashLeftNone')
                  : plural(day.daysLeft, { one: 'photos.trashLeftOne', other: 'photos.trashLeftOther' }),
            photos: day.photos,
          }))
        : groupByDay(photos, language, timeZone).map((day) => ({
            key: day.key,
            heading: day.heading,
            note: null,
            photos: day.photos,
          }))
  const isSelecting = isSelectionMode || selectedIds.size > 0
  let index = 0
  return (
    <div className={isSelecting ? 'photo-timeline is-selecting' : 'photo-timeline'}>
      {groups.map((group) => (
        <section className="photo-day" key={`${group.key}-${group.photos[0].id}`}>
          {group.heading !== null && (
            <h2 className="photo-day-heading">
              {group.heading}
              {group.note && <span className="photo-day-note muted">{group.note}</span>}
            </h2>
          )}
          <div className="photo-grid">
            {group.photos.map((photo) => {
              const photoIndex = index++
              return (
                <PhotoCell
                  key={photo.id}
                  photo={photo}
                  isSelected={selectedIds.has(photo.id)}
                  isSelecting={isSelecting}
                  onOpen={() => onOpen(photo)}
                  onToggle={(isRange) => onToggle(photoIndex, isRange)}
                />
              )
            })}
          </div>
        </section>
      ))}
    </div>
  )
}

function PhotoCell({
  photo,
  isSelected,
  isSelecting,
  onOpen,
  onToggle,
}: {
  photo: Photo
  isSelected: boolean
  isSelecting: boolean
  onOpen: () => void
  onToggle: (isRange: boolean) => void
}) {
  const { t } = useTranslation()
  const altText = usePhotoAltText()
  const name = photo.originalFilename || photo.placeName || t('nav.photos')
  // A video whose length is not known yet (it is still being read) shows no
  // length rather than 0:00.
  const isVideo = photo.photoKind === 'video' && photo.durationMS > 0
  return (
    <div className={isSelected ? 'photo-cell is-selected' : 'photo-cell'}>
      <button
        type="button"
        className="photo-open"
        aria-label={t('photos.openPhoto', { name })}
        // Once something is chosen a click chooses (and with shift, extends
        // to a range) rather than opens, as in every file browser.
        onClick={(event) => (isSelecting ? onToggle(event.shiftKey) : onOpen())}
      >
        {photo.photoProcessingState !== 'failed' && !photo.smallPreviewUrl ? (
          // Waiting, or ready with no small preview yet (the server makes one
          // again): the same wait, not the failure of a photo that has not
          // failed.
          <span className="photo-state" title={t('photos.waiting')} role="img" aria-label={t('photos.waiting')}>
            <span className="photo-spinner" />
          </span>
        ) : photo.photoProcessingState === 'failed' ? (
          <span
            className="photo-state photo-state-failed"
            role="img"
            title={
              photo.processingErrorMessage
                ? t('photos.failed', { message: photo.processingErrorMessage })
                : t('photos.failedNoMessage')
            }
            aria-label={t('photos.failedNoMessage')}
          >
            <WarningIcon size={22} />
          </span>
        ) : (
          <PhotoImage src={photo.smallPreviewUrl} alt={altText(photo)} />
        )}
        {photo.photoKind === 'livePhoto' && (
          <span className="photo-mark photo-mark-live" title={t('photos.livePhoto')}>
            <span className="visually-hidden">{t('photos.livePhoto')}</span>
          </span>
        )}
        {photo.isFavorite && (
          <span className="photo-mark photo-mark-favorite">
            <HeartIcon size={14} />
          </span>
        )}
        {isVideo && (
          <span className="photo-mark photo-mark-length">
            <span className="visually-hidden">
              {t('photos.videoLength', { length: formatDuration(photo.durationMS) })}
            </span>
            <span aria-hidden="true">{formatDuration(photo.durationMS)}</span>
          </span>
        )}
      </button>
      <label className="photo-check" onClick={(event) => event.stopPropagation()}>
        <input
          type="checkbox"
          checked={isSelected}
          aria-label={t('photos.selectPhoto', { name })}
          onChange={() => {}}
          onClick={(event) => onToggle(event.shiftKey)}
        />
        <span className="photo-check-box" aria-hidden="true">
          {isSelected && <CheckIcon size={14} />}
        </span>
      </label>
    </div>
  )
}

// The picture of a cell. Until it has arrived the cell shows its neutral ground
// and not the alt text in a broken-image box, which is what a browser draws
// for a picture still on its way; the alt text stays for a screen reader.
function PhotoImage({ src, alt }: { src: string; alt: string }) {
  const [isLoaded, setIsLoaded] = useState(false)
  const element = useRef<HTMLImageElement>(null)
  // A picture the browser had already will not say it has loaded again.
  useEffect(() => {
    setIsLoaded(element.current?.complete === true && element.current.naturalWidth > 0)
  }, [src])
  return (
    <img
      ref={element}
      className={isLoaded ? 'photo-image is-loaded' : 'photo-image'}
      src={src}
      alt={alt}
      loading="lazy"
      draggable={false}
      onLoad={() => setIsLoaded(true)}
    />
  )
}
