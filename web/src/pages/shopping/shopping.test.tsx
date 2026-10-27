import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../../api'
import type { ShoppingImportItem, ShoppingItem, ShoppingList, ShoppingTrip } from './shoppingApi'
import { callsOf, lastOf, renderShopping, shoppingItem, shoppingList, shoppingTrip } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

// Delivers an event to the live feed the page opened.
let emitEvent: (data: { ShoppingEvents: { shoppingListId: string } }) => void = () => undefined

beforeEach(() => {
  vi.mocked(subscribe).mockImplementation(((_query: string, _variables: unknown, onData: typeof emitEvent) => {
    emitEvent = onData
    return () => undefined
  }) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const peas = shoppingItem()
const apples = shoppingItem({
  id: 'item-2',
  itemDescription: 'Apples',
  aisleName: 'Produce',
  aisleRank: 1,
  picturePath: '/api/v1/shopping/pictures/item-2',
})
const milk = shoppingItem({
  id: 'item-3',
  itemDescription: 'Milk',
  itemDetails: 'Whole',
  storeName: '',
  storeKey: '',
  aisleName: 'Dairy',
  aisleRank: 2,
  isStoreBrand: true,
})
const rice = shoppingItem({
  id: 'item-4',
  itemDescription: 'Rice',
  storeName: '',
  storeKey: '',
  aisleName: '',
  shoppingItemState: 'not_needed',
  lastBoughtAt: '2029-12-24T00:00:00Z',
})

function serve({
  lists = [shoppingList()],
  items = [peas, apples, milk, rice],
  trips = [],
  previewed = [],
}: {
  lists?: ShoppingList[]
  items?: ShoppingItem[]
  trips?: ShoppingTrip[]
  previewed?: ShoppingImportItem[]
} = {}) {
  vi.mocked(graphql).mockImplementation((async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: lists }
    if (document.includes('GetShoppingList(')) {
      return {
        GetShoppingList: {
          totalCount: items.length,
          shoppingList: lists[0],
          shoppingItems: items,
          shoppingTrips: trips,
        },
      }
    }
    if (document.includes('ListShoppingStores')) {
      return {
        ListShoppingStores: [
          { storeName: 'Example Market', storeKey: 'examplemarket', neededCount: 2, isReminded: true },
        ],
      }
    }
    if (document.includes('PreviewShoppingImport')) return { PreviewShoppingImport: { shoppingItems: previewed } }
    if (document.includes('CreateShoppingList')) {
      return { CreateShoppingList: shoppingList({ id: 'list-2', listName: String(variables?.listName) }) }
    }
    if (document.includes('AddShoppingItems')) return { AddShoppingItems: [] }
    return {}
  }) as never)
}

it('lists the items under a heading for each store and aisle, the items for any store last', async () => {
  serve()
  const { container } = renderShopping('/shopping/lists/list-1')

  await screen.findByText('Peas')
  const headings = Array.from(container.querySelectorAll('.shopping-group-row th')).map(
    (heading) => heading.textContent,
  )
  expect(headings).toEqual(['Example Market · Frozen', 'Example Market · Produce', 'Any store · Dairy'])

  // The tabs say how many each holds; the bought item is on the last one.
  expect(screen.getByRole('button', { name: /^To buy/ }).textContent).toContain('3')
  expect(screen.getByRole('button', { name: /^Not needed/ }).textContent).toContain('1')
  expect(screen.queryByText('Rice')).toBeNull()

  // A picture when there is one, and the first letter of the item when not.
  const applesRow = screen.getByText('Apples').closest('tr') as HTMLElement
  expect(applesRow.querySelector('img')?.getAttribute('src')).toBe('/api/v1/shopping/pictures/item-2')
  const peasRow = screen.getByText('Peas').closest('tr') as HTMLElement
  expect(peasRow.querySelector('.shopping-picture-empty')?.textContent).toBe('P')
  expect(within(peasRow).getByText('2 bags')).toBeTruthy()
  expect(within(peasRow).getByText('$1.50')).toBeTruthy()

  // Details sit under the description, and a store brand says so.
  const milkRow = screen.getByText('Milk').closest('tr') as HTMLElement
  expect(within(milkRow).getByText('Whole')).toBeTruthy()
  expect(within(milkRow).getByText('Store brand')).toBeTruthy()
})

it('checks an item off with CheckShoppingItem', async () => {
  serve()
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('checkbox', { name: 'Check off Peas' }))

  await waitFor(() => expect(callsOf('CheckShoppingItem')).toHaveLength(1))
  expect(callsOf('CheckShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-1', isChecked: true, storeName: null })
})

it('keeps the tab in the address and offers Need again on the items that are not needed', async () => {
  serve()
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: /^Not needed/ }))
  expect(screen.getByTestId('where').textContent).toBe('/shopping/lists/list-1?tab=not-needed')
  const riceRow = (await screen.findByText('Rice')).closest('tr') as HTMLElement
  expect(within(riceRow).getByText('Dec 24, 2029')).toBeTruthy()

  fireEvent.click(within(riceRow).getByRole('button', { name: 'Need again' }))
  const quantity = screen.getByLabelText('Need (in bags)') as HTMLInputElement
  fireEvent.change(quantity, { target: { value: '3' } })
  fireEvent.click(lastOf(screen.getAllByRole('button', { name: 'Need again' })))

  await waitFor(() => expect(callsOf('SetShoppingItemNeed')).toHaveLength(1))
  expect(callsOf('SetShoppingItemNeed')[0][1]).toEqual({
    shoppingItemId: 'item-4',
    isNeeded: true,
    neededQuantity: '3',
  })
})

it('removes an item only after asking', async () => {
  serve()
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Peas: Remove' }))
  expect(callsOf('RemoveShoppingItems')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Remove' }))

  await waitFor(() => expect(callsOf('RemoveShoppingItems')).toHaveLength(1))
  expect(callsOf('RemoveShoppingItems')[0][1]).toEqual({ shoppingItemIds: ['item-1'] })
})

it('reads a pasted list, shows what would be added, and adds only the items left ticked', async () => {
  const previewed: ShoppingImportItem[] = [
    {
      itemDescription: 'Peas',
      itemDetails: '',
      neededQuantity: '3',
      quantityUnit: 'bags',
      aisleName: 'Frozen',
      storeName: 'Example Market',
      isStoreBrand: false,
      mergedShoppingItemId: 'item-1',
      mergedShoppingItemState: 'needed',
      mergedNeededQuantity: '2',
    },
    {
      itemDescription: 'Bread',
      itemDetails: 'Sliced',
      neededQuantity: '1',
      quantityUnit: '',
      aisleName: 'Bakery',
      storeName: '',
      isStoreBrand: false,
      mergedShoppingItemId: '',
      mergedShoppingItemState: '',
      mergedNeededQuantity: '',
    },
    {
      itemDescription: 'Tea',
      itemDetails: '',
      neededQuantity: '1',
      quantityUnit: '',
      aisleName: 'Drinks',
      storeName: '',
      isStoreBrand: false,
      mergedShoppingItemId: '',
      mergedShoppingItemState: '',
      mergedNeededQuantity: '',
    },
  ]
  serve({ previewed })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Import' }))
  fireEvent.change(screen.getByLabelText('Pasted list'), { target: { value: 'peas, bread, tea' } })
  fireEvent.click(screen.getByRole('button', { name: 'Read the list' }))

  // The preview: each item with its section, and what adding it would do.
  const breadRow = (await screen.findByText('Bread')).closest('tr') as HTMLElement
  expect(within(breadRow).getByText('Bakery')).toBeTruthy()
  expect(within(breadRow).getByText('New')).toBeTruthy()
  const peasRow = lastOf(screen.getAllByText('Peas')).closest('tr') as HTMLElement
  expect(within(peasRow).getByText('On the list: need 2 to 3')).toBeTruthy()
  expect(callsOf('PreviewShoppingImport')[0][1]).toEqual({
    shoppingListId: 'list-1',
    importText: 'peas, bread, tea',
    agentAttachmentIds: null,
  })
  // Nothing is added by reading.
  expect(callsOf('AddShoppingItems')).toHaveLength(0)

  fireEvent.click(screen.getByRole('checkbox', { name: 'Include Tea' }))
  fireEvent.click(screen.getByRole('button', { name: 'Add 2 items' }))

  await waitFor(() => expect(callsOf('AddShoppingItems')).toHaveLength(1))
  const [, variables] = callsOf('AddShoppingItems')[0]
  expect(variables?.shoppingListId).toBe('list-1')
  expect((variables?.shoppingItems as { itemDescription: string }[]).map((entry) => entry.itemDescription)).toEqual([
    'Peas',
    'Bread',
  ])
  expect((variables?.shoppingItems as Record<string, unknown>[])[1]).toEqual({
    itemDescription: 'Bread',
    itemDetails: 'Sliced',
    storeName: '',
    isStoreBrand: false,
    aisleName: 'Bakery',
    neededQuantity: '1',
    quantityUnit: '',
  })
})

it('makes the new list first when the import goes into a new list', async () => {
  serve({
    previewed: [
      {
        itemDescription: 'Tea',
        itemDetails: '',
        neededQuantity: '1',
        quantityUnit: '',
        aisleName: '',
        storeName: '',
        isStoreBrand: false,
        mergedShoppingItemId: '',
        mergedShoppingItemState: '',
        mergedNeededQuantity: '',
      },
    ],
  })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Import' }))
  fireEvent.change(screen.getByLabelText('Pasted list'), { target: { value: 'tea' } })
  fireEvent.change(screen.getByLabelText('Into the list'), { target: { value: 'new' } })
  fireEvent.change(screen.getByLabelText('List name'), { target: { value: 'Party' } })
  fireEvent.click(screen.getByRole('button', { name: 'Read the list' }))

  await screen.findByText('Tea')
  // Reading used no list, and made none.
  expect(callsOf('PreviewShoppingImport')[0][1]).toMatchObject({ shoppingListId: null })
  expect(callsOf('CreateShoppingList')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Add 1 item' }))

  await waitFor(() => expect(callsOf('AddShoppingItems')).toHaveLength(1))
  expect(callsOf('CreateShoppingList')[0][1]).toEqual({ listName: 'Party' })
  expect(callsOf('AddShoppingItems')[0][1]?.shoppingListId).toBe('list-2')
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/shopping/lists/list-2'))
})

it('says who is shopping the list now, and finishes the trip of the caller', async () => {
  serve({
    trips: [
      shoppingTrip(),
      shoppingTrip({ id: 'trip-2', userId: 'person-a', shopperName: 'Alex', shopperUsername: 'alex' }),
    ],
  })
  renderShopping('/shopping/lists/list-1')

  expect(await screen.findByText('Sam is shopping at Example Market now')).toBeTruthy()
  expect(screen.getByText('You are shopping at Example Market')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Start shopping' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Done shopping' }))

  await waitFor(() => expect(callsOf('FinishShopping')).toHaveLength(1))
  expect(callsOf('FinishShopping')[0][1]).toEqual({ shoppingTripId: 'trip-2' })
})

it('opens a store with the list on it and starts nothing; Done waits for something in the cart', async () => {
  serve()
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Shop at a store' }))
  expect(screen.queryByRole('button', { name: 'Done shopping' })).toBeNull()
  fireEvent.change(screen.getByLabelText('Store'), { target: { value: 'Example Market' } })
  fireEvent.click(screen.getByRole('button', { name: 'Open the store' }))

  await waitFor(() =>
    expect(screen.getByTestId('where').textContent).toBe('/shopping/stores/Example%20Market?lists=list-1'),
  )
  expect(callsOf('StartShopping')).toHaveLength(0)
})

it('buys what the caller put in the cart with Done, with no trip and no receipt', async () => {
  serve({ items: [shoppingItem({ shoppingItemState: 'in_cart', cartedByUserId: 'person-a', cartedByName: 'Alex' })] })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Done shopping' }))
  await waitFor(() => expect(callsOf('FinishShopping')).toHaveLength(1))
  expect(callsOf('FinishShopping')[0][1]).toEqual({ shoppingTripId: null })
})

it('lays the items out as cards with big pictures, and a picture opens in the lightbox', async () => {
  serve({ items: [shoppingItem({ picturePath: '/api/v1/shopping/pictures/item-1' })] })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Pictures' }))
  await waitFor(() => expect(screen.getByTestId('where').textContent).toContain('view=pictures'))
  expect(document.querySelector('.shopping-card')).toBeTruthy()
  fireEvent.click(screen.getByRole('img', { name: 'Peas' }))
  expect(await screen.findByRole('dialog', { name: 'Peas' })).toBeTruthy()
})

it('offers the owner the sharing and the deleting, and the people it is shared with neither', async () => {
  serve({ lists: [shoppingList({ isOwner: false, ownerName: 'Sam' })] })
  renderShopping('/shopping/lists/list-1')

  await screen.findByText('Peas')
  expect(screen.getByText('From Sam', { selector: '.shopping-list-summary p' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
  expect(screen.queryByRole('button', { name: /Delete list/ })).toBeNull()
  expect(screen.getByRole('button', { name: 'Reminders' })).toBeTruthy()
})

it('reads the open list again when the live feed names it, and not for another list', async () => {
  serve()
  renderShopping('/shopping/lists/list-1')
  await screen.findByText('Peas')
  const readCount = () => callsOf('GetShoppingList(').length
  const before = readCount()

  emitEvent({ ShoppingEvents: { shoppingListId: 'list-9' } })
  await new Promise((resolve) => setTimeout(resolve, 20))
  expect(readCount()).toBe(before)

  emitEvent({ ShoppingEvents: { shoppingListId: 'list-1' } })
  await waitFor(() => expect(readCount()).toBe(before + 1))
})

it('sends nobody without shopping:use anywhere', async () => {
  serve()
  renderShopping('/shopping/lists/list-1', [])

  expect(await screen.findByText('Shopping lists are not available to your account.')).toBeTruthy()
  expect(callsOf('GetShoppingList(')).toHaveLength(0)
})

it('lists the lists and the stores in the column, and asks to choose one at /shopping', async () => {
  serve()
  renderShopping('/shopping')

  expect(await screen.findByText('Choose a list or a store.')).toBeTruthy()
  const column = screen.getByRole('navigation', { name: 'Lists' })
  expect(within(column).getByRole('link', { name: /Groceries/ }).getAttribute('href')).toBe('/shopping/lists/list-1')
  expect(await within(column).findByRole('link', { name: /Example Market/ })).toBeTruthy()
})
