import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { askAgentAbout, graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { photoUploads } from '../../photos/uploadQueue'
import { PhotoDateRangeLine } from './dateRange'
import { PhotosPage } from './library'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  askAgentAbout: vi.fn(),
}))
// Whether the session has an assistant to ask, and may use one.
let isAssistantEnabled = false
let sessionPermissions = ['photos:use', 'agent:use']
vi.mock('../../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../session')>()),
  useSession: () => ({
    userId: 'person-1',
    services: { isPhotosEnabled: true, isAssistantEnabled },
    permissions: { everywhere: sessionPermissions, byDomain: [] },
  }),
}))
// Files whose names start with "hang" are never read to the end.
// The details' map needs WebGL, which the test browser does not have.
vi.mock('../../components/locationMap', () => ({ LocationMap: () => null }))
vi.mock('../../photos/uploader', () => ({
  isPhotoFile: () => true,
  sha256Of: vi.fn(
    (file: File, _onProgress: unknown, signal: AbortSignal) =>
      new Promise<string>((resolve, reject) => {
        if (!file.name.startsWith('hang')) return resolve(`digest-${file.name}`)
        signal.addEventListener('abort', () => reject(new DOMException('The upload was canceled.', 'AbortError')))
      }),
  ),
  uploadPhotoFile: vi.fn(async (file: File) => ({ photoId: `photo-${file.name}` })),
}))

// Whose the album is and what the person may do with it: their own, by default.
const ownAlbum = {
  ownerUserId: 'person-1',
  circleId: null,
  ownerUsername: 'ana',
  ownerName: 'Ana',
  ownerAvatarUrl: null,
  ownerAvatarColorIndex: 0,
  circleName: '',
  isOwnedByCaller: true,
  shareLevel: '',
  canCallerContribute: true,
  canCallerManage: true,
  isLocationIncluded: true,
}
let albumOwnership: Record<string, unknown> = ownAlbum
// What a test answers itself, ahead of the defaults: undefined to leave it be.
let extraAnswer: (document: string, variables: Record<string, unknown>) => unknown = () => undefined

const photoFixture = (id: string, overrides: Record<string, unknown> = {}) => ({
  id,
  clientPhotoId: id,
  createdAt: '2030-07-12T09:00:00Z',
  takenAt: '2030-07-12T09:00:00Z',
  takenAtOffsetMinutes: 0,
  photoKind: 'picture',
  photoProcessingState: 'ready',
  originalFilename: `${id}.jpg`,
  smallPreviewUrl: `/small-${id}`,
  largePreviewUrl: `/large-${id}`,
  videoPreviewUrl: '',
  durationMS: 0,
  isFavorite: false,
  processingErrorMessage: '',
  placeName: '',
  latitude: null as number | null,
  longitude: null as number | null,
  cameraMake: '',
  cameraModel: '',
  widthPixels: 0,
  heightPixels: 0,
  trashedAt: null as string | null,
  isOwnedByCaller: true,
  ownerName: '',
  userId: 'person-1',
  ...overrides,
})

// What the server holds, and what each read of it is told to wait for.
let library: ReturnType<typeof photoFixture>[]
let readPhotoAnswer: (photoId: string) => Promise<unknown>
let listAnswer: (variables: Record<string, unknown>) => Promise<unknown>
let updatePhotoAnswer: (variables: Record<string, unknown>) => Promise<unknown>
let retryAnswer: (variables: Record<string, unknown>) => Promise<unknown>

const callsTo = (name: string) =>
  vi
    .mocked(graphql)
    .mock.calls.filter(([document]) => document.includes(` ${name}(`) || document.includes(`{ ${name}(`))
    .map(([, variables]) => (variables ?? {}) as Record<string, unknown>)

beforeEach(() => {
  // Every query matches except the phone's, which would put the page's actions
  // behind the More menu.
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: !query.includes('max-width'),
    addEventListener() {},
    removeEventListener() {},
  }))
  library = ['photo-1', 'photo-2', 'photo-3'].map((id) => photoFixture(id))
  readPhotoAnswer = async (photoId) => ({
    ReadPhoto: {
      photo: {
        ...photoFixture(photoId),
        photoDescription: '',
        descriptionVisibleText: '',
        describedAt: null,
        isDescriptionRequested: false,
      },
      photoFiles: [],
      albums: [],
      faces: [],
    },
  })
  listAnswer = async (variables) => {
    const offset = Number(variables.offset ?? 0)
    return { ListPhotos: library.slice(offset, offset + Number(variables.pageSize)) }
  }
  updatePhotoAnswer = async (variables) => ({
    UpdatePhoto: { ...photoFixture(String(variables.photoId)), isFavorite: variables.isFavorite },
  })
  isAssistantEnabled = false
  albumOwnership = ownAlbum
  extraAnswer = () => undefined
  vi.mocked(askAgentAbout).mockReset()
  retryAnswer = async (variables) => ({ RetryPhotoProcessing: (variables.photoIds as string[]).length })
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    const extra = extraAnswer(document, variables ?? {})
    if (extra !== undefined) return extra as never
    if (document.includes('RetryPhotoProcessing(')) return (await retryAnswer(variables ?? {})) as never
    if (document.includes('ReadPhotoLibrary'))
      return {
        ReadPhotoLibrary: {
          photoCount: library.length,
          videoCount: 0,
          favoriteCount: 0,
          trashedCount: 0,
          usedBytes: 0,
          maximumFileBytes: 1000,
          maximumAccountBytes: 0,
          trashRetentionDays: 30,
          uploadPath: '/api/v1/photos/uploads',
          uploadPartBytes: 1000,
          isDescriptionAvailable: false,
          descriptionUnavailableMessage: '',
          isFaceRecognitionAvailable: false,
          faceRecognitionUnavailableMessage: '',
          isPhotoFaceRecognitionEnabled: false,
        },
      } as never
    if (document.includes('ListPhotos(')) return (await listAnswer(variables ?? {})) as never
    if (document.includes('CountPhotos(')) return { CountPhotos: library.length } as never
    if (document.includes('ReadPhoto(')) return (await readPhotoAnswer(String(variables?.photoId))) as never
    if (document.includes('UpdatePhoto(')) return (await updatePhotoAnswer(variables ?? {})) as never
    if (document.includes('ListPhotoFaceGroups')) return { ListPhotoFaceGroups: [] } as never
    if (document.includes('ListPhotoAlbums')) return { ListPhotoAlbums: [] } as never
    if (document.includes('ReadPhotoAlbum(')) {
      return {
        ReadPhotoAlbum: {
          id: 'album-1',
          albumTitle: 'Picnic',
          createdAt: '',
          updatedAt: '',
          coverPhotoId: null,
          photoCount: 3,
          firstTakenAt: null,
          lastTakenAt: null,
          coverPreviewUrl: '',
          ...albumOwnership,
        },
      } as never
    }
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
    throw new Error(`unexpected document ${document}`)
  })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  photoUploads.setOwner(`other-${Math.random()}`)
})

function Probe() {
  const location = useLocation()
  return <output data-testid="address">{location.pathname + location.search}</output>
}
function Jump({ to, label }: { to: string; label: string }) {
  const navigate = useNavigate()
  return (
    <button type="button" onClick={() => navigate(to)}>
      {label}
    </button>
  )
}
const address = () => screen.getByTestId('address').textContent

const show = (entry: string, links: { to: string; label: string }[] = []) =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[entry]}>
          <Probe />
          {links.map((link) => (
            <Jump key={link.label} {...link} />
          ))}
          {/* The line under the heading, which the layout of the pages draws. */}
          <PhotoDateRangeLine>
            <Routes>
              <Route path="/photos" element={<PhotosPage />} />
              <Route path="/photos/:photoId" element={<PhotosPage />} />
              <Route path="/photos/albums/:photoAlbumId" element={<PhotosPage />} />
              <Route path="/photos/people/:photoFaceGroupId" element={<PhotosPage />} />
              <Route path="/photos/albums/:photoAlbumId/:photoId" element={<PhotosPage />} />
            </Routes>
          </PhotoDateRangeLine>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )

const searchBox = async () => (await screen.findByRole('searchbox')) as HTMLInputElement
const sleep = (milliseconds: number) => new Promise((resolve) => setTimeout(resolve, milliseconds))

// ---- the search box and the address ----

it('shows the search the address says when it changes from outside, and writes nothing back', async () => {
  show('/photos?search=kite', [
    { to: '/photos?search=lake', label: 'to lake' },
    { to: '/photos', label: 'to nothing' },
  ])
  const box = await searchBox()
  expect(box.value).toBe('kite')
  fireEvent.click(screen.getByRole('button', { name: 'to lake' }))
  await waitFor(() => expect(box.value).toBe('lake'))
  fireEvent.click(screen.getByRole('button', { name: 'to nothing' }))
  await waitFor(() => expect(box.value).toBe(''))
  await sleep(450)
  expect(address()).toBe('/photos')
})

it('drops what was typed and not yet sent when the address changes, instead of writing it back', async () => {
  show('/photos?search=kite', [{ to: '/photos?search=oar', label: 'to oar' }])
  const box = await searchBox()
  fireEvent.change(box, { target: { value: 'boat' } })
  fireEvent.click(screen.getByRole('button', { name: 'to oar' }))
  await waitFor(() => expect(box.value).toBe('oar'))
  await sleep(450)
  expect(address()).toBe('/photos?search=oar')
  expect(box.value).toBe('oar')
})

it('sends the search to the address once typing pauses, and keeps typing that goes on', async () => {
  show('/photos')
  const box = await searchBox()
  fireEvent.change(box, { target: { value: 'sa' } })
  await sleep(100)
  fireEvent.change(box, { target: { value: 'sail ' } })
  expect(address()).toBe('/photos')
  await waitFor(() => expect(address()).toBe('/photos?search=sail'))
  // The trailing space the person typed is theirs to keep typing after.
  expect(box.value).toBe('sail ')
})

// ---- polling ----

it('does not run a search again on the timer, and still refreshes a list that is not one', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  show('/photos?search=kite')
  await screen.findByRole('button', { name: 'Open photo-1.jpg' })
  const searches = () => callsTo('ListPhotos').length + callsTo('CountPhotos').length
  const before = searches()
  expect(before).toBeGreaterThan(0)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(35_000)
  })
  expect(searches()).toBe(before)
  cleanup()
  vi.mocked(graphql).mockClear()
  show('/photos')
  await screen.findAllByRole('button', { name: /^Open / })
  const first = searches()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(11_000)
  })
  expect(searches()).toBeGreaterThan(first)
})

it('looks again soon at a ready photo whose small preview is not made yet, until it is', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  library = [photoFixture('photo-1', { photoProcessingState: 'ready', smallPreviewUrl: '' })]
  show('/photos')
  await screen.findByRole('img', { name: 'Getting this photo ready' })
  const listReads = () => callsTo('ListPhotos').length
  const before = listReads()
  library = [photoFixture('photo-1')]
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3500)
  })
  expect(listReads()).toBeGreaterThan(before)
  await waitFor(() => expect(screen.queryByRole('img', { name: 'Getting this photo ready' })).toBeNull())
})

// ---- the viewer for a photo outside the list ----

it('shows a photo that is not in the list once it is read, and never says it is unavailable while it loads', async () => {
  let finish: (answer: unknown) => void = () => {}
  readPhotoAnswer = () => new Promise((resolve) => (finish = resolve))
  show('/photos/photo-9')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(screen.queryByText(/is not available/)).toBeNull()
  expect(screen.queryByRole('dialog', { name: 'Photo viewer' })).toBeNull()
  await act(async () => {
    finish({
      ReadPhoto: {
        photo: {
          ...photoFixture('photo-9', { originalFilename: 'nine.jpg' }),
          photoDescription: '',
          describedAt: null,
          descriptionVisibleText: '',
          isDescriptionRequested: false,
        },
        photoFiles: [],
        albums: [],
        faces: [],
      },
    })
  })
  expect(await screen.findByRole('dialog', { name: 'Photo viewer' })).toBeTruthy()
  expect(await screen.findByText('nine.jpg')).toBeTruthy()
})

it('shows the photo asked for, not the one looked up before it', async () => {
  let finishEight: (answer: unknown) => void = () => {}
  readPhotoAnswer = (photoId) =>
    photoId === 'photo-8'
      ? new Promise((resolve) => (finishEight = resolve))
      : Promise.resolve({
          ReadPhoto: {
            photo: {
              ...photoFixture(photoId, { originalFilename: 'nine.jpg' }),
              photoDescription: '',
              describedAt: null,
              descriptionVisibleText: '',
              isDescriptionRequested: false,
            },
            photoFiles: [],
            albums: [],
            faces: [],
          },
        })
  show('/photos/photo-9', [{ to: '/photos/photo-8', label: 'to eight' }])
  expect(await screen.findByText('nine.jpg')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'to eight' }))
  await waitFor(() => expect(screen.queryByText('nine.jpg')).toBeNull())
  expect(screen.queryByRole('dialog', { name: 'Photo viewer' })).toBeNull()
  expect(screen.queryByText(/is not available/)).toBeNull()
  await act(async () => {
    finishEight({
      ReadPhoto: {
        photo: {
          ...photoFixture('photo-8', { originalFilename: 'eight.jpg' }),
          photoDescription: '',
          describedAt: null,
          descriptionVisibleText: '',
          isDescriptionRequested: false,
        },
        photoFiles: [],
        albums: [],
        faces: [],
      },
    })
  })
  expect(await screen.findByText('eight.jpg')).toBeTruthy()
})

it('says that a photo is not available when it cannot be read', async () => {
  readPhotoAnswer = async () => {
    throw new Error('not found')
  }
  show('/photos/photo-9')
  expect(await screen.findByText('This photo is not available. It may have been deleted.')).toBeTruthy()
})

// ---- the pages ----

const manyPhotos = (count: number) => Array.from({ length: count }, (_, index) => photoFixture(`photo-${index + 1}`))

// These tests draw a page of 200 photos and read the page beside it, which
// takes well over a second when the whole suite shares the machine. Every
// wait is for the thing itself to be there, so the time is only how long to
// give up after, and the test as a whole is allowed to wait that long for
// each of them.
const SLOW_WAIT = { timeout: 15_000 }
const SLOW_TEST_MS = 60_000

it(
  'goes on across the pages: Previous from the first photo of a page opens the last of the page before',
  async () => {
    library = manyPhotos(203)
    show('/photos/photo-201?page=2')
    const previous = await screen.findByRole('button', { name: 'Previous photo' }, SLOW_WAIT)
    expect(screen.queryByRole('button', { name: 'Next photo' })).not.toBeNull()
    fireEvent.click(previous)
    await waitFor(() => expect(address()).toBe('/photos/photo-200'), SLOW_WAIT)
    // Page one is read behind it, so that Previous goes on from there.
    await waitFor(() => expect(callsTo('ListPhotos').some((variables) => variables.offset === 0)).toBe(true), SLOW_WAIT)
    expect(await screen.findByRole('button', { name: 'Previous photo' }, SLOW_WAIT)).toBeTruthy()
  },
  SLOW_TEST_MS,
)

it(
  'goes on across the pages: Next from the last photo of a page opens the first of the page after',
  async () => {
    library = manyPhotos(203)
    show('/photos/photo-200')
    const next = await screen.findByRole('button', { name: 'Next photo' }, SLOW_WAIT)
    fireEvent.click(next)
    await waitFor(() => expect(address()).toBe('/photos/photo-201?page=2'), SLOW_WAIT)
    await waitFor(
      () => expect(callsTo('ListPhotos').some((variables) => variables.offset === 200)).toBe(true),
      SLOW_WAIT,
    )
    // The page after is drawn, so the viewer knows what follows the photo it
    // shows, instead of the Next button of the moment before the page came.
    await screen.findByRole('button', { name: 'Open photo-203.jpg' }, SLOW_WAIT)
    // The last photo has nothing after it.
    fireEvent.click(await screen.findByRole('button', { name: 'Next photo' }, SLOW_WAIT))
    await waitFor(() => expect(address()).toBe('/photos/photo-202?page=2'), SLOW_WAIT)
    fireEvent.click(await screen.findByRole('button', { name: 'Next photo' }, SLOW_WAIT))
    await waitFor(() => expect(address()).toBe('/photos/photo-203?page=2'), SLOW_WAIT)
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Next photo' })).toBeNull(), SLOW_WAIT)
  },
  SLOW_TEST_MS,
)

// ---- filters ----

it('asks for the kind and the days in the address, and writes them back when they change', async () => {
  show('/photos?kind=video&from=2030-07-12&to=2030-07-14')
  await screen.findAllByRole('button', { name: /^Open / })
  const variables = callsTo('ListPhotos')[0]
  expect(variables.photoKind).toBe('video')
  const after = new Date(String(variables.takenAfter))
  const before = new Date(String(variables.takenBefore))
  expect([after.getMonth(), after.getDate(), after.getHours()]).toEqual([6, 12, 0])
  expect([before.getMonth(), before.getDate(), before.getHours()]).toEqual([6, 15, 0])
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  fireEvent.change(screen.getByLabelText('From'), { target: { value: '2030-07-13' } })
  await waitFor(() => expect(address()).toBe('/photos?kind=video&from=2030-07-13&to=2030-07-14'))
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  await waitFor(() => expect(address()).toBe('/photos'))
  expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull()
})

it('asks for an exact window in the address in place of the days, shows it as a chip and takes it off', async () => {
  show('/photos?from=2030-07-12&to=2030-07-14&after=2030-03-05T09:50:00.000Z&before=2030-03-05T10:59:00.000Z')
  await screen.findAllByRole('button', { name: /^Open / })
  const variables = callsTo('ListPhotos')[0]
  expect(variables.takenAfter).toBe('2030-03-05T09:50:00.000Z')
  expect(variables.takenBefore).toBe('2030-03-05T10:59:00.000Z')
  fireEvent.click(await screen.findByRole('button', { name: /^Remove the filter “.* to .*”$/ }))
  await waitFor(() => expect(address()).toBe('/photos?from=2030-07-12&to=2030-07-14'))
  await waitFor(() =>
    expect(callsTo('ListPhotos').some((each) => new Date(String(each.takenAfter)).getFullYear() === 2030)).toBe(true),
  )
  expect(String(callsTo('ListPhotos').slice(-1)[0]?.takenAfter)).not.toBe('2030-03-05T09:50:00.000Z')
})

it('lets each end of the window apply on its own, and shows the day fields empty while it is there', async () => {
  show('/photos?from=2030-07-12&to=2030-07-14&after=2030-03-05T09:50:00.000Z')
  await screen.findAllByRole('button', { name: /^Open / })
  const variables = callsTo('ListPhotos')[0]
  expect(variables.takenAfter).toBe('2030-03-05T09:50:00.000Z')
  expect(variables.takenBefore).toBeFalsy()
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  expect((screen.getByLabelText('From') as HTMLInputElement).value).toBe('')
  expect((screen.getByLabelText('To') as HTMLInputElement).value).toBe('')
  // Setting a day ends the window.
  fireEvent.change(screen.getByLabelText('From'), { target: { value: '2030-07-13' } })
  await waitFor(() => expect(address()).toBe('/photos?from=2030-07-13&to=2030-07-14'))
})

it('ignores a window that is not a moment, and keeps the window when a photo is opened', async () => {
  show('/photos?after=yesterday&before=2030-02-31T10:00:00Z')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(callsTo('ListPhotos')[0].takenAfter).toBeFalsy()
  expect(callsTo('ListPhotos')[0].takenBefore).toBeFalsy()
  cleanup()
  show('/photos?after=2030-03-05T09:50:00.000Z&before=2030-03-05T10:59:00.000Z')
  fireEvent.click((await screen.findAllByRole('button', { name: /^Open / }))[0])
  await waitFor(() =>
    expect(address()).toMatch(/^\/photos\/photo-1\?after=2030-03-05T09(%3A|:)50.*&before=2030-03-05T10(%3A|:)59/),
  )
})

it('offers the kind of photo to keep, and writes the choice to the address', async () => {
  show('/photos')
  await screen.findAllByRole('button', { name: /^Open / })
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  fireEvent.click(await screen.findByRole('radio', { name: 'Videos' }))
  await waitFor(() => expect(address()).toBe('/photos?kind=video'))
})

// ---- the list a person has left ----

it('dims the rows of the list that was left, until the rows of the new one arrive', async () => {
  show('/photos', [{ to: '/photos?view=trash', label: 'to trash' }])
  await screen.findAllByRole('button', { name: /^Open / })
  let finish: (answer: unknown) => void = () => {}
  listAnswer = () => new Promise((resolve) => (finish = resolve))
  fireEvent.click(screen.getByRole('button', { name: 'to trash' }))
  await waitFor(() => expect(document.querySelector('.photo-list.is-loading')).not.toBeNull())
  await act(async () => {
    finish({ ListPhotos: [] })
  })
  await waitFor(() => expect(document.querySelector('.photo-list')).toBeNull())
  expect(await screen.findByText('The Trash is empty.')).toBeTruthy()
})

// ---- uploading ----

it('switches to the library when files are added from Favorites, where new photos would not be seen', async () => {
  show('/photos?view=favorites')
  await screen.findByRole('searchbox')
  const input = document.querySelector<HTMLInputElement>('input[type=file]')!
  fireEvent.change(input, { target: { files: [new File(['x'], 'hang-1.jpg', { type: 'image/jpeg' })] } })
  await waitFor(() => expect(address()).toBe('/photos'))
})

it('says that files go to the library when they are dropped on an album, and starts no upload', async () => {
  show('/photos/albums/album-1')
  await screen.findByRole('searchbox')
  const page = document.querySelector('.photos-page')!
  const file = new File(['x'], 'hang-1.jpg', { type: 'image/jpeg' })
  fireEvent.drop(page, { dataTransfer: { types: ['Files'], files: [file] } })
  expect(await screen.findByText('Upload photos from your library, then add them to an album.')).toBeTruthy()
  expect(photoUploads.getItems()).toHaveLength(0)
})

it('does not claim that files were left out when all of them were pictures and videos', async () => {
  show('/photos')
  await screen.findByRole('searchbox')
  const input = document.querySelector<HTMLInputElement>('input[type=file]')!
  fireEvent.change(input, { target: { files: [new File(['x'], 'hang-1.jpg', { type: 'image/jpeg' })] } })
  await sleep(50)
  expect(screen.queryByText(/were left out/)).toBeNull()
})

// ---- favorites ----

it('sends favorite changes of one photo one after the other, so the heart and the server agree', async () => {
  const sent: boolean[] = []
  const finishers: (() => void)[] = []
  updatePhotoAnswer = (variables) =>
    new Promise((resolve) => {
      sent.push(variables.isFavorite as boolean)
      finishers.push(() => resolve({ UpdatePhoto: photoFixture('photo-1', { isFavorite: variables.isFavorite }) }))
    })
  show('/photos/photo-1')
  const heart = await screen.findByRole('button', { name: 'Add to Favorites' })
  fireEvent.click(heart)
  const unheart = await screen.findByRole('button', { name: 'Remove from Favorites' })
  fireEvent.click(unheart)
  await sleep(50)
  // The second waits for the first.
  expect(sent).toEqual([true])
  await act(async () => finishers[0]())
  await waitFor(() => expect(sent).toEqual([true, false]))
  await act(async () => finishers[1]())
  await waitFor(() => expect(screen.getByRole('button', { name: 'Add to Favorites' })).toBeTruthy())
})

it('keeps the heart where the person put it while the server is slow to answer', async () => {
  let finish: () => void = () => {}
  updatePhotoAnswer = () =>
    new Promise((resolve) => {
      finish = () => resolve({ UpdatePhoto: photoFixture('photo-1', { isFavorite: true }) })
    })
  show('/photos/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Add to Favorites' }))
  await screen.findByRole('button', { name: 'Remove from Favorites' })
  // The list is read again with the old answer in it.
  await act(async () => {
    window.dispatchEvent(new Event('teanode:photos-changed'))
    await sleep(20)
  })
  expect(screen.getByRole('button', { name: 'Remove from Favorites' })).toBeTruthy()
  library = library.map((photo) => (photo.id === 'photo-1' ? { ...photo, isFavorite: true } : photo))
  await act(async () => finish())
  await waitFor(() => expect(screen.getByRole('button', { name: 'Remove from Favorites' })).toBeTruthy())
  expect(within(document.body).queryAllByRole('button', { name: 'Add to Favorites' })).toHaveLength(0)
})

// ---- asking about a photo, and trying again ----

// The assistant is pointed at the photo, not sent its id in a sentence: the
// drawer shows a chip, and the person writes the question.
it('hands the drawer the photo as a reference, with the place and day, and closes the viewer', async () => {
  isAssistantEnabled = true
  vi.mocked(askAgentAbout).mockReturnValue(true)
  library = library.map((photo) => (photo.id === 'photo-1' ? { ...photo, placeName: 'Harbor Town' } : photo))
  readPhotoAnswer = async (photoId) => ({
    ReadPhoto: {
      photo: {
        ...photoFixture(photoId, { placeName: 'Harbor Town' }),
        photoDescription: '',
        descriptionVisibleText: '',
        describedAt: null,
        isDescriptionRequested: false,
      },
      photoFiles: [],
      albums: [],
      faces: [],
    },
  })
  show('/photos/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Ask the assistant about this photo' }))
  expect(askAgentAbout).toHaveBeenCalledWith({ photoId: 'photo-1', photoTitle: 'Harbor Town · 2030-07-12' })
  await waitFor(() => expect(address()).toBe('/photos'))
})

it('goes to the agent settings when there is no agent to ask, and keeps the viewer', async () => {
  isAssistantEnabled = true
  vi.mocked(askAgentAbout).mockReturnValue(false)
  show('/photos/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Ask the assistant about this photo' }))
  expect(askAgentAbout).toHaveBeenCalledTimes(1)
  await waitFor(() => expect(address()).toBe('/settings/agent'))
})

const selectPhotos = async (...ids: string[]) => {
  for (const id of ids) fireEvent.click(await screen.findByRole('checkbox', { name: `Select ${id}.jpg` }))
}

// Every selected photo has failed: the bar offers Try again, a toast says
// how many were moved, and the list is read again.
it('offers Try again in the selection bar when every selected photo has failed, and says how many', async () => {
  library = library.map((photo) =>
    photo.id === 'photo-3' ? photo : { ...photo, photoProcessingState: 'failed', smallPreviewUrl: '' },
  )
  show('/photos')
  await selectPhotos('photo-1', 'photo-2')
  const listsBefore = callsTo('ListPhotos').length
  fireEvent.click(await screen.findByRole('button', { name: 'Try again' }))
  await waitFor(() => expect(callsTo('RetryPhotoProcessing')).toEqual([{ photoIds: ['photo-1', 'photo-2'] }]))
  expect(await screen.findByText('Trying 2 photos again.')).toBeTruthy()
  await waitFor(() => expect(callsTo('ListPhotos').length).toBeGreaterThan(listsBefore))
  expect(screen.queryByRole('toolbar')).toBeNull()
})

it('changes the place of the selected photos through the dialog, and reads the list again', async () => {
  extraAnswer = (document, variables) =>
    document.includes('SearchPhotoPlaces(')
      ? {
          SearchPhotoPlaces: [
            { placeName: 'Lisbon, Portugal', latitude: 38.72, longitude: -9.14, distanceKilometers: null },
          ],
        }
      : document.includes('SetPhotoLocation(')
        ? { SetPhotoLocation: (variables.photoIds as string[]).length }
        : undefined
  show('/photos')
  await selectPhotos('photo-1', 'photo-2')
  const listsBefore = callsTo('ListPhotos').length
  fireEvent.click(await screen.findByRole('button', { name: 'Change location' }))
  fireEvent.change(await screen.findByRole('searchbox', { name: 'Search for a city' }), { target: { value: 'Lisb' } })
  fireEvent.click(await screen.findByRole('button', { name: /Lisbon, Portugal/ }))
  await waitFor(() =>
    expect(callsTo('SetPhotoLocation')).toEqual([
      { photoIds: ['photo-1', 'photo-2'], latitude: 38.72, longitude: -9.14 },
    ]),
  )
  expect(await screen.findByText('Location changed for 2 photos')).toBeTruthy()
  await waitFor(() => expect(callsTo('ListPhotos').length).toBeGreaterThan(listsBefore))
  expect(screen.queryByRole('toolbar')).toBeNull()
})

it('offers no Change location for a selection with a photo that is not the person\u2019s own', async () => {
  library = library.map((photo) => (photo.id === 'photo-2' ? { ...photo, isOwnedByCaller: false } : photo))
  show('/photos')
  await selectPhotos('photo-1', 'photo-2')
  await screen.findByRole('toolbar')
  expect(screen.queryByRole('button', { name: 'Change location' })).toBeNull()
})

it('offers no Try again when a selected photo has not failed', async () => {
  library = library.map((photo) =>
    photo.id === 'photo-1' ? { ...photo, photoProcessingState: 'failed', smallPreviewUrl: '' } : photo,
  )
  show('/photos')
  await selectPhotos('photo-1', 'photo-2')
  await screen.findByRole('toolbar')
  expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull()
})

it('retries the photo of the viewer, and makes thumbnails again for a ready one', async () => {
  library = library.map((photo) =>
    photo.id === 'photo-1'
      ? { ...photo, photoProcessingState: 'failed', smallPreviewUrl: '', largePreviewUrl: '' }
      : photo,
  )
  show('/photos/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Try again' }))
  await waitFor(() => expect(callsTo('RetryPhotoProcessing')).toEqual([{ photoIds: ['photo-1'] }]))
  expect(await screen.findByText('Trying 1 photo again.')).toBeTruthy()
  cleanup()
  show('/photos/photo-2')
  fireEvent.click(await screen.findByRole('button', { name: 'More' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Make thumbnails again' }))
  await waitFor(() => expect(callsTo('RetryPhotoProcessing')).toHaveLength(2))
  expect(callsTo('RetryPhotoProcessing')[1]).toEqual({ photoIds: ['photo-2'] })
})

it('says so when nothing was moved, and when the server refuses', async () => {
  retryAnswer = async () => ({ RetryPhotoProcessing: 0 })
  show('/photos/photo-2')
  fireEvent.click(await screen.findByRole('button', { name: 'More' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Make thumbnails again' }))
  expect(await screen.findByText('Nothing to try again.')).toBeTruthy()
})

// ---- albums shared with the person, and sharing the person's own ----

const BEN = { userId: 'person-2', username: 'ben', name: 'Ben', avatarUrl: null, avatarColorIndex: 3 }
const sharedWithMe = {
  ownerUserId: 'person-2',
  ownerUsername: 'ben',
  ownerName: 'Ben',
  isOwnedByCaller: false,
  shareLevel: 'view',
  canCallerContribute: false,
  canCallerManage: false,
  isLocationIncluded: false,
}
const bensPhotos = () =>
  ['photo-1', 'photo-2', 'photo-3'].map((id) =>
    photoFixture(id, { isOwnedByCaller: false, ownerName: 'Ben', userId: 'person-2', isFavorite: false }),
  )

it('says an album is shared with the person, and offers no sharing, adding or renaming of it', async () => {
  albumOwnership = sharedWithMe
  library = bensPhotos()
  show('/photos/albums/album-1')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(
    screen.getByText(
      'Shared with you. You can see these photos and save them. Where the photos were taken is not shown.',
    ),
  ).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Add photos' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Album options' })).toBeNull()
})

it('shows a photo of somebody else with its owner and Save to my library, and no favorite or Trash', async () => {
  albumOwnership = sharedWithMe
  library = bensPhotos()
  extraAnswer = (document) => {
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [BEN] }
    if (document.includes('SavePhotoToLibrary('))
      return { SavePhotoToLibrary: photoFixture('saved-1', { isOwnedByCaller: true }) }
    return undefined
  }
  show('/photos/albums/album-1/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: /Save to my library/ }))
  await waitFor(() => expect(callsTo('SavePhotoToLibrary')).toEqual([{ photoId: 'photo-1' }]))
  expect(await screen.findByText('Saved 1 photo to your library.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'View photo' })).toBeTruthy()
  const viewer = screen.getByRole('dialog', { name: 'Photo viewer' })
  expect(within(viewer).getByText('Shared by')).toBeTruthy()
  expect(within(viewer).getByText('Ben')).toBeTruthy()
  expect(within(viewer).queryByRole('button', { name: 'Add to Favorites' })).toBeNull()
  expect(within(viewer).queryByRole('button', { name: 'Move to Trash' })).toBeNull()
  expect(within(viewer).queryByRole('button', { name: 'Add to album' })).toBeNull()
  expect(within(viewer).queryByRole('button', { name: 'Change date' })).toBeNull()
})

it('offers only Save to my library for a selection of somebody else’s photos', async () => {
  albumOwnership = sharedWithMe
  library = bensPhotos()
  show('/photos/albums/album-1')
  await selectPhotos('photo-1', 'photo-2')
  const bar = await screen.findByRole('toolbar')
  expect(within(bar).getByRole('button', { name: 'Save to my library' })).toBeTruthy()
  expect(within(bar).queryByRole('button', { name: 'Move to Trash' })).toBeNull()
  expect(within(bar).queryByRole('button', { name: 'Add to Favorites' })).toBeNull()
  expect(within(bar).queryByRole('button', { name: 'Remove from album' })).toBeNull()
})

const sharingAnswers = (shares: unknown[] = []) => {
  extraAnswer = (document) => {
    if (document.includes('ListPhotoAlbumShares')) return { ListPhotoAlbumShares: shares }
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [BEN] }
    if (document.includes('ListCircles')) return { ListCircles: [{ id: 'circle-1', name: 'Family' }] }
    if (document.includes('SharePhotoAlbum(')) return { SharePhotoAlbum: { id: 'share-1' } }
    if (document.includes('RemovePhotoAlbumShare(')) return { RemovePhotoAlbumShare: true }
    return undefined
  }
}
const benShare = {
  id: 'share-1',
  photoAlbumId: 'album-1',
  granteeUserId: 'person-2',
  granteeUsername: 'ben',
  granteeName: 'Ben',
  granteeAvatarUrl: null,
  granteeAvatarColorIndex: 3,
  granteeCircleId: null,
  granteeCircleName: '',
  shareLevel: 'view',
  isLocationIncluded: false,
  createdAt: '2030-07-12T09:00:00Z',
}

it('shares the album with a person at the level and with the places the person chose', async () => {
  sharingAnswers()
  show('/photos/albums/album-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share “Picnic”' })
  expect(await within(dialog).findByText('This album is not shared with anybody yet.')).toBeTruthy()
  // Nothing is chosen, so there is nothing to send.
  expect(within(dialog).getByRole('button', { name: 'Share' }).hasAttribute('disabled')).toBe(true)
  fireEvent.click(await within(dialog).findByRole('radio', { name: 'Ben' }))
  fireEvent.click(within(dialog).getByRole('radio', { name: /Contribute/ }))
  fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Include where photos were taken' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(callsTo('SharePhotoAlbum')).toEqual([
      {
        photoAlbumId: 'album-1',
        granteeUserId: 'person-2',
        granteeCircleId: null,
        shareLevel: 'contribute',
        isLocationIncluded: true,
      },
    ]),
  )
  expect(await screen.findByText('Shared with Ben.')).toBeTruthy()
})

it('shares the album with a circle, named as everyone in it', async () => {
  sharingAnswers()
  show('/photos/albums/album-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share “Picnic”' })
  fireEvent.click(await within(dialog).findByRole('radio', { name: 'Everyone in Family' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(callsTo('SharePhotoAlbum')).toHaveLength(1))
  expect(callsTo('SharePhotoAlbum')[0]).toMatchObject({
    granteeUserId: null,
    granteeCircleId: 'circle-1',
    shareLevel: 'view',
  })
})

it('changes what somebody has by sharing again, starting from what they have now', async () => {
  sharingAnswers([{ ...benShare, shareLevel: 'view', isLocationIncluded: true }])
  show('/photos/albums/album-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share “Picnic”' })
  expect(await within(dialog).findByText('Places included')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Change what Ben can do' }))
  expect(
    (within(dialog).getByRole('checkbox', { name: 'Include where photos were taken' }) as HTMLInputElement).checked,
  ).toBe(true)
  fireEvent.click(within(dialog).getByRole('radio', { name: /Contribute/ }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(callsTo('SharePhotoAlbum')).toHaveLength(1))
  expect(callsTo('SharePhotoAlbum')[0]).toMatchObject({
    granteeUserId: 'person-2',
    shareLevel: 'contribute',
    isLocationIncluded: true,
  })
  expect(await screen.findByText('Changed what Ben can do.')).toBeTruthy()
})

it('asks before taking a share back, and says their photos leave the album', async () => {
  sharingAnswers([benShare])
  show('/photos/albums/album-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share “Picnic”' })
  fireEvent.click(await within(dialog).findByRole('button', { name: 'Stop sharing with Ben' }))
  const confirm = await screen.findByRole('alertdialog', { name: 'Stop sharing with Ben?' })
  expect(within(confirm).getByText(/the photos they added to it leave the album/)).toBeTruthy()
  expect(callsTo('RemovePhotoAlbumShare')).toEqual([])
  fireEvent.click(within(confirm).getByRole('button', { name: 'Stop sharing' }))
  await waitFor(() => expect(callsTo('RemovePhotoAlbumShare')).toEqual([{ photoAlbumShareId: 'share-1' }]))
})

it('adds photos of the person’s own to an album they contribute to, from their library', async () => {
  extraAnswer = (document) => (document.includes('AddPhotosToAlbum(') ? { AddPhotosToAlbum: 2 } : undefined)
  show('/photos/albums/album-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Add photos' }))
  const dialog = await screen.findByRole('dialog', { name: 'Add photos to “Picnic”' })
  expect(within(dialog).getByRole('button', { name: 'Add photos' }).hasAttribute('disabled')).toBe(true)
  const cells = await within(dialog).findAllByRole('button', { name: /^Select photo-/ })
  fireEvent.click(cells[0])
  fireEvent.click(cells[2])
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add 2 photos' }))
  await waitFor(() =>
    expect(callsTo('AddPhotosToAlbum')).toEqual([{ photoAlbumId: 'album-1', photoIds: ['photo-1', 'photo-3'] }]),
  )
  expect(await screen.findByText('Added 2 photos to “Picnic”.')).toBeTruthy()
})

// ---- correcting a date ----

it('changes the date at the photo’s own offset, writing it as RFC 3339 with that offset', async () => {
  library = library.map((photo) => (photo.id === 'photo-2' ? { ...photo, takenAtOffsetMinutes: 540 } : photo))
  updatePhotoAnswer = async (variables) => ({ UpdatePhoto: photoFixture(String(variables.photoId)) })
  show('/photos/photo-2')
  fireEvent.click(await screen.findByRole('button', { name: 'Change date' }))
  const dialog = await screen.findByRole('dialog', { name: 'Change the date' })
  const field = within(dialog).getByLabelText('Date and time') as HTMLInputElement
  // 09:00 UTC is 18:00 where the photo was taken.
  expect(field.value).toBe('2030-07-12T18:00')
  expect(within(dialog).getByText('In the time zone where the photo was taken (UTC+09:00).')).toBeTruthy()
  fireEvent.change(field, { target: { value: '2030-07-13T07:30' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(callsTo('UpdatePhoto').filter((call) => call.takenAt !== undefined)).toEqual([
      { photoId: 'photo-2', takenAt: '2030-07-13T07:30:00+09:00' },
    ]),
  )
  expect(await screen.findByText('Changed the date.')).toBeTruthy()
})

it('says the browser’s zone when the photo has no offset of its own', async () => {
  library = library.map((photo) =>
    photo.id === 'photo-2' ? photoFixture('photo-2', { takenAtOffsetMinutes: null }) : photo,
  )
  show('/photos/photo-2')
  fireEvent.click(await screen.findByRole('button', { name: 'Change date' }))
  const dialog = await screen.findByRole('dialog', { name: 'Change the date' })
  expect(within(dialog).getByText(/In your browser's time zone \(UTC[+-]\d\d:\d\d\)/)).toBeTruthy()
})

// ---- one toolbar on every page ----

it('draws the timeline as one grid with no day headings, and the days on the screen under the heading', async () => {
  library = [
    photoFixture('photo-1', { takenAt: '2030-07-14T09:00:00Z' }),
    photoFixture('photo-2', { takenAt: '2030-07-12T09:00:00Z' }),
    photoFixture('photo-3', { takenAt: '2030-07-09T09:00:00Z' }),
  ]
  show('/photos')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(screen.queryAllByRole('heading', { level: 2 })).toHaveLength(0)
  expect(document.querySelectorAll('.photo-grid')).toHaveLength(1)
  // A browser that cannot say what is on screen reads the whole page.
  await waitFor(() =>
    expect(screen.getByTestId('photo-date-range').textContent?.replace(/\s/g, ' ')).toBe('Jul 9 \u2013 14, 2030'),
  )
})

it('says the days of the photos that are on the screen, and keeps the words when nothing is on screen for a moment', async () => {
  const observers: { callback: IntersectionObserverCallback; targets: Element[] }[] = []
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      targets: Element[] = []
      constructor(callback: IntersectionObserverCallback) {
        observers.push({ callback, targets: this.targets })
      }
      observe(target: Element) {
        this.targets.push(target)
      }
      disconnect() {}
      unobserve() {}
    },
  )
  library = [
    photoFixture('photo-1', { takenAt: '2030-07-14T09:00:00Z' }),
    photoFixture('photo-2', { takenAt: '2030-07-12T09:00:00Z' }),
    photoFixture('photo-3', { takenAt: '2030-07-09T09:00:00Z' }),
  ]
  show('/photos')
  await screen.findAllByRole('button', { name: /^Open / })
  await waitFor(() => expect(observers.length).toBeGreaterThan(0))
  const observer = observers[observers.length - 1]
  const cell = (photoId: string) => observer.targets.find((target) => target.getAttribute('data-photo-id') === photoId)!
  const scrollTo = (photoIds: string[], isIntersecting: boolean) =>
    act(() => {
      observer.callback(
        photoIds.map((photoId) => ({ target: cell(photoId), isIntersecting }) as unknown as IntersectionObserverEntry),
        {} as IntersectionObserver,
      )
    })
  const words = () => screen.getByTestId('photo-date-range').textContent?.replace(/\s/g, ' ')
  scrollTo(['photo-1', 'photo-2'], true)
  await waitFor(() => expect(words()).toBe('Jul 12 \u2013 14, 2030'))
  // Scrolled on: the first has gone, the last has come.
  scrollTo(['photo-1'], false)
  scrollTo(['photo-3'], true)
  await waitFor(() => expect(words()).toBe('Jul 9 \u2013 12, 2030'))
  // Nothing on screen for a moment: the words stay.
  scrollTo(['photo-2', 'photo-3'], false)
  expect(words()).toBe('Jul 9 \u2013 12, 2030')
})

it('says no days for a search, whose photos are not in the order of the days', async () => {
  show('/photos?search=kite')
  await screen.findByRole('button', { name: 'Open photo-1.jpg' })
  expect(screen.queryByText('Friday, July 12, 2030')).toBeNull()
  expect(screen.getByTestId('photo-date-range').textContent).toBe('\u00a0')
})

// ---- Years, Months and All ----

// A library the way the server keeps it for these questions: the photos newest
// first, narrowed by the two ends of a date range, counted the same way.
const serveDatedLibrary = (dates: string[]) => {
  library = dates.map((date, index) => photoFixture(`dated-${index}`, { takenAt: `${date}T12:00:00Z` }))
  const narrowed = (variables: Record<string, unknown>) =>
    library
      .filter(
        (photo) =>
          (!variables.takenAfter || new Date(photo.takenAt) >= new Date(String(variables.takenAfter))) &&
          (!variables.takenBefore || new Date(photo.takenAt) < new Date(String(variables.takenBefore))) &&
          (!variables.photoKind || photo.photoKind === variables.photoKind),
      )
      .sort((first, second) => second.takenAt.localeCompare(first.takenAt))
  listAnswer = async (variables) => {
    const offset = Number(variables.offset ?? 0)
    return { ListPhotos: narrowed(variables).slice(offset, offset + Number(variables.pageSize)) }
  }
  extraAnswer = (document, variables) =>
    document.includes('CountPhotos(') ? { CountPhotos: narrowed(variables).length } : undefined
}
const SMALL_DATED_LIBRARY = ['2031-03-20', '2030-12-25', '2030-07-12', '2030-07-02', '2029-01-15']
const groupNamed = (name: string) => screen.queryByRole('group', { name })

it('offers Years, Months and All over the library, with All chosen, and writes the choice to the address', async () => {
  serveDatedLibrary(SMALL_DATED_LIBRARY)
  show('/photos')
  const group = await screen.findByRole('group', { name: 'Browse by' })
  const pressed = (name: string) => within(group).getByRole('button', { name }).getAttribute('aria-pressed')
  expect([pressed('Years'), pressed('Months'), pressed('All')]).toEqual(['false', 'false', 'true'])
  fireEvent.click(within(group).getByRole('button', { name: 'Months' }))
  await waitFor(() => expect(address()).toBe('/photos?show=months'))
  expect(within(group).getByRole('button', { name: 'Months' }).getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(within(group).getByRole('button', { name: 'All' }))
  await waitFor(() => expect(address()).toBe('/photos'))
})

it('shows a picture for each year that has photos, newest first', async () => {
  serveDatedLibrary(SMALL_DATED_LIBRARY)
  show('/photos?show=years')
  await screen.findByRole('button', { name: 'Show 2031' })
  const tiles = screen.getAllByRole('button', { name: /^Show \d{4}$/ })
  expect(tiles.map((tile) => tile.getAttribute('aria-label'))).toEqual(['Show 2031', 'Show 2030', 'Show 2029'])
  // The picture is the newest of the year.
  expect(tiles[1].querySelector('img')?.getAttribute('src')).toBe('/large-dated-1')
  // Under the heading, the years the library spans.
  expect(screen.getByTestId('photo-date-range').textContent?.replace(/\s/g, ' ')).toBe('2029 \u2013 2031')
})

it('shows a picture for each month that has photos, under its year, and none for a month without', async () => {
  serveDatedLibrary(SMALL_DATED_LIBRARY)
  show('/photos?show=months')
  await screen.findByRole('button', { name: 'Show December 2030' })
  expect(screen.getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent)).toEqual([
    '2031',
    '2030',
    '2029',
  ])
  const tiles = screen.getAllByRole('button', { name: /^Show [A-Z][a-z]+ \d{4}$/ })
  expect(tiles.map((tile) => tile.getAttribute('aria-label'))).toEqual([
    'Show March 2031',
    'Show December 2030',
    'Show July 2030',
    'Show January 2029',
  ])
  // The month with two photos has the newer of them.
  expect(tiles[2].querySelector('img')?.getAttribute('src')).toBe('/large-dated-2')
})

it('keeps what the filters keep, and has no Select, in an overview', async () => {
  serveDatedLibrary(SMALL_DATED_LIBRARY)
  show('/photos?show=years&from=2030-01-01')
  await screen.findByRole('button', { name: 'Show 2030' })
  // Nothing before the first day chosen is in an overview of the days chosen.
  expect(screen.queryByRole('button', { name: 'Show 2029' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Show 2031' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Select photos' })).toBeNull()
})

it('goes from a month to the grid at the page that starts it, with its first photo brought to the top', async () => {
  // Thirty photos in December and thirty in July, on pages of twenty-five:
  // July starts at the sixth photo of the second page.
  const dates = [
    ...Array.from({ length: 30 }, (_, index) => `2030-12-${String(30 - index).padStart(2, '0')}`),
    ...Array.from({ length: 30 }, (_, index) => `2030-07-${String(30 - index).padStart(2, '0')}`),
  ]
  serveDatedLibrary(dates)
  const revealed: (string | null)[] = []
  Element.prototype.scrollIntoView = function (this: Element) {
    revealed.push(this.getAttribute('data-photo-id'))
  }
  try {
    show('/photos?show=months&rows=25')
    fireEvent.click(await screen.findByRole('button', { name: 'Show July 2030' }))
    await waitFor(() => expect(address()).toBe('/photos?rows=25&page=2'))
    await screen.findAllByRole('button', { name: /^Open / })
    await waitFor(() => expect(revealed).toEqual(['dated-30']))
    // Once is enough: the address forgets it, and the grid is not scrolled again.
    await sleep(50)
    expect(revealed).toEqual(['dated-30'])
    expect(screen.queryByRole('group', { name: 'Browse by' })).toBeTruthy()
    expect(
      within(screen.getByRole('group', { name: 'Browse by' }))
        .getByRole('button', { name: 'All' })
        .getAttribute('aria-pressed'),
    ).toBe('true')
  } finally {
    delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView
  }
})

it('goes from a year to the first page when nothing is newer', async () => {
  serveDatedLibrary(SMALL_DATED_LIBRARY)
  show('/photos?show=years')
  fireEvent.click(await screen.findByRole('button', { name: 'Show 2031' }))
  await waitFor(() => expect(address()).toBe('/photos'))
  await screen.findAllByRole('button', { name: /^Open / })
})

it('offers the switch for the Favorites, and not for the Trash, a search, an album or the empty library', async () => {
  serveDatedLibrary(SMALL_DATED_LIBRARY)
  show('/photos?view=favorites')
  expect(await screen.findByRole('group', { name: 'Browse by' })).toBeTruthy()
  cleanup()
  show('/photos?view=trash&show=months')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(groupNamed('Browse by')).toBeNull()
  cleanup()
  show('/photos?search=kite&show=months')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(groupNamed('Browse by')).toBeNull()
  cleanup()
  show('/photos/albums/album-1?show=months')
  await screen.findAllByRole('button', { name: /^Open / })
  expect(groupNamed('Browse by')).toBeNull()
  cleanup()
  library = []
  show('/photos')
  await screen.findByText(/No photos/i)
  expect(groupNamed('Browse by')).toBeNull()
})

it('says the closest matches when no photo has a word of the search, and not when one does', async () => {
  show('/photos?search=zzqx')
  expect(await screen.findByText('Closest matches')).toBeTruthy()
  cleanup()
  library = library.map((photo) => ({ ...photo, placeName: 'Kite Hill' }))
  show('/photos?search=kite')
  await screen.findByRole('button', { name: 'Open photo-1.jpg' })
  expect(screen.queryByText('Closest matches')).toBeNull()
})

it('does not call a search by a name the closest matches before the names have answered', async () => {
  let answerNames: (answer: unknown) => void = () => {}
  const namesAnswer = new Promise((resolve) => (answerNames = resolve))
  extraAnswer = (document) => (document.includes('ListPhotoFaceGroups') ? namesAnswer : undefined)
  show('/photos?search=zzqx')
  await screen.findByRole('button', { name: 'Open photo-1.jpg' })
  expect(screen.queryByText('Closest matches')).toBeNull()
  answerNames({ ListPhotoFaceGroups: [] })
  expect(await screen.findByText('Closest matches')).toBeTruthy()
})

it('keeps the Trash in the order the server lists it, newest moved first, under one heading a day', async () => {
  library = [
    photoFixture('photo-1', { trashedAt: '2030-07-19T08:00:00Z', takenAt: '2010-01-01T00:00:00Z' }),
    photoFixture('photo-2', { trashedAt: '2030-07-19T07:00:00Z', takenAt: '2030-01-01T00:00:00Z' }),
    photoFixture('photo-3', { trashedAt: '2030-07-10T08:00:00Z', takenAt: '2020-01-01T00:00:00Z' }),
  ]
  show('/photos?view=trash')
  await screen.findByRole('button', { name: 'Open photo-1.jpg' })
  const names = screen
    .getAllByRole('button', { name: /^Open photo-/ })
    .map((button) => button.getAttribute('aria-label'))
  expect(names).toEqual(['Open photo-1.jpg', 'Open photo-2.jpg', 'Open photo-3.jpg'])
  expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(2)
})

it('draws the Trash once the library has said how long it keeps a photo, not before', async () => {
  library = library.map((photo) => ({ ...photo, trashedAt: new Date().toISOString() }))
  let answerLibrary: (answer: unknown) => void = () => {}
  const libraryAnswer = new Promise((resolve) => (answerLibrary = resolve))
  extraAnswer = (document) => (document.includes('ReadPhotoLibrary') ? libraryAnswer : undefined)
  show('/photos?view=trash')
  await waitFor(() => expect(callsTo('ListPhotos').length).toBeGreaterThan(0))
  await new Promise((resolve) => setTimeout(resolve, 30))
  expect(screen.queryByRole('button', { name: 'Open photo-1.jpg' })).toBeNull()
  answerLibrary({
    ReadPhotoLibrary: {
      photoCount: 0,
      videoCount: 0,
      favoriteCount: 0,
      trashedCount: 3,
      usedBytes: 0,
      maximumFileBytes: 1000,
      maximumAccountBytes: 0,
      trashRetentionDays: 30,
      uploadPath: '/api/v1/photos/uploads',
      uploadPartBytes: 1000,
      isDescriptionAvailable: false,
      descriptionUnavailableMessage: '',
      isFaceRecognitionAvailable: false,
      faceRecognitionUnavailableMessage: '',
      isPhotoFaceRecognitionEnabled: false,
    },
  })
  expect(await screen.findByText('Deleted forever in 30 days')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Open photo-1.jpg' })).toBeTruthy()
})

it('offers no search or filter on a list with nothing in it, and keeps them once a search has found nothing', async () => {
  library = []
  show('/photos')
  await screen.findByText(/No photos yet/)
  expect(screen.queryByRole('searchbox')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Filter' })).toBeNull()
  cleanup()
  show('/photos?search=kite')
  await screen.findByText('No photos match your search.')
  expect(screen.getByRole('searchbox')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Filter' })).toBeTruthy()
})

it('says how to add the first photos, with a button for each way that opens it', async () => {
  library = []
  const pressFileInput = vi.spyOn(HTMLInputElement.prototype, 'click')
  show('/photos')
  await screen.findByText('No photos yet. There are three ways to add some.')
  expect(screen.getByRole('heading', { name: 'Back up an iPhone' })).toBeTruthy()
  // One Upload photos button is the page's own; the other is in the card for the computer.
  const uploadButtons = screen.getAllByRole('button', { name: 'Upload photos' })
  expect(uploadButtons).toHaveLength(2)
  fireEvent.click(uploadButtons[1])
  expect(pressFileInput).toHaveBeenCalledTimes(1)
  expect(screen.getByText('teanode photo upload ~/Pictures')).toBeTruthy()
  expect(screen.getByText(`teanode auth login --url ${window.location.origin}`)).toBeTruthy()
  // Two copy buttons on one page are told apart by what they copy.
  expect(screen.getByRole('button', { name: 'Copy the sign-in command' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Copy the upload command' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'The command line guide' }).getAttribute('href')).toContain(
    'command-line.md#teanode-photo',
  )
})

it('leaves the first-photos ways out of the Trash, favorites and a search that found nothing', async () => {
  library = []
  for (const entry of ['/photos?view=trash', '/photos?view=favorites', '/photos?search=kite']) {
    show(entry)
    await screen.findByText(/The Trash is empty|No favorites yet|No photos match your search/)
    expect(screen.queryByRole('heading', { name: 'Back up an iPhone' })).toBeNull()
    expect(screen.queryByText('teanode photo upload ~/Pictures')).toBeNull()
    cleanup()
  }
})

it('says how many are selected in the bar, short enough for a phone', async () => {
  show('/photos')
  await selectPhotos('photo-1', 'photo-2')
  expect((await screen.findByRole('toolbar')).textContent).toContain('2 selected')
})

it('puts rename and delete of an album among its actions as icons, not in a menu', async () => {
  show('/photos/albums/album-1')
  await screen.findByRole('button', { name: 'Rename album' })
  expect(screen.getByRole('button', { name: 'Delete album' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Album options' })).toBeNull()
})

it('says an album was renamed', async () => {
  extraAnswer = (document) =>
    document.includes('RenamePhotoAlbum(')
      ? { RenamePhotoAlbum: { id: 'album-1', albumTitle: 'Lake', ...ownAlbum } }
      : undefined
  show('/photos/albums/album-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Rename album' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Lake' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(await screen.findByText('Album renamed.')).toBeTruthy()
})

it('names the album in the toast that says its cover was changed', async () => {
  extraAnswer = (document) =>
    document.includes('SetPhotoAlbumCover(')
      ? { SetPhotoAlbumCover: { id: 'album-1', albumTitle: 'Picnic' } }
      : undefined
  show('/photos/albums/album-1/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'More' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Use as album cover' }))
  expect(await screen.findByText('Changed the cover of “Picnic”.')).toBeTruthy()
})

it('says so with a way to add photos when the album has none, and offers none to somebody who cannot add', async () => {
  library = []
  show('/photos/albums/album-1')
  expect(await screen.findByText(/This album has no photos yet\. Add some from your library\./)).toBeTruthy()
  expect(screen.getAllByRole('button', { name: 'Add photos' })).toHaveLength(2)
  cleanup()
  albumOwnership = sharedWithMe
  show('/photos/albums/album-1')
  await screen.findByText('This album has no photos yet.')
  expect(screen.queryByRole('button', { name: 'Add photos' })).toBeNull()
})

it('offers no upload in the Trash, and says how long each day of it has left', async () => {
  library = library.map((photo) => ({ ...photo, trashedAt: new Date().toISOString() }))
  show('/photos?view=trash')
  expect(await screen.findByText('Deleted forever in 30 days')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Upload photos' })).toBeNull()
  cleanup()
  show('/photos')
  expect(await screen.findByRole('button', { name: 'Upload photos' })).toBeTruthy()
})

it('draws no blank toolbar band above an empty Trash, and keeps the one above a Trash with photos', async () => {
  library = []
  show('/photos?view=trash')
  await screen.findByText('The Trash is empty.')
  expect(document.querySelector('.photo-toolbar')).toBeNull()
  cleanup()
  library = ['photo-1'].map((id) => photoFixture(id, { trashedAt: new Date().toISOString() }))
  show('/photos?view=trash')
  expect(await screen.findByRole('button', { name: 'Select photos' })).toBeTruthy()
  expect(document.querySelector('.photo-toolbar')).not.toBeNull()
})

it('offers to show a restored photo in the library', async () => {
  library = library.map((photo) => ({ ...photo, trashedAt: new Date().toISOString() }))
  extraAnswer = (document) => (document.includes('RestorePhotos(') ? { RestorePhotos: 1 } : undefined)
  show('/photos?view=trash')
  await selectPhotos('photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Restore' }))
  expect(await screen.findByText('Restored 1 photo.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Show in library' })).toBeTruthy()
})

it('opens the library near where a photo was taken, as a filter that stays in the address and can be taken off', async () => {
  library = library.map((photo) => ({ ...photo, placeName: 'Old Harbor', latitude: 10.5, longitude: 20.25 }))
  show('/photos/albums/album-1/photo-1')
  fireEvent.click(await screen.findByRole('button', { name: 'Photos taken near here' }))
  await waitFor(() => expect(address()).toBe('/photos?near=10.50000%2C20.25000&nearPlace=Old+Harbor'))
  expect(await screen.findByText('Near Old Harbor')).toBeTruthy()
  await waitFor(() =>
    expect(
      callsTo('ListPhotos').some(
        (variables) => variables.nearLatitude === 10.5 && variables.nearDistanceKilometers === 1,
      ),
    ).toBe(true),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Remove the filter “Near Old Harbor”' }))
  await waitFor(() => expect(address()).toBe('/photos'))
  expect(screen.queryByText('Near Old Harbor')).toBeNull()
})

it('puts the place of the near filter in a part that gives way, beside a close button of its own', async () => {
  show('/photos?near=10.50000%2C20.25000&nearPlace=A+Very+Long+Place+Name+That+Does+Not+Fit')
  const label = await screen.findByText('Near A Very Long Place Name That Does Not Fit')
  expect(label.className).toBe('photo-near-label')
  const close = screen.getByRole('button', { name: /^Remove the filter/ })
  expect(label.parentElement?.contains(close)).toBe(true)
  expect(close.className).toBe('icon-button')
})

it('opens the filters as a labelled dialog, not a menu, and closes it with Escape', async () => {
  show('/photos')
  const button = await screen.findByRole('button', { name: 'Filter' })
  expect(button.getAttribute('aria-haspopup')).toBe('dialog')
  fireEvent.click(button)
  const popover = await screen.findByRole('dialog', { name: 'Filter' })
  expect(screen.queryByRole('menu')).toBeNull()
  expect(within(popover).getByRole('radio', { name: 'All kinds' })).toBeTruthy()
  // The keyboard is taken into it, since it is at the end of the page, and
  // given back to the button when it closes.
  await waitFor(() => expect(popover.contains(document.activeElement)).toBe(true))
  fireEvent.keyDown(document.body, { key: 'Escape' })
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Filter' })).toBeNull())
  expect(document.activeElement).toBe(button)
})

it('opens the filters on the kind that is chosen, and gives the keyboard back to Filter when they are cleared', async () => {
  show('/photos?kind=video')
  const button = await screen.findByRole('button', { name: 'Filter' })
  fireEvent.click(button)
  const popover = await screen.findByRole('dialog', { name: 'Filter' })
  await waitFor(() => expect(document.activeElement).toBe(within(popover).getByRole('radio', { name: 'Videos' })))
  fireEvent.click(within(popover).getByRole('button', { name: 'Clear filters' }))
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Filter' })).toBeNull())
  expect(document.activeElement).toBe(button)
})

it('offers nothing near here for a photo with no place', async () => {
  show('/photos/photo-1')
  await screen.findByRole('button', { name: 'Details' })
  expect(screen.queryByRole('button', { name: 'Photos taken near here' })).toBeNull()
})

it('offers no asking about a photo to a person who may not use an assistant', async () => {
  isAssistantEnabled = true
  sessionPermissions = ['photos:use']
  library = library.map((photo) => (photo.id === 'photo-1' ? { ...photo, placeName: 'Harbor Town' } : photo))
  readPhotoAnswer = async (photoId) => ({
    ReadPhoto: {
      photo: {
        ...photoFixture(photoId, { placeName: 'Harbor Town' }),
        photoDescription: '',
        descriptionVisibleText: '',
        describedAt: null,
        isDescriptionRequested: false,
      },
      photoFiles: [],
      albums: [],
      faces: [],
    },
  })
  try {
    show('/photos/photo-1')
    // The details have loaded: the place is shown, and nothing offers asking.
    await screen.findByText('Harbor Town')
    // Asking is offered once the picture shown is known, a moment after the
    // details: long enough for it to have come if it were going to.
    await new Promise((resolve) => setTimeout(resolve, 600))
    expect(screen.queryByRole('button', { name: /about this photo/ })).toBeNull()
  } finally {
    isAssistantEnabled = false
    sessionPermissions = ['photos:use', 'agent:use']
  }
})

// ---- the photos of a person in the circles ----

const circlePerson = { userId: 'user-sam', username: 'sam', name: 'Sam Example', avatarUrl: null, avatarColorIndex: 1 }

function answerCirclePeople(people: unknown[] = [circlePerson]) {
  extraAnswer = (document) => (document.includes('ListCirclePeople') ? { ListCirclePeople: people } : undefined)
}

it('lists the photos of a person in the circles from the address, as a chip that can be taken off', async () => {
  answerCirclePeople()
  show('/photos?person=user-sam')
  expect(await screen.findByText('Photos of Sam Example')).toBeTruthy()
  await waitFor(() =>
    expect(callsTo('ListPhotos').some((variables) => variables.accountUserId === 'user-sam')).toBe(true),
  )
  await waitFor(() =>
    expect(callsTo('CountPhotos').some((variables) => variables.accountUserId === 'user-sam')).toBe(true),
  )
  // The library is never listed whole while the person is being looked up.
  expect(callsTo('ListPhotos').every((variables) => variables.accountUserId === 'user-sam')).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'Remove the filter “Photos of Sam Example”' }))
  await waitFor(() => expect(address()).toBe('/photos'))
  expect(screen.queryByText('Photos of Sam Example')).toBeNull()
  await waitFor(() => expect(callsTo('ListPhotos').some((variables) => variables.accountUserId === null)).toBe(true))
})

it('leaves the person behind in the address when another person is picked from People', async () => {
  answerCirclePeople()
  extraAnswer = (document) =>
    document.includes('ListCirclePeople')
      ? { ListCirclePeople: [circlePerson] }
      : document.includes('ReadPhotoFaceGroup')
        ? {
            ReadPhotoFaceGroup: {
              id: 'group-1',
              createdAt: '',
              updatedAt: '',
              contactId: 'contact-1',
              contactAddressBookId: 'book-1',
              contactName: 'Robin Example',
              isOwnContact: false,
              faceCount: 1,
              photoCount: 3,
              facePictureUrl: '',
              coverPhotoFaceId: '',
              isHidden: false,
            },
          }
        : undefined
  show('/photos?person=user-sam', [{ to: '/photos/people/group-1', label: 'to Robin' }])
  expect(await screen.findByText('Photos of Sam Example')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'to Robin' }))
  await waitFor(() => expect(address()).toBe('/photos/people/group-1'))
  expect(screen.queryByText('Photos of Sam Example')).toBeNull()
  await waitFor(() =>
    expect(
      callsTo('ListPhotos').some(
        (variables) => variables.faceGroupId === 'group-1' && variables.accountUserId === null,
      ),
    ).toBe(true),
  )
})

it('takes a person who is not among the people in the circles out of the address, says so, and lists nothing meanwhile', async () => {
  answerCirclePeople([])
  show('/photos?person=user-gone')
  expect(await screen.findByText('That person is not in your circles, so there are no photos to show.')).toBeTruthy()
  await waitFor(() => expect(address()).toBe('/photos'))
  expect(callsTo('ListPhotos').every((variables) => variables.accountUserId === null)).toBe(true)
})
