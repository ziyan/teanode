import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { en } from '../i18n/en'
import type { Key } from '../i18n/i18n'
import { DocumentsDialog, moreFoundInGraph, useSearchPages } from './knowledge'

vi.mock('../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, plural: (count: number) => String(count), language: 'en' }),
}))
const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../components/toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)

// The English catalogue, filled in, so that the line reads as it would.
const english = {
  t: (key: Key, values?: Record<string, unknown>) =>
    en[key].replace(/\{(\w+)\}/g, (_, name: string) => String(values?.[name] ?? '')),
  plural: (count: number, forms: { one: Key; other: Key }) =>
    en[count === 1 ? forms.one : forms.other].replace('{count}', String(count)),
}

function foundInGraph(overrides: Partial<Parameters<typeof moreFoundInGraph>[0]> = {}) {
  return {
    nodes: [],
    facts: [],
    moreNodeCount: 0,
    isMoreNodeCountLowerBound: false,
    moreFactCount: 0,
    isMoreFactCountLowerBound: false,
    nextOffset: 60,
    ...overrides,
  }
}

function passage(documentId: string, number: number) {
  return {
    documentId,
    externalId: `${documentId}.txt`,
    title: `Title of ${documentId}`,
    kind: 'file',
    author: '',
    sourceId: 'source01',
    source: 'Notes',
    happenedAt: null,
    number,
    text: `Passage ${number} of ${documentId}`,
  }
}

beforeEach(() => {
  execute.mockReset()
})
afterEach(cleanup)

it('says how many more pages and facts the search found, and where it stopped counting', () => {
  expect(
    moreFoundInGraph(foundInGraph({ moreNodeCount: 37, moreFactCount: 120, isMoreFactCountLowerBound: true }), english),
  ).toBe('37 more pages and at least 120 more facts')
  expect(moreFoundInGraph(foundInGraph({ moreNodeCount: 1 }), english)).toBe('1 more page')
  expect(moreFoundInGraph(foundInGraph({ moreFactCount: 60, isMoreFactCountLowerBound: true }), english)).toBe(
    'at least 60 more facts',
  )
})

it('reads the next passages by their offset in place of the first, and the first again', async () => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListAgentKnowledgeSources')) return { ListAgentKnowledgeSources: [] }
    if (variables?.offset === 20 && variables?.query === 'quarterly plan')
      return {
        SearchAgentDocuments: {
          passages: [passage('document02', 1)],
          definitions: [],
          meaningful: true,
          moreCount: 0,
          isMoreCountLowerBound: false,
          nextOffset: 0,
        },
      }
    return {
      SearchAgentDocuments: {
        passages: [passage('document01', 1)],
        definitions: [],
        meaningful: true,
        moreCount: 1,
        isMoreCountLowerBound: false,
        nextOffset: 20,
      },
    }
  })
  render(
    <MemoryRouter>
      <DocumentsDialog onClose={() => {}} />
    </MemoryRouter>,
  )
  fireEvent.change(screen.getByPlaceholderText('knowledge.documents.placeholder'), {
    target: { value: 'quarterly plan' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'knowledge.documents.search' }))
  expect(await screen.findByText('Passage 1 of document01')).toBeTruthy()
  // One passage shown and one more counted: the whole is two.
  expect(screen.getByText('table.range')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'table.previous' }) as HTMLButtonElement).disabled).toBe(true)

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('Passage 1 of document02')).toBeTruthy()
  expect(screen.queryByText('Passage 1 of document01')).toBeNull()
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('SearchAgentDocuments'), {
    query: 'quarterly plan',
    first: 20,
    offset: 20,
  })
  await waitFor(() =>
    expect((screen.getByRole('button', { name: 'table.next' }) as HTMLButtonElement).disabled).toBe(true),
  )

  fireEvent.click(screen.getByRole('button', { name: 'table.previous' }))
  expect(await screen.findByText('Passage 1 of document01')).toBeTruthy()
  expect(screen.queryByText('Passage 1 of document02')).toBeNull()

  // A new search starts again at its first page.
  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  await screen.findByText('Passage 1 of document02')
  fireEvent.change(screen.getByPlaceholderText('knowledge.documents.placeholder'), { target: { value: 'harbor' } })
  fireEvent.click(screen.getByRole('button', { name: 'knowledge.documents.search' }))
  expect(await screen.findByText('Passage 1 of document01')).toBeTruthy()
  expect(execute).toHaveBeenLastCalledWith(expect.stringContaining('SearchAgentDocuments'), { query: 'harbor', first: 20 })
  expect((screen.getByRole('button', { name: 'table.previous' }) as HTMLButtonElement).disabled).toBe(true)
})

// The search box reads a later page by its offset, in place of the one
// shown, and other words start again at the first page.
it('pages through the search and starts again for other words', async () => {
  execute.mockImplementation(async (_document: string, variables?: Record<string, unknown>) => ({
    SearchAgentGraph: foundInGraph({
      nodes: [{ id: `${variables?.query}-${variables?.offset ?? 0}` }] as never,
      nextOffset: Number(variables?.offset ?? 0) + 60,
    }),
  }))
  const { result, rerender } = renderHook(({ search }) => useSearchPages(search), { initialProps: { search: 'boat' } })
  await waitFor(() => expect(result.current.found?.nodes.map((node) => node.id)).toEqual(['boat-0']))
  expect(execute.mock.calls[0][1]).toEqual({ query: 'boat', first: 60 })

  act(() => result.current.setPageIndex(2))
  await waitFor(() => expect(result.current.found?.nodes.map((node) => node.id)).toEqual(['boat-120']))
  expect(result.current.pageIndex).toBe(2)
  expect(execute).toHaveBeenLastCalledWith(expect.any(String), { query: 'boat', first: 60, offset: 120 })

  rerender({ search: 'mooring' })
  expect(result.current.pageIndex).toBe(0)
  await waitFor(() => expect(result.current.found?.nodes.map((node) => node.id)).toEqual(['mooring-0']))
  expect(execute).toHaveBeenLastCalledWith(expect.any(String), { query: 'mooring', first: 60 })
})
