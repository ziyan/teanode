import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { recordingOperation } from './processing'
import { RecordingSourceSchedule } from './sourceSchedule'
vi.mock('./processing', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./processing')>()),
  recordingOperation: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})
it('sends an explicit empty schedule without changing source name, path or pause state', async () => {
  vi.mocked(recordingOperation).mockResolvedValue({})
  const reloadSource = vi.fn().mockResolvedValue(undefined)
  render(
    <TranslationProvider>
      <ToastProvider>
        <RecordingSourceSchedule
          sourceId="fixture-source"
          sourceCron="17 3 * * *"
          sourceTitle="Fixture recording"
          onSaved={reloadSource}
        />
      </ToastProvider>
    </TranslationProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Source reading schedule' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: '' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(reloadSource).toHaveBeenCalledOnce())
  expect(recordingOperation).toHaveBeenCalledExactlyOnceWith('SaveRecordingSourceSchedule', {
    sourceId: 'fixture-source',
    cron: '',
  })
  expect(screen.queryByRole('dialog')).toBeNull()
})
