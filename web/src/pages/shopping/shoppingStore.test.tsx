import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../../api'
import type { ShoppingTrip } from './shoppingApi'
import { callsOf, lastOf, renderShopping, shoppingItem, shoppingList, shoppingTrip } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const groceries = shoppingList()
const party = shoppingList({ id: 'list-2', listName: 'Party', neededCount: 1 })
const peas = shoppingItem()
const crackers = shoppingItem({
  id: 'item-5',
  shoppingListId: 'list-2',
  listName: 'Party',
  itemDescription: 'Crackers',
  aisleName: 'Snacks',
  aisleRank: 1,
  storeName: '',
  storeKey: '',
})
const ice = shoppingItem({
  id: 'item-6',
  itemDescription: 'Ice',
  aisleName: 'Frozen',
  shoppingItemState: 'in_cart',
  outOfStockAt: '2030-01-01T10:00:00Z',
})

// The store screen as the server builds it: every list with items here is a
// button, and the items are those of the lists switched on.
function serve({ trips = [], isReminded = true }: { trips?: ShoppingTrip[]; isReminded?: boolean } = {}) {
  vi.mocked(graphql).mockImplementation((async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: [groceries, party] }
    if (document.includes('ListShoppingStoreItems')) {
      const asked = variables?.shoppingListIds as string[] | null
      const isIncluded = (id: string) => !asked || asked.includes(id)
      return {
        ListShoppingStoreItems: {
          storeName: 'Example Market',
          isReminded,
          shoppingItems: [peas, crackers, ice].filter((item) => isIncluded(item.shoppingListId)),
          shoppingLists: [
            { shoppingListId: 'list-1', listName: 'Groceries', neededCount: 1, isIncluded: isIncluded('list-1') },
            { shoppingListId: 'list-2', listName: 'Party', neededCount: 1, isIncluded: isIncluded('list-2') },
          ],
          shoppingTrips: trips,
        },
      }
    }
    return {}
  }) as never)
}

const storeAddress = '/shopping/stores/Example%20Market'

it('shows the items of every list by aisle, each saying its list while more than one is included', async () => {
  serve()
  const { container } = renderShopping(storeAddress)

  await screen.findByText('Crackers')
  const headings = Array.from(container.querySelectorAll('.shopping-group-row th')).map(
    (heading) => heading.textContent,
  )
  expect(headings).toEqual(['Frozen', 'Snacks'])
  const crackersRow = screen.getByText('Crackers').closest('tr') as HTMLElement
  expect(within(crackersRow).getByText('Party')).toBeTruthy()
  const peasRow = screen.getByText('Peas').closest('tr') as HTMLElement
  expect(within(peasRow).getByText('Groceries')).toBeTruthy()
  expect(callsOf('ListShoppingStoreItems')[0][1]).toEqual({ storeName: 'Example Market', shoppingListIds: null })
})

it('narrows to the lists switched on, keeps them in the address, and stops naming the list when one is left', async () => {
  serve()
  renderShopping(storeAddress)
  await screen.findByText('Crackers')

  const toggle = screen.getByRole('button', { name: /^Party/ })
  expect(toggle.getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(toggle)

  await waitFor(() => expect(screen.queryByText('Crackers')).toBeNull())
  expect(screen.getByTestId('where').textContent).toBe(`${storeAddress}?lists=list-1`)
  expect(lastOf(callsOf('ListShoppingStoreItems'))[1]).toEqual({
    storeName: 'Example Market',
    shoppingListIds: ['list-1'],
  })
  expect(screen.getByRole('button', { name: /^Party/ }).getAttribute('aria-pressed')).toBe('false')
  const peasRow = screen.getByText('Peas').closest('tr') as HTMLElement
  expect(within(peasRow).queryByText('Groceries')).toBeNull()

  // The last list switched on stays on, and all of them on is no list in the address.
  fireEvent.click(screen.getByRole('button', { name: /^Groceries/ }))
  expect(screen.getByTestId('where').textContent).toBe(`${storeAddress}?lists=list-1`)
  fireEvent.click(screen.getByRole('button', { name: /^Party/ }))
  await screen.findByText('Crackers')
  expect(screen.getByTestId('where').textContent).toBe(storeAddress)
})

it('opens with the lists named in the address', async () => {
  serve()
  renderShopping(`${storeAddress}?lists=list-2`)

  await screen.findByText('Crackers')
  expect(screen.queryByText('Peas')).toBeNull()
  expect(callsOf('ListShoppingStoreItems')[0][1]).toEqual({ storeName: 'Example Market', shoppingListIds: ['list-2'] })
})

it('checks an item off and marks one out of stock for the store', async () => {
  serve()
  renderShopping(storeAddress)

  fireEvent.click(await screen.findByRole('checkbox', { name: 'Check off Peas' }))
  await waitFor(() => expect(callsOf('CheckShoppingItem')).toHaveLength(1))
  expect(callsOf('CheckShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-1', isChecked: true })

  fireEvent.click(screen.getByRole('button', { name: 'Peas: Mark out of stock' }))
  await waitFor(() => expect(callsOf('SetShoppingItemOutOfStock')).toHaveLength(1))
  expect(callsOf('SetShoppingItemOutOfStock')[0][1]).toEqual({
    shoppingItemId: 'item-1',
    isOutOfStock: true,
    storeName: 'Example Market',
  })

  // An item that is out of stock says so and offers to undo it.
  const iceRow = screen.getByText('Ice').closest('tr') as HTMLElement
  expect(within(iceRow).getByText('Out of stock')).toBeTruthy()
  fireEvent.click(within(iceRow).getByRole('button', { name: 'Ice: Back in stock' }))
  await waitFor(() => expect(callsOf('SetShoppingItemOutOfStock')).toHaveLength(2))
  expect(callsOf('SetShoppingItemOutOfStock')[1][1]).toMatchObject({ shoppingItemId: 'item-6', isOutOfStock: false })
})

it('starts shopping with the lists switched on, and stops the reminders at this store', async () => {
  serve()
  renderShopping(`${storeAddress}?lists=list-2`)

  fireEvent.click(await screen.findByRole('button', { name: 'Start shopping' }))
  await waitFor(() => expect(callsOf('StartShopping')).toHaveLength(1))
  expect(callsOf('StartShopping')[0][1]).toEqual({ storeName: 'Example Market', shoppingListIds: ['list-2'] })

  fireEvent.click(screen.getByRole('button', { name: "Don't remind me at this store" }))
  await waitFor(() => expect(callsOf('SetShoppingStoreReminder')).toHaveLength(1))
  expect(callsOf('SetShoppingStoreReminder')[0][1]).toEqual({ storeName: 'Example Market', isReminded: false })
})

it('finishes the caller’s trip here and says who else is shopping', async () => {
  serve({
    trips: [
      shoppingTrip(),
      shoppingTrip({ id: 'trip-2', userId: 'person-a', shopperName: 'Alex' }),
      shoppingTrip({ id: 'trip-3', storeName: 'Other Shop', storeKey: 'othershop', shopperName: 'Kim' }),
    ],
  })
  renderShopping(storeAddress)

  expect(await screen.findByText('Sam is shopping at Example Market now')).toBeTruthy()
  expect(screen.queryByText(/Kim/)).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Done shopping' }))
  await waitFor(() => expect(callsOf('FinishShopping')).toHaveLength(1))
  expect(callsOf('FinishShopping')[0][1]).toEqual({ shoppingTripId: 'trip-2' })
})

it('offers to remind again once reminders at the store are off', async () => {
  serve({ isReminded: false })
  renderShopping(storeAddress)

  fireEvent.click(await screen.findByRole('button', { name: 'Remind me at this store' }))
  await waitFor(() =>
    expect(callsOf('SetShoppingStoreReminder')[0][1]).toEqual({ storeName: 'Example Market', isReminded: true }),
  )
})
