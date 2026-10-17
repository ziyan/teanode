import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { photoUploads } from '../../photos/uploadQueue'
import { PhotosPage } from './library'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../session')>()),
  useSession: () => ({
    userId: 'person-1',
    services: { isPhotosEnabled: true },
    permissions: { everywhere: ['photos:use'], byDomain: [] },
  }),
}))
// Files whose names start with "hang" are never read to the end.
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
  cameraMake: '',
  cameraModel: '',
  widthPixels: 0,
  heightPixels: 0,
  trashedAt: null,
  ...overrides,
})

// What the server holds, and what each read of it is told to wait for.
let library: ReturnType<typeof photoFixture>[]
let readPhotoAnswer: (photoId: string) => Promise<unknown>
let listAnswer: (variables: Record<string, unknown>) => Promise<unknown>
let updatePhotoAnswer: (variables: Record<string, unknown>) => Promise<unknown>

const callsTo = (name: string) =>
  vi
    .mocked(graphql)
    .mock.calls.filter(([document]) => document.includes(` ${name}(`) || document.includes(`{ ${name}(`))
    .map(([, variables]) => (variables ?? {}) as Record<string, unknown>)

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
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
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
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
        },
      } as never
    }
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
          <Routes>
            <Route path="/photos" element={<PhotosPage />} />
            <Route path="/photos/:photoId" element={<PhotosPage />} />
            <Route path="/photos/albums/:photoAlbumId" element={<PhotosPage />} />
          </Routes>
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
  await screen.findByText('Friday, July 12, 2030')
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
  await screen.findByText('Friday, July 12, 2030')
  const first = searches()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(11_000)
  })
  expect(searches()).toBeGreaterThan(first)
})

// ---- the viewer for a photo outside the list ----

it('shows a photo that is not in the list once it is read, and never says it is unavailable while it loads', async () => {
  let finish: (answer: unknown) => void = () => {}
  readPhotoAnswer = () => new Promise((resolve) => (finish = resolve))
  show('/photos/photo-9')
  await screen.findByText('Friday, July 12, 2030')
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

it('goes on across the pages: Previous from the first photo of a page opens the last of the page before', async () => {
  library = manyPhotos(203)
  show('/photos/photo-201?page=2')
  const previous = await screen.findByRole('button', { name: 'Previous photo' })
  expect(screen.queryByRole('button', { name: 'Next photo' })).not.toBeNull()
  fireEvent.click(previous)
  await waitFor(() => expect(address()).toBe('/photos/photo-200'))
  // Page one is read behind it, so that Previous goes on from there.
  await waitFor(() => expect(callsTo('ListPhotos').some((variables) => variables.offset === 0)).toBe(true))
  expect(await screen.findByRole('button', { name: 'Previous photo' })).toBeTruthy()
})

it('goes on across the pages: Next from the last photo of a page opens the first of the page after', async () => {
  library = manyPhotos(203)
  show('/photos/photo-200')
  const next = await screen.findByRole('button', { name: 'Next photo' })
  fireEvent.click(next)
  await waitFor(() => expect(address()).toBe('/photos/photo-201?page=2'))
  await waitFor(() => expect(callsTo('ListPhotos').some((variables) => variables.offset === 200)).toBe(true))
  // The last photo has nothing after it.
  fireEvent.click(await screen.findByRole('button', { name: 'Next photo' }))
  await waitFor(() => expect(address()).toBe('/photos/photo-202?page=2'))
  fireEvent.click(await screen.findByRole('button', { name: 'Next photo' }))
  await waitFor(() => expect(address()).toBe('/photos/photo-203?page=2'))
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Next photo' })).toBeNull())
})

// ---- filters ----

it('asks for the kind and the days in the address, and writes them back when they change', async () => {
  show('/photos?kind=video&from=2030-07-12&to=2030-07-14')
  await screen.findByText('Friday, July 12, 2030')
  const variables = callsTo('ListPhotos')[0]
  expect(variables.photoKind).toBe('video')
  const after = new Date(String(variables.takenAfter))
  const before = new Date(String(variables.takenBefore))
  expect([after.getMonth(), after.getDate(), after.getHours()]).toEqual([6, 12, 0])
  expect([before.getMonth(), before.getDate(), before.getHours()]).toEqual([6, 15, 0])
  fireEvent.change(screen.getByLabelText('From'), { target: { value: '2030-07-13' } })
  await waitFor(() => expect(address()).toBe('/photos?kind=video&from=2030-07-13&to=2030-07-14'))
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  await waitFor(() => expect(address()).toBe('/photos'))
  expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull()
})

it('offers the kind of photo to keep, and writes the choice to the address', async () => {
  show('/photos')
  await screen.findByText('Friday, July 12, 2030')
  fireEvent.click(screen.getByRole('combobox', { name: 'Kind' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Videos' }))
  await waitFor(() => expect(address()).toBe('/photos?kind=video'))
})

// ---- the list a person has left ----

it('dims the rows of the list that was left, until the rows of the new one arrive', async () => {
  show('/photos', [{ to: '/photos?view=trash', label: 'to trash' }])
  await screen.findByText('Friday, July 12, 2030')
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
