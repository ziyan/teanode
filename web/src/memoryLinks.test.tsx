import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from './api'
import { TranslationProvider } from './i18n/i18n'
import {
  InMemory,
  canOpen,
  groupLinksByKind,
  pagesOf,
  recordLinkDate,
  referenceOf,
  type RecordLink,
} from './memoryLinks'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

function link(overrides: Partial<RecordLink> = {}): RecordLink {
  return {
    id: 'link-1',
    nodePath: 'people/fixture-guest',
    nodeName: 'Fixture Guest',
    nodeKind: 'person',
    recordKind: 'mail',
    recordId: 'item-1',
    recordRelation: 'wrote',
    linkStatus: 'stated',
    passageStartMS: 0,
    passageEndMS: 0,
    transcriptRunId: '',
    passageCount: 0,
    note: '',
    createdAt: '2026-01-01T00:00:00Z',
    recordTitle: 'Fixture subject',
    dashboardPath: '/mailbox/starred/item-1',
    isUnavailable: false,
    ...overrides,
  }
}

it('groups links by kind in the order a page lists them, and keeps a kind it does not know', () => {
  const groups = groupLinksByKind([
    link({ id: 'a', recordKind: 'recording_passage' }),
    link({ id: 'b', recordKind: 'mail' }),
    link({ id: 'c', recordKind: 'contact' }),
    link({ id: 'd', recordKind: 'mail' }),
    link({ id: 'e', recordKind: 'something_new' }),
  ])
  expect(groups.map((group) => [group.recordKind, group.links.map((each) => each.id)])).toEqual([
    ['contact', ['c']],
    ['mail', ['b', 'd']],
    ['recording_passage', ['a']],
    ['something_new', ['e']],
  ])
})

it('names a record as the command line does, with the range of a passage', () => {
  expect(referenceOf(link())).toBe('mail:item-1')
  expect(
    referenceOf(link({ recordKind: 'recording_passage', recordId: 'rec-1', passageStartMS: 1500, passageEndMS: 4000 })),
  ).toBe('recording_passage:rec-1@1500-4000')
})

it('opens only a record that is still there and has a place in the dashboard', () => {
  expect(canOpen(link())).toBe(true)
  expect(canOpen(link({ isUnavailable: true }))).toBe(false)
  expect(canOpen(link({ dashboardPath: '' }))).toBe(false)
  expect(canOpen(link({ dashboardPath: 'https://elsewhere.example.com/x' }))).toBe(false)
})

it('dates a link by the record, else by the link, else not at all', () => {
  expect(
    recordLinkDate(
      link({ recordHappenedAt: '2026-02-03T10:00:00Z', happenedAt: '2025-01-01T00:00:00Z' }),
    )?.getUTCFullYear(),
  ).toBe(2026)
  expect(recordLinkDate(link({ happenedAt: '2025-01-01T00:00:00Z' }))?.getUTCFullYear()).toBe(2025)
  expect(recordLinkDate(link())).toBeNull()
  expect(recordLinkDate(link({ recordHappenedAt: 'not a date' }))).toBeNull()
})

it('lists each page once, a statement winning over a guess', () => {
  const pages = pagesOf([
    link({ id: 'a', linkStatus: 'proposed' }),
    link({ id: 'b', linkStatus: 'stated', recordRelation: 'received' }),
    link({ id: 'c', nodePath: 'topics/fixture-topic', nodeName: 'Fixture Topic' }),
  ])
  expect(pages.map((each) => [each.nodePath, each.linkStatus])).toEqual([
    ['people/fixture-guest', 'stated'],
    ['topics/fixture-topic', 'stated'],
  ])
})

function inMemory() {
  render(
    <TranslationProvider>
      <MemoryRouter>
        <InMemory recordKind="mail" recordId="item-1" />
      </MemoryRouter>
    </TranslationProvider>,
  )
}

it('says In memory with the pages a record is linked to, a guess marked as one', async () => {
  vi.mocked(graphql).mockResolvedValue({
    ReadRecordMemoryLinks: [
      link(),
      link({
        id: 'two',
        nodePath: 'topics/fixture-topic',
        nodeName: 'Fixture Topic',
        linkStatus: 'proposed',
        recordRelation: 'about',
      }),
    ],
  } as never)
  inMemory()
  expect(await screen.findByRole('heading', { name: 'In memory' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Fixture Guest' }).getAttribute('href')).toBe(
    '/knowledge/people/fixture-guest',
  )
  expect(screen.getByRole('link', { name: 'Fixture Topic' }).getAttribute('title')).toBe("About, The agent's guess")
  expect(screen.getAllByText("The agent's guess")).toHaveLength(1)
  expect(vi.mocked(graphql).mock.calls[0][1]).toMatchObject({ recordKind: 'mail', recordId: 'item-1' })
})

it('draws nothing for a record no page is linked to', async () => {
  vi.mocked(graphql).mockResolvedValue({ ReadRecordMemoryLinks: [] } as never)
  inMemory()
  await vi.waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  expect(screen.queryByRole('heading', { name: 'In memory' })).toBeNull()
})

it('draws nothing when the question fails', async () => {
  vi.mocked(graphql).mockImplementation(() => Promise.reject(new Error('refused')))
  inMemory()
  await vi.waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalledTimes(1))
  expect(screen.queryByRole('heading', { name: 'In memory' })).toBeNull()
})
