import { useEffect, useRef } from 'react'
import { useTranslation } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'

// The strip of small pictures along the foot of the viewer: the photos either
// side of the one shown, the one shown marked and larger, scrolling sideways
// and keeping the one shown in view. Clicking one shows it.
//
// It is one stop for the Tab key and not a stop for each picture: the arrow
// keys already turn from photo to photo, and the strip follows them, so two
// hundred stops would only be two hundred Tabs to get past it.

// The photos either side that are drawn, at most. The page a photo is on is at
// most this many photos long, so in practice this is the page.
const MOST_STRIP_PHOTOS = 400

// How far from the left edge of the strip the middle of the shown picture
// should be: the strip is scrolled to keep it in the middle.
export function stripScrollLeft(
  buttonLeft: number,
  buttonWidth: number,
  stripWidth: number,
  contentWidth: number,
): number {
  const centered = buttonLeft - (stripWidth - buttonWidth) / 2
  return Math.max(0, Math.min(centered, Math.max(0, contentWidth - stripWidth)))
}

// The photos the strip draws: the whole run when it is short, otherwise a
// stretch of it around the shown photo. The shown photo is always in it, and
// is the only one when it is not in the run (a photo opened from a link).
export function stripPhotos(photos: Photo[], current: Photo, previous: Photo | null, next: Photo | null): Photo[] {
  const index = photos.findIndex((photo) => photo.id === current.id)
  if (index < 0) return [previous, current, next].filter((photo): photo is Photo => photo !== null)
  if (photos.length <= MOST_STRIP_PHOTOS) return photos
  const start = Math.max(0, Math.min(index - MOST_STRIP_PHOTOS / 2, photos.length - MOST_STRIP_PHOTOS))
  return photos.slice(start, start + MOST_STRIP_PHOTOS)
}

export function ViewerStrip({
  photos,
  current,
  onShow,
}: {
  photos: Photo[]
  current: Photo
  onShow: (photo: Photo) => void
}) {
  const { t } = useTranslation()
  const strip = useRef<HTMLDivElement>(null)
  const currentButton = useRef<HTMLButtonElement>(null)
  const hasSettled = useRef(false)

  // Keeps the shown picture in the middle: the first time at once, since the
  // viewer has only just opened, and afterwards smoothly.
  useEffect(() => {
    const element = strip.current
    const button = currentButton.current
    if (!element || !button) return
    const left = stripScrollLeft(button.offsetLeft, button.offsetWidth, element.clientWidth, element.scrollWidth)
    const isMotionWanted = hasSettled.current && !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    if (typeof element.scrollTo === 'function') element.scrollTo({ left, behavior: isMotionWanted ? 'smooth' : 'auto' })
    else element.scrollLeft = left
    hasSettled.current = true
  }, [current.id, photos.length])

  return (
    <div className="photo-viewer-strip" ref={strip} role="group" aria-label={t('photos.neighbors')}>
      {photos.map((photo) => {
        const isCurrent = photo.id === current.id
        const name = photo.originalFilename || photo.placeName || t('nav.photos')
        return (
          <button
            key={photo.id}
            type="button"
            ref={isCurrent ? currentButton : undefined}
            className={isCurrent ? 'photo-strip-item is-current' : 'photo-strip-item'}
            aria-label={t('photos.showPhoto', { name })}
            aria-current={isCurrent ? 'true' : undefined}
            tabIndex={isCurrent ? 0 : -1}
            onClick={() => onShow(photo)}
          >
            {photo.smallPreviewUrl ? <img src={photo.smallPreviewUrl} alt="" loading="lazy" draggable={false} /> : null}
          </button>
        )
      })}
    </div>
  )
}
