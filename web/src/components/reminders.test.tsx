import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { RemindersView } from './reminders'
import { ToastProvider } from './toast'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

const reminder = (id: string, title: string, isDone: boolean) => ({
  id,
  title,
  notes: '',
  dueAt: null,
  isDueDate: false,
  isDone,
  doneAt: null,
  priority: 0,
  isRepeating: false,
})

function show(focusReminderId: string, onFocused = vi.fn()) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ReadRecordMemoryLinks')) return { ReadRecordMemoryLinks: [] } as never
    const everything = [
      reminder('open-1', 'Water the fixture plants', false),
      reminder('done-1', 'File the fixture form', true),
    ]
    const isDone = variables?.isDone
    return {
      ListReminders:
        isDone === null || isDone === undefined ? everything : everything.filter((each) => each.isDone === isDone),
    } as never
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <RemindersView focusReminderId={focusReminderId} onFocused={onFocused} />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  return { onFocused }
}

it('opens the reminder a link names, whether or not it is done, and tells the page when it has been shown and closed', async () => {
  const { onFocused } = show('done-1')
  const dialog = await screen.findByRole('dialog')
  expect((dialog.querySelector('input[type="text"]') as HTMLInputElement).value).toBe('File the fixture form')
  expect(onFocused).not.toHaveBeenCalled()
  screen.getByRole('button', { name: 'Cancel' }).click()
  await waitFor(() => expect(onFocused).toHaveBeenCalledTimes(1))
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('opens nothing for a link that names no reminder, and says so', async () => {
  show('missing')
  await waitFor(() => expect(screen.getByText('No longer available')).toBeTruthy())
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('asks In memory about the reminder it opened', async () => {
  show('open-1')
  await screen.findByRole('dialog')
  await waitFor(() =>
    expect(
      vi
        .mocked(graphql)
        .mock.calls.some(
          ([document, variables]) =>
            document.includes('ReadRecordMemoryLinks') &&
            (variables as { recordKind: string }).recordKind === 'reminder',
        ),
    ).toBe(true),
  )
})
