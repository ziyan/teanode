import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { TranslationProvider } from '../../i18n/i18n'
import { photoUploads } from '../../photos/uploadQueue'
import { UploadTray } from './uploadTray'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
// Files whose names start with "hang" never finish being read, and files whose
// names start with "bad" are refused by the server.
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
  vi.mocked(graphql).mockResolvedValue({ FindPhotos: [] } as never)
  photoUploads.setOwner(`owner-${Math.random()}`)
})
afterEach(cleanup)

const files = (prefix: string, count: number) =>
  Array.from({ length: count }, (_, index) => new File(['x'], `${prefix}-${index + 1}.jpg`, { type: 'image/jpeg' }))

it('draws a few lines of the files being sent and counts the rest, so thousands of files stay one small box', async () => {
  render(
    <TranslationProvider>
      <UploadTray />
    </TranslationProvider>,
  )
  photoUploads.add([...files('ok', 12), ...files('bad', 3)])
  await vi.waitFor(() => expect(photoUploads.isBusy).toBe(false))
  photoUploads.add(files('hang', 40))
  // Two are being read and thirty-eight wait.
  await screen.findByText('Uploading 15 of 55')
  const list = screen.getByRole('list')
  const rows = within(list).getAllByRole('listitem')
  // Six of the files being sent, three that failed, and the lines that count.
  expect(rows.filter((row) => row.className.includes('is-failed'))).toHaveLength(3)
  expect(rows.filter((row) => /is-(waiting|hashing|sending)/.test(row.className))).toHaveLength(6)
  expect(within(list).getByText('34 more waiting')).toBeTruthy()
  expect(within(list).getByText('12 finished')).toBeTruthy()
  expect(rows.length).toBeLessThan(15)
  // What failed says why.
  expect(within(list).getAllByText('The server refused it.')).toHaveLength(3)
})

it('stays open over a failure and says how many files failed, rather than only that the upload finished', async () => {
  render(
    <TranslationProvider>
      <UploadTray />
    </TranslationProvider>,
  )
  photoUploads.add([...files('ok', 1), ...files('bad', 2)])
  await screen.findByText('Upload finished, 2 failed')
  expect(within(screen.getByRole('list')).getAllByText('The server refused it.')).toHaveLength(2)
})

it('folds away when everything went through', async () => {
  render(
    <TranslationProvider>
      <UploadTray />
    </TranslationProvider>,
  )
  photoUploads.add(files('ok', 2))
  await screen.findByText('Upload finished')
  expect(screen.queryByRole('list')).toBeNull()
})
