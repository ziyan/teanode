import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../../api'
import { matchesShoppingSearch } from './shoppingItems'
import { renderShopping, shoppingItem, shoppingList } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

const oatMilk = shoppingItem({
  id: 'item-1',
  itemDescription: 'Oat milk',
  itemDetails: 'unsweetened',
  storeName: 'Example Market',
  aisleName: 'Dairy',
})
const tomatoes = shoppingItem({
  id: 'item-2',
  itemDescription: 'Tomatoes',
  itemDetails: '',
  storeName: 'Corner Shop',
  aisleName: 'Produce',
})
const cartedMilkBread = shoppingItem({
  id: 'item-3',
  itemDescription: 'Milk bread',
  shoppingItemState: 'in_cart',
  cartedByUserId: 'person-a',
  cartedByName: 'Alex',
})

function serve() {
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: [shoppingList()] }
    if (document.includes('GetShoppingList(')) {
      return {
        GetShoppingList: {
          totalCount: 3,
          shoppingList: shoppingList(),
          shoppingItems: [oatMilk, tomatoes, cartedMilkBread],
          shoppingTrips: [],
        },
      }
    }
    if (document.includes('ListOpenShoppingTrips')) return { ListOpenShoppingTrips: [] }
    if (document.includes('GetResumableShoppingTrip')) return { GetResumableShoppingTrip: null }
    return {}
  }) as never)
}

beforeEach(() => {
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

it('matches when every word is found, ignoring case, in any of the four fields', () => {
  expect(matchesShoppingSearch(oatMilk, 'OAT milk')).toBe(true)
  expect(matchesShoppingSearch(oatMilk, 'oat butter')).toBe(false)
  expect(matchesShoppingSearch(oatMilk, 'sweet')).toBe(true)
  expect(matchesShoppingSearch(oatMilk, 'example dairy')).toBe(true)
  expect(matchesShoppingSearch(oatMilk, 'corner')).toBe(false)
})

it('matches everything when the text is empty or only spaces', () => {
  expect(matchesShoppingSearch(oatMilk, '')).toBe(true)
  expect(matchesShoppingSearch(oatMilk, '   ')).toBe(true)
})

it('filters the rows and counts each tab by the search', async () => {
  serve()
  renderShopping('/shopping/lists/list-1?q=milk')

  expect(await screen.findByText('Oat milk')).toBeTruthy()
  expect(screen.queryByText('Tomatoes')).toBeNull()
  expect(screen.getByRole('button', { name: /^To buy/ }).textContent).toContain('1')
  expect(screen.getByRole('button', { name: /^In the cart/ }).textContent).toContain('1')
})

it('writes the typed text to the address and clears it with Escape', async () => {
  serve()
  renderShopping('/shopping/lists/list-1?tab=in-cart&page=2')

  const field = await screen.findByPlaceholderText('Search this list')
  fireEvent.change(field, { target: { value: 'bread' } })
  await waitFor(() =>
    expect(screen.getByTestId('where').textContent).toBe('/shopping/lists/list-1?tab=in-cart&q=bread'),
  )

  fireEvent.keyDown(field, { key: 'Escape' })
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/shopping/lists/list-1?tab=in-cart'))
})

it('says nothing matches and clears the search with a button', async () => {
  serve()
  renderShopping('/shopping/lists/list-1?q=Pickles')

  expect(await screen.findByText('No items match “Pickles”')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))

  expect(await screen.findByText('Oat milk')).toBeTruthy()
  expect(screen.getByTestId('where').textContent).toBe('/shopping/lists/list-1')
})
