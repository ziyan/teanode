import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { APIError, graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { KnowledgePage } from './knowledge'
import { TranslationProvider } from '../i18n/i18n'
import {
  PageShares,
  SharePageDialog,
  SharedKnowledge,
  SharedWithYou,
  groupByOwner,
  isCovering,
  sharedAddress,
  type SharedPage,
  type SharedRoot,
} from './knowledgeShared'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

function root(overrides: Partial<SharedRoot> = {}): SharedRoot {
  return {
    ownerUserId: 'ada-id',
    ownerUsername: 'ada',
    ownerName: 'Ada Example',
    ownerAvatarUrl: null,
    ownerAvatarColorIndex: 3,
    path: 'people/a-grandparent',
    name: 'A Grandparent',
    nodeKind: 'person',
    opening: 'Lives by the sea. Keeps bees.',
    isQuoteShared: false,
    ...overrides,
  }
}

function sharedPage(overrides: Partial<SharedPage> = {}): SharedPage {
  return {
    ownerUserId: 'ada-id',
    ownerUsername: 'ada',
    ownerName: 'Ada Example',
    ownerAvatarUrl: null,
    ownerAvatarColorIndex: 3,
    path: 'people/a-grandparent',
    name: 'A Grandparent',
    nodeKind: 'person',
    aliases: [],
    opening: 'Lives by the sea.',
    isQuoteShared: false,
    facts: [
      {
        number: 3,
        factKind: 'fact',
        text: 'Next appointment is Tuesday at three.',
        isInferred: false,
        evidenceSource: "from Ada Example's mail",
        evidenceQuote: '',
      },
    ],
    edges: [],
    children: [
      { path: 'people/a-grandparent/doctor', name: 'The doctor', nodeKind: 'person', opening: 'Sees them in spring' },
    ],
    ...overrides,
  }
}

function inApp(element: React.ReactElement, at = '/knowledge') {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[at]}>{element}</MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('opens a shared page by an address that names the person who shared it', () => {
  expect(sharedAddress('people/a-grandparent', 'ada')).toBe('/knowledge/people/a-grandparent?owner=ada')
  expect(sharedAddress('people/zoë', 'a b')).toBe('/knowledge/people/zo%C3%AB?owner=a%20b')
})

it('reaches a page from the share of the page itself or of one above it, at a slash', () => {
  expect(isCovering('people/a', 'people/a')).toBe(true)
  expect(isCovering('people/a', 'people/a/x')).toBe(true)
  expect(isCovering('people/a', 'people/ab')).toBe(false)
  expect(isCovering('people/a/x', 'people/a')).toBe(false)
})

it('groups the shared pages under whoever shared them, in the order given', () => {
  const groups = groupByOwner([
    root(),
    root({ path: 'projects/garden', name: 'The garden' }),
    root({ ownerUserId: 'ben-id', ownerUsername: 'ben', ownerName: 'Ben Example' }),
  ])
  expect(groups.map((group) => [group.ownerName, group.roots.map((each) => each.path)])).toEqual([
    ['Ada Example', ['people/a-grandparent', 'projects/garden']],
    ['Ben Example', ['people/a-grandparent']],
  ])
})

it('lists what is shared in the sidebar under each person, each row opening their page', async () => {
  vi.mocked(graphql).mockResolvedValue({
    ListSharedAgentPages: [
      root(),
      root({
        ownerUserId: 'ben-id',
        ownerUsername: 'ben',
        ownerName: 'Ben Example',
        path: 'projects/boat',
        name: 'The boat',
      }),
    ],
  })
  inApp(<SharedWithYou />)
  const section = await screen.findByRole('region', { name: 'Shared with you' })
  expect(within(section).getByText('Ada Example')).toBeTruthy()
  expect(within(section).getByText('Ben Example')).toBeTruthy()
  const link = within(section).getByRole('link', { name: /A Grandparent/ })
  expect(link.getAttribute('href')).toBe('/knowledge/people/a-grandparent?owner=ada')
  expect(
    within(section)
      .getByRole('link', { name: /The boat/ })
      .getAttribute('href'),
  ).toBe('/knowledge/projects/boat?owner=ben')
})

it('searches what is shared, and lists the pages and facts found with whose they are', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('SearchSharedAgentPages')) {
      return {
        SearchSharedAgentPages: {
          hasMoreHits: true,
          hits: [
            {
              ownerUserId: 'ada-id',
              ownerUsername: 'ada',
              ownerName: 'Ada Example',
              path: 'people/a-grandparent',
              name: 'A Grandparent',
              opening: 'Lives by the sea.',
              factNumber: 0,
              text: '',
            },
            {
              ownerUserId: 'ada-id',
              ownerUsername: 'ada',
              ownerName: 'Ada Example',
              path: 'people/a-grandparent',
              name: 'A Grandparent',
              opening: '',
              factNumber: 3,
              text: 'Next appointment is Tuesday at three.',
            },
          ],
        },
      }
    }
    return { ListSharedAgentPages: [root()] }
  })
  inApp(<SharedWithYou />)
  const section = await screen.findByRole('region', { name: 'Shared with you' })
  fireEvent.change(within(section).getByRole('searchbox', { name: 'Search what is shared with you' }), {
    target: { value: 'appointment' },
  })
  const fact = await within(section).findByRole('link', { name: /A Grandparent #3/ })
  expect(fact.getAttribute('href')).toBe('/knowledge/people/a-grandparent?owner=ada')
  expect(within(fact).getByText(/Shared by Ada Example: Next appointment is Tuesday at three\./)).toBeTruthy()
  expect(within(section).getByText(/Shared by Ada Example: Lives by the sea\./)).toBeTruthy()
  // The search left hits out, and says so.
  expect(within(section).getByText('There may be more; use more words.')).toBeTruthy()
  const searched = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SearchSharedAgentPages'))
  expect(searched?.[1]).toEqual({ query: 'appointment' })

  // Words that match nothing say so, and clearing them brings the people back.
  vi.mocked(graphql).mockImplementation(async (document: string) =>
    document.includes('SearchSharedAgentPages')
      ? { SearchSharedAgentPages: { hits: [], hasMoreHits: false } }
      : { ListSharedAgentPages: [root()] },
  )
  fireEvent.change(within(section).getByRole('searchbox'), { target: { value: 'nothing like it' } })
  expect(await within(section).findByText('Nothing shared with you has those words.')).toBeTruthy()
  fireEvent.change(within(section).getByRole('searchbox'), { target: { value: '' } })
  expect(await within(section).findByText('Ada Example')).toBeTruthy()
})

it('draws nothing when nothing is shared, and nothing when it cannot ask', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListSharedAgentPages: [] })
  const { container, unmount } = inApp(<SharedWithYou />)
  await waitFor(() => expect(graphql).toHaveBeenCalled())
  expect(container.querySelector('.knowledge-shared')).toBeNull()
  unmount()

  vi.mocked(graphql).mockRejectedValue(new Error('not found'))
  const failing = inApp(<SharedWithYou />)
  await waitFor(() => expect(graphql).toHaveBeenCalledTimes(2))
  expect(failing.container.querySelector('.knowledge-shared')).toBeNull()
})

// A person whose agent is off has no pages of their own: the page is only what
// is shared, says so in a line, opens the first shared page and never asks for
// pages of their own, which would only fail.
it('shows a person whose agent is off only what is shared, with the first page open', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ReadAgent')) return { ReadAgent: { agent: { id: 'agent-id', enabled: false } } }
    if (document.includes('ReadSharedAgentPage')) return { ReadSharedAgentPage: sharedPage() }
    if (document.includes('ListSharedAgentPages')) return { ListSharedAgentPages: [root()] }
    throw new Error(`asked for ${document}`)
  })
  inApp(
    <Routes>
      <Route path="/knowledge/*" element={<KnowledgePage />} />
    </Routes>,
  )
  expect(
    await screen.findByText(
      'Your agent is off, so you have no pages of your own. What others share with you is below.',
    ),
  ).toBeTruthy()
  expect(await screen.findByText('#3 Next appointment is Tuesday at three.')).toBeTruthy()
  expect(screen.queryByText('Recall')).toBeNull()
  expect(screen.queryByPlaceholderText('Search the pages')).toBeNull()
  const asked = vi.mocked(graphql).mock.calls.map(([document]) => document)
  expect(asked.some((document) => document.includes('AgentGraphPage'))).toBe(false)
  // The heading of the list is said once: the line above it says the rest.
  expect(screen.getAllByText('Shared with you').length).toBeGreaterThan(0)
})

it('says nothing is shared yet to a person whose agent is off and who is shared nothing', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ReadAgent')) return { ReadAgent: { agent: null } }
    return { ListSharedAgentPages: [] }
  })
  inApp(
    <Routes>
      <Route path="/knowledge/*" element={<KnowledgePage />} />
    </Routes>,
  )
  expect(await screen.findByText('Nothing is shared with you yet.')).toBeTruthy()
})

function SharedRoute() {
  return (
    <Routes>
      <Route path="/knowledge/*" element={<SharedKnowledge owner="ada" />} />
    </Routes>
  )
}

it('reads a shared page with its owner and no way to change it', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ReadSharedAgentPage')) return { ReadSharedAgentPage: sharedPage() }
    return { ListSharedAgentPages: [root()] }
  })
  inApp(<SharedRoute />, '/knowledge/people/a-grandparent?owner=ada')

  expect(await screen.findByText('#3 Next appointment is Tuesday at three.')).toBeTruthy()
  expect(screen.getByText("from Ada Example's mail")).toBeTruthy()
  expect(screen.getByText('Read only')).toBeTruthy()
  const badge = screen.getByTestId('owner-badge')
  expect(within(badge).getByText('Shared by Ada Example')).toBeTruthy()
  expect(screen.queryByText(/Quotes shown/)).toBeNull()

  // The page was asked for by its owner's identifier, not by the name in the address.
  const read = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ReadSharedAgentPage'))
  expect(read?.[1]).toEqual({ ownerUserId: 'ada-id', path: 'people/a-grandparent' })

  // Nothing here writes: no edit, move, merge, pin or forget, and no fact to change.
  for (const control of screen.queryAllByRole('button')) {
    expect(control.getAttribute('aria-label') ?? '').not.toMatch(/Edit|Move|Merge|Pin|Forget|Strike|Add/i)
  }
  expect(screen.queryByText('Add a fact')).toBeNull()
  // The page under it opens as theirs too.
  expect(screen.getByRole('link', { name: 'The doctor' }).getAttribute('href')).toBe(
    '/knowledge/people/a-grandparent/doctor?owner=ada',
  )
})

it('shows a quote only when the owner shared the quotes', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ReadSharedAgentPage'))
      return {
        ReadSharedAgentPage: sharedPage({
          isQuoteShared: true,
          facts: [
            {
              number: 3,
              factKind: 'fact',
              text: 'Next appointment is Tuesday at three.',
              isInferred: false,
              evidenceSource: "from Ada Example's mail",
              evidenceQuote: 'see you Tuesday at three',
            },
          ],
        }),
      }
    return { ListSharedAgentPages: [root({ isQuoteShared: true })] }
  })
  inApp(<SharedRoute />, '/knowledge/people/a-grandparent?owner=ada')
  expect(await screen.findByText('see you Tuesday at three')).toBeTruthy()
  expect(screen.getByText('Quotes shown')).toBeTruthy()
})

it('says so when the page is not shared with the person, or the owner shared nothing', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ReadSharedAgentPage')) throw new APIError('api: not found')
    return { ListSharedAgentPages: [root()] }
  })
  inApp(<SharedRoute />, '/knowledge/people/a-grandparent-private?owner=ada')
  expect(await screen.findByText('This page is not shared with you, or the person stopped sharing it.')).toBeTruthy()
  cleanup()

  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockResolvedValue({ ListSharedAgentPages: [] })
  inApp(<SharedRoute />, '/knowledge/people/a-grandparent?owner=nobody')
  expect(await screen.findByText('This page is not shared with you, or the person stopped sharing it.')).toBeTruthy()
})

const people = [{ userId: 'ben-id', username: 'ben', name: 'Ben Example', avatarUrl: null, avatarColorIndex: 1 }]
const circles = [
  { id: 'home-id', name: 'Home', members: [{ userId: 'a' }, { userId: 'b' }, { userId: 'c' }, { userId: 'd' }] },
  { id: 'solo-id', name: 'Home', members: [{ userId: 'a' }] },
]

function preview(isQuoteShared: boolean) {
  return {
    PreviewAgentPageShare: {
      page: sharedPage({
        isQuoteShared,
        facts: [
          {
            number: 3,
            factKind: 'fact',
            text: 'Next appointment is Tuesday at three.',
            isInferred: false,
            evidenceSource: "from Ada Example's mail",
            evidenceQuote: isQuoteShared ? 'see you Tuesday at three' : '',
          },
        ],
      }),
      reachedPageCount: 2,
      isReachedPageCountCut: false,
      reachedPagePaths: ['people/a-grandparent'],
      withheldFactCount: 3,
    },
  }
}

function sharingServer() {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: people }
    if (document.includes('ListCircles')) return { ListCircles: circles }
    if (document.includes('PreviewAgentPageShare')) return preview(variables?.isQuoteShared === true)
    if (document.includes('ShareAgentPage')) return { ShareAgentPage: { id: 'share-1' } }
    return {}
  })
}

it('shows what they will see before the page is shared, with the quotes only when asked', async () => {
  sharingServer()
  const onChanged = vi.fn()
  const onClose = vi.fn()
  inApp(<SharePageDialog path="people/a-grandparent" name="A Grandparent" onClose={onClose} onChanged={onChanged} />)

  const dialog = await screen.findByRole('dialog', { name: 'Share A Grandparent' })
  const share = within(dialog).getByRole('button', { name: 'Share' }) as HTMLButtonElement
  // Nothing to confirm until somebody is chosen, and a preview has arrived.
  expect(share.disabled).toBe(true)
  await waitFor(() => expect(within(dialog).getByRole('option', { name: 'Ben Example (ben)' })).toBeTruthy())

  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'user:ben-id' } })
  expect(await within(dialog).findByText('What Ben Example will see')).toBeTruthy()
  expect(await within(dialog).findByText('#3 Next appointment is Tuesday at three.')).toBeTruthy()
  expect(
    within(dialog).getByText(
      'This shares 2 pages: this one and everything under it. A page you add under it later is shared too.',
    ),
  ).toBeTruthy()
  // The pages it reaches, the rest counted, and the facts it keeps back.
  expect(within(dialog).getByText('people/a-grandparent')).toBeTruthy()
  expect(within(dialog).getByText('and 1 more')).toBeTruthy()
  expect(
    within(dialog).getByText(
      '3 facts from finance, recordings or location stay private. Those have their own sharing.',
    ),
  ).toBeTruthy()
  expect(within(dialog).queryByText('see you Tuesday at three')).toBeNull()

  fireEvent.click(within(dialog).getByRole('checkbox'))
  expect(await within(dialog).findByText('see you Tuesday at three')).toBeTruthy()
  const previews = vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('PreviewAgentPageShare'))
  expect(previews[previews.length - 1]?.[1]).toEqual({
    path: 'people/a-grandparent',
    granteeUserId: 'ben-id',
    granteeCircleId: null,
    isQuoteShared: true,
  })
  // Nothing was shared by looking.
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('mutation'))).toBe(false)

  await waitFor(() =>
    expect((within(dialog).getByRole('button', { name: 'Share' }) as HTMLButtonElement).disabled).toBe(false),
  )
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(onChanged).toHaveBeenCalled())
  const shared = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('mutation'))
  expect(shared?.[1]).toEqual({
    path: 'people/a-grandparent',
    granteeUserId: 'ben-id',
    granteeCircleId: null,
    isQuoteShared: true,
  })
  expect(onClose).toHaveBeenCalled()
})

it('says one thing about what is shared in the dialog, under the box that chooses the quotes', async () => {
  sharingServer()
  inApp(<SharePageDialog path="people/a-grandparent" name="A Grandparent" onClose={vi.fn()} onChanged={vi.fn()} />)
  const dialog = await screen.findByRole('dialog', { name: 'Share A Grandparent' })
  await waitFor(() => expect(within(dialog).getByRole('option', { name: 'Ben Example (ben)' })).toBeTruthy())
  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'user:ben-id' } })
  await within(dialog).findByText('What Ben Example will see')
  // The hint on the quotes stays; the explanations of what a reader is shown do not repeat it.
  expect(within(dialog).getByText(/Those words are quoted from your mail and documents/)).toBeTruthy()
  expect(within(dialog).queryByText(/They read this page, and every page under it/)).toBeNull()
  expect(within(dialog).queryByText(/What the page says, as its owner's agent last wrote it/)).toBeNull()
  expect(within(dialog).queryByText(/longer overview of this page is not shared/)).toBeNull()
})

it('shares with a circle by its identifier', async () => {
  sharingServer()
  inApp(
    <SharePageDialog
      path="people/a-grandparent"
      name="A Grandparent"
      onClose={() => undefined}
      onChanged={() => undefined}
    />,
  )
  const dialog = await screen.findByRole('dialog', { name: 'Share A Grandparent' })
  await waitFor(() => expect(within(dialog).getByRole('option', { name: 'Everyone in Home (4 people)' })).toBeTruthy())
  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'circle:home-id' } })
  await within(dialog).findByText('What Everyone in Home will see')
  const previews = vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('PreviewAgentPageShare'))
  expect(previews[previews.length - 1]?.[1]).toMatchObject({
    granteeUserId: null,
    granteeCircleId: 'home-id',
    isQuoteShared: false,
  })
})

it('sends a person with nobody to share with to Circles', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [] }
    if (document.includes('ListCircles')) return { ListCircles: [] }
    return {}
  })
  inApp(
    <SharePageDialog
      path="people/a-grandparent"
      name="A Grandparent"
      onClose={() => undefined}
      onChanged={() => undefined}
    />,
  )
  const dialog = await screen.findByRole('dialog', { name: 'Share A Grandparent' })
  expect(await within(dialog).findByText(/You do not share a circle with anybody yet/)).toBeTruthy()
  expect(within(dialog).getByRole('link', { name: 'Open Circles' }).getAttribute('href')).toBe('/circles')
})

it('says a share that did not go through once, with what to do', async () => {
  sharingServer()
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('mutation')) throw new TypeError('Failed to fetch')
    return answer(document, variables)
  })
  inApp(
    <SharePageDialog
      path="people/a-grandparent"
      name="A Grandparent"
      onClose={() => undefined}
      onChanged={() => undefined}
    />,
  )
  const dialog = await screen.findByRole('dialog', { name: 'Share A Grandparent' })
  await waitFor(() => expect(within(dialog).getByRole('option', { name: 'Ben Example (ben)' })).toBeTruthy())
  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'user:ben-id' } })
  await within(dialog).findByText('What Ben Example will see')
  await waitFor(() =>
    expect((within(dialog).getByRole('button', { name: 'Share' }) as HTMLButtonElement).disabled).toBe(false),
  )
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(screen.getAllByText('Sharing was not changed. Check your connection and try again.')).toHaveLength(1),
  )
})

it('lists who a page is shared with, including by a page above it, and stops one', async () => {
  const listed = [
    {
      id: 'direct',
      path: 'people/a',
      pageName: 'A',
      granteeUserId: 'ben-id',
      granteeUsername: 'ben',
      granteeName: 'Ben Example',
      granteeCircleId: null,
      granteeCircleName: '',
      isQuoteShared: true,
    },
    {
      id: 'above',
      path: 'people',
      pageName: 'People',
      granteeUserId: null,
      granteeUsername: '',
      granteeName: '',
      granteeCircleId: 'home-id',
      granteeCircleName: 'Home',
      isQuoteShared: false,
    },
    {
      id: 'beside',
      path: 'people/ab',
      pageName: 'AB',
      granteeUserId: 'cora-id',
      granteeUsername: 'cora',
      granteeName: 'Cora Example',
      granteeCircleId: null,
      granteeCircleName: '',
      isQuoteShared: false,
    },
  ]
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('UnshareAgentPage')) return { UnshareAgentPage: true }
    return { ListAgentPageShares: listed }
  })
  inApp(<PageShares path="people/a" refreshKey={0} />)

  expect(await screen.findByText('Ben Example')).toBeTruthy()
  expect(screen.getByText('Everyone in Home')).toBeTruthy()
  expect(screen.getByText('As part of people')).toBeTruthy()
  // A page whose name only starts the same way is not this page.
  expect(screen.queryByText('Cora Example')).toBeNull()

  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing with Ben Example' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(graphql)
        .mock.calls.some(
          ([document, variables]) => document.includes('UnshareAgentPage') && variables?.shareId === 'direct',
        ),
    ).toBe(true),
  )
})

it('draws a share with the person\'s own picture and color, and a circle with a mark in the same place', async () => {
  vi.mocked(graphql).mockResolvedValue({
    ListAgentPageShares: [
      {
        id: 'direct',
        path: 'people/a',
        pageName: 'A',
        granteeUserId: 'ben-id',
        granteeUsername: 'ben',
        granteeName: 'Ben Example',
        granteeAvatarUrl: '/api/v1/avatar/user/ben-id?v=1',
        granteeAvatarColorIndex: 7,
        granteeCircleId: null,
        granteeCircleName: '',
        isQuoteShared: false,
      },
      {
        id: 'plain',
        path: 'people/a',
        pageName: 'A',
        granteeUserId: 'cora-id',
        granteeUsername: 'cora',
        granteeName: 'Cora Example',
        granteeAvatarUrl: null,
        granteeAvatarColorIndex: 5,
        granteeCircleId: null,
        granteeCircleName: '',
        isQuoteShared: false,
      },
      {
        id: 'circle',
        path: 'people/a',
        pageName: 'A',
        granteeUserId: null,
        granteeUsername: '',
        granteeName: '',
        granteeAvatarUrl: null,
        granteeAvatarColorIndex: 0,
        granteeCircleId: 'home-id',
        granteeCircleName: 'Home',
        isQuoteShared: false,
      },
    ],
  })
  const { container } = inApp(<PageShares path="people/a" refreshKey={0} />)
  await screen.findByText('Ben Example')
  const rows = Array.from(container.querySelectorAll('.settings-row'))
  expect(rows).toHaveLength(3)
  // The picture, where the person has one, as the rest of the dashboard shows it.
  expect(rows[0].querySelector('img.avatar-picture')?.getAttribute('src')).toContain('/api/v1/avatar/user/ben-id')
  // The color the server chose, not one made from the name.
  expect(rows[1].querySelector('.avatar')?.className).toContain('avatar-color-5')
  // A circle has a mark where the others have a face, so the names line up.
  expect(rows[2].querySelector('.avatar')).toBeNull()
  expect(rows[2].querySelector('.settings-row-mark')).not.toBeNull()
})

it('draws nothing for a page that is shared with nobody', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListAgentPageShares: [] })
  const { container } = inApp(<PageShares path="people/a" refreshKey={0} />)
  await waitFor(() => expect(graphql).toHaveBeenCalled())
  expect(container.querySelector('.settings-section')).toBeNull()
})
