import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { en } from '../i18n/en'
import type { Key } from '../i18n/i18n'
import { DocumentsDialog, appendNew, moreFoundInGraph } from './knowledge'

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

it('appends only the rows an earlier page did not already have', () => {
  expect(appendNew(['first', 'second'], ['second', 'third'], (row) => row)).toEqual(['first', 'second', 'third'])
})

it('reads the next passages with the offset the search returned and puts them under the first', async () => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListAgentKnowledgeSources')) return { ListAgentKnowledgeSources: [] }
    if (variables?.offset === 20)
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
  expect(screen.getByText('1')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'list.showMore' }))
  expect(await screen.findByText('Passage 1 of document02')).toBeTruthy()
  expect(screen.getByText('Passage 1 of document01')).toBeTruthy()
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('SearchAgentDocuments'), {
    query: 'quarterly plan',
    first: 20,
    offset: 20,
  })
  await waitFor(() => expect(screen.queryByRole('button', { name: 'list.showMore' })).toBeNull())
})
