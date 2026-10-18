import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import type { Photo, PhotoLibrary } from '../../photos/api'
import { PhotoViewer } from './viewer'
import { stripPhotos, stripScrollLeft } from './viewerStrip'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))

const photo = {
  id: 'photo-1',
  createdAt: '2030-01-01T00:00:00Z',
  takenAt: '2030-07-12T09:00:00Z',
  takenAtOffsetMinutes: 0,
  photoKind: 'picture',
  photoProcessingState: 'ready',
  originalFilename: 'one.jpg',
  smallPreviewUrl: '/small',
  largePreviewUrl: '/large',
  videoPreviewUrl: '',
  durationMS: 0,
  isFavorite: false,
  processingErrorMessage: '',
  placeName: '',
  cameraMake: '',
  cameraModel: '',
  widthPixels: 0,
  heightPixels: 0,
  isOwnedByCaller: true,
  ownerName: '',
  userId: 'person-1',
} as Photo

beforeEach(() => {
  // A wide window, where the details sit beside the picture.
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockResolvedValue({
    ReadPhoto: { photo, photoFiles: [], albums: [{ id: 'album-1', albumTitle: 'Picnic' }] },
  } as never)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const sleep = (milliseconds: number) => new Promise((resolve) => setTimeout(resolve, milliseconds))

const describedPhoto = {
  ...photo,
  photoDescription: 'A red kite over a field.',
  descriptionVisibleText: 'KITE FESTIVAL',
  describedAt: '2030-07-13T10:00:00Z',
  isDescriptionRequested: false,
}
const undescribedPhoto = {
  ...photo,
  photoDescription: '',
  descriptionVisibleText: '',
  describedAt: null,
  isDescriptionRequested: false,
}
const library = (overrides: Partial<PhotoLibrary> = {}) =>
  ({ isDescriptionAvailable: true, descriptionUnavailableMessage: '', ...overrides }) as PhotoLibrary
const serveRead = (read: Record<string, unknown>) =>
  vi.mocked(graphql).mockResolvedValue({
    ReadPhoto: { photoFiles: [], albums: [], faces: [], ...read },
  } as never)

const show = (onClose = vi.fn(), extra: Partial<React.ComponentProps<typeof PhotoViewer>> = {}) => {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <PhotoViewer
            photo={photo}
            previousPhoto={null}
            nextPhoto={null}
            isTrashed={false}
            isPaused={false}
            onNavigate={vi.fn()}
            onClose={onClose}
            onToggleFavorite={vi.fn()}
            onTrash={vi.fn()}
            onRestore={vi.fn()}
            onDeleteForever={vi.fn()}
            onAddToAlbum={vi.fn()}
            onNewAlbum={vi.fn()}
            {...extra}
          />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  return onClose
}

it('names the albums the photo is in, each a link to the album', async () => {
  show()
  const link = await screen.findByRole('link', { name: 'Picnic' })
  expect(link.getAttribute('href')).toBe('/photos/albums/album-1')
})

it('takes Escape before a listener that was on the page first, such as the agent drawer', async () => {
  let wasTakenWhenHeard: boolean | null = null
  // On the document before the viewer is there, as the drawer's is.
  const earlier = (event: KeyboardEvent) => {
    if (event.key === 'Escape') wasTakenWhenHeard = event.defaultPrevented
  }
  document.addEventListener('keydown', earlier)
  const onClose = show()
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Photo viewer' })).toBeTruthy())
  fireEvent.keyDown(document.body, { key: 'Escape' })
  document.removeEventListener('keydown', earlier)
  expect(onClose).toHaveBeenCalledTimes(1)
  expect(wasTakenWhenHeard).toBe(true)
})

it('leaves Escape to an open menu', async () => {
  const onClose = show()
  fireEvent.click(await screen.findByRole('button', { name: 'Add to album' }))
  await screen.findByRole('menu')
  fireEvent.keyDown(document.body, { key: 'Escape' })
  expect(onClose).not.toHaveBeenCalled()
})

it('shows the description and the words read in the picture, with the day it was written', async () => {
  serveRead({ photo: describedPhoto })
  show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
  expect(await screen.findByText('A red kite over a field.')).toBeTruthy()
  expect(screen.getByText('KITE FESTIVAL')).toBeTruthy()
  expect(screen.getByText('Described on July 13, 2030')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Describe again' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Describe this photo' })).toBeNull()
})

it('offers Describe for a photo nobody described, and asks for it', async () => {
  serveRead({ photo: undescribedPhoto })
  const onDescribe = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onDescribe })
  fireEvent.click(await screen.findByRole('button', { name: 'Describe this photo' }))
  await waitFor(() => expect(onDescribe).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }), false))
})

it('asks for a new description with Describe again', async () => {
  serveRead({ photo: describedPhoto })
  const onDescribe = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onDescribe })
  fireEvent.click(await screen.findByRole('button', { name: 'Describe again' }))
  await waitFor(() => expect(onDescribe).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }), true))
})

it("says why a photo cannot be described, in the server's words, and offers nothing to press", async () => {
  serveRead({ photo: undescribedPhoto })
  show(vi.fn(), {
    photoLibrary: library({ isDescriptionAvailable: false, descriptionUnavailableMessage: 'The assistant is off.' }),
    onDescribe: vi.fn(),
  })
  expect(await screen.findByText('The assistant is off.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Describe this photo' })).toBeNull()
})

it('reads the photo again every three seconds while a description is on its way', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  try {
    serveRead({ photo: { ...undescribedPhoto, isDescriptionRequested: true } })
    show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
    expect(await screen.findByText(/Describing… this can take a minute/)).toBeTruthy()
    const reads = () => vi.mocked(graphql).mock.calls.length
    const before = reads()
    await vi.advanceTimersByTimeAsync(3100)
    expect(reads()).toBe(before + 1)
    serveRead({ photo: describedPhoto })
    await vi.advanceTimersByTimeAsync(3100)
    expect(await screen.findByText('A red kite over a field.')).toBeTruthy()
    expect(screen.queryByText(/Describing… this can take a minute/)).toBeNull()
  } finally {
    vi.useRealTimers()
  }
})

it('keeps looking for a description after two minutes, less often, and says it is taking longer than usual', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  try {
    serveRead({ photo: { ...undescribedPhoto, isDescriptionRequested: true } })
    show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
    await screen.findByText(/Describing… this can take a minute/)
    await vi.advanceTimersByTimeAsync(2 * 60 * 1000 + 3100)
    expect(await screen.findByText(/taking longer than usual/)).toBeTruthy()
    const reads = vi.mocked(graphql).mock.calls.length
    await vi.advanceTimersByTimeAsync(16_000)
    expect(vi.mocked(graphql).mock.calls.length).toBeGreaterThan(reads)
    // It arrives without the page being read again by hand.
    serveRead({ photo: describedPhoto })
    await vi.advanceTimersByTimeAsync(16_000)
    expect(await screen.findByText('A red kite over a field.')).toBeTruthy()
  } finally {
    vi.useRealTimers()
  }
})

it('does not leave the waiting for a description on a photo it was not asked for', async () => {
  vi.mocked(graphql).mockImplementation((async (_document: string, variables?: Record<string, unknown>) => ({
    ReadPhoto: {
      photo: { ...undescribedPhoto, id: String(variables?.photoId) },
      photoFiles: [],
      albums: [],
      faces: [],
    },
  })) as never)
  let answerRequest: () => void = () => {}
  const onDescribe = vi.fn(() => new Promise<void>((resolve) => (answerRequest = resolve)))
  const tree = (shown: Photo) => (
    <TranslationProvider>
      <MemoryRouter>
        <PhotoViewer
          photo={shown}
          previousPhoto={null}
          nextPhoto={null}
          isTrashed={false}
          isPaused={false}
          onNavigate={vi.fn()}
          onClose={vi.fn()}
          onToggleFavorite={vi.fn()}
          onTrash={vi.fn()}
          onRestore={vi.fn()}
          onDeleteForever={vi.fn()}
          onAddToAlbum={vi.fn()}
          onNewAlbum={vi.fn()}
          photoLibrary={library()}
          onDescribe={onDescribe}
        />
      </MemoryRouter>
    </TranslationProvider>
  )
  const { rerender } = render(tree(photo))
  fireEvent.click(await screen.findByRole('button', { name: 'Describe this photo' }))
  expect(await screen.findByText(/Describing… this can take a minute/)).toBeTruthy()
  // The person moves on to the next photo while the request is on its way.
  rerender(tree({ ...photo, id: 'photo-2' }))
  expect(await screen.findByRole('button', { name: 'Describe this photo' })).toBeTruthy()
  expect(screen.queryByText(/Describing… this can take a minute/)).toBeNull()
  // The answer for the first photo comes back: the second is not made to wait for it.
  answerRequest()
  await waitFor(() => expect(onDescribe).toHaveBeenCalledTimes(1))
  await new Promise((resolve) => setTimeout(resolve, 20))
  expect(screen.queryByText(/Describing… this can take a minute/)).toBeNull()
  expect(screen.getByRole('button', { name: 'Describe this photo' })).toBeTruthy()
})

it('does not look for a description in a tab nobody is looking at, and looks when it is shown again', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  const visibility = vi.spyOn(document, 'visibilityState', 'get')
  try {
    serveRead({ photo: { ...undescribedPhoto, isDescriptionRequested: true } })
    show(vi.fn(), { photoLibrary: library(), onDescribe: vi.fn() })
    await screen.findByText(/Describing… this can take a minute/)
    visibility.mockReturnValue('hidden')
    const before = vi.mocked(graphql).mock.calls.length
    await vi.advanceTimersByTimeAsync(10_000)
    expect(vi.mocked(graphql).mock.calls.length).toBe(before)
    visibility.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await waitFor(() => expect(vi.mocked(graphql).mock.calls.length).toBeGreaterThan(before))
  } finally {
    visibility.mockRestore()
    vi.useRealTimers()
  }
})

it('opens the assistant on this photo', async () => {
  serveRead({ photo: describedPhoto })
  const onAskAssistant = vi.fn()
  show(vi.fn(), { photoLibrary: library(), onAskAssistant })
  fireEvent.click(await screen.findByRole('button', { name: 'Ask the assistant about this photo' }))
  expect(onAskAssistant).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }))
})

const face = (overrides: Record<string, unknown>) => ({
  id: 'face-1',
  photoId: 'photo-1',
  boxLeftFraction: 0.1,
  boxTopFraction: 0.1,
  boxWidthFraction: 0.2,
  boxHeightFraction: 0.2,
  photoFaceGroupId: 'group-1',
  contactId: 'contact-1',
  contactName: 'Sample Person',
  facePictureUrl: '/face-1',
  ...overrides,
})

it('lists the people in the photo, a named one a link to the person, and takes a face out of a group', async () => {
  serveRead({
    photo: describedPhoto,
    faces: [face({}), face({ id: 'face-2', photoFaceGroupId: 'group-2', contactName: '', contactId: '' })],
  })
  const onRemoveFaceFromGroup = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onRemoveFaceFromGroup })
  const link = await screen.findByRole('link', { name: 'Sample Person' })
  expect(link.getAttribute('href')).toBe('/photos/people/group-1')
  expect(screen.queryByRole('link', { name: 'Unnamed person' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Not Sample Person' }))
  await waitFor(() => expect(onRemoveFaceFromGroup).toHaveBeenCalledWith(expect.objectContaining({ id: 'face-1' })))
  expect(screen.getByRole('button', { name: 'Not this person' })).toBeTruthy()
})

// Naming an unnamed person happens over the photo: leaving for their page lost
// the photo, and their page is not in the People list yet.
it('names an unnamed person where the photo is, without closing it', async () => {
  serveRead({
    photo: describedPhoto,
    faces: [face({ id: 'face-2', photoFaceGroupId: 'group-2', contactName: '', contactId: '' })],
  })
  const onClose = vi.fn()
  show(onClose, { photoLibrary: library() })
  fireEvent.click(await screen.findByRole('button', { name: 'Unnamed person' }))
  expect(await screen.findByRole('dialog', { name: 'Name this person' })).toBeTruthy()
  expect(onClose).not.toHaveBeenCalled()
})

it('leaves out a face that is in no group, which has no person to link to', async () => {
  serveRead({ photo: describedPhoto, faces: [face({ photoFaceGroupId: '' })] })
  show(vi.fn(), { photoLibrary: library() })
  await screen.findByText('A red kite over a field.')
  expect(screen.queryByText('People')).toBeNull()
})

it('moves the focus into the viewer, to the button that closes it, and keeps Tab inside', async () => {
  const outside = document.createElement('button')
  outside.textContent = 'behind'
  document.body.appendChild(outside)
  outside.focus()
  show()
  const dialog = await screen.findByRole('dialog', { name: 'Photo viewer' })
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' }))
  const controls = Array.from(dialog.querySelectorAll<HTMLElement>('a[href], button:not([disabled])'))
  controls[controls.length - 1].focus()
  fireEvent.keyDown(document.activeElement!, { key: 'Tab' })
  expect(document.activeElement).toBe(controls[0])
  fireEvent.keyDown(document.activeElement!, { key: 'Tab', shiftKey: true })
  expect(document.activeElement).toBe(controls[controls.length - 1])
  // Focus that is somewhere behind the viewer comes back into it.
  outside.focus()
  fireEvent.keyDown(outside, { key: 'Tab' })
  expect(dialog.contains(document.activeElement)).toBe(true)
  outside.remove()
})

it('gives the focus back to what opened it when it closes', async () => {
  const opener = document.createElement('button')
  document.body.appendChild(opener)
  opener.focus()
  show()
  await screen.findByRole('dialog', { name: 'Photo viewer' })
  cleanup()
  expect(document.activeElement).toBe(opener)
  opener.remove()
})

it('turns to the next photo with the arrow keys, but not while the key goes to a field', async () => {
  const onNavigate = vi.fn()
  const next = { ...photo, id: 'photo-2' } as Photo
  show(vi.fn(), { nextPhoto: next, onNavigate })
  await screen.findByRole('dialog', { name: 'Photo viewer' })
  const fields = [
    document.createElement('textarea'),
    document.createElement('select'),
    document.createElement('input'),
    document.createElement('div'),
    document.createElement('div'),
  ]
  fields[3].setAttribute('contenteditable', 'true')
  fields[4].setAttribute('role', 'textbox')
  const inner = document.createElement('span')
  fields[3].appendChild(inner)
  for (const field of fields) document.body.appendChild(field)
  for (const field of [...fields, inner]) fireEvent.keyDown(field, { key: 'ArrowRight' })
  expect(onNavigate).not.toHaveBeenCalled()
  fireEvent.keyDown(document.body, { key: 'ArrowRight' })
  expect(onNavigate).toHaveBeenCalledWith(next)
  for (const field of fields) field.remove()
})

it('describes the picture to a screen reader: the first sentence of its description, else where and when', async () => {
  serveRead({ photo: { ...describedPhoto, photoDescription: 'A red kite over a field. It is windy.' } })
  show()
  const picture = () => document.querySelector<HTMLImageElement>('.photo-fitted')!
  expect(picture().alt).toBe('Photo taken on July 12, 2030')
  await waitFor(() => expect(picture().alt).toBe('A red kite over a field.'))
})

it('names the place in the alternative text when the picture has no description', async () => {
  serveRead({ photo: { ...undescribedPhoto, placeName: 'Fixtureville, Nowhere' } })
  show(vi.fn(), { photo: { ...photo, placeName: 'Fixtureville, Nowhere' } as Photo })
  expect(document.querySelector<HTMLImageElement>('.photo-fitted')!.alt).toBe(
    'Photo taken in Fixtureville, Nowhere on July 12, 2030',
  )
})

it('waits to trash or restore while the page is busy with it', async () => {
  show(vi.fn(), { isBusy: true })
  expect((await screen.findByRole('button', { name: 'Move to Trash' })).hasAttribute('disabled')).toBe(true)
  cleanup()
  show(vi.fn(), { isBusy: true, isTrashed: true })
  expect((await screen.findByRole('button', { name: 'Restore' })).hasAttribute('disabled')).toBe(true)
  expect(screen.getByRole('button', { name: 'Delete forever' }).hasAttribute('disabled')).toBe(true)
})

it('says that the details could not be read, and reads them again on request', async () => {
  vi.mocked(graphql).mockRejectedValueOnce(new Error('connection refused'))
  show()
  expect(await screen.findByText('The details of this photo could not be read.')).toBeTruthy()
  serveRead({ photo, albums: [{ id: 'album-1', albumTitle: 'Picnic' }] })
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(await screen.findByRole('link', { name: 'Picnic' })).toBeTruthy()
  expect(screen.queryByText('The details of this photo could not be read.')).toBeNull()
})

it('downloads the original with one click when it is the only file', async () => {
  serveRead({
    photo,
    photoFiles: [{ photoFileRole: 'original', contentType: 'image/jpeg', fileSizeBytes: 10, fileUrl: '/o' }],
  })
  show()
  const link = await screen.findByRole('link', { name: 'Download original' })
  expect(link.getAttribute('href')).toBe('/api/v1/photos/photo-1/files/original')
})

it('lists the edited version and the Live Photo video in the download menu when the photo has them', async () => {
  serveRead({
    photo,
    photoFiles: ['original', 'edited', 'liveVideo'].map((photoFileRole) => ({
      photoFileRole,
      contentType: 'image/jpeg',
      fileSizeBytes: 10,
      fileUrl: `/${photoFileRole}`,
    })),
  })
  show()
  fireEvent.click(await screen.findByRole('button', { name: 'Download' }))
  const items = await screen.findAllByRole('menuitem')
  expect(items.map((item) => [item.textContent, item.getAttribute('href')])).toEqual([
    ['Download original', '/api/v1/photos/photo-1/files/original'],
    ['Download edited version', '/api/v1/photos/photo-1/files/edited'],
    ['Download Live Photo video', '/api/v1/photos/photo-1/files/liveVideo'],
  ])
})

const failedPhoto = {
  ...photo,
  photoProcessingState: 'failed',
  processingErrorMessage: 'the file is cut short',
  smallPreviewUrl: '',
  largePreviewUrl: '',
} as Photo

// A photo that could not be read says why, and offers Try again beside the
// reason; the caller says how many it moved.
it('offers Try again beside the reason a photo failed', async () => {
  const onRetryProcessing = vi.fn(async () => {})
  show(vi.fn(), { photo: failedPhoto, onRetryProcessing })
  expect(await screen.findByText('This photo could not be read: the file is cut short')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(onRetryProcessing).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }))
})

it('offers no Try again on a failed photo in the Trash, or to a page that cannot retry', async () => {
  show(vi.fn(), { photo: failedPhoto, onRetryProcessing: vi.fn(), isTrashed: true })
  await screen.findByText('This photo could not be read: the file is cut short')
  expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull()
  cleanup()
  show(vi.fn(), { photo: failedPhoto })
  await screen.findByText('This photo could not be read: the file is cut short')
  expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull()
})

// A ready photo can have its previews made again from the menu; a photo that
// is waiting or failed has no such item, since nothing is ready to redo.
it('puts Make thumbnails again in the menu of a ready photo', async () => {
  const onRetryProcessing = vi.fn(async () => {})
  show(vi.fn(), { onRetryProcessing })
  fireEvent.click(await screen.findByRole('button', { name: 'More' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Make thumbnails again' }))
  expect(onRetryProcessing).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }))
  cleanup()
  show(vi.fn(), { photo: failedPhoto, onRetryProcessing })
  await screen.findByText('This photo could not be read: the file is cut short')
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
})

// ---- what the bar holds, and what it says of a video ----

it('keeps the cover and the take-out in More with the thumbnails, each with its own icon, and not on the bar', async () => {
  show(vi.fn(), {
    onSetCover: vi.fn(async () => {}),
    onRemoveFromAlbum: vi.fn(async () => {}),
    onRetryProcessing: vi.fn(),
  })
  await screen.findByRole('button', { name: 'More' })
  expect(screen.queryByRole('button', { name: 'Use as album cover' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Remove from album' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'More' }))
  const items = await screen.findAllByRole('menuitem')
  expect(items.map((item) => item.textContent)).toEqual([
    'Use as album cover',
    'Remove from album',
    'Make thumbnails again',
  ])
  const paths = items.map((item) => item.querySelector('svg')?.innerHTML)
  expect(new Set(paths).size).toBe(3)
})

it('has no More when there is nothing in it', async () => {
  show()
  await screen.findByRole('button', { name: 'Details' })
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
})

it('says video, not photo, for what can be done with a video', async () => {
  const video = { ...undescribedPhoto, photoKind: 'video' }
  serveRead({ photo: video })
  show(vi.fn(), { photo: video as Photo, photoLibrary: library(), onDescribe: vi.fn(), onAskAssistant: vi.fn() })
  expect(await screen.findByRole('button', { name: 'Describe this video' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Ask the assistant about this video' })).toBeTruthy()
  expect(screen.getByText(/looks at the video's poster/)).toBeTruthy()
})

it('shows no Description section to a reader who was given no description', async () => {
  const shared = {
    ...photo,
    isOwnedByCaller: false,
    ownerName: 'Ben',
    userId: 'person-2',
    originalFilename: '',
  } as Photo
  serveRead({
    photo: {
      ...shared,
      photoDescription: '',
      descriptionVisibleText: '',
      describedAt: '2030-07-13T10:00:00Z',
      isDescriptionRequested: false,
    },
  })
  show(vi.fn(), { photo: shared, photoLibrary: library() })
  await screen.findByText('Ben')
  expect(screen.queryByText('Description')).toBeNull()
  expect(screen.queryByText('File name')).toBeNull()
})

it('offers to ask the assistant about a photo shared with the person, with no Description section if there is none', async () => {
  const shared = { ...photo, isOwnedByCaller: false, ownerName: 'Ben', userId: 'person-2' } as Photo
  serveRead({ photo: { ...shared, photoDescription: '', descriptionVisibleText: '', describedAt: null } })
  const onAskAssistant = vi.fn()
  show(vi.fn(), { photo: shared, photoLibrary: library(), onAskAssistant })
  fireEvent.click(await screen.findByRole('button', { name: 'Ask the assistant about this photo' }))
  expect(onAskAssistant).toHaveBeenCalledWith(expect.objectContaining({ id: 'photo-1' }))
  expect(screen.queryByText('Description')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Describe this photo' })).toBeNull()
})

it('keeps the description of a shared photo above the way to ask about it', async () => {
  const shared = { ...photo, isOwnedByCaller: false, ownerName: 'Ben', userId: 'person-2' } as Photo
  serveRead({
    photo: {
      ...shared,
      photoDescription: 'A red kite.',
      descriptionVisibleText: '',
      describedAt: '2030-07-13T10:00:00Z',
    },
  })
  show(vi.fn(), { photo: shared, photoLibrary: library(), onAskAssistant: vi.fn() })
  expect(await screen.findByText('A red kite.')).toBeTruthy()
  expect(screen.getByText('Description')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Ask the assistant about this photo' })).toBeTruthy()
})

it('says how long a photo in the Trash has left', async () => {
  const trashed = { ...photo, trashedAt: new Date().toISOString() } as Photo
  serveRead({ photo: trashed })
  show(vi.fn(), {
    photo: trashed,
    isTrashed: true,
    photoLibrary: { ...library(), trashRetentionDays: 30 } as PhotoLibrary,
  })
  expect(await screen.findByText('Deleted forever in 30 days')).toBeTruthy()
})

// ---- the look: the title, the strip, the grouped actions, drag to close ----

const photoAt = (id: string, overrides: Partial<Photo> = {}) =>
  ({ ...photo, id, originalFilename: `${id}.jpg`, smallPreviewUrl: `/small-${id}`, ...overrides }) as Photo

it('names the day and the time at the top, on the clock where the photo was taken, not its file name', async () => {
  // 19:52 at +09:00 is 10:52 UTC.
  const taken = photoAt('photo-1', { takenAt: '2030-07-12T10:52:00Z', takenAtOffsetMinutes: 540 })
  serveRead({ photo: taken })
  show(vi.fn(), { photo: taken })
  const title = await screen.findByRole('heading', { level: 2, name: /July 12, 2030/ })
  expect(title.querySelector('.photo-viewer-date')?.textContent).toBe('July 12, 2030')
  expect(title.querySelector('.photo-viewer-time')?.textContent).toBe('7:52 PM')
  expect(title.querySelector('time')?.getAttribute('datetime')).toBe('2030-07-12T19:52')
  expect(title.textContent).not.toContain('photo-1.jpg')
})

it('puts the actions in three places: a copy at the left, a capsule in the middle, the Trash at the right', async () => {
  show()
  const capsule = await screen.findByRole('group', { name: 'Photo actions' })
  const names = (container: HTMLElement) =>
    within(container)
      .getAllByRole('button')
      .map((button) => button.getAttribute('aria-label'))
  expect(names(capsule)).toEqual(['Add to Favorites', 'Details', 'Add to album'])
  const bar = capsule.closest('.photo-viewer-actions') as HTMLElement
  expect(bar.querySelector('.photo-viewer-actions-start a')?.getAttribute('aria-label')).toBe('Download original')
  expect(names(bar.querySelector('.photo-viewer-actions-end') as HTMLElement)).toEqual(['Move to Trash'])
  // The top is the way back and what belongs to the picture.
  const top = document.querySelector('.photo-viewer-bar') as HTMLElement
  expect(names(top)).toEqual(['Close', 'Zoom in'])
})

it('puts Restore in the capsule and Delete forever at the right for a photo in the Trash', async () => {
  show(vi.fn(), { isTrashed: true })
  const capsule = await screen.findByRole('group', { name: 'Photo actions' })
  expect(
    within(capsule)
      .getAllByRole('button')
      .map((button) => button.getAttribute('aria-label')),
  ).toEqual(['Restore', 'Details'])
  const bar = capsule.closest('.photo-viewer-actions') as HTMLElement
  expect(
    within(bar.querySelector('.photo-viewer-actions-end') as HTMLElement).getByRole('button', {
      name: 'Delete forever',
    }),
  ).toBeTruthy()
  expect(screen.queryByRole('link', { name: 'Download original' })).toBeNull()
})

it('keeps the less used actions of an album under More at the top', async () => {
  show(vi.fn(), { onSetCover: vi.fn(), onRemoveFromAlbum: vi.fn() })
  const top = document.querySelector('.photo-viewer-bar') as HTMLElement
  fireEvent.click(await within(top).findByRole('button', { name: 'More' }))
  expect(await screen.findByRole('menuitem', { name: 'Use as album cover' })).toBeTruthy()
  expect(screen.getByRole('menuitem', { name: 'Remove from album' })).toBeTruthy()
})

it('offers Save to my library in the capsule for a photo that is somebody else’s, with nothing to trash', async () => {
  const shared = photoAt('photo-1', { isOwnedByCaller: false, ownerName: 'Ben', userId: 'person-2' })
  serveRead({ photo: shared })
  const onSaveToLibrary = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photo: shared, onSaveToLibrary })
  const capsule = await screen.findByRole('group', { name: 'Photo actions' })
  fireEvent.click(within(capsule).getByRole('button', { name: 'Save to my library' }))
  expect(onSaveToLibrary).toHaveBeenCalled()
  expect(screen.queryByRole('button', { name: 'Move to Trash' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Add to Favorites' })).toBeNull()
})

describe('the strip of neighbors', () => {
  const run = [photoAt('photo-0'), photoAt('photo-1'), photoAt('photo-2'), photoAt('photo-3')]

  it('shows a small picture for each, marks the one shown, and shows the one clicked', async () => {
    const onNavigate = vi.fn()
    show(vi.fn(), { photo: run[1], previousPhoto: run[0], nextPhoto: run[2], neighborPhotos: run, onNavigate })
    const strip = await screen.findByRole('group', { name: 'Photos next to this one' })
    const buttons = within(strip).getAllByRole('button')
    expect(buttons.map((button) => button.getAttribute('aria-label'))).toEqual([
      'Show photo-0.jpg',
      'Show photo-1.jpg',
      'Show photo-2.jpg',
      'Show photo-3.jpg',
    ])
    expect(buttons.map((button) => button.getAttribute('aria-current'))).toEqual([null, 'true', null, null])
    expect(buttons[3].querySelector('img')?.getAttribute('src')).toBe('/small-photo-3')
    fireEvent.click(buttons[3])
    expect(onNavigate).toHaveBeenCalledWith(run[3])
  })

  it('is one stop for the Tab key', async () => {
    show(vi.fn(), { photo: run[1], neighborPhotos: run })
    const strip = await screen.findByRole('group', { name: 'Photos next to this one' })
    expect(
      within(strip)
        .getAllByRole('button')
        .map((button) => button.tabIndex),
    ).toEqual([-1, 0, -1, -1])
  })

  it('holds the photo and the two beside it when it is given no list, and is left out for one photo', async () => {
    show(vi.fn(), { photo: run[1], previousPhoto: run[0], nextPhoto: run[2] })
    const strip = await screen.findByRole('group', { name: 'Photos next to this one' })
    expect(within(strip).getAllByRole('button')).toHaveLength(3)
    cleanup()
    show(vi.fn(), { photo: run[1] })
    await screen.findByRole('group', { name: 'Photo actions' })
    expect(screen.queryByRole('group', { name: 'Photos next to this one' })).toBeNull()
  })

  it('scrolls to keep the photo shown in the middle when the photo changes', async () => {
    const scrolled: number[] = []
    const original = HTMLElement.prototype.scrollTo
    HTMLElement.prototype.scrollTo = function (this: HTMLElement, options?: ScrollToOptions | number) {
      if (this.classList.contains('photo-viewer-strip') && typeof options === 'object')
        scrolled.push(options.left ?? -1)
    } as typeof HTMLElement.prototype.scrollTo
    try {
      const props = { onNavigate: vi.fn(), onClose: vi.fn() }
      const view = (current: Photo) => (
        <TranslationProvider>
          <MemoryRouter>
            <PhotoViewer
              photo={current}
              previousPhoto={null}
              nextPhoto={null}
              neighborPhotos={run}
              isTrashed={false}
              isPaused={false}
              onToggleFavorite={vi.fn()}
              onTrash={vi.fn()}
              onRestore={vi.fn()}
              onDeleteForever={vi.fn()}
              onAddToAlbum={vi.fn()}
              onNewAlbum={vi.fn()}
              {...props}
            />
          </MemoryRouter>
        </TranslationProvider>
      )
      const { rerender } = render(view(run[0]))
      await screen.findByRole('group', { name: 'Photos next to this one' })
      const afterOpening = scrolled.length
      expect(afterOpening).toBeGreaterThan(0)
      rerender(view(run[2]))
      expect(scrolled.length).toBeGreaterThan(afterOpening)
    } finally {
      HTMLElement.prototype.scrollTo = original
    }
  })

  it('works out the scroll that puts a picture in the middle, and never past either end', () => {
    // A 100px strip of 1000px: a 50px picture at 500 is centered at 475.
    expect(stripScrollLeft(500, 50, 100, 1000)).toBe(475)
    expect(stripScrollLeft(0, 50, 100, 1000)).toBe(0)
    expect(stripScrollLeft(990, 50, 100, 1000)).toBe(900)
    // A strip with room to spare does not scroll.
    expect(stripScrollLeft(40, 50, 400, 200)).toBe(0)
  })

  it('leaves the photo shown alone when the list does not hold it, and cuts a very long list around it', () => {
    const current = photoAt('photo-9')
    expect(stripPhotos(run, current, run[0], run[1]).map((each) => each.id)).toEqual(['photo-0', 'photo-9', 'photo-1'])
    const long = Array.from({ length: 1000 }, (_, index) => photoAt(`long-${index}`))
    const cut = stripPhotos(long, long[500], null, null)
    expect(cut).toHaveLength(400)
    expect(cut.some((each) => each.id === 'long-500')).toBe(true)
    const atEnd = stripPhotos(long, long[999], null, null)
    expect(atEnd[atEnd.length - 1].id).toBe('long-999')
    expect(stripPhotos(long, long[0], null, null)[0].id).toBe('long-0')
  })
})

describe('dragging down to close, on a touch screen', () => {
  // A wide window that does not ask for less motion, so that closing takes its
  // short time to leave.
  beforeEach(() => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: !query.includes('reduced-motion'),
      addEventListener() {},
      removeEventListener() {},
    }))
  })

  const stage = async () =>
    (await screen.findByRole('dialog', { name: 'Photo viewer' })).querySelector('.photo-viewer-stage') as HTMLElement
  const drag = (target: HTMLElement, downPixels: number, pointerType = 'touch') => {
    fireEvent.pointerDown(target, { pointerId: 1, pointerType, isPrimary: true, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(target, {
      pointerId: 1,
      pointerType,
      isPrimary: true,
      clientX: 100,
      clientY: 100 + downPixels / 2,
    })
    fireEvent.pointerMove(target, {
      pointerId: 1,
      pointerType,
      isPrimary: true,
      clientX: 100,
      clientY: 100 + downPixels,
    })
    fireEvent.pointerUp(target, { pointerId: 1, pointerType, isPrimary: true, clientX: 100, clientY: 100 + downPixels })
  }

  it('follows the finger, then closes once it lets go far enough down', async () => {
    const onClose = show()
    const target = await stage()
    fireEvent.pointerDown(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 104, clientY: 220 })
    const picture = target.querySelector('.photo-viewer-picture') as HTMLElement
    expect(picture.style.transform).toContain('translate(4px, 120px)')
    expect(picture.style.transform).toContain('scale(')
    expect(screen.getByRole('dialog', { name: 'Photo viewer' }).style.getPropertyValue('--photo-viewer-fade')).not.toBe(
      '1',
    )
    fireEvent.pointerUp(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 104, clientY: 220 })
    // It is on its way out, and the viewer closes when it has gone.
    expect(onClose).not.toHaveBeenCalled()
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it('springs back when it lets go too soon, and does not close', async () => {
    const onClose = show()
    const target = await stage()
    fireEvent.pointerDown(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 100, clientY: 125 })
    fireEvent.pointerUp(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 100, clientY: 125 })
    const picture = target.querySelector('.photo-viewer-picture') as HTMLElement
    expect(picture.style.transform).toContain('translate(0px, 0px)')
    await waitFor(() => expect(picture.style.transform).toBe(''))
    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not close for a mouse, which has Close and Escape', async () => {
    const onClose = show()
    drag(await stage(), 300, 'mouse')
    await sleep(250)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not close for a drag that is mostly sideways, which turns the page', async () => {
    const onClose = show()
    const target = await stage()
    fireEvent.pointerDown(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 100, clientY: 100 })
    fireEvent.pointerMove(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 260, clientY: 150 })
    fireEvent.pointerUp(target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientX: 260, clientY: 300 })
    await sleep(250)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not drag a photo that is zoomed in: there a drag pans', async () => {
    const onClose = show()
    fireEvent.click(await screen.findByRole('button', { name: 'Zoom in' }))
    const target = await stage()
    expect(target.classList.contains('is-draggable')).toBe(false)
    drag(target, 300)
    await sleep(250)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('does not take a touch that began on a control', async () => {
    const onClose = show(vi.fn(), { previousPhoto: photoAt('photo-0'), nextPhoto: photoAt('photo-2') })
    await stage()
    drag(screen.getByRole('button', { name: 'Next photo' }), 300)
    await sleep(250)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes at once for somebody who asks for less motion', async () => {
    vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
    const onClose = show()
    drag(await stage(), 300)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('can also be closed without the gesture, by the button and by Escape', async () => {
    const onClose = show()
    fireEvent.click(await screen.findByRole('button', { name: 'Close' }))
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(2)
  })
})
