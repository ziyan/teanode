import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { photoUploads } from '../../photos/uploadQueue'
import { PhotosLayout } from './layout'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../../session', () => ({ useSession: () => ({ userId: 'person-1' }) }))
// "hang" files are never read to the end, "bad" ones are refused, "again" ones
// are in the Trash already.
vi.mock('../../photos/uploader', () => ({
  isPhotoFile: () => true,
  sha256Of: vi.fn(
    (file: File, _onProgress: unknown, signal: AbortSignal) =>
      new Promise<string>((resolve, reject) => {
        if (!file.name.startsWith('hang')) return resolve(`digest-${file.name}`)
        signal.addEventListener('abort', () => reject(new DOMException('The upload was canceled.', 'AbortError')))
      }),
  ),
  uploadPhotoFile: vi.fn(async (file: File) => {
    if (file.name.startsWith('bad')) throw new Error('The server refused it.')
    return { photoId: `photo-${file.name}` }
  }),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('FindPhotos')) {
      const ids = (variables?.clientPhotoIds as string[]).filter((id) => id.startsWith('digest-again'))
      return {
        FindPhotos: ids.map((id) => ({ id: 'photo-trashed', clientPhotoId: id, trashedAt: '2030-07-14T00:00:00Z' })),
      } as never
    }
    if (document.includes('RestorePhotos')) return { RestorePhotos: 1 } as never
    throw new Error(`unexpected document ${document}`)
  })
})
afterEach(() => {
  cleanup()
  photoUploads.setOwner(`other-${Math.random()}`)
})

// The Albums page, which has nothing of its own for uploads.
const show = () =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/photos/albums']}>
          <Routes>
            <Route element={<PhotosLayout />}>
              <Route path="/photos/albums" element={<p>The albums page</p>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )

const file = (name: string) => new File(['x'], name, { type: 'image/jpeg' })

it('shows the tray and warns before the tab is left while files are on their way, on any page of Photos', async () => {
  show()
  expect(screen.getByText('The albums page')).toBeTruthy()
  expect(screen.queryByRole('region')).toBeNull()
  const leaving = () => {
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    return event.defaultPrevented
  }
  expect(leaving()).toBe(false)
  act(() => void photoUploads.add([file('hang-1.jpg')]))
  expect(await screen.findByRole('region', { name: 'Uploading 0 of 1' })).toBeTruthy()
  expect(leaving()).toBe(true)
  act(() => photoUploads.cancelAll())
  await waitFor(() => expect(leaving()).toBe(false))
})

it('says in a toast that a file failed, whichever page of Photos is open', async () => {
  show()
  act(() => void photoUploads.add([file('bad-1.jpg')]))
  expect(await screen.findByText('bad-1.jpg: The server refused it.')).toBeTruthy()
})

it('says that a file is in the Trash, not that it is in the library, and restores it from the toast', async () => {
  show()
  act(() => void photoUploads.add([file('again-1.jpg')]))
  expect(await screen.findByText('That photo is in the Trash.')).toBeTruthy()
  expect(screen.queryByText(/already in your library/)).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Restore' }))
  expect(await screen.findByText('Restored 1 photo.')).toBeTruthy()
  const restoreCall = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('RestorePhotos'))
  expect(restoreCall?.[1]).toEqual({ photoIds: ['photo-trashed'] })
})
