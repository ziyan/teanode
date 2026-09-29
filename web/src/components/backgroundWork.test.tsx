import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation } from '../api'
import { BackgroundWork, BackgroundWorkCard, isOpenable, isStoppable } from './backgroundWork'

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

it('stops what is queued or running, and opens what finished or failed', () => {
  expect(isStoppable(work('a', 'queued'))).toBe(true)
  expect(isStoppable(work('a', 'running'))).toBe(true)
  expect(isStoppable(work('a', 'done'))).toBe(false)
  expect(isOpenable(work('a', 'done'))).toBe(true)
  expect(isOpenable(work('a', 'failed'))).toBe(true)
  expect(isOpenable(work('a', 'stopped'))).toBe(false)
  expect(isOpenable(work('a', 'running'))).toBe(false)
})

it('draws no card when there is no work', async () => {
  execute.mockResolvedValueOnce({ ListAgentBackgroundWork: [] })
  const { container } = render(<BackgroundWorkCard />)
  await waitFor(() => expect(execute).toHaveBeenCalled())
  expect(container.innerHTML).toBe('')
})

it('stops running work with a toast', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StopAgentBackgroundWork'))
      return { StopAgentBackgroundWork: { id: 'running', workStatus: 'stopped' } }
    return { ListAgentBackgroundWork: [work('running', 'running')] }
  })
  render(<BackgroundWorkCard />)
  const stop = await screen.findByRole('button', { name: 'Survey: the orchard running: backgroundWork.stop' })
  fireEvent.click(stop)
  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('backgroundWork.stoppedOne'))
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('StopAgentBackgroundWork'), { id: 'running' })
})

it('opens what a finished survey reported, with its runs, whichever of them holds it', async () => {
  const finished = work('finished', 'done', ['page-run', 'combining-run'])
  execute.mockImplementation(async (document: string) => {
    if (document.includes('GetAgentBackgroundWork'))
      return { GetAgentBackgroundWork: { ...finished, resultText: '## Strengths\n\nThe orchard is pruned.' } }
    return { ListAgentBackgroundWork: [finished] }
  })
  render(<BackgroundWorkCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'Survey: the orchard finished: backgroundWork.open' }))
  expect(await screen.findByText('The orchard is pruned.')).toBeTruthy()
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('GetAgentBackgroundWork'), { id: 'finished' })
  expect(open).not.toHaveBeenCalled()

  const runs = screen.getAllByRole('button', { name: 'backgroundWork.run' })
  expect(runs).toHaveLength(2)
  fireEvent.click(runs[0])
  expect(open).toHaveBeenCalledWith('page-run')
  expect(screen.queryByText('The orchard is pruned.')).toBeNull()
})

it('opens why failed work failed', async () => {
  const failed = { ...work('failed', 'failed'), errorMessage: 'nothing in that scope has an overview yet' }
  execute.mockImplementation(async (document: string) => {
    if (document.includes('GetAgentBackgroundWork')) return { GetAgentBackgroundWork: { ...failed, resultText: '' } }
    return { ListAgentBackgroundWork: [failed] }
  })
  render(<BackgroundWorkCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'Survey: the orchard failed: backgroundWork.open' }))
  expect(await screen.findByText('backgroundWork.error')).toBeTruthy()
  expect(screen.getAllByText(/nothing in that scope has an overview yet/).length).toBeGreaterThan(1)
})

it('says once with a toast that the list cannot be read', async () => {
  execute.mockRejectedValue(new Error('the server is away'))
  const { container, rerender } = render(<BackgroundWorkCard />)
  await waitFor(() => expect(notices.failure).toHaveBeenCalledTimes(1))
  rerender(<BackgroundWorkCard />)
  expect(notices.failure).toHaveBeenCalledWith(expect.any(Error), 'backgroundWork.listFailed')
  expect(notices.failure).toHaveBeenCalledTimes(1)
  expect(container.innerHTML).toBe('')
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
