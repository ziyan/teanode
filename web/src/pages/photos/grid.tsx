import { CheckIcon, HeartIcon, WarningIcon } from '../../components/icons'
import { useTranslation } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { formatDuration, groupByDay, personTimeZone } from '../../photos/days'

// The timeline's grid: day headings, and under each the day's photos as
// square cells. A cell opens the photo; its checkbox, shown on hover and
// always once anything is selected, selects it.

export function PhotoGrid({
  photos,
  selectedIds,
  onOpen,
  onToggle,
}: {
  photos: Photo[]
  selectedIds: ReadonlySet<string>
  onOpen: (photo: Photo) => void
  // The index is into photos, for the range a shift-click selects.
  onToggle: (index: number, isRange: boolean) => void
}) {
  const { language } = useTranslation()
  const days = groupByDay(photos, language, personTimeZone())
  const isSelecting = selectedIds.size > 0
  let index = 0
  return (
    <div className={isSelecting ? 'photo-timeline is-selecting' : 'photo-timeline'}>
      {days.map((day) => (
        <section className="photo-day" key={`${day.key}-${day.photos[0].id}`}>
          <h2 className="photo-day-heading">{day.heading}</h2>
          <div className="photo-grid">
            {day.photos.map((photo) => {
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
        {photo.photoProcessingState === 'waiting' ? (
          <span className="photo-state" title={t('photos.waiting')} role="img" aria-label={t('photos.waiting')}>
            <span className="photo-spinner" />
          </span>
        ) : photo.photoProcessingState === 'failed' || !photo.smallPreviewUrl ? (
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
          <img className="photo-image" src={photo.smallPreviewUrl} alt="" loading="lazy" draggable={false} />
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
