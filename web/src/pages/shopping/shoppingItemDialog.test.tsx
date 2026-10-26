import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import type { ShoppingItemPrice } from './shoppingApi'
import { ShoppingItemDialog, pricePoints } from './shoppingItemDialog'
import { callsOf, lastOf, renderInProviders, shoppingItem } from './shoppingTestSupport'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function price(overrides: Partial<ShoppingItemPrice> = {}): ShoppingItemPrice {
  return {
    id: 'price-1',
    shoppingItemId: 'item-1',
    storeName: 'Example Market',
    unitPriceAmount: '1.50',
    currencyCode: 'USD',
    quantity: '1',
    quantityUnit: '',
    observedOn: '2030-02-10',
    priceSource: 'person',
    financeReceiptId: '',
    ...overrides,
  }
}

function serve(prices: ShoppingItemPrice[] = []) {
  vi.mocked(graphql).mockImplementation((async (document: string) => {
    if (document.includes('ListShoppingItemPrices')) return { ListShoppingItemPrices: prices }
    if (document.includes('ListShoppingStores')) {
      return {
        ListShoppingStores: [
          { storeName: 'Example Market', storeKey: 'examplemarket', neededCount: 1, isReminded: true },
        ],
      }
    }
    return {}
  }) as never)
}

function open(item = shoppingItem()) {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  renderInProviders(<ShoppingItemDialog target={{ shoppingItem: item }} onSaved={onSaved} onClose={onClose} />)
  return { onSaved, onClose }
}

it('saves only what was changed, and remembers a changed price as a new price', async () => {
  serve()
  const { onSaved, onClose } = open()

  fireEvent.change(screen.getByLabelText('Need'), { target: { value: '3' } })
  fireEvent.change(screen.getByLabelText('Unit price'), { target: { value: '1.75' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(callsOf('UpdateShoppingItem')).toHaveLength(1)
  expect(callsOf('UpdateShoppingItem')[0][1]).toEqual({ shoppingItemId: 'item-1', neededQuantity: '3' })
  expect(callsOf('RecordShoppingItemPrice')).toHaveLength(1)
  expect(callsOf('RecordShoppingItemPrice')[0][1]).toEqual({
    shoppingItemId: 'item-1',
    unitPriceAmount: '1.75',
    storeName: 'Example Market',
    currencyCode: 'USD',
  })
  expect(onSaved).toHaveBeenCalled()
})

it('changes nothing on the server when nothing was edited', async () => {
  serve()
  const { onClose } = open()

  fireEvent.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(callsOf('UpdateShoppingItem')).toHaveLength(0)
  expect(callsOf('RecordShoppingItemPrice')).toHaveLength(0)
})

it('shows what the item has cost: a line over time and a table, a receipt price linking to the receipt', async () => {
  serve([
    price({
      id: 'price-3',
      observedOn: '2030-03-01',
      unitPriceAmount: '1.75',
      priceSource: 'receipt',
      financeReceiptId: 'receipt-1',
    }),
    price({ id: 'price-2', observedOn: '2030-02-10', unitPriceAmount: '1.50' }),
    price({ id: 'price-1', observedOn: '2030-01-05', unitPriceAmount: '1.25', storeName: '' }),
  ])
  open()

  const table = (await screen.findAllByText('Entered by hand'))[0].closest('table') as HTMLElement
  const rows = within(table).getAllByRole('row').slice(1)
  expect(rows).toHaveLength(3)
  expect(within(rows[0]).getByText('$1.75')).toBeTruthy()
  expect(within(rows[0]).getByRole('link', { name: 'Receipt' }).getAttribute('href')).toBe(
    '/finance/receipts?receipt=receipt-1',
  )
  expect(within(rows[2]).getByText('Any store')).toBeTruthy()
  expect(screen.getByRole('img', { name: 'Price went from $1.25 to $1.75' })).toBeTruthy()
})

it('draws no line for one price, or for prices in different currencies', () => {
  expect(pricePoints([price()])).toEqual([])
  expect(
    pricePoints([
      price({ id: 'a', currencyCode: 'USD', observedOn: '2030-01-01' }),
      price({ id: 'b', currencyCode: 'EUR', observedOn: '2030-02-01' }),
    ]),
  ).toEqual([])
  const points = pricePoints([
    price({ id: 'b', observedOn: '2030-02-01', unitPriceAmount: '2.00' }),
    price({ id: 'a', observedOn: '2030-01-01', unitPriceAmount: '1.00' }),
  ])
  expect(points.map((point) => point.price.id)).toEqual(['a', 'b'])
  // Dearer is higher on the picture, which counts y from the top.
  expect(points[1].y).toBeLessThan(points[0].y)
})

it('asks for another picture', async () => {
  serve()
  open()

  fireEvent.click(screen.getByRole('button', { name: 'Find another' }))

  await waitFor(() => expect(callsOf('FindShoppingItemPicture')).toHaveLength(1))
  expect(callsOf('FindShoppingItemPicture')[0][1]).toEqual({ shoppingItemId: 'item-1' })
})

it('uploads a photo as an attachment and sets it as the picture', async () => {
  serve()
  vi.mocked(uploadFiles).mockReturnValue({
    promise: Promise.resolve({ attachments: [{ id: 'attachment-1' }] }),
    cancel: () => undefined,
  })
  const { onSaved } = open()

  const file = new File(['picture'], 'peas.png', { type: 'image/png' })
  const input = document.querySelector('input[type="file"]') as HTMLInputElement
  fireEvent.change(input, { target: { files: [file] } })

  await waitFor(() => expect(callsOf('SetShoppingItemPicture')).toHaveLength(1))
  expect(vi.mocked(uploadFiles).mock.calls[0].slice(0, 3)).toEqual(['POST', '/api/v1/agent/attachments', [file]])
  expect(callsOf('SetShoppingItemPicture')[0][1]).toEqual({
    shoppingItemId: 'item-1',
    agentAttachmentId: 'attachment-1',
  })
  expect(onSaved).toHaveBeenCalled()
})

it('marks the item out of stock and closes', async () => {
  serve()
  const { onClose } = open()

  fireEvent.click(screen.getByRole('button', { name: 'Mark out of stock' }))
  await waitFor(() => expect(callsOf('SetShoppingItemOutOfStock')).toHaveLength(1))
  expect(callsOf('SetShoppingItemOutOfStock')[0][1]).toEqual({ shoppingItemId: 'item-1', isOutOfStock: true })
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
})

it('removes the item after the dialog asks', async () => {
  serve()
  const { onClose } = open()

  fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
  expect(callsOf('RemoveShoppingItems')).toHaveLength(0)
  fireEvent.click(lastOf(screen.getAllByRole('button', { name: 'Remove' })))

  await waitFor(() => expect(callsOf('RemoveShoppingItems')).toHaveLength(1))
  expect(callsOf('RemoveShoppingItems')[0][1]).toEqual({ shoppingItemIds: ['item-1'] })
  await waitFor(() => expect(onClose).toHaveBeenCalled())
})

it('adds a new item to the list it was opened on', async () => {
  serve()
  const onClose = vi.fn()
  renderInProviders(
    <ShoppingItemDialog
      target={{ shoppingListId: 'list-1', shoppingItem: null }}
      onSaved={() => undefined}
      onClose={onClose}
    />,
  )

  expect((screen.getByRole('button', { name: 'Add item' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'Tea' } })
  fireEvent.change(screen.getByLabelText('Store'), { target: { value: 'Example Market' } })
  fireEvent.click(screen.getByLabelText('Store brand'))
  fireEvent.click(screen.getByRole('button', { name: 'Add item' }))

  await waitFor(() => expect(callsOf('AddShoppingItems')).toHaveLength(1))
  expect(callsOf('AddShoppingItems')[0][1]).toEqual({
    shoppingListId: 'list-1',
    shoppingItems: [
      {
        itemDescription: 'Tea',
        itemDetails: '',
        storeName: 'Example Market',
        isStoreBrand: true,
        aisleName: '',
        neededQuantity: '1',
        quantityUnit: '',
      },
    ],
  })
  // A new item has no picture to change and no prices to show.
  expect(screen.queryByRole('button', { name: 'Find another' })).toBeNull()
  expect(callsOf('ListShoppingItemPrices')).toHaveLength(0)
})
