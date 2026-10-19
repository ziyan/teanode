import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { Avatar } from '../../components/avatar'
import {
  AlbumCoverIcon,
  AlbumRemoveIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CloseIcon,
  DownloadIcon,
  HeartIcon,
  InfoIcon,
  MoreIcon,
  PencilIcon,
  PhotoSaveIcon,
  RefreshIcon,
  RestoreIcon,
  SparkIcon,
  TrashIcon,
  ZoomInIcon,
} from '../../components/icons'
import { LocationMap } from '../../components/locationMap'
import { MenuButton } from '../../components/menuButton'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  photoFilePath,
  readPhoto,
  readPhotoPlace,
  type Photo,
  type PhotoAlbum,
  type PhotoFace,
  type PhotoFile,
  type PhotoFileRole,
  type PhotoLibrary,
} from '../../photos/api'
import {
  formatByteSize,
  formatDuration,
  formatDate,
  personTimeZone,
  photoDateTime,
  photoViewerTitle,
  trashDaysLeft,
} from '../../photos/days'
import { listCirclePeople } from '../../photos/sharing'
import { albumAddress } from './albums'
import { AddToAlbumMenu } from './albumControls'
import { usePhotoAltText } from './altText'
import { useAssistantWords } from './assistantName'
import { ChangeDateDialog } from './changeDate'
import { DRAG_SETTLE_MS, dragProgress, useDragToClose } from './dragToClose'
import { FacePicture, NamePersonFlow, YouMark, personAddress } from './personControls'
import { useViewerZoom } from './useViewerZoom'
import { ViewerStrip, stripPhotos } from './viewerStrip'
import { viewTransform } from './zoom'

// The viewer: one photo over the grid, with its details beside it (under it
// on a phone). It has an address of its own, so Back closes it and a link
// opens it; the page decides what moving and closing do to the address.

// The kinds of file a browser draws by itself. A zoom goes to the original
// only when it is one of these; a HEIC picture is zoomed from the large
// preview instead.
const DISPLAYABLE_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/avif']

// How far a finger must travel sideways to turn to the next photo.
const SWIPE_DISTANCE = 60

// How often a description that is on its way is looked for, how long it takes
// before the viewer says it is taking longer than usual, and how often it
// looks after that. It looks for as long as the viewer is on the photo: a
// description that arrives while the person reads should be there without a
// reload.
const DESCRIPTION_POLL_MS = 3000
const DESCRIPTION_SLOW_AFTER_MS = 2 * 60 * 1000
const DESCRIPTION_SLOW_POLL_MS = 15 * 1000

// How close the details' map starts: a street, not a city.
const PHOTO_MAP_ZOOM = 16

// The files of a photo that can be downloaded, in the order the menu lists them.
const DOWNLOADABLE_ROLES: {
  photoFileRole: PhotoFileRole
  label: 'photos.downloadOriginal' | 'photos.downloadEdited' | 'photos.downloadLiveVideo'
}[] = [
  { photoFileRole: 'original', label: 'photos.downloadOriginal' },
  { photoFileRole: 'edited', label: 'photos.downloadEdited' },
  { photoFileRole: 'liveVideo', label: 'photos.downloadLiveVideo' },
]

// Whether the key is meant for the element it went to: a video's own controls
// use the arrows to seek, and a field uses them to move its cursor.
function isTakingKeys(element: HTMLElement): boolean {
  return (
    element.tagName === 'VIDEO' ||
    element.tagName === 'INPUT' ||
    element.tagName === 'TEXTAREA' ||
    element.tagName === 'SELECT' ||
    element.isContentEditable ||
    element.closest('[contenteditable]:not([contenteditable="false"])') !== null ||
    element.closest('[role="textbox"]') !== null
  )
}

const FOCUSABLE =
  'a[href], button:not([disabled]):not([tabindex="-1"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

// Tab and Shift+Tab stay inside the viewer: from its last control to its
// first, and from its first to its last. A menu open over it (it is drawn in
// the body, outside the viewer) keeps the Tab to itself.
function keepFocusInside(event: KeyboardEvent, viewer: HTMLElement | null) {
  if (!viewer || document.querySelector('.menu[role="menu"], .menu[role="dialog"]')) return
  const controls = Array.from(viewer.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((control) => {
    const style = window.getComputedStyle(control)
    return style.display !== 'none' && style.visibility !== 'hidden'
  })
  if (controls.length === 0) return
  const first = controls[0]
  const last = controls[controls.length - 1]
  const active = document.activeElement
  if (!(active instanceof Node) || !viewer.contains(active)) {
    event.preventDefault()
    ;(event.shiftKey ? last : first).focus()
  } else if (event.shiftKey && active === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

export function PhotoViewer({
  photo,
  previousPhoto,
  nextPhoto,
  neighborPhotos,
  isTrashed,
  isBusy = false,
  isPaused,
  photoAlbumId,
  onNavigate,
  onShowNearby,
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
  onRetryProcessing,
  onRemoveFaceFromGroup,
  onSaveToLibrary,
  onChangeDate,
  onChangeLocation,
}: {
  photo: Photo
  previousPhoto: Photo | null
  nextPhoto: Photo | null
  // The photos of the list the viewer was opened in, newest first, for the
  // strip of small pictures along its foot. Left out, the strip holds the
  // photo and the two beside it.
  neighborPhotos?: Photo[]
  isTrashed: boolean
  // The page is moving, restoring or deleting photos: the buttons that would
  // do it again wait.
  isBusy?: boolean
  // Set while a dialog is open over the viewer, so that its keys are the
  // dialog's.
  isPaused: boolean
  // The album the photo is being looked at in, if any.
  photoAlbumId?: string
  onNavigate: (photo: Photo) => void
  // Opens the library narrowed to the photos taken near this one.
  onShowNearby?: (photo: Photo) => void
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
  // Puts a photo whose previews failed back to waiting, or makes the
  // previews of a ready one again; the caller says how many it moved.
  onRetryProcessing?: (photo: Photo) => Promise<void>
  // "This is not them": takes a face out of its group.
  onRemoveFaceFromGroup?: (face: PhotoFace) => Promise<void>
  // Keeps a photo that is somebody else's, seen through an album shared with
  // the person, as a photo of their own. Offered only for such a photo.
  onSaveToLibrary?: (photo: Photo) => Promise<void>
  // Corrects the date of one of the person's own photos; takenAt is RFC 3339
  // at the offset it is shown at. Rejects with what went wrong.
  onChangeDate?: (photo: Photo, takenAt: string) => Promise<void>
  // Opens the dialog that says where one of the person's own photos was taken.
  // The caller refreshes the photo once the place has changed.
  onChangeLocation?: (photo: Photo) => void
}) {
  const { t, plural, language } = useTranslation()
  const altText = usePhotoAltText()
  const assistantWords = useAssistantWords()
  const files = useQuery(() => readPhoto(photo.id), [photo.id], { refresh: false })
  const hasPosition = typeof photo.latitude === 'number' && typeof photo.longitude === 'number'
  const [isShowingDetails, setIsShowingDetails] = useState(() => window.matchMedia('(min-width: 761px)').matches)
  // Where the photo was taken, asked for when the details are open. Keyed on
  // the position as well as the photo, so a photo that was moved asks again. A
  // failed request leaves the city text the photo already has.
  const placeRead = useQuery(
    async () => (isShowingDetails && hasPosition ? await readPhotoPlace(photo.id).catch(() => null) : null),
    [photo.id, photo.latitude, photo.longitude, isShowingDetails],
    { refresh: false },
  )
  const ownPlace = hasPosition ? (placeRead.data?.locationPlace ?? null) : null
  const addressText = hasPosition ? (placeRead.data?.addressText ?? '') : ''
  const isRead = files.data !== null && files.data.photo.id === photo.id
  const photoFiles: PhotoFile[] = isRead ? files.data!.photoFiles : []
  const albums = isRead ? (files.data!.albums ?? []) : []
  const faces = isRead ? (files.data!.faces ?? []).filter((face) => face.photoFaceGroupId !== '') : []
  const detail = isRead ? files.data!.photo : null
  const isDescriptionRequested = detail?.isDescriptionRequested === true
  // The photo a request to describe is on its way for. The answer comes back
  // after the person may have moved on, and belongs to the photo it was asked
  // about, not to whichever one is shown by then.
  const [askingPhotoId, setAskingPhotoId] = useState<string | null>(null)
  const isAskingToDescribe = askingPhotoId === photo.id
  const shownPhotoId = useRef(photo.id)
  shownPhotoId.current = photo.id
  // Asked for here and not yet seen to arrive. The server says a description is
  // on its way once it has the request; this covers the moment before it does,
  // and a description that is asked for again, which has one already.
  const [isWaitingForDescription, setIsWaitingForDescription] = useState(false)
  const [isDescriptionSlow, setIsDescriptionSlow] = useState(false)
  const describedAtWhenAsked = useRef<string | null>(null)
  const hasSeenRequest = useRef(false)
  const [isLive, setIsLive] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [isChangingDate, setIsChangingDate] = useState(false)
  const [isDateBusy, setIsDateBusy] = useState(false)
  const [dateError, setDateError] = useState<string | null>(null)
  // A photo that is somebody else's has the owner's name and none of what is
  // theirs to change: no favorite, no Trash, no date, no description to ask for.
  const isOwn = photo.isOwnedByCaller
  const owner = useQuery(async () => (isOwn ? [] : await listCirclePeople()), [isOwn], { refresh: false })
  const ownerPerson = (owner.data ?? []).find((person) => person.userId === photo.userId) ?? null
  const viewerElement = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  // The shape of the picture as the browser drew it, for the photo it was read
  // from; the photo's own size until then.
  const [drawnShape, setDrawnShape] = useState<{ photoId: string; aspectRatio: number } | null>(null)
  // The original of a zoomed photo that has arrived, so that the fitted
  // picture stays under it until then.
  const [readyOriginal, setReadyOriginal] = useState('')
  const swipeStart = useRef<{ x: number; y: number } | null>(null)

  // Each photo starts fitted and not playing. Only when the photo changes, not
  // when the viewer opens: everything here already starts that way, and this
  // runs after the first paint, so a click that lands before it (the pencil
  // beside the date) would have what it just set undone.
  const previousPhotoId = useRef(photo.id)
  useEffect(() => {
    if (previousPhotoId.current === photo.id) return
    previousPhotoId.current = photo.id
    setIsLive(false)
    setIsWaitingForDescription(false)
    setAskingPhotoId(null)
    setIsDescriptionSlow(false)
    setIsChangingDate(false)
  }, [photo.id])

  // A description that is on its way is looked for every few seconds while
  // the viewer stays on the photo, and every fifteen after two minutes, when
  // the viewer also says it is taking longer than usual. It is shown when it
  // arrives, which is when the photo says it was described again, and it stops
  // when the server says nothing is on its way any more. A tab nobody is
  // looking at does not look: it looks once when it is shown again.
  const isDescribing = isDescriptionRequested || isWaitingForDescription
  const reloadFiles = files.reload
  useEffect(() => {
    if (!isDescribing) return
    const startedAt = Date.now()
    let timer = 0
    const look = () => {
      const isSlow = Date.now() - startedAt >= DESCRIPTION_SLOW_AFTER_MS
      if (isSlow) setIsDescriptionSlow(true)
      if (document.visibilityState !== 'hidden') void reloadFiles(true)
      timer = window.setTimeout(look, isSlow ? DESCRIPTION_SLOW_POLL_MS : DESCRIPTION_POLL_MS)
    }
    const lookWhenShown = () => {
      if (document.visibilityState === 'visible') void reloadFiles(true)
    }
    timer = window.setTimeout(look, DESCRIPTION_POLL_MS)
    document.addEventListener('visibilitychange', lookWhenShown)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', lookWhenShown)
    }
  }, [isDescribing, photo.id, reloadFiles])
  useEffect(() => {
    if (!isWaitingForDescription || !isRead) return
    if (isDescriptionRequested) hasSeenRequest.current = true
    const hasArrived = (detail?.describedAt ?? null) !== describedAtWhenAsked.current
    const hasBeenDropped = hasSeenRequest.current && !isDescriptionRequested
    if (hasArrived || hasBeenDropped) {
      setIsWaitingForDescription(false)
      setIsDescriptionSlow(false)
    }
  }, [isWaitingForDescription, isRead, isDescriptionRequested, detail?.describedAt])

  async function saveToLibrary() {
    if (!onSaveToLibrary) return
    setIsSaving(true)
    try {
      await onSaveToLibrary(photo)
    } finally {
      setIsSaving(false)
    }
  }

  async function changeDate(takenAt: string) {
    if (!onChangeDate) return
    setIsDateBusy(true)
    setDateError(null)
    try {
      await onChangeDate(photo, takenAt)
      setIsChangingDate(false)
    } catch (caught) {
      setDateError(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsDateBusy(false)
    }
  }

  async function describe(isRedescribed: boolean) {
    if (!onDescribe) return
    const askedPhoto = photo
    setAskingPhotoId(askedPhoto.id)
    setIsDescriptionSlow(false)
    describedAtWhenAsked.current = detail?.describedAt ?? null
    hasSeenRequest.current = false
    try {
      // The caller has said what went wrong when this rejects.
      await onDescribe(askedPhoto, isRedescribed)
      if (shownPhotoId.current === askedPhoto.id) {
        setIsWaitingForDescription(true)
        await files.reload(true)
      }
    } catch {
      // Said already.
    } finally {
      setAskingPhotoId((current) => (current === askedPhoto.id ? null : current))
    }
  }

  // The focus moves into the viewer when it opens, to the button that closes
  // it, and the element that opened the viewer gets it back when it closes.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    closeButton.current?.focus()
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
  const originalSource = canZoomOriginal ? original.fileUrl : ''
  const videoSource = photo.videoPreviewUrl || (isVideo ? photoFilePath(photo.id, 'original') : '')
  const liveSource = photo.videoPreviewUrl || photoFilePath(photo.id, 'liveVideo')

  const isPicture = !isVideo && Boolean(fittedSource)
  const drawnAspectRatio =
    drawnShape?.photoId === photo.id
      ? drawnShape.aspectRatio
      : photo.widthPixels > 0 && photo.heightPixels > 0
        ? photo.widthPixels / photo.heightPixels
        : 0
  // Zooming and panning the picture: the wheel, a pinch, a double click or tap,
  // and a drag of a picture that is zoomed.
  const zoom = useViewerZoom({ isEnabled: isPicture, resetKey: photo.id, aspectRatio: drawnAspectRatio })
  const isZoomed = zoom.isZoomed
  // A finger dragging the picture down closes the viewer; a zoomed picture,
  // one being pinched, a video and a photo with no picture yet are left alone.
  const isDraggable = !isZoomed && !zoom.isPinching && isPicture
  const { position: dragPosition, handlers: dragHandlers } = useDragToClose({ isEnabled: isDraggable, onClose })
  const dragAmount = dragPosition ? dragProgress(dragPosition.downPixels, window.innerHeight) : 0
  const dragTransition = dragPosition?.phase === 'dragging' ? 'none' : `transform ${DRAG_SETTLE_MS}ms ease-out`
  const pictureStyle: React.CSSProperties | undefined = dragPosition
    ? {
        transform: `translate(${dragPosition.acrossPixels}px, ${dragPosition.downPixels}px) scale(${1 - 0.3 * dragAmount})`,
        transition: dragTransition,
      }
    : undefined
  // The black behind fades with the drag, and the controls over it faster.
  const viewerStyle = dragPosition
    ? ({
        '--photo-viewer-fade': 1 - 0.8 * dragAmount,
        '--photo-chrome-fade': 1 - dragAmount,
      } as React.CSSProperties)
    : undefined
  const stripItems = stripPhotos(neighborPhotos ?? [], photo, previousPhoto, nextPhoto)
  const title = photoViewerTitle(photo, language, personTimeZone(), new Date())

  // The files the photo has that can be saved: the original always, and the
  // edited version and a Live Photo's video when it has them.
  const downloadChoices = DOWNLOADABLE_ROLES.filter(
    (choice) =>
      choice.photoFileRole === 'original' || photoFiles.some((file) => file.photoFileRole === choice.photoFileRole),
  )

  const goTo = useCallback(
    (target: Photo | null) => {
      if (target) onNavigate(target)
    },
    [onNavigate],
  )

  useEffect(() => {
    if (isPaused || isChangingDate) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return
      // An open menu takes the key first: Escape closes the menu, not the
      // viewer behind it.
      if (document.querySelector('.menu[role="menu"], .menu[role="dialog"]')) return
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
        return
      }
      // Tab goes round inside the viewer: the page behind it is not reachable
      // while it is open.
      if (event.key === 'Tab') {
        keepFocusInside(event, viewerElement.current)
        return
      }
      // A video's own controls use the arrows to seek, and a field uses them
      // to move its cursor.
      const target = event.target
      if (target instanceof HTMLElement && isTakingKeys(target)) return
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
  }, [isPaused, isChangingDate, onClose, goTo, previousPhoto, nextPhoto])

  // The photos either side are fetched ahead, so that an arrow key shows
  // the next one at once.
  useEffect(() => {
    for (const neighbor of [previousPhoto, nextPhoto]) {
      const source = neighbor && neighbor.photoKind !== 'video' ? neighbor.largePreviewUrl : ''
      if (source) new Image().src = source
    }
  }, [previousPhoto, nextPhoto])

  const toggleZoom = () => {
    if (isPicture) zoom.toggleAtCenter()
  }

  // What the picture is to somebody who cannot see it: the first sentence of
  // its description once it has one.
  const alt = altText(photo, detail?.photoDescription ?? '')
  const cameraText = [photo.cameraMake, photo.cameraModel]
    .filter((part, index, parts) => part && !(index === 1 && part.startsWith(parts[0])))
    .join(' ')
  // A reader of somebody else's photo may be given none of what a model wrote:
  // then there is no description to show, and no empty row for it.
  const hasDescriptionText = Boolean(detail?.photoDescription || detail?.descriptionVisibleText)
  const hasDescriptionToRead = Boolean(detail?.describedAt && hasDescriptionText)
  // The agent can be asked about a photo shared with the person as well as
  // about their own: it reads it by reference.
  const canAskAssistant = Boolean(onAskAssistant && fittedSource)
  const canChangeDate = isOwn && !isTrashed && onChangeDate !== undefined
  const canChangeLocation = isOwn && !isTrashed && onChangeLocation !== undefined
  // A position with no city near it is shown as it is.
  const coordinatesText =
    typeof photo.latitude === 'number' && typeof photo.longitude === 'number'
      ? `${photo.latitude.toFixed(4)}, ${photo.longitude.toFixed(4)}`
      : ''
  // What is not offered on the bar itself, because it is done once in a while
  // and the bar is for what is done to most photos: choosing the album's cover,
  // taking the photo out of the album, making its thumbnails again.
  const canMakeThumbnailsAgain = isOwn && onRetryProcessing !== undefined && photo.photoProcessingState === 'ready'
  const hasMoreActions = onSetCover !== undefined || onRemoveFromAlbum !== undefined || canMakeThumbnailsAgain
  const daysLeftInTrash = trashDaysLeft(photo.trashedAt, photoLibrary?.trashRetentionDays ?? 0, new Date())
  const details: [string, string][] = [
    [t('photos.detailDate'), photoDateTime(photo, language, personTimeZone())],
    [
      t('photos.detailPlace'),
      ownPlace?.placeName ||
        addressText ||
        photo.placeName ||
        coordinatesText ||
        (canChangeLocation ? t('photos.noPlace') : ''),
    ],
    [t('photos.detailCamera'), cameraText],
    [
      t('photos.detailKind'),
      photo.photoKind === 'video'
        ? t('photos.kindOneVideo')
        : photo.photoKind === 'livePhoto'
          ? t('photos.livePhoto')
          : t('photos.kindOnePicture'),
    ],
    [
      t('photos.detailSize'),
      photo.widthPixels > 0 && photo.heightPixels > 0
        ? t('photos.pixels', { width: photo.widthPixels, height: photo.heightPixels })
        : '',
    ],
    [t('photos.detailLength'), isVideo ? formatDuration(photo.durationMS) : ''],
    [t('photos.detailFileSize'), originalFile ? formatByteSize(originalFile.fileSizeBytes) : ''],
    [t('photos.detailFileName'), photo.originalFilename || originalFile?.originalFilename || ''],
    [
      t('photos.inTrash'),
      daysLeftInTrash === null
        ? ''
        : daysLeftInTrash === 0
          ? t('photos.trashLeftNone')
          : plural(daysLeftInTrash, { one: 'photos.trashLeftOne', other: 'photos.trashLeftOther' }),
    ],
  ]

  return (
    <>
      {createPortal(
        <div
          className={[
            'photo-viewer',
            isShowingDetails ? 'has-details' : '',
            dragPosition?.phase === 'dragging' ? 'is-dragging' : '',
          ]
            .filter(Boolean)
            .join(' ')}
          style={viewerStyle}
          role="dialog"
          aria-modal="true"
          aria-label={t('photos.viewer')}
          ref={viewerElement}
        >
          <div className="photo-viewer-main">
            <div className="photo-viewer-bar">
              <div className="photo-viewer-bar-start">
                <Tooltip label={t('common.close')}>
                  <button
                    type="button"
                    className="icon-button photo-round-button"
                    aria-label={t('common.close')}
                    ref={closeButton}
                    onClick={onClose}
                  >
                    <ChevronLeftIcon size={20} />
                  </button>
                </Tooltip>
              </div>
              <h2 className="photo-viewer-title">
                {title.dateText && (
                  <time dateTime={title.isoText}>
                    <span className="photo-viewer-date">{title.dateText}</span>
                    <span className="photo-viewer-time">{title.timeText}</span>
                  </time>
                )}
              </h2>
              <div className="photo-viewer-bar-end">
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
                      className={isZoomed ? 'icon-button photo-round-button is-on' : 'icon-button photo-round-button'}
                      aria-label={isZoomed ? t('photos.zoomOut') : t('photos.zoomIn')}
                      aria-pressed={isZoomed}
                      onClick={toggleZoom}
                    >
                      <ZoomInIcon size={18} />
                    </button>
                  </Tooltip>
                )}
                {!isTrashed && hasMoreActions && (
                  <Tooltip label={t('photos.more')}>
                    <MenuButton
                      label={t('photos.more')}
                      className="icon-button photo-round-button"
                      icon={<MoreIcon size={18} />}
                      render={(close) => (
                        <>
                          {onSetCover && (
                            <button
                              type="button"
                              role="menuitem"
                              onClick={() => {
                                close()
                                void onSetCover(photo)
                              }}
                            >
                              <AlbumCoverIcon size={16} />
                              {t('photos.useAsCover')}
                            </button>
                          )}
                          {onRemoveFromAlbum && (
                            <button
                              type="button"
                              role="menuitem"
                              onClick={() => {
                                close()
                                void onRemoveFromAlbum(photo)
                              }}
                            >
                              <AlbumRemoveIcon size={16} />
                              {t('photos.removeFromAlbum')}
                            </button>
                          )}
                          {canMakeThumbnailsAgain && onRetryProcessing && (
                            <button
                              type="button"
                              role="menuitem"
                              disabled={isBusy}
                              onClick={() => {
                                close()
                                void onRetryProcessing(photo)
                              }}
                            >
                              <RefreshIcon size={16} />
                              {t('photos.makeThumbnailsAgain')}
                            </button>
                          )}
                        </>
                      )}
                    />
                  </Tooltip>
                )}
              </div>
            </div>
            <div
              className={[
                'photo-viewer-stage',
                isPicture ? 'is-zoomable' : '',
                isDraggable ? 'is-draggable' : '',
                isZoomed ? 'is-zoomed' : '',
                zoom.isPanning ? 'is-panning' : '',
              ]
                .filter(Boolean)
                .join(' ')}
              ref={zoom.stage}
              onPointerDown={(event) => {
                zoom.handlers.onPointerDown(event)
                dragHandlers.onPointerDown(event)
              }}
              onPointerMove={(event) => {
                zoom.handlers.onPointerMove(event)
                dragHandlers.onPointerMove(event)
              }}
              onPointerUp={(event) => {
                zoom.handlers.onPointerUp(event)
                dragHandlers.onPointerUp(event)
              }}
              onPointerCancel={(event) => {
                zoom.handlers.onPointerCancel(event)
                dragHandlers.onPointerCancel(event)
              }}
              onLostPointerCapture={zoom.handlers.onLostPointerCapture}
              onTouchStart={(event) => {
                const touch = event.touches[0]
                swipeStart.current =
                  event.touches.length === 1 && !isZoomed ? { x: touch.clientX, y: touch.clientY } : null
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
                <div className="photo-viewer-message-block">
                  <p className="photo-viewer-message">
                    {photo.photoProcessingState === 'failed'
                      ? photo.processingErrorMessage
                        ? t('photos.failed', { message: photo.processingErrorMessage })
                        : t('photos.failedNoMessage')
                      : t('photos.waiting')}
                  </p>
                  {photo.photoProcessingState === 'failed' && onRetryProcessing && isOwn && !isTrashed && (
                    <button type="button" disabled={isBusy} onClick={() => void onRetryProcessing(photo)}>
                      {t('photos.tryAgain')}
                    </button>
                  )}
                </div>
              ) : (
                <div
                  className={zoom.isEasing ? 'photo-viewer-picture is-easing' : 'photo-viewer-picture'}
                  style={isZoomed ? { transform: viewTransform(zoom.view) } : pictureStyle}
                  onDoubleClick={zoom.onDoubleClick}
                >
                  <img
                    className="photo-fitted"
                    key={photo.id}
                    src={fittedSource}
                    alt={alt}
                    draggable={false}
                    onLoad={(event) => {
                      const { naturalWidth, naturalHeight } = event.currentTarget
                      if (naturalWidth > 0 && naturalHeight > 0) {
                        setDrawnShape({ photoId: photo.id, aspectRatio: naturalWidth / naturalHeight })
                      }
                    }}
                  />
                  {isZoomed && originalSource && (
                    // The file itself over the preview once it has arrived, so
                    // that a zoom is sharp and the picture does not blink.
                    <img
                      className={
                        readyOriginal === originalSource
                          ? 'photo-fitted photo-original is-ready'
                          : 'photo-fitted photo-original'
                      }
                      key={originalSource}
                      src={originalSource}
                      alt=""
                      aria-hidden="true"
                      draggable={false}
                      onLoad={() => setReadyOriginal(originalSource)}
                    />
                  )}
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
            {stripItems.length > 1 && <ViewerStrip photos={stripItems} current={photo} onShow={goTo} />}
            <div className="photo-viewer-actions">
              <div className="photo-viewer-actions-start">
                {!isTrashed &&
                  (downloadChoices.length > 1 ? (
                    <Tooltip label={t('photos.download')}>
                      <MenuButton
                        label={t('photos.download')}
                        className="icon-button photo-round-button"
                        icon={<DownloadIcon size={20} />}
                        placement="above"
                        render={(close) =>
                          downloadChoices.map((choice) => (
                            <a
                              key={choice.photoFileRole}
                              role="menuitem"
                              href={photoFilePath(photo.id, choice.photoFileRole)}
                              download
                              onClick={close}
                            >
                              {t(choice.label)}
                            </a>
                          ))
                        }
                      />
                    </Tooltip>
                  ) : (
                    <Tooltip label={t('photos.downloadOriginal')}>
                      <a
                        className="icon-button photo-round-button"
                        href={photoFilePath(photo.id, 'original')}
                        download
                        aria-label={t('photos.downloadOriginal')}
                      >
                        <DownloadIcon size={20} />
                      </a>
                    </Tooltip>
                  ))}
              </div>
              <div className="photo-action-group" role="group" aria-label={t('photos.photoActions')}>
                {isTrashed ? (
                  <Tooltip label={t('photos.restore')}>
                    <button
                      type="button"
                      className="icon-button photo-round-button"
                      aria-label={t('photos.restore')}
                      disabled={isBusy}
                      onClick={() => onRestore(photo)}
                    >
                      <RestoreIcon size={20} />
                    </button>
                  </Tooltip>
                ) : isOwn ? (
                  <Tooltip label={photo.isFavorite ? t('photos.unfavorite') : t('photos.favorite')}>
                    <button
                      type="button"
                      className={
                        photo.isFavorite
                          ? 'icon-button photo-round-button photo-favorite is-on'
                          : 'icon-button photo-round-button photo-favorite'
                      }
                      aria-label={photo.isFavorite ? t('photos.unfavorite') : t('photos.favorite')}
                      aria-pressed={photo.isFavorite}
                      onClick={() => onToggleFavorite(photo)}
                    >
                      <HeartIcon size={20} />
                    </button>
                  </Tooltip>
                ) : (
                  onSaveToLibrary && (
                    <button
                      type="button"
                      className="photo-save-button"
                      disabled={isSaving}
                      onClick={() => void saveToLibrary()}
                    >
                      <PhotoSaveIcon size={16} /> <span className="photo-save-label">{t('photos.saveToLibrary')}</span>
                    </button>
                  )
                )}
                <Tooltip label={t('photos.details')}>
                  <button
                    type="button"
                    className={
                      isShowingDetails ? 'icon-button photo-round-button is-on' : 'icon-button photo-round-button'
                    }
                    aria-label={t('photos.details')}
                    aria-pressed={isShowingDetails}
                    onClick={() => setIsShowingDetails((previous) => !previous)}
                  >
                    <InfoIcon size={20} />
                  </button>
                </Tooltip>
                {!isTrashed && isOwn && (
                  <AddToAlbumMenu
                    hiddenAlbumId={photoAlbumId}
                    placement="above"
                    isRound
                    onChooseAlbum={(target) => void onAddToAlbum(photo, target).then(() => files.reload(true))}
                    onNewAlbum={() => onNewAlbum(photo)}
                  />
                )}
              </div>
              <div className="photo-viewer-actions-end">
                {isTrashed ? (
                  <Tooltip label={t('photos.deleteForever')}>
                    <button
                      type="button"
                      className="icon-button photo-round-button danger"
                      aria-label={t('photos.deleteForever')}
                      disabled={isBusy}
                      onClick={() => onDeleteForever(photo)}
                    >
                      <TrashIcon size={20} />
                    </button>
                  </Tooltip>
                ) : (
                  isOwn && (
                    <Tooltip label={t('photos.trash')}>
                      <button
                        type="button"
                        className="icon-button photo-round-button"
                        aria-label={t('photos.trash')}
                        disabled={isBusy}
                        onClick={() => onTrash(photo)}
                      >
                        <TrashIcon size={20} />
                      </button>
                    </Tooltip>
                  )
                )}
              </div>
            </div>
          </div>
          {isShowingDetails && (
            <aside className="photo-viewer-details" aria-label={t('photos.details')}>
              <h2>{t('photos.details')}</h2>
              {Boolean(files.error) && !isRead && !files.loading && (
                <div className="photo-viewer-failure" role="alert">
                  <p>{t('photos.detailsFailed')}</p>
                  <button type="button" onClick={() => void files.reload()}>
                    {t('photos.tryAgain')}
                  </button>
                </div>
              )}
              <dl>
                {!isOwn && photo.ownerName && (
                  <div>
                    <dt>{t('photos.detailOwner')}</dt>
                    <dd>
                      <span className="photo-owner-mark">
                        <Avatar
                          name={photo.ownerName}
                          avatarUrl={ownerPerson?.avatarUrl}
                          colorIndex={ownerPerson?.avatarColorIndex}
                          size={20}
                        />
                        <span className="photo-owner-name">{photo.ownerName}</span>
                      </span>
                    </dd>
                  </div>
                )}
                {details
                  .filter(([, value]) => value !== '')
                  .map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>
                        {ownPlace && label === t('photos.detailPlace') ? (
                          <Link to={`/location?tab=places&place=${encodeURIComponent(ownPlace.id)}`}>{value}</Link>
                        ) : (
                          value
                        )}
                        {canChangeLocation && label === t('photos.detailPlace') && (
                          <Tooltip label={t('photos.changeLocation')}>
                            <button
                              type="button"
                              className="icon-action photo-change-location"
                              aria-label={t('photos.changeLocation')}
                              onClick={() => onChangeLocation(photo)}
                            >
                              <PencilIcon size={14} />
                            </button>
                          </Tooltip>
                        )}
                        {label === t('photos.detailPlace') &&
                          onShowNearby &&
                          typeof photo.latitude === 'number' &&
                          typeof photo.longitude === 'number' && (
                            <button type="button" className="photo-near-button" onClick={() => onShowNearby(photo)}>
                              {t('photos.showNearby')}
                            </button>
                          )}
                        {label === t('photos.detailPlace') && hasPosition && (
                          <div className="photo-viewer-map">
                            <LocationMap
                              key={`${photo.latitude},${photo.longitude}`}
                              people={[]}
                              places={[
                                {
                                  id: photo.id,
                                  placeName: value,
                                  placeLabel: ownPlace?.placeLabel ?? '',
                                  latitudeDegrees: photo.latitude as number,
                                  longitudeDegrees: photo.longitude as number,
                                  radiusMeters: 0,
                                },
                              ]}
                              singlePointZoom={PHOTO_MAP_ZOOM}
                              isScrollZoomEnabled={false}
                              mapLabel={t('photos.detailMapLabel')}
                            />
                          </div>
                        )}
                        {canChangeDate && label === t('photos.detailDate') && (
                          <Tooltip label={t('photos.changeDate')}>
                            <button
                              type="button"
                              className="icon-action photo-change-date"
                              aria-label={t('photos.changeDate')}
                              onClick={() => {
                                setDateError(null)
                                setIsChangingDate(true)
                              }}
                            >
                              <PencilIcon size={14} />
                            </button>
                          </Tooltip>
                        )}
                      </dd>
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
                        isLinked={isOwn}
                        onNamed={() => files.reload(true)}
                        onNotThem={
                          isOwn && onRemoveFaceFromGroup
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
              {isRead && (isOwn || hasDescriptionToRead || canAskAssistant) && (
                <section
                  className="photo-viewer-section"
                  aria-labelledby={isOwn || hasDescriptionToRead ? 'photo-viewer-description-heading' : undefined}
                >
                  {/* A photo shared with the person that nobody described has no
                      description to show, but the agent can still be asked about it. */}
                  {(isOwn || hasDescriptionToRead) && (
                    <div className="photo-viewer-section-head">
                      <h3 id="photo-viewer-description-heading">{t('photos.description')}</h3>
                      {detail?.describedAt && isOwn && onDescribe && photoLibrary?.isDescriptionAvailable && (
                        <Tooltip label={t('photos.describeAgain')}>
                          <button
                            type="button"
                            className="icon-action"
                            aria-label={t('photos.describeAgain')}
                            disabled={isAskingToDescribe || isDescribing}
                            onClick={() => void describe(true)}
                          >
                            <RefreshIcon size={16} />
                          </button>
                        </Tooltip>
                      )}
                    </div>
                  )}
                  {detail?.describedAt && hasDescriptionText && (
                    <>
                      {detail.photoDescription && <p className="photo-description">{detail.photoDescription}</p>}
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
                  {isDescribing || isAskingToDescribe ? (
                    <p className="muted photo-describing" role="status">
                      <span className="photo-spinner" aria-hidden="true" />
                      {isDescriptionSlow
                        ? t('photos.describingSlow')
                        : detail?.describedAt
                          ? t('photos.describingAgain')
                          : t('photos.describing')}
                    </p>
                  ) : !detail?.describedAt && photoLibrary && isOwn ? (
                    photoLibrary.isDescriptionAvailable && onDescribe ? (
                      <>
                        <p className="muted">{assistantWords.describeHint(isVideo)}</p>
                        <button type="button" className="photo-viewer-action" onClick={() => void describe(false)}>
                          {isVideo ? t('photos.describeVideo') : t('photos.describe')}
                        </button>
                      </>
                    ) : (
                      <p className="muted">
                        {photoLibrary.descriptionUnavailableMessage || t('photos.describeUnavailable')}
                      </p>
                    )
                  ) : null}
                  {canAskAssistant && (
                    <button type="button" className="photo-viewer-action" onClick={() => onAskAssistant?.(photo)}>
                      <SparkIcon size={16} /> {assistantWords.askAbout(isVideo)}
                    </button>
                  )}
                </section>
              )}
            </aside>
          )}
        </div>,
        document.body,
      )}
      {isChangingDate && (
        <ChangeDateDialog
          photo={photo}
          isBusy={isDateBusy}
          error={dateError}
          onSubmit={(takenAt) => void changeDate(takenAt)}
          onClose={() => setIsChangingDate(false)}
        />
      )}
    </>
  )
}

// One person in the photo: the face, the name (a link to the person's page),
// and "Not <name>" for a face the server put in the wrong group. On a screen
// with a pointer the action shows when the row is hovered or focused; on a
// touch screen it is always there, since there is no hover to reveal it.
function ViewerPerson({
  face,
  isLinked,
  onNotThem,
  onNamed,
}: {
  face: PhotoFace
  // The person's page is the photo owner's; somebody else's photo has none to open.
  isLinked: boolean
  onNotThem?: () => Promise<void>
  // Called once an unnamed person is named here, so that the photo's people are read again.
  onNamed?: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [isBusy, setIsBusy] = useState(false)
  const [isNaming, setIsNaming] = useState(false)
  const name = face.contactName.trim()
  const shownName = name || t('photos.unnamedPerson')
  const notLabel = name ? t('photos.notPerson', { name }) : t('photos.notThisPerson')
  // An unnamed person is named where it is seen: their page is not one the
  // People list shows yet, and leaving the photo to name them loses it.
  const isNameable = isLinked && !name && face.photoFaceGroupId !== ''
  return (
    <li className="photo-viewer-person">
      {isNameable ? (
        <Tooltip label={t('photos.nameThisPerson')}>
          <button
            type="button"
            className="photo-viewer-person-link photo-viewer-person-name-button"
            onClick={() => setIsNaming(true)}
          >
            <FacePicture facePictureUrl={face.facePictureUrl} name={shownName} size="small" />
            <span className="photo-viewer-person-name muted">{shownName}</span>
          </button>
        </Tooltip>
      ) : isLinked && face.photoFaceGroupId !== '' ? (
        <Link className="photo-viewer-person-link" to={personAddress(face.photoFaceGroupId)}>
          <FacePicture facePictureUrl={face.facePictureUrl} name={shownName} size="small" />
          <span className={name ? 'photo-viewer-person-name' : 'photo-viewer-person-name muted'}>{shownName}</span>
          {face.isOwnContact && <YouMark />}
        </Link>
      ) : (
        <span className="photo-viewer-person-link">
          <FacePicture facePictureUrl={face.facePictureUrl} name={shownName} size="small" />
          <span className={name ? 'photo-viewer-person-name' : 'photo-viewer-person-name muted'}>{shownName}</span>
          {face.isOwnContact && <YouMark />}
        </span>
      )}
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
      {isNaming && (
        <NamePersonFlow
          group={{
            id: face.photoFaceGroupId,
            createdAt: '',
            updatedAt: '',
            contactId: face.contactId,
            contactAddressBookId: '',
            contactName: face.contactName,
            isOwnContact: face.isOwnContact,
            faceCount: 0,
            photoCount: 0,
            facePictureUrl: face.facePictureUrl,
            coverPhotoFaceId: face.id,
            isHidden: false,
          }}
          onClose={() => setIsNaming(false)}
          onNamed={() => void onNamed?.()}
        />
      )}
    </li>
  )
}
