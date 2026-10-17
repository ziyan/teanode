import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { PageHeadingActions } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { CloseIcon, DownloadIcon, HeartIcon, RestoreIcon, TrashIcon, UploadIcon } from '../../components/icons'
import { Pager, useKeepPageInRange, usePageInAddress } from '../../components/pager'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation, type Translation } from '../../i18n/i18n'
import {
  PHOTOS_PER_PAGE,
  countPhotos,
  deletePhotosForever,
  emptyPhotoTrash,
  listPhotos,
  photoFilePath,
  readPhoto,
  readPhotoLibrary,
  restorePhotos,
  setPhotoFavorite,
  trashPhotos,
  type Photo,
  type PhotoListKind,
} from '../../photos/api'
import { formatByteSize, indexRange } from '../../photos/days'
import { photoUploads, useUploadItems } from '../../photos/uploadQueue'
import { useSession } from '../../session'
import { PhotoGrid } from './grid'
import { UploadTray } from './uploadTray'
import { PhotoViewer } from './viewer'

// The Photos page: the timeline of the person's library with its three
// lists, the viewer over it, selection, and uploading. The list, the search,
// the page and the photo being looked at are all in the address, so a
// reload, a link and the Back button each come back to where the person was.

// How often a list that holds photos still being made ready is read again.
const WAITING_POLL_MS = 3000
// How long after an upload finishes the list is read again; a burst of files
// finishing together is one read.
const UPLOADED_REFRESH_MS = 800
// How many favorites are changed at once.
const FAVORITE_CONCURRENCY = 6
// The gap between downloads when several are saved together, which browsers
// otherwise take for a flood and block.
const DOWNLOAD_GAP_MS = 300

const LIST_KINDS: { kind: PhotoListKind; label: 'photos.listLibrary' | 'photos.listFavorites' | 'photos.listTrash' }[] =
  [
    { kind: 'library', label: 'photos.listLibrary' },
    { kind: 'favorites', label: 'photos.listFavorites' },
    { kind: 'trash', label: 'photos.listTrash' },
  ]

type Confirmation = { kind: 'trash'; photos: Photo[] } | { kind: 'delete'; photos: Photo[] } | { kind: 'emptyTrash' }

export function PhotosPage() {
  const session = useSession()
  return <AccountPhotosPage key={session.userId ?? ''} />
}

function AccountPhotosPage() {
  const { t, plural } = useTranslation()
  const session = useSession()
  const toast = useToast()
  const navigate = useNavigate()
  const location = useLocation()
  const { photoId } = useParams()
  const [parameters, setParameters] = useSearchParams()
  const fileInput = useRef<HTMLInputElement>(null)
  const [isDragging, setIsDragging] = useState(false)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(new Set())
  const anchorIndex = useRef<number | null>(null)
  const [favoriteOverrides, setFavoriteOverrides] = useState<ReadonlyMap<string, boolean>>(new Map())

  const listKind: PhotoListKind =
    parameters.get('view') === 'favorites' ? 'favorites' : parameters.get('view') === 'trash' ? 'trash' : 'library'
  const searchText = parameters.get('search') ?? ''
  const [searchInput, setSearchInput] = useState(searchText)
  const { pageIndex, pageSize, offset, setPageIndex } = usePageInAddress({ defaultPageSize: PHOTOS_PER_PAGE })
  const filter = useMemo(() => ({ listKind, searchText }), [listKind, searchText])
  const userId = session.userId ?? ''

  // The search is sent once typing pauses, and starts again at the first
  // page.
  useEffect(() => {
    if (searchInput.trim() === searchText) return
    const timer = window.setTimeout(() => {
      setParameters(
        (previous) => {
          const written = new URLSearchParams(previous)
          if (searchInput.trim()) written.set('search', searchInput.trim())
          else written.delete('search')
          written.delete('page')
          return written
        },
        { replace: true },
      )
    }, 300)
    return () => window.clearTimeout(timer)
  }, [searchInput, searchText, setParameters])

  const library = useQuery(() => listPhotos(filter, offset, pageSize), [filter, offset, pageSize, userId])
  const count = useQuery(() => countPhotos(filter), [filter, userId])
  const info = useQuery(() => readPhotoLibrary(), [userId])
  useKeepPageInRange(pageIndex, pageSize, count.data ?? null, setPageIndex)

  const photos = useMemo(
    () =>
      (library.data ?? []).map((photo) =>
        favoriteOverrides.has(photo.id) ? { ...photo, isFavorite: favoriteOverrides.get(photo.id)! } : photo,
      ),
    [library.data, favoriteOverrides],
  )
  // What the server says wins again once it has been asked.
  useEffect(() => setFavoriteOverrides(new Map()), [library.data])
  // A different list, search or page is a different set of cells.
  useEffect(() => {
    setSelectedIds(new Set())
    anchorIndex.current = null
  }, [listKind, searchText, offset])

  const refresh = useCallback(() => {
    void library.reload(true)
    void count.reload(true)
    void info.reload(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [library.reload, count.reload, info.reload])
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh

  // Photos that are still being made ready are looked at again soon, rather
  // than after the page's slow refresh.
  const hasWaitingPhotos = photos.some((photo) => photo.photoProcessingState === 'waiting')
  useEffect(() => {
    if (!hasWaitingPhotos) return
    const timer = window.setInterval(() => void library.reload(true), WAITING_POLL_MS)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasWaitingPhotos, library.reload])

  // ---- Uploading ----

  const uploadItems = useUploadItems()
  const isUploading = uploadItems.some(
    (item) => item.state === 'waiting' || item.state === 'hashing' || item.state === 'sending',
  )
  useEffect(() => {
    photoUploads.setOwner(userId)
  }, [userId])
  useEffect(() => {
    if (info.data?.uploadPartBytes) photoUploads.setPartBytes(info.data.uploadPartBytes)
  }, [info.data?.uploadPartBytes])
  // Leaving the tab while files are on their way loses them.
  useEffect(() => {
    if (!isUploading) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [isUploading])

  const speak = useRef({ toast, t, plural })
  speak.current = { toast, t, plural }
  useEffect(() => {
    let refreshTimer: number | null = null
    let hasToldOfFailure = false
    const stopListening = photoUploads.onEvent((event) => {
      const { toast: toaster, t: translate, plural: pluralize } = speak.current
      if (event.kind === 'itemDone') {
        if (refreshTimer !== null) window.clearTimeout(refreshTimer)
        refreshTimer = window.setTimeout(() => refreshRef.current(), UPLOADED_REFRESH_MS)
      } else if (event.kind === 'itemFailed') {
        // The tray lists every failure; one toast says that there was one.
        if (!hasToldOfFailure) {
          hasToldOfFailure = true
          toaster.failed(
            translate('photos.uploadFailedFile', { name: event.item.name, message: event.item.errorMessage }),
          )
        }
      } else {
        hasToldOfFailure = false
        const { uploadedCount, skippedCount, failedCount } = event.summary
        const sentences = [
          uploadedCount > 0 && pluralize(uploadedCount, { one: 'photos.uploadedOne', other: 'photos.uploadedOther' }),
          skippedCount > 0 && pluralize(skippedCount, { one: 'photos.skippedOne', other: 'photos.skippedOther' }),
          failedCount > 0 &&
            pluralize(failedCount, { one: 'photos.failedUploadOne', other: 'photos.failedUploadOther' }),
        ].filter(Boolean)
        if (failedCount > 0) toaster.failed(sentences.join(' '))
        else toaster.done(sentences.join(' '))
        refreshRef.current()
      }
    })
    return () => {
      stopListening()
      if (refreshTimer !== null) window.clearTimeout(refreshTimer)
    }
  }, [])

  const addFiles = (files: File[]) => {
    const { addedCount, ignoredCount } = photoUploads.add(files)
    if (ignoredCount > 0 && addedCount >= 0) toast.failed(t('photos.uploadIgnored'))
  }
  const hasFiles = (event: React.DragEvent) => Array.from(event.dataTransfer.types).includes('Files')

  // ---- The viewer and its address ----

  const viewerIndex = photoId ? photos.findIndex((photo) => photo.id === photoId) : -1
  const lookup = useQuery(
    async () => (photoId && viewerIndex < 0 ? await readPhoto(photoId).catch(() => null) : null),
    [photoId, viewerIndex < 0, userId],
    { refresh: false },
  )
  const viewerPhoto = viewerIndex >= 0 ? photos[viewerIndex] : (lookup.data?.photo ?? null)
  const viewerPrevious = viewerIndex > 0 ? photos[viewerIndex - 1] : null
  const viewerNext = viewerIndex >= 0 && viewerIndex < photos.length - 1 ? photos[viewerIndex + 1] : null

  const openPhoto = (photo: Photo) =>
    navigate(
      { pathname: `/photos/${encodeURIComponent(photo.id)}`, search: location.search },
      { state: { isFromGrid: true } },
    )
  const showPhoto = (photo: Photo) =>
    navigate(
      { pathname: `/photos/${encodeURIComponent(photo.id)}`, search: location.search },
      { replace: true, state: location.state },
    )
  const closeViewer = useCallback(() => {
    // Opened from the grid, the viewer is one step on the way back; opened
    // from a link, there is no step to go back to.
    if ((location.state as { isFromGrid?: boolean } | null)?.isFromGrid) navigate(-1)
    else navigate({ pathname: '/photos', search: location.search }, { replace: true })
  }, [location.state, location.search, navigate])
  // After the photo in the viewer leaves the list, the viewer moves on to the
  // one beside it, or closes when it was the only one.
  const moveViewerOn = () => {
    const after = viewerNext ?? viewerPrevious
    if (after) showPhoto(after)
    else closeViewer()
  }

  // ---- Changing photos ----

  const failedWith = (caught: unknown) => toast.failure(caught, t('photos.actionFailed'))

  async function setFavorites(targets: Photo[], isFavorite: boolean) {
    setFavoriteOverrides(
      (previous) => new Map([...previous, ...targets.map((photo) => [photo.id, isFavorite] as const)]),
    )
    try {
      for (let start = 0; start < targets.length; start += FAVORITE_CONCURRENCY) {
        await Promise.all(
          targets.slice(start, start + FAVORITE_CONCURRENCY).map((photo) => setPhotoFavorite(photo.id, isFavorite)),
        )
      }
      refresh()
    } catch (caught) {
      setFavoriteOverrides(new Map())
      failedWith(caught)
      refresh()
    }
  }

  async function restore(targets: Photo[]) {
    setIsBusy(true)
    try {
      const restoredCount = await restorePhotos(targets.map((photo) => photo.id))
      toast.done(plural(restoredCount, { one: 'photos.restoredOne', other: 'photos.restoredOther' }))
      if (targets.some((photo) => photo.id === photoId)) moveViewerOn()
      setSelectedIds(new Set())
      refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  async function confirmTrash(targets: Photo[]) {
    setIsBusy(true)
    try {
      const trashedCount = await trashPhotos(targets.map((photo) => photo.id))
      const trashedIds = targets.map((photo) => photo.id)
      toast.done(plural(trashedCount, { one: 'photos.trashedOne', other: 'photos.trashedOther' }), {
        label: t('common.undo'),
        run: async () => {
          try {
            await restorePhotos(trashedIds)
            refreshRef.current()
          } catch (caught) {
            failedWith(caught)
          }
        },
      })
      setConfirmation(null)
      if (trashedIds.includes(photoId ?? '')) moveViewerOn()
      setSelectedIds(new Set())
      refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  async function confirmDelete(targets: Photo[]) {
    setIsBusy(true)
    try {
      const deletedCount = await deletePhotosForever(targets.map((photo) => photo.id))
      toast.done(plural(deletedCount, { one: 'photos.deletedOne', other: 'photos.deletedOther' }))
      setConfirmation(null)
      if (targets.some((photo) => photo.id === photoId)) moveViewerOn()
      setSelectedIds(new Set())
      refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  async function confirmEmptyTrash() {
    setIsBusy(true)
    try {
      const deletedCount = await emptyPhotoTrash()
      toast.done(plural(deletedCount, { one: 'photos.deletedOne', other: 'photos.deletedOther' }))
      setConfirmation(null)
      setSelectedIds(new Set())
      if (photoId) closeViewer()
      refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  // One file each; several are several downloads, a moment apart.
  async function downloadOriginals(targets: Photo[]) {
    for (const photo of targets) {
      const link = document.createElement('a')
      link.href = photoFilePath(photo.id, 'original')
      link.download = photo.originalFilename || ''
      document.body.appendChild(link)
      link.click()
      link.remove()
      if (targets.length > 1) await new Promise((resolve) => window.setTimeout(resolve, DOWNLOAD_GAP_MS))
    }
  }

  // ---- Selection ----

  function toggleSelection(index: number, isRange: boolean) {
    setSelectedIds((previous) => {
      const next = new Set(previous)
      const clicked = photos[index]
      if (isRange && anchorIndex.current !== null && anchorIndex.current < photos.length) {
        const chosen = !previous.has(clicked.id)
        for (const rangeIndex of indexRange(anchorIndex.current, index)) {
          if (chosen) next.add(photos[rangeIndex].id)
          else next.delete(photos[rangeIndex].id)
        }
      } else if (previous.has(clicked.id)) {
        next.delete(clicked.id)
      } else {
        next.add(clicked.id)
      }
      return next
    })
    anchorIndex.current = index
  }

  const selectedPhotos = photos.filter((photo) => selectedIds.has(photo.id))
  const areAllSelectedFavorites = selectedPhotos.length > 0 && selectedPhotos.every((photo) => photo.isFavorite)

  const setListKind = (kind: PhotoListKind) =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      if (kind === 'library') written.delete('view')
      else written.set('view', kind)
      written.delete('page')
      return written
    })

  // ---- Drawing ----

  const isTrash = listKind === 'trash'
  const totalCount = count.data ?? 0
  const emptyText =
    searchText !== ''
      ? t('photos.emptySearch')
      : listKind === 'favorites'
        ? t('photos.emptyFavorites')
        : isTrash
          ? t('photos.emptyTrash')
          : t('photos.emptyLibrary')
  const trashedCount = info.data?.trashedCount ?? 0
  const trashDays = info.data?.trashRetentionDays ?? 0

  return (
    <div
      className="photos-page"
      onDragEnter={(event) => {
        if (!hasFiles(event)) return
        event.preventDefault()
        setIsDragging(true)
      }}
      onDragOver={(event) => {
        if (!hasFiles(event)) return
        event.preventDefault()
        event.dataTransfer.dropEffect = 'copy'
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setIsDragging(false)
      }}
      onDrop={(event) => {
        if (!hasFiles(event)) return
        event.preventDefault()
        setIsDragging(false)
        addFiles(Array.from(event.dataTransfer.files))
      }}
    >
      <ErrorMessage error={library.error} />
      <PageHeadingActions>
        <button type="button" className="primary" onClick={() => fileInput.current?.click()}>
          <UploadIcon size={16} /> {t('photos.upload')}
        </button>
      </PageHeadingActions>
      <input
        ref={fileInput}
        type="file"
        className="visually-hidden"
        accept="image/*,video/*"
        multiple
        tabIndex={-1}
        aria-label={t('photos.upload')}
        onChange={(event) => {
          addFiles(Array.from(event.target.files ?? []))
          // The same files can be chosen again.
          event.target.value = ''
        }}
      />

      {selectedPhotos.length > 0 ? (
        <div
          className="photo-toolbar photo-selection-bar"
          role="toolbar"
          aria-label={plural(selectedPhotos.length, { one: 'photos.selectedOne', other: 'photos.selectedOther' })}
        >
          <Tooltip label={t('photos.clearSelection')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('photos.clearSelection')}
              onClick={() => setSelectedIds(new Set())}
            >
              <CloseIcon size={18} />
            </button>
          </Tooltip>
          <strong>{plural(selectedPhotos.length, { one: 'photos.selectedOne', other: 'photos.selectedOther' })}</strong>
          <span className="photo-viewer-spacer" />
          {isTrash ? (
            <>
              <Tooltip label={t('photos.restore')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('photos.restore')}
                  disabled={isBusy}
                  onClick={() => void restore(selectedPhotos)}
                >
                  <RestoreIcon size={18} />
                </button>
              </Tooltip>
              <Tooltip label={t('photos.deleteForever')}>
                <button
                  type="button"
                  className="icon-button danger"
                  aria-label={t('photos.deleteForever')}
                  disabled={isBusy}
                  onClick={() => setConfirmation({ kind: 'delete', photos: selectedPhotos })}
                >
                  <TrashIcon size={18} />
                </button>
              </Tooltip>
            </>
          ) : (
            <>
              <Tooltip label={areAllSelectedFavorites ? t('photos.unfavorite') : t('photos.favorite')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={areAllSelectedFavorites ? t('photos.unfavorite') : t('photos.favorite')}
                  onClick={() => void setFavorites(selectedPhotos, !areAllSelectedFavorites)}
                >
                  <HeartIcon size={18} />
                </button>
              </Tooltip>
              <Tooltip label={t('photos.download')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('photos.download')}
                  onClick={() => void downloadOriginals(selectedPhotos)}
                >
                  <DownloadIcon size={18} />
                </button>
              </Tooltip>
              <Tooltip label={t('photos.trash')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('photos.trash')}
                  disabled={isBusy}
                  onClick={() => setConfirmation({ kind: 'trash', photos: selectedPhotos })}
                >
                  <TrashIcon size={18} />
                </button>
              </Tooltip>
            </>
          )}
        </div>
      ) : (
        <div className="photo-toolbar">
          <input
            type="search"
            value={searchInput}
            placeholder={t('photos.search')}
            aria-label={t('photos.search')}
            onChange={(event) => setSearchInput(event.target.value)}
          />
          <div className="segmented" role="group" aria-label={t('photos.show')}>
            {LIST_KINDS.map((entry) => (
              <button
                key={entry.kind}
                type="button"
                className={entry.kind === listKind ? 'active' : ''}
                aria-pressed={entry.kind === listKind}
                onClick={() => setListKind(entry.kind)}
              >
                {t(entry.label)}
              </button>
            ))}
          </div>
          {isTrash && trashedCount > 0 && (
            <button type="button" className="danger" onClick={() => setConfirmation({ kind: 'emptyTrash' })}>
              {t('photos.emptyTrashButton')}
            </button>
          )}
        </div>
      )}

      {isTrash && trashDays > 0 && (
        <p className="muted photo-trash-note">
          {plural(trashDays, { one: 'photos.trashRetentionOne', other: 'photos.trashRetentionOther' })}
        </p>
      )}

      {library.loading && !library.data ? (
        <Loading />
      ) : photos.length === 0 ? (
        <SettingsEmpty>{emptyText}</SettingsEmpty>
      ) : (
        <PhotoGrid photos={photos} selectedIds={selectedIds} onOpen={openPhoto} onToggle={toggleSelection} />
      )}

      {(totalCount > pageSize || pageIndex > 0) && (
        <div className="list-foot">
          <span />
          <Pager
            pageIndex={pageIndex}
            pageSize={pageSize}
            shownCount={photos.length}
            totalCount={Math.max(totalCount, offset + photos.length)}
            isLoading={library.loading && !library.data}
            onPageIndex={(next) => setPageIndex(next)}
          />
        </div>
      )}

      {info.data && !searchText && listKind === 'library' && (
        <SettingsSection card title={t('photos.libraryTitle')} description={t('photos.libraryHint')}>
          <SettingsRow
            title={t('photos.libraryContents')}
            subtitle={libraryContents(info.data.photoCount + info.data.videoCount, info.data.videoCount, plural, t)}
          />
          <SettingsRow
            title={t('photos.spaceUsed')}
            subtitle={
              info.data.maximumAccountBytes > 0
                ? t('photos.spaceUsedOf', {
                    used: formatByteSize(info.data.usedBytes),
                    limit: formatByteSize(info.data.maximumAccountBytes),
                  })
                : `${formatByteSize(info.data.usedBytes)} · ${t('photos.noLimit')}`
            }
          />
          <SettingsRow title={t('photos.largestFile')} subtitle={formatByteSize(info.data.maximumFileBytes)} />
        </SettingsSection>
      )}

      {isDragging && (
        <div className="photo-drop" aria-hidden="true">
          <UploadIcon size={28} />
          <p>{t('photos.dropToUpload')}</p>
        </div>
      )}

      {photoId && viewerPhoto && (
        <PhotoViewer
          photo={viewerPhoto}
          previousPhoto={viewerPrevious}
          nextPhoto={viewerNext}
          isTrashed={Boolean(viewerPhoto.trashedAt) || isTrash}
          isPaused={confirmation !== null}
          onNavigate={showPhoto}
          onClose={closeViewer}
          onToggleFavorite={(photo) => void setFavorites([photo], !photo.isFavorite)}
          onTrash={(photo) => setConfirmation({ kind: 'trash', photos: [photo] })}
          onRestore={(photo) => void restore([photo])}
          onDeleteForever={(photo) => setConfirmation({ kind: 'delete', photos: [photo] })}
        />
      )}
      {photoId && !viewerPhoto && !library.loading && !lookup.loading && (
        <ConfirmDialog
          title={t('nav.photos')}
          body={t('photos.unavailable')}
          destructive={false}
          onClose={closeViewer}
        />
      )}

      {confirmation?.kind === 'trash' && (
        <ConfirmDialog
          title={plural(confirmation.photos.length, { one: 'photos.trashTitleOne', other: 'photos.trashTitleOther' })}
          body={
            trashDays > 0
              ? plural(trashDays, { one: 'photos.trashRetentionOne', other: 'photos.trashRetentionOther' })
              : undefined
          }
          confirmLabel={t('photos.trash')}
          destructive={false}
          busy={isBusy}
          onConfirm={() => void confirmTrash(confirmation.photos)}
          onClose={() => setConfirmation(null)}
        />
      )}
      {confirmation?.kind === 'delete' && (
        <ConfirmDialog
          title={plural(confirmation.photos.length, { one: 'photos.deleteTitleOne', other: 'photos.deleteTitleOther' })}
          body={t('photos.deleteBody')}
          confirmLabel={t('photos.deleteForever')}
          busy={isBusy}
          onConfirm={() => void confirmDelete(confirmation.photos)}
          onClose={() => setConfirmation(null)}
        />
      )}
      {confirmation?.kind === 'emptyTrash' && (
        <ConfirmDialog
          title={t('photos.emptyTrashTitle')}
          body={plural(trashedCount, { one: 'photos.emptyTrashBodyOne', other: 'photos.emptyTrashBodyOther' })}
          confirmLabel={t('photos.emptyTrashButton')}
          busy={isBusy}
          onConfirm={() => void confirmEmptyTrash()}
          onClose={() => setConfirmation(null)}
        />
      )}

      <UploadTray />
    </div>
  )
}

// "120 photos, including 4 videos". A photo here is any item; a video is
// named only as the part of them that is one.
function libraryContents(
  itemCount: number,
  videoCount: number,
  plural: Translation['plural'],
  t: Translation['t'],
): string {
  const items = plural(itemCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' })
  if (videoCount === 0) return items
  return t('photos.contentsWithVideos', {
    photos: items,
    videos: plural(videoCount, { one: 'photos.videoCountOne', other: 'photos.videoCountOther' }),
  })
}
