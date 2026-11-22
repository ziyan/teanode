import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../../api'
import { uploadFiles } from '../../upload'
import type { ShoppingItem, ShoppingTrip } from './shoppingApi'
import { callsOf, renderShopping, shoppingItem, shoppingList, shoppingTrip } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))

beforeEach(() => {
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
  vi.mocked(uploadFiles).mockImplementation((() => ({
    promise: Promise.resolve({ attachments: [{ id: 'attachment-1' }] }),
    cancel: () => undefined,
  })) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const ownTrip = shoppingTrip({ id: 'trip-own', userId: 'person-a', shopperName: 'Alex', shopperUsername: 'alex' })

const peas = shoppingItem()
const checkedOffRice = shoppingItem({
  id: 'item-4',
  itemDescription: 'Rice',
  storeName: '',
  storeKey: '',
  aisleName: '',
  shoppingItemState: 'not_needed',
})
const cartedBread = shoppingItem({
  id: 'item-7',
  itemDescription: 'Bread',
  neededQuantity: '2',
  quantityUnit: '',
  unitPriceAmount: '2.50',
  shoppingItemState: 'in_cart',
  cartedByUserId: 'person-a',
  cartedByName: 'Alex',
})
const cartedJam = shoppingItem({
  id: 'item-8',
  shoppingListId: 'list-2',
  listName: 'Party',
  itemDescription: 'Jam',
  neededQuantity: '1',
  quantityUnit: '',
  unitPriceAmount: '4.00',
  shoppingItemState: 'in_cart',
  cartedByUserId: 'person-a',
  cartedByName: 'Alex',
})
const samsEggs = shoppingItem({
  id: 'item-9',
  itemDescription: 'Eggs',
  shoppingItemState: 'in_cart',
  cartedByUserId: 'person-b',
  cartedByName: 'Sam',
})

function serve({
  items = [peas, checkedOffRice],
  trips = [],
  openTrips = trips.filter((trip) => trip.finishedAt === null),
  resumable = null,
  storeItems = items,
}: {
  items?: ShoppingItem[]
  trips?: ShoppingTrip[]
  openTrips?: ShoppingTrip[]
  resumable?: ShoppingTrip | null
  storeItems?: ShoppingItem[]
} = {}) {
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: [shoppingList()] }
    if (document.includes('GetShoppingList(')) {
      return {
        GetShoppingList: {
          totalCount: items.length,
          shoppingList: shoppingList(),
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
    if (document.includes('ListShoppingStoreItems')) {
      return {
        ListShoppingStoreItems: {
          storeName: 'Example Market',
          isReminded: true,
          shoppingItems: storeItems,
          shoppingLists: [{ shoppingListId: 'list-1', listName: 'Groceries', neededCount: 1, isIncluded: true }],
          shoppingTrips: trips,
        },
      }
    }
    if (document.includes('ListOpenShoppingTrips')) return { ListOpenShoppingTrips: openTrips }
    if (document.includes('GetResumableShoppingTrip')) return { GetResumableShoppingTrip: resumable }
    return {}
  }) as never)
}

it('checks an item off with CheckOffShoppingItem while the person is not shopping', async () => {
  serve()
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('checkbox', { name: 'Check off Peas' }))

  await waitFor(() => expect(callsOf('CheckOffShoppingItem')).toHaveLength(1))
  expect(callsOf('CheckOffShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-1', isCheckedOff: true })
  expect(callsOf('CheckShoppingItem')).toHaveLength(0)
})

it('puts an item in the cart with the same check while the person is shopping', async () => {
  serve({ trips: [ownTrip] })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('checkbox', { name: 'Check off Peas' }))

  await waitFor(() => expect(callsOf('CheckShoppingItem')).toHaveLength(1))
  expect(callsOf('CheckShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-1', isChecked: true, storeName: null })
  expect(callsOf('CheckOffShoppingItem')).toHaveLength(0)
})

it('puts a checked off row back on the list, and leaves another shopper’s item alone', async () => {
  serve({ items: [checkedOffRice, samsEggs] })
  renderShopping('/shopping/lists/list-1?tab=not-needed')

  const check = (await screen.findByRole('checkbox', { name: 'Put Rice back on the list' })) as HTMLInputElement
  expect(check.checked).toBe(true)
  fireEvent.click(check)

  await waitFor(() => expect(callsOf('CheckOffShoppingItem')).toHaveLength(1))
  expect(callsOf('CheckOffShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-4', isCheckedOff: false })

  fireEvent.click(screen.getByRole('button', { name: /^In the cart/ }))
  expect(await screen.findByText('In Sam’s cart'.replace('’', "'"))).toBeTruthy()
  const eggsCheck = screen.getByRole('checkbox', { name: 'Check off Eggs' }) as HTMLInputElement
  expect(eggsCheck.disabled).toBe(true)
})

it('puts the check right and says so when the server refuses it', async () => {
  serve()
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation((async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('CheckOffShoppingItem')) throw new Error('Not allowed.')
    return answer(document, variables)
  }) as never)
  renderShopping('/shopping/lists/list-1')

  const check = (await screen.findByRole('checkbox', { name: 'Check off Peas' })) as HTMLInputElement
  fireEvent.click(check)

  expect(await screen.findByText('Not allowed.')).toBeTruthy()
  await waitFor(() =>
    expect((screen.getByRole('checkbox', { name: 'Check off Peas' }) as HTMLInputElement).checked).toBe(false),
  )
})

it('shows the cart across the lists of the trip, with a total, and takes an item out', async () => {
  serve({ items: [cartedBread], trips: [ownTrip], storeItems: [cartedBread, cartedJam, samsEggs] })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Done shopping · 1' }))

  const cart = await screen.findByRole('alertdialog', { name: 'Your cart' })
  expect(await within(cart).findByText('Jam')).toBeTruthy()
  expect(within(cart).getByText('2 items, about $9.00')).toBeTruthy()
  expect(within(cart).queryByText('Eggs')).toBeNull()
  // Two lists are in it, so each item says which.
  expect(within(cart).getByText(/Party/)).toBeTruthy()
  expect(within(cart).getByText('$5.00')).toBeTruthy()

  fireEvent.click(within(cart).getByRole('checkbox', { name: 'Take Jam out of the cart' }))
  await waitFor(() => expect(callsOf('CheckShoppingItem')).toHaveLength(1))
  expect(callsOf('CheckShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-8', isChecked: false, storeName: null })
  await waitFor(() => expect(within(cart).queryByText('Jam')).toBeNull())
  expect(within(cart).getByText('1 item, about $5.00')).toBeTruthy()
})

it('finishes without a receipt only after asking, and closing keeps shopping', async () => {
  serve({ items: [cartedBread], trips: [ownTrip], storeItems: [cartedBread] })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Done shopping · 1' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Finish without a receipt' }))
  expect(await screen.findByRole('alertdialog', { name: 'Finish without a receipt?' })).toBeTruthy()
  expect(
    screen.getByText('The 1 item in your cart is marked bought. You can resume this trip for the next three hours.'),
  ).toBeTruthy()
  expect(callsOf('FinishShopping')).toHaveLength(0)

  // Cancel goes back to the cart, and closing that keeps shopping.
  fireEvent.click(
    within(screen.getByRole('alertdialog', { name: 'Finish without a receipt?' })).getByRole('button', {
      name: 'Cancel',
    }),
  )
  expect(screen.getByRole('alertdialog', { name: 'Your cart' })).toBeTruthy()
  fireEvent.click(
    within(screen.getByRole('alertdialog', { name: 'Your cart' })).getByRole('button', { name: 'Cancel' }),
  )
  expect(screen.queryByRole('alertdialog')).toBeNull()
  expect(callsOf('FinishShopping')).toHaveLength(0)

  fireEvent.click(screen.getByRole('button', { name: 'Done shopping · 1' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Finish without a receipt' }))
  fireEvent.click(screen.getByRole('button', { name: 'Finish' }))
  await waitFor(() => expect(callsOf('FinishShopping')).toHaveLength(1))
  expect(callsOf('FinishShopping')[0][1]).toEqual({ shoppingTripId: 'trip-own' })
  expect(await screen.findByText('Done shopping.')).toBeTruthy()
  expect(callsOf('ReadReceipt(')).toHaveLength(0)
})

it('uploads the receipt, has it read, then finishes the trip', async () => {
  serve({ items: [cartedBread], trips: [ownTrip], storeItems: [cartedBread] })
  renderShopping('/shopping/lists/list-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Done shopping · 1' }))
  await screen.findByRole('alertdialog', { name: 'Your cart' })
  const file = new File(['receipt'], 'receipt.pdf', { type: 'application/pdf' })
  fireEvent.change(screen.getByLabelText('Receipt file'), { target: { files: [file] } })

  await waitFor(() => expect(callsOf('FinishShopping')).toHaveLength(1))
  expect(vi.mocked(uploadFiles).mock.calls[0].slice(0, 3)).toEqual(['POST', '/api/v1/agent/attachments', [file]])
  expect(callsOf('ReadReceipt(')[0][1]).toEqual({ agentAttachmentId: 'attachment-1' })
  expect(callsOf('FinishShopping')[0][1]).toEqual({ shoppingTripId: 'trip-own' })
  expect(
    await screen.findByText('Done shopping. The receipt is being read; prices are added when it is done.'),
  ).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
})

const finishedTrip = shoppingTrip({
  id: 'trip-done',
  userId: 'person-a',
  shopperName: 'Alex',
  finishedAt: '2030-01-01T15:41:00',
  boughtItemCount: 3,
})

it('offers to resume a finished trip on the list it names, and calls ResumeShopping', async () => {
  serve({ resumable: finishedTrip })
  renderShopping('/shopping/lists/list-1')

  expect(await screen.findByText(/You finished shopping at Example Market at/)).toBeTruthy()
  expect(screen.getByText('3 items go back in your cart')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Resume' }))

  await waitFor(() => expect(callsOf('ResumeShopping')).toHaveLength(1))
  expect(callsOf('ResumeShopping')[0][1]).toEqual({ shoppingTripId: 'trip-done' })
})

it('offers the resume row on the Shopping home and opens the store after resuming', async () => {
  serve({ resumable: finishedTrip })
  renderShopping('/shopping')

  fireEvent.click(await screen.findByRole('button', { name: 'Resume' }))

  await waitFor(() => expect(callsOf('ResumeShopping')).toHaveLength(1))
  await waitFor(() =>
    expect(screen.getByTestId('where').textContent).toBe('/shopping/stores/Example%20Market?lists=list-1'),
  )
})

it('says in the column that the person is shopping, with what is in their cart, and opens the store', async () => {
  serve({ trips: [ownTrip], storeItems: [cartedBread, cartedJam, samsEggs] })
  renderShopping('/shopping')

  const row = await screen.findByRole('link', { name: 'Shopping at Example Market · 2 in your cart' })
  fireEvent.click(row)
  await waitFor(() =>
    expect(screen.getByTestId('where').textContent).toBe('/shopping/stores/Example%20Market?lists=list-1'),
  )
})

it('moves the cart to the last section of the store, with the count on Done', async () => {
  serve({ trips: [ownTrip], storeItems: [peas, cartedBread, samsEggs] })
  const { container } = renderShopping('/shopping/stores/Example%20Market')

  await screen.findByText('Bread')
  const headings = Array.from(container.querySelectorAll('.shopping-group-row th')).map((each) => each.textContent)
  expect(headings).toEqual(['Frozen', 'In the cart'])
  expect(screen.getByRole('button', { name: 'Done shopping · 1' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Upload receipt' })).toBeTruthy()
})
