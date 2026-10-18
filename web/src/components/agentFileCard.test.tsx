import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { FileCard } from './agentDrawer'
import { ToastProvider } from './toast'

// The drawer asks the window about the screen when it loads, which jsdom
// does not answer.
vi.hoisted(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    }),
  })
})
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
// Whether Photos is on for the person and they may use it.
let hasPhotos = true
vi.mock('../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../session')>()),
  useSession: () => ({
    userId: 'person-1',
    services: { isPhotosEnabled: hasPhotos },
    permissions: { everywhere: ['photos:use'], byDomain: [] },
  }),
}))

beforeEach(() => {
  hasPhotos = true
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockResolvedValue({ SavePhotoFromAttachment: { id: 'photo-9' } } as never)
})
afterEach(cleanup)

const file = (overrides: Record<string, unknown> = {}) => ({
  attachment_id: 'attachment-1',
  name: 'IMG_0001.jpg',
  content_type: 'image/jpeg',
  size: 12,
  url: '/api/v1/agent/attachments/attachment-1',
  ...overrides,
})

const show = (shared: ReturnType<typeof file>) =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/agent']}>
          <Routes>
            <Route path="/agent" element={<FileCard file={shared} />} />
            <Route path="/photos/:photoId" element={<p>The photo page</p>} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )

it('keeps a picture of the conversation in the library, and the toast opens the photo', async () => {
  show(file())
  fireEvent.click(screen.getByRole('button', { name: 'Save to my library' }))
  await waitFor(() => expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({ agentAttachmentId: 'attachment-1' }))
  expect(vi.mocked(graphql).mock.calls[0][0]).toContain('SavePhotoFromAttachment(')
  // Kept, the card is about the photo: it links there and offers nothing more to save.
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Save to my library' })).toBeNull())
  expect(await screen.findByText('Saved to your library.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Open in Photos' }))
  expect(await screen.findByText('The photo page')).toBeTruthy()
})

it('offers it for a video too', () => {
  show(file({ name: 'clip.mp4', content_type: 'video/mp4' }))
  expect(screen.getByRole('button', { name: 'Save to my library' })).toBeTruthy()
})

it('offers nothing for a file that is not a picture or video, a photo already, or without Photos', () => {
  show(file({ name: 'notes.pdf', content_type: 'application/pdf' }))
  expect(screen.queryByRole('button', { name: 'Save to my library' })).toBeNull()
  cleanup()
  show(file({ photo_id: 'photo-1' }))
  expect(screen.queryByRole('button', { name: 'Save to my library' })).toBeNull()
  cleanup()
  hasPhotos = false
  show(file())
  expect(screen.queryByRole('button', { name: 'Save to my library' })).toBeNull()
})

it('says so when it could not be saved, and keeps the button', async () => {
  vi.mocked(graphql).mockRejectedValue(new Error('api: over quota'))
  show(file())
  fireEvent.click(screen.getByRole('button', { name: 'Save to my library' }))
  expect(await screen.findByText('api: over quota')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Save to my library' })).toBeTruthy()
})

it('opens a picture full size with a click on it, and not as a new page', async () => {
  show(file())
  const picture = await screen.findByRole('img', { name: 'IMG_0001.jpg' })
  fireEvent.click(picture)
  expect(await screen.findByRole('dialog')).toBeTruthy()
})

it('opens a photo of the library on its page with a click on the picture', async () => {
  show(file({ photo_id: 'photo-1' }))
  fireEvent.click(await screen.findByRole('link', { name: 'Open in Photos' }))
  expect(await screen.findByText('The photo page')).toBeTruthy()
})
