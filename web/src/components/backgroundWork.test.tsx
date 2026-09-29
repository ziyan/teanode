import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation } from '../api'
import { BackgroundWork, BackgroundWorkCard, isStoppable, resultRunOf } from './backgroundWork'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn(), openAgentConversation: vi.fn(() => true) }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, language: 'en' }),
  Trans: ({ k }: { k: string }) => k,
}))
vi.mock('./relativeTime', () => ({ RelativeTime: () => null }))
vi.mock('./toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)
const open = vi.mocked(openAgentConversation)

beforeEach(() => {
  execute.mockReset()
  open.mockClear()
  notices.done.mockReset()
  notices.failure.mockReset()
})
afterEach(cleanup)

function work(identifier: string, workStatus: BackgroundWork['workStatus'], runIds: string[] = []): BackgroundWork {
  return {
    id: identifier,
    workKind: 'survey',
    workStatus,
    title: `Survey: the orchard ${identifier}`,
    errorMessage: '',
    runIds,
    createdAt: '2030-01-01T00:00:00Z',
    finishedAt: null,
  }
}

it('stops what is queued or running, and opens the run that combined a finished survey', () => {
  expect(isStoppable(work('a', 'queued'))).toBe(true)
  expect(isStoppable(work('a', 'running'))).toBe(true)
  expect(isStoppable(work('a', 'done'))).toBe(false)
  expect(resultRunOf(work('a', 'done', ['page-run', 'combining-run']))).toBe('combining-run')
  expect(resultRunOf(work('a', 'failed', ['page-run']))).toBeNull()
  expect(resultRunOf(work('a', 'done'))).toBeNull()
})

it('draws no card when there is no work', async () => {
  execute.mockResolvedValueOnce({ ListAgentBackgroundWork: [] })
  const { container } = render(<BackgroundWorkCard />)
  await waitFor(() => expect(execute).toHaveBeenCalled())
  expect(container.innerHTML).toBe('')
})

it('stops running work with a toast, and opens a finished one in the drawer', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StopAgentBackgroundWork'))
      return { StopAgentBackgroundWork: { id: 'running', workStatus: 'stopped' } }
    return {
      ListAgentBackgroundWork: [work('running', 'running'), work('finished', 'done', ['page-run', 'combining-run'])],
    }
  })
  render(<BackgroundWorkCard />)
  const stop = await screen.findByRole('button', { name: 'Survey: the orchard running: backgroundWork.stop' })
  fireEvent.click(stop)
  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('backgroundWork.stoppedOne'))
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('StopAgentBackgroundWork'), { id: 'running' })

  fireEvent.click(screen.getByRole('button', { name: 'Survey: the orchard finished: backgroundWork.open' }))
  expect(open).toHaveBeenCalledWith('combining-run')
})

it('says so with a toast when a stop fails', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StopAgentBackgroundWork')) throw new Error('the server is away')
    return { ListAgentBackgroundWork: [work('running', 'running')] }
  })
  render(<BackgroundWorkCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'Survey: the orchard running: backgroundWork.stop' }))
  await waitFor(() => expect(notices.failure).toHaveBeenCalled())
  expect(notices.done).not.toHaveBeenCalled()
})
