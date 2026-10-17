import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { askAgentAbout } from '../../api'
import { PageHeadingActions, useBreadcrumbDetail } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import {
  AlbumRemoveIcon,
  CloseIcon,
  DownloadIcon,
  HeartIcon,
  MoreIcon,
  RefreshIcon,
  RestoreIcon,
  TrashIcon,
  UploadIcon,
} from '../../components/icons'
import { MenuButton } from '../../components/menuButton'
import { Pager, useKeepPageInRange, usePageInAddress } from '../../components/pager'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation, type Translation } from '../../i18n/i18n'
import {
  PHOTOS_PER_PAGE,
  addPhotosToAlbum,
  countPhotos,
  createPhotoAlbum,
  deletePhotoAlbum,
  deletePhotosForever,
  describePhoto,
  emptyPhotoTrash,
  listPhotos,
  photoFilePath,
  readPhoto,
  readPhotoAlbum,
  readPhotoLibrary,
  removePhotosFromAlbum,
  renamePhotoAlbum,
  restorePhotos,
  retryPhotoProcessing,
  setPhotoAlbumCover,
  setPhotoFavorite,
  trashPhotos,
  type Photo,
  type PhotoAlbum,
  type PhotoFace,
  type PhotoKind,
  type PhotoListKind,
} from '../../photos/api'
import { usePhotosChanged } from '../../photos/changes'
import { FILTER_KINDS, parseDay, parsePhotoKind, startOfDay, startOfNextDay } from '../../photos/filters'
import { formatByteSize, indexRange, personTimeZone, photoReferenceTitle } from '../../photos/days'
import {
  NoAddressBookError,
  createContact,
  hidePhotoFaceGroup,
  listPhotoFaceGroups,
  mergePhotoFaceGroups,
  namePhotoFaceGroup,
  readPhotoFaceGroup,
  removePhotoFaceFromGroup,
  unnamePhotoFaceGroup,
} from '../../photos/people'
import { photoUploads } from '../../photos/uploadQueue'
import { hasPermission, useSession } from '../../session'
import { albumAddress } from './albums'
import { AddToAlbumMenu, AlbumTitleDialog, PhotoSections, type PhotoSection } from './albumControls'
import { PhotoGrid } from './grid'
import {
  FacePicture,
  MergePersonDialog,
  NamePersonDialog,
  PersonMenu,
  personAddress,
  useDisplayedPersonName,
  type PersonNaming,
} from './personControls'
import { PhotoViewer } from './viewer'

// The Photos page: the timeline of the person's library with its three
// lists, the viewer over it, selection, and uploading. The list, the search,
// the page and the photo being looked at are all in the address, so a
// reload, a link and the Back button each come back to where the person was.

// How often a list that holds photos still being made ready is read again.
const WAITING_POLL_MS = 3000
// How long typing in the search box pauses before the search is sent.
const SEARCH_PAUSE_MS = 300
// How many favorites are changed at once.
const FAVORITE_CONCURRENCY = 6
// The gap between downloads when several are saved together, which browsers
// otherwise take for a flood and block.
const DOWNLOAD_GAP_MS = 300

const KIND_LABELS: Record<PhotoKind, 'photos.kindPicture' | 'photos.kindLivePhoto' | 'photos.kindVideo'> = {
  picture: 'photos.kindPicture',
  livePhoto: 'photos.kindLivePhoto',
  video: 'photos.kindVideo',
}

type Confirmation =
  | { kind: 'trash'; photos: Photo[] }
  | { kind: 'delete'; photos: Photo[] }
  | { kind: 'emptyTrash' }
  | { kind: 'deleteAlbum' }
  | { kind: 'needsContacts' }

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
  // The album being looked into, when the address is an album's: the same
  // page then shows that album's photos, in the same grid and viewer.
  // The person being looked into, when the address is a person's: the photos
  // with a face in that group, in the same grid and viewer.
  const { photoId, photoAlbumId, photoFaceGroupId } = useParams()
  const [parameters, setParameters] = useSearchParams()
  const fileInput = useRef<HTMLInputElement>(null)
  const [isDragging, setIsDragging] = useState(false)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(new Set())
  // Entered with the Select button, so that a touch screen, which has no hover
  // to reveal the checkboxes, can choose photos; left with Cancel.
  const [isSelectionMode, setIsSelectionMode] = useState(false)
  const anchorIndex = useRef<number | null>(null)
  const [favoriteOverrides, setFavoriteOverrides] = useState<ReadonlyMap<string, boolean>>(new Map())
  // The photos a new album is being named for, from a selection or the viewer.
  const [newAlbumPhotos, setNewAlbumPhotos] = useState<Photo[] | null>(null)
  const [isRenamingAlbum, setIsRenamingAlbum] = useState(false)
  const [isNamingPerson, setIsNamingPerson] = useState(false)
  const [isMergingPerson, setIsMergingPerson] = useState(false)
  const [personError, setPersonError] = useState<string | null>(null)
  const basePath = photoAlbumId
    ? albumAddress(photoAlbumId)
    : photoFaceGroupId
      ? personAddress(photoFaceGroupId)
      : '/photos'
  const isInsideCollection = Boolean(photoAlbumId || photoFaceGroupId)
  const displayedName = useDisplayedPersonName()
  const canNamePeople =
    session.services?.isContactsEnabled === true && hasPermission(session.permissions, 'contacts:use')
  const isAssistantOn = session.services?.isAssistantEnabled === true

  const listKind: PhotoListKind = isInsideCollection
    ? 'library'
    : parameters.get('view') === 'favorites'
      ? 'favorites'
      : parameters.get('view') === 'trash'
        ? 'trash'
        : 'library'
  const searchText = parameters.get('search') ?? ''
  const photoKind = parsePhotoKind(parameters.get('kind'))
  const fromDay = parseDay(parameters.get('from'))
  const toDay = parseDay(parameters.get('to'))
  const hasFilters = photoKind !== '' || fromDay !== '' || toDay !== ''
  const { pageIndex, pageSize, offset, setPageIndex } = usePageInAddress({ defaultPageSize: PHOTOS_PER_PAGE })
  const takenAfter = fromDay ? startOfDay(fromDay) : undefined
  const takenBefore = toDay ? startOfNextDay(toDay) : undefined
  const filter = useMemo(
    () => ({
      listKind,
      searchText,
      photoAlbumId,
      photoFaceGroupId,
      photoKind: photoKind || undefined,
      takenAfter,
      takenBefore,
    }),
    [listKind, searchText, photoAlbumId, photoFaceGroupId, photoKind, takenAfter, takenBefore],
  )
  const userId = session.userId ?? ''
  // A search costs something every time it runs, so a list that is a search
  // is read again when the search changes or the person does something to it,
  // and not on a timer.
  const isSearching = searchText !== ''

  // ---- The search box and the address ----

  // The box holds what is typed; the address holds what is searched for. The
  // search is written to the address once typing pauses, and starts again at
  // the first page. When the address changes some other way (the sidebar, a
  // link in a toast, Back) the box shows what the address says, and what was
  // typed but not yet sent is dropped, so a pause cannot write it back.
  const [searchInput, setSearchInput] = useState(searchText)
  const searchTimer = useRef<number | null>(null)
  const latestSearchText = useRef(searchText)
  latestSearchText.current = searchText
  // The search this page wrote itself, so that its arrival in the address is
  // not mistaken for a change from outside.
  const writtenSearchText = useRef<string | null>(null)
  useEffect(() => {
    if (writtenSearchText.current === searchText) {
      writtenSearchText.current = null
      return
    }
    writtenSearchText.current = null
    if (searchTimer.current !== null) {
      window.clearTimeout(searchTimer.current)
      searchTimer.current = null
    }
    setSearchInput(searchText)
  }, [searchText])
  useEffect(
    () => () => {
      if (searchTimer.current !== null) window.clearTimeout(searchTimer.current)
    },
    [],
  )
  const typeSearch = (typed: string) => {
    setSearchInput(typed)
    if (searchTimer.current !== null) window.clearTimeout(searchTimer.current)
    searchTimer.current = window.setTimeout(() => {
      searchTimer.current = null
      const searched = typed.trim()
      // The address already says this: nothing to write.
      if (searched === latestSearchText.current) return
      writtenSearchText.current = searched
      setParameters(
        (previous) => {
          const written = new URLSearchParams(previous)
          if (searched) written.set('search', searched)
          else written.delete('search')
          written.delete('page')
          return written
        },
        { replace: true },
      )
    }, SEARCH_PAUSE_MS)
  }

  // A filter is one parameter of the address; changing one starts again at
  // the first page.
  const setFilterParameter = (name: 'kind' | 'from' | 'to', value: string) =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      if (value) written.set(name, value)
      else written.delete(name)
      written.delete('page')
      return written
    })
  const clearFilters = () =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      for (const name of ['kind', 'from', 'to']) written.delete(name)
      written.delete('page')
      return written
    })

  // ---- Reading ----

  // Which list a set of photos is of, so that rows that arrive for a list the
  // person has left are not drawn under the new list's buttons.
  const listKey = JSON.stringify([filter, offset, pageSize, userId])
  const library = useQuery(
    async () => ({ listKey, photos: await listPhotos(filter, offset, pageSize) }),
    [filter, offset, pageSize, userId],
    { refresh: !isSearching },
  )
  const count = useQuery(() => countPhotos(filter), [filter, userId], { refresh: !isSearching })
  const info = useQuery(() => readPhotoLibrary(), [userId])
  const album = useQuery(async () => (photoAlbumId ? await readPhotoAlbum(photoAlbumId) : null), [photoAlbumId, userId])
  const person = useQuery(
    async () => (photoFaceGroupId ? await readPhotoFaceGroup(photoFaceGroupId) : null),
    [photoFaceGroupId, userId],
  )
  const people = useQuery(async () => (isMergingPerson ? await listPhotoFaceGroups(true) : []), [isMergingPerson])
  // What was read for a different album or person is not this one's.
  const shownAlbum = album.data?.id === photoAlbumId ? album.data : null
  const shownPerson = person.data?.id === photoFaceGroupId ? person.data : null
  // The trail and the heading read the album's title, or the person's name,
  // not the id.
  useBreadcrumbDetail(shownAlbum?.albumTitle ?? (shownPerson ? displayedName(shownPerson) : null))
  useKeepPageInRange(pageIndex, pageSize, count.data ?? null, setPageIndex)

  const listedPhotos = library.data?.photos
  // Rows of another list stay on the screen, dimmed and out of reach, until
  // the rows of this one arrive.
  const isListStale = library.data !== null && library.data.listKey !== listKey
  const withFavoriteOverride = useCallback(
    <T extends Photo>(photo: T): T =>
      favoriteOverrides.has(photo.id) ? { ...photo, isFavorite: favoriteOverrides.get(photo.id)! } : photo,
    [favoriteOverrides],
  )
  const photos = useMemo(() => (listedPhotos ?? []).map(withFavoriteOverride), [listedPhotos, withFavoriteOverride])

  // A favorite changed here shows at once and is sent in the order it was
  // chosen, one request after another for the same photo; the heart and the
  // server cannot end up disagreeing because two clicks arrived out of order.
  // The server's answer replaces the shown one when nothing is on its way.
  const favoriteQueues = useRef(new Map<string, Promise<unknown>>())
  const pendingFavoriteCounts = useRef(new Map<string, number>())
  const settleFavoriteOverrides = useCallback((photoIds?: string[]) => {
    setFavoriteOverrides((previous) => {
      const next = new Map(previous)
      for (const photoId of photoIds ?? previous.keys()) {
        if (!pendingFavoriteCounts.current.get(photoId)) next.delete(photoId)
      }
      return next.size === previous.size ? previous : next
    })
  }, [])
  useEffect(() => settleFavoriteOverrides(), [listedPhotos, settleFavoriteOverrides])

  function clearSelection() {
    setSelectedIds(new Set())
    setIsSelectionMode(false)
  }
  // A different list, search or page is a different set of cells.
  useEffect(() => {
    setSelectedIds(new Set())
    setIsSelectionMode(false)
    anchorIndex.current = null
  }, [listKind, searchText, photoKind, fromDay, toDay, offset, photoAlbumId, photoFaceGroupId])

  // ---- The viewer's photo, when it is not in the list ----

  const viewerIndex = photoId ? photos.findIndex((photo) => photo.id === photoId) : -1
  const isOutsideList = Boolean(photoId) && viewerIndex < 0
  // A link to a photo of another page, or one found some other way, is read on
  // its own. What failed is remembered by the photo it failed for, so that the
  // answer to the photo before is never taken for this one's.
  const [failedLookupId, setFailedLookupId] = useState<string | null>(null)
  const lookup = useQuery(
    async () => {
      if (!photoId || !isOutsideList) return null
      try {
        const view = await readPhoto(photoId)
        setFailedLookupId(null)
        return view
      } catch (caught) {
        setFailedLookupId(photoId)
        throw caught
      }
    },
    [photoId, isOutsideList, userId],
    { refresh: false },
  )
  const lookedUpPhoto = photoId && lookup.data && lookup.data.photo.id === photoId ? lookup.data.photo : null

  // ---- The photos either side of the page ----

  // At the first or last photo of a page the neighbor is on the page before
  // or after. One photo is read from there, so that Previous and Next go on
  // across the pages.
  const totalCount = count.data ?? 0
  const needsPreviousEdge = viewerIndex === 0 && offset > 0
  const needsNextEdge = viewerIndex >= 0 && viewerIndex === photos.length - 1 && offset + photos.length < totalCount
  const previousEdge = useQuery(
    async () => ({
      position: offset - 1,
      photo: needsPreviousEdge ? ((await listPhotos(filter, offset - 1, 1))[0] ?? null) : null,
    }),
    [needsPreviousEdge, filter, offset, userId],
    { refresh: false },
  )
  const nextEdge = useQuery(
    async () => ({
      position: offset + photos.length,
      photo: needsNextEdge ? ((await listPhotos(filter, offset + photos.length, 1))[0] ?? null) : null,
    }),
    [needsNextEdge, filter, offset, photos.length, userId],
    { refresh: false },
  )
  const previousEdgePhoto =
    needsPreviousEdge && previousEdge.data?.position === offset - 1 ? previousEdge.data.photo : null
  const nextEdgePhoto = needsNextEdge && nextEdge.data?.position === offset + photos.length ? nextEdge.data.photo : null

  const refresh = useCallback(() => {
    return Promise.all([
      library.reload(true),
      count.reload(true),
      info.reload(true),
      album.reload(true),
      person.reload(true),
      isOutsideList ? lookup.reload(true) : null,
    ]).then(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [library.reload, count.reload, info.reload, album.reload, person.reload, lookup.reload, isOutsideList])
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh

  // Files that finished uploading, or photos restored from a toast on another
  // page, change the library. A search is not read again for that: it is the
  // person's to repeat.
  usePhotosChanged(() => {
    void info.reload(true)
    if (isSearching) return
    void library.reload(true)
    void count.reload(true)
  })

  // Photos that are still being made ready are looked at again soon, rather
  // than after the page's slow refresh. Not when the list is a search.
  const hasWaitingPhotos = photos.some((photo) => photo.photoProcessingState === 'waiting')
  useEffect(() => {
    if (!hasWaitingPhotos || isSearching) return
    const timer = window.setInterval(() => void library.reload(true), WAITING_POLL_MS)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasWaitingPhotos, isSearching, library.reload])

  // ---- Uploading ----

  // The queue, its tray, what it says in toasts and the warning before the tab
  // is left are the layout's (layout.tsx), since they outlive this page.
  useEffect(() => {
    if (info.data?.uploadPartBytes) photoUploads.setPartBytes(info.data.uploadPartBytes)
  }, [info.data?.uploadPartBytes])

  // Files go into the library; an album is filled from the photos in it.
  const canUpload = !isInsideCollection
  const addFiles = (files: File[]) => {
    if (!canUpload) {
      toast.failed(t('photos.uploadInLibrary'))
      return
    }
    const { addedCount, ignoredCount } = photoUploads.add(files)
    if (ignoredCount > 0) toast.failed(t('photos.uploadIgnored'))
    // New photos arrive in the library, not in the Favorites or the Trash.
    if (addedCount > 0 && listKind !== 'library') setListKind('library')
  }
  const hasFiles = (event: React.DragEvent) => Array.from(event.dataTransfer.types).includes('Files')

  // ---- The viewer and its address ----

  const viewerPhoto =
    viewerIndex >= 0 ? photos[viewerIndex] : lookedUpPhoto ? withFavoriteOverride(lookedUpPhoto) : null
  const viewerPrevious = viewerIndex > 0 ? photos[viewerIndex - 1] : previousEdgePhoto
  const viewerNext = viewerIndex >= 0 && viewerIndex < photos.length - 1 ? photos[viewerIndex + 1] : nextEdgePhoto
  const isViewerUnavailable = Boolean(photoId) && !viewerPhoto && !lookup.loading && failedLookupId === photoId

  const openPhoto = (photo: Photo) =>
    navigate(
      { pathname: `${basePath}/${encodeURIComponent(photo.id)}`, search: location.search },
      { state: { isFromGrid: true } },
    )
  // A photo from the page before or after is shown with that page loaded
  // behind it, so that Previous and Next go on from there.
  const showPhoto = (photo: Photo) => {
    let search = location.search
    if (!photos.some((listed) => listed.id === photo.id)) {
      const targetPageIndex =
        photo.id === nextEdgePhoto?.id ? pageIndex + 1 : photo.id === previousEdgePhoto?.id ? pageIndex - 1 : null
      if (targetPageIndex !== null) {
        const written = new URLSearchParams(location.search)
        if (targetPageIndex <= 0) written.delete('page')
        else written.set('page', String(targetPageIndex + 1))
        search = written.toString() ? `?${written.toString()}` : ''
      }
    }
    navigate(
      { pathname: `${basePath}/${encodeURIComponent(photo.id)}`, search },
      { replace: true, state: location.state },
    )
  }
  const closeViewer = useCallback(() => {
    // Opened from the grid, the viewer is one step on the way back; opened
    // from a link, there is no step to go back to.
    if ((location.state as { isFromGrid?: boolean } | null)?.isFromGrid) navigate(-1)
    else navigate({ pathname: basePath, search: location.search }, { replace: true })
  }, [location.state, location.search, navigate, basePath])
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
    const targetIds = targets.map((photo) => photo.id)
    setFavoriteOverrides((previous) => new Map([...previous, ...targetIds.map((id) => [id, isFavorite] as const)]))
    for (const id of targetIds) pendingFavoriteCounts.current.set(id, (pendingFavoriteCounts.current.get(id) ?? 0) + 1)
    const send = async (photo: Photo) => {
      const earlier = favoriteQueues.current.get(photo.id) ?? Promise.resolve()
      const mine = earlier.catch(() => undefined).then(() => setPhotoFavorite(photo.id, isFavorite))
      favoriteQueues.current.set(photo.id, mine)
      try {
        await mine
      } finally {
        pendingFavoriteCounts.current.set(photo.id, (pendingFavoriteCounts.current.get(photo.id) ?? 1) - 1)
      }
    }
    let failure: { reason: unknown } | null = null
    for (let start = 0; start < targets.length; start += FAVORITE_CONCURRENCY) {
      const results = await Promise.allSettled(targets.slice(start, start + FAVORITE_CONCURRENCY).map(send))
      for (const result of results) if (result.status === 'rejected' && !failure) failure = { reason: result.reason }
    }
    if (failure) failedWith(failure.reason)
    // What the server says wins again, once it has been asked.
    await refresh()
    settleFavoriteOverrides(targetIds)
  }

  async function restore(targets: Photo[]) {
    setIsBusy(true)
    try {
      const restoredCount = await restorePhotos(targets.map((photo) => photo.id))
      toast.done(plural(restoredCount, { one: 'photos.restoredOne', other: 'photos.restoredOther' }))
      if (targets.some((photo) => photo.id === photoId)) moveViewerOn()
      clearSelection()
      void refresh()
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
      clearSelection()
      void refresh()
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
      clearSelection()
      void refresh()
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
      clearSelection()
      if (photoId) closeViewer()
      void refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  // ---- Albums ----

  async function addToAlbum(targets: Photo[], target: PhotoAlbum) {
    try {
      const addedCount = await addPhotosToAlbum(
        target.id,
        targets.map((photo) => photo.id),
      )
      const viewAlbum = { label: t('photos.viewAlbum'), run: () => navigate(albumAddress(target.id)) }
      if (addedCount === 0) toast.done(t('photos.alreadyInAlbum', { title: target.albumTitle }), viewAlbum)
      else
        toast.done(
          plural(
            addedCount,
            { one: 'photos.addedToAlbumOne', other: 'photos.addedToAlbumOther' },
            { title: target.albumTitle },
          ),
          viewAlbum,
        )
      clearSelection()
      void refresh()
    } catch (caught) {
      failedWith(caught)
    }
  }

  async function createAlbumWith(targets: Photo[], albumTitle: string) {
    setIsBusy(true)
    try {
      const created = await createPhotoAlbum(
        albumTitle,
        targets.map((photo) => photo.id),
      )
      toast.done(t('photos.albumCreated', { title: created.albumTitle }), {
        label: t('photos.viewAlbum'),
        run: () => navigate(albumAddress(created.id)),
      })
      setNewAlbumPhotos(null)
      clearSelection()
      void refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  async function removeFromAlbum(targets: Photo[]) {
    if (!photoAlbumId) return
    const removedIds = targets.map((photo) => photo.id)
    try {
      const removedCount = await removePhotosFromAlbum(photoAlbumId, removedIds)
      toast.done(plural(removedCount, { one: 'photos.removedFromAlbumOne', other: 'photos.removedFromAlbumOther' }), {
        label: t('common.undo'),
        run: async () => {
          try {
            await addPhotosToAlbum(photoAlbumId, removedIds)
            refreshRef.current()
          } catch (caught) {
            failedWith(caught)
          }
        },
      })
      if (removedIds.includes(photoId ?? '')) moveViewerOn()
      clearSelection()
      void refresh()
    } catch (caught) {
      failedWith(caught)
    }
  }

  async function makeCover(photo: Photo) {
    if (!photoAlbumId) return
    try {
      await setPhotoAlbumCover(photoAlbumId, photo.id)
      toast.done(t('photos.coverSet'))
      void refresh()
    } catch (caught) {
      failedWith(caught)
    }
  }

  async function renameAlbum(albumTitle: string) {
    if (!photoAlbumId) return
    setIsBusy(true)
    try {
      await renamePhotoAlbum(photoAlbumId, albumTitle)
      setIsRenamingAlbum(false)
      await album.reload(true)
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  async function confirmDeleteAlbum() {
    if (!photoAlbumId) return
    setIsBusy(true)
    try {
      await deletePhotoAlbum(photoAlbumId)
      toast.done(t('photos.albumDeleted', { title: shownAlbum?.albumTitle ?? '' }))
      setConfirmation(null)
      navigate('/photos/albums', { replace: true })
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  // ---- Describing, asking, and people ----

  // Asks for a description and says so when that failed; the viewer reads
  // the photo again and shows it arriving.
  async function describe(photo: Photo, isRedescribed: boolean) {
    try {
      await describePhoto(photo.id, isRedescribed)
    } catch (caught) {
      failedWith(caught)
    }
  }

  // The assistant, pointed at this photo: the drawer opens with a chip for
  // it (the photo's small picture and where and when it was taken) and the
  // person writes their own question. The server words the title again from
  // the stored row. The viewer covers the drawer, so it closes and the drawer
  // is what is seen.
  function askAssistant(photo: Photo) {
    if (!askAgentAbout({ photoId: photo.id, photoTitle: photoReferenceTitle(photo, personTimeZone()) })) {
      navigate('/settings/agent')
      return
    }
    closeViewer()
  }

  // Puts photos whose previews failed, or that are ready and should have
  // their previews made again, back to waiting, and says how many it moved.
  // The grid is read again, and the viewer with it, so a photo shows as
  // waiting at once.
  async function retryProcessing(targets: Photo[]) {
    setIsBusy(true)
    try {
      const retriedCount = await retryPhotoProcessing(targets.map((photo) => photo.id))
      toast.done(
        retriedCount === 0
          ? t('photos.retriedNone')
          : plural(retriedCount, { one: 'photos.retriedOne', other: 'photos.retriedOther' }),
      )
      clearSelection()
      await refresh()
    } catch (caught) {
      failedWith(caught)
    } finally {
      setIsBusy(false)
    }
  }

  async function removeFaceFromGroup(face: PhotoFace) {
    try {
      await removePhotoFaceFromGroup(face.id)
      toast.done(t('photos.faceRemoved'))
      void refresh()
    } catch (caught) {
      failedWith(caught)
    }
  }

  async function nameGroup(naming: PersonNaming) {
    if (!photoFaceGroupId) return
    setIsBusy(true)
    setPersonError(null)
    try {
      const contactId = naming.kind === 'new' ? (await createContact(naming.contactName)).id : naming.contactId
      const named = await namePhotoFaceGroup(photoFaceGroupId, contactId)
      setIsNamingPerson(false)
      toast.done(t('photos.personNamed', { name: displayedName(named) }))
      await person.reload(true)
    } catch (caught) {
      setPersonError(
        caught instanceof NoAddressBookError
          ? t('photos.noAddressBook')
          : caught instanceof Error
            ? caught.message
            : String(caught),
      )
    } finally {
      setIsBusy(false)
    }
  }

  async function removeName() {
    if (!photoFaceGroupId || !shownPerson) return
    const previousContactId = shownPerson.contactId
    try {
      await unnamePhotoFaceGroup(photoFaceGroupId)
      toast.done(t('photos.nameRemoved'), {
        label: t('common.undo'),
        run: async () => {
          try {
            await namePhotoFaceGroup(photoFaceGroupId, previousContactId)
            refreshRef.current()
          } catch (caught) {
            failedWith(caught)
          }
        },
      })
      await person.reload(true)
    } catch (caught) {
      failedWith(caught)
    }
  }

  async function mergeGroup(other: { id: string }) {
    if (!photoFaceGroupId) return
    setIsBusy(true)
    setPersonError(null)
    try {
      await mergePhotoFaceGroups([photoFaceGroupId, other.id])
      setIsMergingPerson(false)
      toast.done(t('photos.peopleMerged'))
      void refresh()
    } catch (caught) {
      setPersonError(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsBusy(false)
    }
  }

  async function toggleHidden() {
    if (!photoFaceGroupId || !shownPerson) return
    const isNowHidden = !shownPerson.isHidden
    try {
      await hidePhotoFaceGroup(photoFaceGroupId, isNowHidden)
      toast.done(isNowHidden ? t('photos.personHidden') : t('photos.personShown'), {
        label: t('common.undo'),
        run: async () => {
          try {
            await hidePhotoFaceGroup(photoFaceGroupId, !isNowHidden)
            refreshRef.current()
          } catch (caught) {
            failedWith(caught)
          }
        },
      })
      await person.reload(true)
    } catch (caught) {
      failedWith(caught)
    }
  }

  function startNamingPerson() {
    setPersonError(null)
    if (canNamePeople) setIsNamingPerson(true)
    else setConfirmation({ kind: 'needsContacts' })
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
  const haveAllSelectedFailed =
    selectedPhotos.length > 0 && selectedPhotos.every((photo) => photo.photoProcessingState === 'failed')

  const selectSection = (section: PhotoSection) => {
    if (section === 'albums') navigate('/photos/albums')
    else if (section === 'people') navigate('/photos/people')
    else setListKind(section)
  }
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
  const emptyText =
    searchText !== '' && !hasFilters
      ? t('photos.emptySearch')
      : searchText !== '' || hasFilters
        ? t('photos.emptyFiltered')
        : photoAlbumId
          ? t('photos.emptyAlbum')
          : photoFaceGroupId
            ? t('photos.emptyPerson')
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
        if (canUpload) setIsDragging(true)
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
        {canUpload && (
          <button type="button" className="primary" onClick={() => fileInput.current?.click()}>
            <UploadIcon size={16} /> {t('photos.upload')}
          </button>
        )}
        {photos.length > 0 && (
          <button
            type="button"
            aria-pressed={isSelectionMode || selectedIds.size > 0}
            onClick={() => (isSelectionMode || selectedIds.size > 0 ? clearSelection() : setIsSelectionMode(true))}
          >
            {isSelectionMode || selectedIds.size > 0 ? t('common.cancel') : t('photos.select')}
          </button>
        )}
        {photoAlbumId && (
          <MenuButton
            label={t('photos.albumMenu')}
            className="icon-button"
            icon={<MoreIcon size={18} />}
            render={(close) => (
              <>
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    close()
                    setIsRenamingAlbum(true)
                  }}
                >
                  {t('photos.renameAlbum')}
                </button>
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    close()
                    setConfirmation({ kind: 'deleteAlbum' })
                  }}
                >
                  {t('photos.deleteAlbum')}
                </button>
              </>
            )}
          />
        )}
        {shownPerson && (
          <PersonMenu
            person={shownPerson}
            onName={startNamingPerson}
            onRemoveName={() => void removeName()}
            onMerge={() => {
              setPersonError(null)
              setIsMergingPerson(true)
            }}
            onToggleHidden={() => void toggleHidden()}
          />
        )}
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
              onClick={() => clearSelection()}
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
              <AddToAlbumMenu
                hiddenAlbumId={photoAlbumId}
                onChooseAlbum={(target) => void addToAlbum(selectedPhotos, target)}
                onNewAlbum={() => setNewAlbumPhotos(selectedPhotos)}
              />
              {photoAlbumId && (
                <Tooltip label={t('photos.removeFromAlbum')}>
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={t('photos.removeFromAlbum')}
                    onClick={() => void removeFromAlbum(selectedPhotos)}
                  >
                    <AlbumRemoveIcon size={18} />
                  </button>
                </Tooltip>
              )}
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
              {haveAllSelectedFailed && (
                <Tooltip label={t('photos.tryAgain')}>
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={t('photos.tryAgain')}
                    disabled={isBusy}
                    onClick={() => void retryProcessing(selectedPhotos)}
                  >
                    <RefreshIcon size={18} />
                  </button>
                </Tooltip>
              )}
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
            onChange={(event) => typeSearch(event.target.value)}
          />
          {!isInsideCollection && <PhotoSections current={listKind} onSelect={selectSection} />}
          {isTrash && trashedCount > 0 && (
            <button type="button" className="danger" onClick={() => setConfirmation({ kind: 'emptyTrash' })}>
              {t('photos.emptyTrashButton')}
            </button>
          )}
        </div>
      )}
      {selectedPhotos.length === 0 && (
        <div className="row photo-filters">
          <label>
            <span>{t('photos.filterKind')}</span>
            <Select
              block
              value={photoKind}
              label={t('photos.filterKind')}
              options={[
                { value: '', label: t('photos.kindAll') },
                ...FILTER_KINDS.map((kind) => ({ value: kind, label: t(KIND_LABELS[kind]) })),
              ]}
              onChange={(value) => setFilterParameter('kind', value)}
            />
          </label>
          <label>
            <span>{t('photos.filterFrom')}</span>
            <input
              type="date"
              value={fromDay}
              max={toDay || undefined}
              onChange={(event) => setFilterParameter('from', parseDay(event.target.value))}
            />
          </label>
          <label>
            <span>{t('photos.filterTo')}</span>
            <input
              type="date"
              value={toDay}
              min={fromDay || undefined}
              onChange={(event) => setFilterParameter('to', parseDay(event.target.value))}
            />
          </label>
          {hasFilters && (
            <button type="button" className="shrink" onClick={clearFilters}>
              {t('photos.clearFilters')}
            </button>
          )}
        </div>
      )}

      {shownPerson && (
        <div className="photo-person-head">
          <FacePicture facePictureUrl={shownPerson.facePictureUrl} name={displayedName(shownPerson)} size="large" />
          <div className="photo-person-head-body">
            <span className="muted">
              {plural(shownPerson.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' })}
              {shownPerson.isHidden ? ` · ${t('photos.hiddenFromPeople')}` : ''}
            </span>
            {shownPerson.contactName.trim() === '' && (
              <button type="button" onClick={startNamingPerson}>
                {t('photos.nameThisPerson')}
              </button>
            )}
          </div>
        </div>
      )}

      {isTrash && trashDays > 0 && (
        <p className="muted photo-trash-note">
          {plural(trashDays, { one: 'photos.trashRetentionOne', other: 'photos.trashRetentionOther' })}
        </p>
      )}

      {library.loading && !library.data ? (
        <Loading />
      ) : photos.length === 0 && !isListStale ? (
        <SettingsEmpty>{emptyText}</SettingsEmpty>
      ) : (
        <div className={isListStale ? 'photo-list is-loading' : 'photo-list'} aria-busy={isListStale}>
          <PhotoGrid
            photos={photos}
            selectedIds={selectedIds}
            isSelectionMode={isSelectionMode}
            onOpen={openPhoto}
            onToggle={toggleSelection}
          />
        </div>
      )}

      {(totalCount > pageSize || pageIndex > 0) && (
        <div className="list-foot">
          <span />
          <Pager
            pageIndex={pageIndex}
            pageSize={pageSize}
            shownCount={photos.length}
            totalCount={Math.max(totalCount, offset + photos.length)}
            isLoading={library.loading && (!library.data || isListStale)}
            onPageIndex={(next) => setPageIndex(next)}
          />
        </div>
      )}

      {info.data && !searchText && !isInsideCollection && listKind === 'library' && (
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

      {isDragging && canUpload && (
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
          isBusy={isBusy}
          isPaused={
            confirmation !== null || newAlbumPhotos !== null || isRenamingAlbum || isNamingPerson || isMergingPerson
          }
          photoAlbumId={photoAlbumId}
          photoLibrary={info.data}
          onDescribe={describe}
          onAskAssistant={isAssistantOn ? askAssistant : undefined}
          onRetryProcessing={(photo) => retryProcessing([photo])}
          onRemoveFaceFromGroup={removeFaceFromGroup}
          onNavigate={showPhoto}
          onClose={closeViewer}
          onToggleFavorite={(photo) => void setFavorites([photo], !photo.isFavorite)}
          onTrash={(photo) => setConfirmation({ kind: 'trash', photos: [photo] })}
          onRestore={(photo) => void restore([photo])}
          onDeleteForever={(photo) => setConfirmation({ kind: 'delete', photos: [photo] })}
          onAddToAlbum={(photo, target) => addToAlbum([photo], target)}
          onNewAlbum={(photo) => setNewAlbumPhotos([photo])}
          onRemoveFromAlbum={photoAlbumId ? (photo) => removeFromAlbum([photo]) : undefined}
          onSetCover={photoAlbumId ? (photo) => makeCover(photo) : undefined}
        />
      )}
      {isViewerUnavailable && (
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

      {confirmation?.kind === 'deleteAlbum' && (
        <ConfirmDialog
          title={t('photos.deleteAlbumTitle', { title: shownAlbum?.albumTitle ?? '' })}
          body={t('photos.deleteAlbumBody')}
          confirmLabel={t('photos.deleteAlbum')}
          busy={isBusy}
          onConfirm={() => void confirmDeleteAlbum()}
          onClose={() => setConfirmation(null)}
        />
      )}
      {confirmation?.kind === 'needsContacts' && (
        <ConfirmDialog
          title={t('photos.nameThisPerson')}
          body={t('photos.nameNeedsContacts')}
          destructive={false}
          onClose={() => setConfirmation(null)}
        />
      )}
      {isNamingPerson && shownPerson && (
        <NamePersonDialog
          currentName={shownPerson.contactName}
          isBusy={isBusy}
          error={personError}
          onSubmit={(naming) => void nameGroup(naming)}
          onClose={() => setIsNamingPerson(false)}
        />
      )}
      {isMergingPerson && shownPerson && (
        <MergePersonDialog
          person={shownPerson}
          others={(people.data ?? []).filter((other) => other.id !== shownPerson!.id)}
          isLoading={people.loading && !people.data}
          isBusy={isBusy}
          error={personError}
          onSubmit={(other) => void mergeGroup(other)}
          onClose={() => setIsMergingPerson(false)}
        />
      )}
      {newAlbumPhotos && (
        <AlbumTitleDialog
          title={t('photos.newAlbum')}
          submitLabel={t('common.create')}
          isBusy={isBusy}
          onSubmit={(albumTitle) => void createAlbumWith(newAlbumPhotos, albumTitle)}
          onClose={() => setNewAlbumPhotos(null)}
        />
      )}
      {isRenamingAlbum && (
        <AlbumTitleDialog
          title={t('photos.renameAlbum')}
          submitLabel={t('common.save')}
          initialTitle={shownAlbum?.albumTitle ?? ''}
          isBusy={isBusy}
          onSubmit={(albumTitle) => void renameAlbum(albumTitle)}
          onClose={() => setIsRenamingAlbum(false)}
        />
      )}
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
