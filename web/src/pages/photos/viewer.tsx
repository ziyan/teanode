import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import {
  AlbumRemoveIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CloseIcon,
  DownloadIcon,
  HeartIcon,
  InfoIcon,
  PictureIcon,
  RefreshIcon,
  RestoreIcon,
  SparkIcon,
  TrashIcon,
  ZoomInIcon,
} from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  photoFilePath,
  readPhoto,
  type Photo,
  type PhotoAlbum,
  type PhotoFace,
  type PhotoFile,
  type PhotoLibrary,
} from '../../photos/api'
import { formatByteSize, formatDuration, formatDate, personTimeZone, photoDateTime } from '../../photos/days'
import { albumAddress } from './albums'
import { AddToAlbumMenu } from './albumControls'
import { FacePicture, personAddress } from './personControls'

// The viewer: one photo over the grid, with its details beside it (under it
// on a phone). It has an address of its own, so Back closes it and a link
// opens it; the page decides what moving and closing do to the address.

// The kinds of file a browser draws by itself. A zoom goes to the original
// only when it is one of these; a HEIC picture is zoomed from the large
// preview instead.
const DISPLAYABLE_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/avif']

// How far a finger must travel sideways to turn to the next photo.
const SWIPE_DISTANCE = 60

// How often a description that is on its way is looked for, and how long the
// viewer keeps looking before it leaves the rest to the person coming back.
const DESCRIPTION_POLL_MS = 3000
const DESCRIPTION_POLL_LIMIT_MS = 2 * 60 * 1000

export function PhotoViewer({
  photo,
  previousPhoto,
  nextPhoto,
  isTrashed,
  isPaused,
  photoAlbumId,
  onNavigate,
  onClose,
  onToggleFavorite,
  onTrash,
  onRestore,
  onDeleteForever,
  onAddToAlbum,
  onNewAlbum,
  onRemoveFromAlbum,
  onSetCover,
  photoLibrary,
  onDescribe,
  onAskAssistant,
  onRemoveFaceFromGroup,
}: {
  photo: Photo
  previousPhoto: Photo | null
  nextPhoto: Photo | null
  isTrashed: boolean
  // Set while a dialog is open over the viewer, so that its keys are the
  // dialog's.
  isPaused: boolean
  // The album the photo is being looked at in, if any.
  photoAlbumId?: string
  onNavigate: (photo: Photo) => void
  onClose: () => void
  onToggleFavorite: (photo: Photo) => void
  onTrash: (photo: Photo) => void
  onRestore: (photo: Photo) => void
  onDeleteForever: (photo: Photo) => void
  onAddToAlbum: (photo: Photo, album: PhotoAlbum) => Promise<void>
  onNewAlbum: (photo: Photo) => void
  // Only inside an album: take the photo out of it, or make it the cover.
  onRemoveFromAlbum?: (photo: Photo) => Promise<void>
  onSetCover?: (photo: Photo) => Promise<void>
  // What the library says about describing: whether the assistant can, and
  // why not when it cannot. Left out, the description is shown when there is
  // one and never asked for.
  photoLibrary?: PhotoLibrary | null
  // Asks for a description, or a new one; the caller says why it failed.
  onDescribe?: (photo: Photo, isRedescribed: boolean) => Promise<void>
  // Opens the assistant with a question about this photo.
  onAskAssistant?: (photo: Photo) => void
  // "This is not them": takes a face out of its group.
  onRemoveFaceFromGroup?: (face: PhotoFace) => Promise<void>
}) {
  const { t, language } = useTranslation()
  const files = useQuery(() => readPhoto(photo.id), [photo.id], { refresh: false })
  const isRead = files.data !== null && files.data.photo.id === photo.id
  const photoFiles: PhotoFile[] = isRead ? files.data!.photoFiles : []
  const albums = isRead ? (files.data!.albums ?? []) : []
  const faces = isRead ? (files.data!.faces ?? []).filter((face) => face.photoFaceGroupId !== '') : []
  const detail = isRead ? files.data!.photo : null
  const isDescriptionRequested = detail?.isDescriptionRequested === true
  const [isAskingToDescribe, setIsAskingToDescribe] = useState(false)
  const [hasGivenUpWaiting, setHasGivenUpWaiting] = useState(false)
  const [isZoomed, setIsZoomed] = useState(false)
  const [isLive, setIsLive] = useState(false)
  const [isShowingDetails, setIsShowingDetails] = useState(() => window.matchMedia('(min-width: 761px)').matches)
  const zoomScroller = useRef<HTMLDivElement>(null)
  const swipeStart = useRef<{ x: number; y: number } | null>(null)

  // Each photo starts fitted and not playing.
  useEffect(() => {
    setIsZoomed(false)
    setIsLive(false)
    setHasGivenUpWaiting(false)
  }, [photo.id])

  // A description that is on its way is looked for every few seconds while
  // the viewer stays on the photo, and not for longer than two minutes: after
  // that the person comes back to it, the job goes on without the page.
  const reloadFiles = files.reload
  useEffect(() => {
    if (!isDescriptionRequested) return
    const startedAt = Date.now()
    const timer = window.setInterval(() => {
      if (Date.now() - startedAt >= DESCRIPTION_POLL_LIMIT_MS) {
        window.clearInterval(timer)
        setHasGivenUpWaiting(true)
        return
      }
      void reloadFiles(true)
    }, DESCRIPTION_POLL_MS)
    return () => window.clearInterval(timer)
  }, [isDescriptionRequested, photo.id, reloadFiles])

  async function describe(isRedescribed: boolean) {
    if (!onDescribe) return
    setIsAskingToDescribe(true)
    setHasGivenUpWaiting(false)
    try {
      await onDescribe(photo, isRedescribed)
      await files.reload(true)
    } finally {
      setIsAskingToDescribe(false)
    }
  }

  // The element that opened the viewer gets the focus back when it closes.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    return () => {
      if (opener && document.contains(opener)) opener.focus()
    }
  }, [])

  const isVideo = photo.photoKind === 'video'
  const original =
    photoFiles.find((file) => file.photoFileRole === 'edited') ??
    photoFiles.find((file) => file.photoFileRole === 'original')
  const originalFile = photoFiles.find((file) => file.photoFileRole === 'original')
  const canZoomOriginal = original !== undefined && DISPLAYABLE_TYPES.includes(original.contentType)
  const fittedSource = photo.largePreviewUrl || photo.smallPreviewUrl
  const zoomedSource = canZoomOriginal ? original.fileUrl : fittedSource
  const videoSource = photo.videoPreviewUrl || (isVideo ? photoFilePath(photo.id, 'original') : '')
  const liveSource = photo.videoPreviewUrl || photoFilePath(photo.id, 'liveVideo')

  const goTo = useCallback(
    (target: Photo | null) => {
      if (target) onNavigate(target)
    },
    [onNavigate],
  )

  useEffect(() => {
    if (isPaused) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return
      // An open menu takes the key first: Escape closes the menu, not the
      // viewer behind it.
      if (document.querySelector('.menu[role="menu"]')) return
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
        return
      }
      // A video's own controls use the arrows to seek, and a field uses them
      // to move its cursor.
      const target = event.target
      if (
        target instanceof HTMLElement &&
        (target.tagName === 'VIDEO' || target.tagName === 'INPUT' || target.isContentEditable)
      )
        return
      if (event.key === 'ArrowLeft') {
        event.preventDefault()
        goTo(previousPhoto)
      } else if (event.key === 'ArrowRight') {
        event.preventDefault()
        goTo(nextPhoto)
      }
    }
    // Listened for while the key goes down to the page, and marked taken: the
    // agent drawer listens on the document from before the viewer was there
    // and toggles on Escape, so it heard the key first and opened itself
    // over a viewer that stayed open.
    document.addEventListener('keydown', onKeyDown, true)
    return () => document.removeEventListener('keydown', onKeyDown, true)
  }, [isPaused, onClose, goTo, previousPhoto, nextPhoto])

  // The photos either side are fetched ahead, so that an arrow key shows
  // the next one at once.
  useEffect(() => {
    for (const neighbor of [previousPhoto, nextPhoto]) {
      const source = neighbor && neighbor.photoKind !== 'video' ? neighbor.largePreviewUrl : ''
      if (source) new Image().src = source
    }
  }, [previousPhoto, nextPhoto])

  const toggleZoom = () => {
    if (!isVideo && fittedSource) setIsZoomed((previous) => !previous)
  }

  const cameraText = [photo.cameraMake, photo.cameraModel]
    .filter((part, index, parts) => part && !(index === 1 && part.startsWith(parts[0])))
    .join(' ')
  const details: [string, string][] = [
    [t('photos.detailDate'), photoDateTime(photo, language, personTimeZone())],
    [t('photos.detailPlace'), photo.placeName],
    [t('photos.detailCamera'), cameraText],
    [
      t('photos.detailSize'),
      photo.widthPixels > 0 && photo.heightPixels > 0
        ? t('photos.pixels', { width: photo.widthPixels, height: photo.heightPixels })
        : '',
    ],
    [t('photos.detailLength'), isVideo ? formatDuration(photo.durationMS) : ''],
    [t('photos.detailFileSize'), originalFile ? formatByteSize(originalFile.fileSizeBytes) : ''],
    [t('photos.detailFileName'), photo.originalFilename || originalFile?.originalFilename || ''],
  ]

  return createPortal(
    <div className="photo-viewer" role="dialog" aria-modal="true" aria-label={t('photos.viewer')}>
      <div className="photo-viewer-main">
        <div className="photo-viewer-bar">
          <Tooltip label={t('common.close')}>
            <button type="button" className="icon-button" aria-label={t('common.close')} onClick={onClose}>
              <CloseIcon size={18} />
            </button>
          </Tooltip>
          <span className="photo-viewer-spacer" />
          {photo.photoKind === 'livePhoto' && (
            <Tooltip label={t('photos.liveHint')}>
              <button
                type="button"
                className={isLive ? 'photo-live-button is-on' : 'photo-live-button'}
                aria-label={t('photos.liveHint')}
                onPointerDown={() => setIsLive(true)}
                onPointerUp={() => setIsLive(false)}
                onPointerLeave={() => setIsLive(false)}
                onPointerCancel={() => setIsLive(false)}
                onKeyDown={(event) => (event.key === ' ' || event.key === 'Enter') && setIsLive(true)}
                onKeyUp={() => setIsLive(false)}
                onContextMenu={(event) => event.preventDefault()}
              >
                {t('photos.liveButton')}
              </button>
            </Tooltip>
          )}
          {!isVideo && fittedSource && (
            <Tooltip label={isZoomed ? t('photos.zoomOut') : t('photos.zoomIn')}>
              <button
                type="button"
                className={isZoomed ? 'icon-button is-on' : 'icon-button'}
                aria-label={isZoomed ? t('photos.zoomOut') : t('photos.zoomIn')}
                aria-pressed={isZoomed}
                onClick={toggleZoom}
              >
                <ZoomInIcon size={18} />
              </button>
            </Tooltip>
          )}
          {isTrashed ? (
            <>
              <Tooltip label={t('photos.restore')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('photos.restore')}
                  onClick={() => onRestore(photo)}
                >
                  <RestoreIcon size={18} />
                </button>
              </Tooltip>
              <Tooltip label={t('photos.deleteForever')}>
                <button
                  type="button"
                  className="icon-button danger"
                  aria-label={t('photos.deleteForever')}
                  onClick={() => onDeleteForever(photo)}
                >
                  <TrashIcon size={18} />
                </button>
              </Tooltip>
            </>
          ) : (
            <>
              <Tooltip label={photo.isFavorite ? t('photos.unfavorite') : t('photos.favorite')}>
                <button
                  type="button"
                  className={photo.isFavorite ? 'icon-button photo-favorite is-on' : 'icon-button photo-favorite'}
                  aria-label={photo.isFavorite ? t('photos.unfavorite') : t('photos.favorite')}
                  aria-pressed={photo.isFavorite}
                  onClick={() => onToggleFavorite(photo)}
                >
                  <HeartIcon size={18} />
                </button>
              </Tooltip>
              <AddToAlbumMenu
                hiddenAlbumId={photoAlbumId}
                onChooseAlbum={(target) => void onAddToAlbum(photo, target).then(() => files.reload(true))}
                onNewAlbum={() => onNewAlbum(photo)}
              />
              {onSetCover && (
                <Tooltip label={t('photos.useAsCover')}>
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={t('photos.useAsCover')}
                    onClick={() => void onSetCover(photo)}
                  >
                    <PictureIcon size={18} />
                  </button>
                </Tooltip>
              )}
              {onRemoveFromAlbum && (
                <Tooltip label={t('photos.removeFromAlbum')}>
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={t('photos.removeFromAlbum')}
                    onClick={() => void onRemoveFromAlbum(photo)}
                  >
                    <AlbumRemoveIcon size={18} />
                  </button>
                </Tooltip>
              )}
              <Tooltip label={t('photos.downloadOriginal')}>
                <a
                  className="icon-button"
                  href={photoFilePath(photo.id, 'original')}
                  download
                  aria-label={t('photos.downloadOriginal')}
                >
                  <DownloadIcon size={18} />
                </a>
              </Tooltip>
              <Tooltip label={t('photos.trash')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('photos.trash')}
                  onClick={() => onTrash(photo)}
                >
                  <TrashIcon size={18} />
                </button>
              </Tooltip>
            </>
          )}
          <Tooltip label={t('photos.details')}>
            <button
              type="button"
              className={isShowingDetails ? 'icon-button is-on' : 'icon-button'}
              aria-label={t('photos.details')}
              aria-pressed={isShowingDetails}
              onClick={() => setIsShowingDetails((previous) => !previous)}
            >
              <InfoIcon size={18} />
            </button>
          </Tooltip>
        </div>
        <div
          className="photo-viewer-stage"
          onTouchStart={(event) => {
            const touch = event.touches[0]
            swipeStart.current = event.touches.length === 1 && !isZoomed ? { x: touch.clientX, y: touch.clientY } : null
          }}
          onTouchEnd={(event) => {
            const start = swipeStart.current
            swipeStart.current = null
            if (!start) return
            const touch = event.changedTouches[0]
            const across = touch.clientX - start.x
            if (Math.abs(across) > SWIPE_DISTANCE && Math.abs(across) > Math.abs(touch.clientY - start.y) * 1.5) {
              goTo(across > 0 ? previousPhoto : nextPhoto)
            }
          }}
        >
          {previousPhoto && (
            <Tooltip label={t('photos.previous')}>
              <button
                type="button"
                className="photo-viewer-step photo-viewer-previous"
                aria-label={t('photos.previous')}
                onClick={() => goTo(previousPhoto)}
              >
                <ChevronLeftIcon size={22} />
              </button>
            </Tooltip>
          )}
          {nextPhoto && (
            <Tooltip label={t('photos.next')}>
              <button
                type="button"
                className="photo-viewer-step photo-viewer-next"
                aria-label={t('photos.next')}
                onClick={() => goTo(nextPhoto)}
              >
                <ChevronRightIcon size={22} />
              </button>
            </Tooltip>
          )}
          {isVideo && videoSource ? (
            <video
              key={photo.id}
              className="photo-viewer-video"
              src={videoSource}
              poster={photo.largePreviewUrl || photo.smallPreviewUrl || undefined}
              controls
              playsInline
              autoPlay
            />
          ) : !fittedSource ? (
            <p className="photo-viewer-message">
              {photo.photoProcessingState === 'failed'
                ? photo.processingErrorMessage
                  ? t('photos.failed', { message: photo.processingErrorMessage })
                  : t('photos.failedNoMessage')
                : t('photos.waiting')}
            </p>
          ) : isZoomed ? (
            <div className="photo-viewer-zoom" ref={zoomScroller} onDoubleClick={toggleZoom}>
              <img
                key={zoomedSource}
                className={canZoomOriginal ? 'photo-zoomed' : 'photo-zoomed photo-zoomed-preview'}
                src={zoomedSource}
                alt=""
                draggable={false}
                onLoad={() => {
                  const scroller = zoomScroller.current
                  if (!scroller) return
                  scroller.scrollLeft = (scroller.scrollWidth - scroller.clientWidth) / 2
                  scroller.scrollTop = (scroller.scrollHeight - scroller.clientHeight) / 2
                }}
              />
            </div>
          ) : (
            <div className="photo-viewer-picture" onDoubleClick={toggleZoom}>
              <img className="photo-fitted" key={photo.id} src={fittedSource} alt="" draggable={false} />
              {photo.photoKind === 'livePhoto' && isLive && (
                <video
                  className="photo-live-video"
                  src={liveSource}
                  autoPlay
                  muted
                  playsInline
                  onEnded={() => setIsLive(false)}
                />
              )}
            </div>
          )}
        </div>
      </div>
      {isShowingDetails && (
        <aside className="photo-viewer-details" aria-label={t('photos.details')}>
          <h2>{t('photos.details')}</h2>
          <dl>
            {details
              .filter(([, value]) => value !== '')
              .map(([label, value]) => (
                <div key={label}>
                  <dt>{label}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            {albums.length > 0 && (
              <div>
                <dt>{t('photos.detailAlbums')}</dt>
                <dd>
                  <ul className="photo-viewer-albums">
                    {albums.map((album) => (
                      <li key={album.id}>
                        <Link to={albumAddress(album.id)}>{album.albumTitle}</Link>
                      </li>
                    ))}
                  </ul>
                </dd>
              </div>
            )}
          </dl>
          {faces.length > 0 && (
            <section className="photo-viewer-section" aria-labelledby="photo-viewer-people-heading">
              <h3 id="photo-viewer-people-heading">{t('photos.detailPeople')}</h3>
              <ul className="photo-viewer-people">
                {faces.map((face) => (
                  <ViewerPerson
                    key={face.id}
                    face={face}
                    onNotThem={
                      onRemoveFaceFromGroup
                        ? async () => {
                            await onRemoveFaceFromGroup(face)
                            await files.reload(true)
                          }
                        : undefined
                    }
                  />
                ))}
              </ul>
            </section>
          )}
          {isRead && (
            <section className="photo-viewer-section" aria-labelledby="photo-viewer-description-heading">
              <div className="photo-viewer-section-head">
                <h3 id="photo-viewer-description-heading">{t('photos.description')}</h3>
                {detail?.describedAt && onDescribe && photoLibrary?.isDescriptionAvailable && (
                  <Tooltip label={t('photos.describeAgain')}>
                    <button
                      type="button"
                      className="icon-action"
                      aria-label={t('photos.describeAgain')}
                      disabled={isAskingToDescribe || isDescriptionRequested}
                      onClick={() => void describe(true)}
                    >
                      <RefreshIcon size={16} />
                    </button>
                  </Tooltip>
                )}
              </div>
              {detail?.describedAt && (
                <>
                  <p className="photo-description">{detail.photoDescription}</p>
                  {detail.descriptionVisibleText && (
                    <>
                      <p className="photo-description-label muted">{t('photos.visibleText')}</p>
                      <p className="photo-description-text">{detail.descriptionVisibleText}</p>
                    </>
                  )}
                  <p className="muted photo-description-source">
                    {t('photos.describedOn', {
                      date: formatDate(detail.describedAt, language, personTimeZone()),
                    })}
                  </p>
                </>
              )}
              {isDescriptionRequested || isAskingToDescribe ? (
                <p className="muted photo-describing" role="status">
                  <span className="photo-spinner" aria-hidden="true" />
                  {hasGivenUpWaiting
                    ? t('photos.describingSlow')
                    : detail?.describedAt
                      ? t('photos.describingAgain')
                      : t('photos.describing')}
                </p>
              ) : !detail?.describedAt && photoLibrary ? (
                photoLibrary.isDescriptionAvailable && onDescribe ? (
                  <>
                    <p className="muted">{t('photos.describeHint')}</p>
                    <button type="button" onClick={() => void describe(false)}>
                      {t('photos.describe')}
                    </button>
                  </>
                ) : (
                  <p className="muted">
                    {photoLibrary.descriptionUnavailableMessage || t('photos.describeUnavailable')}
                  </p>
                )
              ) : null}
              {onAskAssistant && fittedSource && (
                <button type="button" className="photo-ask-assistant" onClick={() => onAskAssistant(photo)}>
                  <SparkIcon size={16} /> {t('photos.askAssistant')}
                </button>
              )}
            </section>
          )}
        </aside>
      )}
    </div>,
    document.body,
  )
}

// One person in the photo: the face, the name (a link to the person's page),
// and "Not <name>" for a face the server put in the wrong group. On a screen
// with a pointer the action shows when the row is hovered or focused; on a
// touch screen it is always there, since there is no hover to reveal it.
function ViewerPerson({ face, onNotThem }: { face: PhotoFace; onNotThem?: () => Promise<void> }) {
  const { t } = useTranslation()
  const [isBusy, setIsBusy] = useState(false)
  const name = face.contactName.trim()
  const shownName = name || t('photos.unnamedPerson')
  const notLabel = name ? t('photos.notPerson', { name }) : t('photos.notThisPerson')
  return (
    <li className="photo-viewer-person">
      <Link className="photo-viewer-person-link" to={personAddress(face.photoFaceGroupId)}>
        <FacePicture facePictureUrl={face.facePictureUrl} name={shownName} size="small" />
        <span className={name ? 'photo-viewer-person-name' : 'photo-viewer-person-name muted'}>{shownName}</span>
      </Link>
      {onNotThem && (
        <Tooltip label={notLabel}>
          <button
            type="button"
            className="icon-action photo-viewer-person-not"
            aria-label={notLabel}
            disabled={isBusy}
            onClick={() => {
              setIsBusy(true)
              void onNotThem().finally(() => setIsBusy(false))
            }}
          >
            <CloseIcon size={14} />
          </button>
        </Tooltip>
      )}
    </li>
  )
}
