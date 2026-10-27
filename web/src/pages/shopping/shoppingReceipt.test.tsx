import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../../api'
import type { ShoppingReceipt } from './shoppingApi'
import { ShoppingReceiptSection } from './shoppingReceipt'
import { callsOf, lastOf, renderInProviders, renderShopping, shoppingItem, shoppingList } from './shoppingTestSupport'

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

const peas = shoppingItem({ shoppingItemState: 'in_cart' })
const apples = shoppingItem({ id: 'item-2', itemDescription: 'Apples', aisleName: 'Produce' })
const bread = shoppingItem({ id: 'item-3', itemDescription: 'Bread', aisleName: 'Bakery' })

function receipt(overrides: Partial<ShoppingReceipt> = {}): ShoppingReceipt {
  return {
    financeReceiptId: 'receipt-1',
    merchantName: 'Example Market',
    purchasedOn: '2030-03-02',
    totalAmount: '12.40',
    currencyCode: 'USD',
    shoppingReceiptLines: [
      {
        financeReceiptLineId: 'line-1',
        lineNumber: 1,
        description: 'PEAS 2LB',
        quantity: '2',
        lineAmount: '3.00',
        shoppingReceiptMatches: [
          {
            shoppingReceiptMatch: {
              id: 'match-1',
              financeReceiptId: 'receipt-1',
              financeReceiptLineId: 'line-1',
              shoppingItemId: 'item-1',
              receiptMatchState: 'applied',
              matchedQuantity: '2',
            },
            shoppingItem: peas,
          },
        ],
      },
      {
        financeReceiptLineId: 'line-2',
        lineNumber: 2,
        description: 'APLS',
        quantity: '1',
        lineAmount: '4.40',
        shoppingReceiptMatches: [
          {
            shoppingReceiptMatch: {
              id: 'match-2',
              financeReceiptId: 'receipt-1',
              financeReceiptLineId: 'line-2',
              shoppingItemId: 'item-2',
              receiptMatchState: 'proposed',
              matchedQuantity: '1',
            },
            shoppingItem: apples,
          },
        ],
      },
      {
        financeReceiptLineId: 'line-3',
        lineNumber: 3,
        description: 'CANDLES',
        quantity: '1',
        lineAmount: '5.00',
        shoppingReceiptMatches: [],
      },
    ],
    stillNeededItems: [bread],
    ...overrides,
  }
}

function serve(view: ShoppingReceipt | null = receipt()) {
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingReceiptMatches')) return { ListShoppingReceiptMatches: view }
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: [shoppingList()] }
    return {}
  }) as never)
}

it('shows each line of the receipt with its match, and what is still needed', async () => {
  serve()
  renderShopping('/shopping/receipts/receipt-1')

  const peasLine = (await screen.findByText('PEAS 2LB')).closest('tr') as HTMLElement
  expect(within(peasLine).getByText('Peas')).toBeTruthy()
  expect(within(peasLine).getByText('Checked off')).toBeTruthy()
  const applesLine = screen.getByText('APLS').closest('tr') as HTMLElement
  expect(within(applesLine).getByText('Proposed')).toBeTruthy()
  const candlesLine = screen.getByText('CANDLES').closest('tr') as HTMLElement
  expect(within(candlesLine).getByText('Nothing on your lists matched')).toBeTruthy()
  expect(screen.getByText('Still needed')).toBeTruthy()
  expect(callsOf('ListShoppingReceiptMatches')[0][1]).toEqual({ financeReceiptId: 'receipt-1' })
})

it('applies a proposed match and dismisses an applied one', async () => {
  serve()
  renderShopping('/shopping/receipts/receipt-1')

  const applesLine = (await screen.findByText('APLS')).closest('tr') as HTMLElement
  fireEvent.click(within(applesLine).getByRole('button', { name: 'Apply' }))
  await waitFor(() => expect(callsOf('DecideShoppingReceiptMatch')).toHaveLength(1))
  expect(callsOf('DecideShoppingReceiptMatch')[0][1]).toEqual({ shoppingReceiptMatchId: 'match-2', isApplied: true })

  const peasLine = screen.getByText('PEAS 2LB').closest('tr') as HTMLElement
  expect(within(peasLine).queryByRole('button', { name: 'Apply' })).toBeNull()
  fireEvent.click(within(peasLine).getByRole('button', { name: 'Dismiss' }))
  await waitFor(() => expect(callsOf('DecideShoppingReceiptMatch')).toHaveLength(2))
  expect(callsOf('DecideShoppingReceiptMatch')[1][1]).toEqual({ shoppingReceiptMatchId: 'match-1', isApplied: false })
})

it('adds a line with no match to the list', async () => {
  serve()
  renderShopping('/shopping/receipts/receipt-1')

  const candlesLine = (await screen.findByText('CANDLES')).closest('tr') as HTMLElement
  fireEvent.click(within(candlesLine).getByRole('button', { name: 'Add to the list' }))

  await waitFor(() => expect(callsOf('AddShoppingItemFromReceiptLine')).toHaveLength(1))
  expect(callsOf('AddShoppingItemFromReceiptLine')[0][1]).toEqual({
    financeReceiptLineId: 'line-3',
    shoppingListId: 'list-1',
  })
})

it('ties a line to another item that is still needed', async () => {
  serve()
  renderShopping('/shopping/receipts/receipt-1')

  const candlesLine = (await screen.findByText('CANDLES')).closest('tr') as HTMLElement
  fireEvent.click(within(candlesLine).getByRole('combobox', { name: 'CANDLES: Match to another item' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Bread (Groceries)' }))

  await waitFor(() => expect(callsOf('MatchShoppingReceiptLine')).toHaveLength(1))
  expect(callsOf('MatchShoppingReceiptLine')[0][1]).toEqual({
    shoppingItemId: 'item-3',
    financeReceiptLineId: 'line-3',
  })
})

it('matches again, and undoes the check-offs after asking', async () => {
  serve()
  renderShopping('/shopping/receipts/receipt-1')

  fireEvent.click(await screen.findByRole('button', { name: 'Match again' }))
  await waitFor(() => expect(callsOf('MatchShoppingReceipt(')).toHaveLength(1))
  expect(callsOf('MatchShoppingReceipt(')[0][1]).toEqual({ financeReceiptId: 'receipt-1' })

  fireEvent.click(screen.getByRole('button', { name: 'Undo the check-offs' }))
  expect(callsOf('UndoShoppingReceipt')).toHaveLength(0)
  fireEvent.click(lastOf(screen.getAllByRole('button', { name: 'Undo the check-offs' })))
  await waitFor(() => expect(callsOf('UndoShoppingReceipt')).toHaveLength(1))
  expect(callsOf('UndoShoppingReceipt')[0][1]).toEqual({ financeReceiptId: 'receipt-1' })
})

it('cannot undo check-offs when nothing was checked off', async () => {
  serve(
    receipt({
      shoppingReceiptLines: receipt().shoppingReceiptLines.slice(2),
    }),
  )
  renderShopping('/shopping/receipts/receipt-1')

  const undo = (await screen.findByRole('button', { name: 'Undo the check-offs' })) as HTMLButtonElement
  expect(undo.disabled).toBe(true)
})

it('is a section of Finance’s receipt for a person who uses shopping lists, linking to the full view', async () => {
  serve()
  renderInProviders(<ShoppingReceiptSection financeReceiptId="receipt-1" />)

  expect(await screen.findByText('Shopping lists')).toBeTruthy()
  expect((await screen.findByText('PEAS 2LB')).closest('tr')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Open the full view' }).getAttribute('href')).toBe(
    '/shopping/receipts/receipt-1',
  )
})

it('draws nothing in Finance’s receipt for a person without shopping:use', async () => {
  serve()
  const { container } = renderInProviders(<ShoppingReceiptSection financeReceiptId="receipt-1" />, [])

  expect(container.textContent).toBe('')
  expect(callsOf('ListShoppingReceiptMatches')).toHaveLength(0)
})

it('draws nothing in Finance’s receipt for a person with no list', async () => {
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingLists')) return { ListShoppingLists: [] }
    if (document.includes('ListShoppingReceiptMatches')) return { ListShoppingReceiptMatches: receipt() }
    return {}
  }) as never)
  const { container } = renderInProviders(<ShoppingReceiptSection financeReceiptId="receipt-1" />)

  await waitFor(() => expect(callsOf('ListShoppingLists')).toHaveLength(1))
  await new Promise((resolve) => setTimeout(resolve, 30))
  expect(container.textContent).toBe('')
  expect(callsOf('ListShoppingReceiptMatches')).toHaveLength(0)
})
