import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { TranslationProvider } from '../../i18n/i18n'
import type { Photo, PhotoLibrary } from '../../photos/api'
import { PhotoViewer } from './viewer'

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

it('lists the people in the photo, each a link to the person, and takes a face out of a group', async () => {
  serveRead({
    photo: describedPhoto,
    faces: [face({}), face({ id: 'face-2', photoFaceGroupId: 'group-2', contactName: '', contactId: '' })],
  })
  const onRemoveFaceFromGroup = vi.fn().mockResolvedValue(undefined)
  show(vi.fn(), { photoLibrary: library(), onRemoveFaceFromGroup })
  const link = await screen.findByRole('link', { name: 'Sample Person' })
  expect(link.getAttribute('href')).toBe('/photos/people/group-1')
  expect(screen.getByRole('link', { name: 'Unnamed person' }).getAttribute('href')).toBe('/photos/people/group-2')
  fireEvent.click(screen.getByRole('button', { name: 'Not Sample Person' }))
  await waitFor(() => expect(onRemoveFaceFromGroup).toHaveBeenCalledWith(expect.objectContaining({ id: 'face-1' })))
  expect(screen.getByRole('button', { name: 'Not this person' })).toBeTruthy()
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
