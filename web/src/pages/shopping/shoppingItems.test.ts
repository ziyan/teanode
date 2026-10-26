import { expect, it } from 'vitest'

import {
  countShoppingTabs,
  groupShoppingItems,
  groupShoppingItemsByAisle,
  isNumber,
  isPositiveNumber,
  plainNumber,
  quantityText,
  shoppingListAddress,
  shoppingStoreAddress,
  isOnShoppingTab,
  storeKeyOf,
} from './shoppingItems'
import { shoppingItem } from './shoppingTestSupport'

it('keeps an item out of stock among those to buy, and shows it on Out of stock too', () => {
  const outOfStock = shoppingItem({ outOfStockAt: '2030-01-01T00:00:00Z' })
  expect(isOnShoppingTab(shoppingItem(), 'to-buy')).toBe(true)
  expect(isOnShoppingTab(outOfStock, 'to-buy')).toBe(true)
  expect(isOnShoppingTab(outOfStock, 'out-of-stock')).toBe(true)
  expect(isOnShoppingTab(shoppingItem({ shoppingItemState: 'in_cart' }), 'in-cart')).toBe(true)
  expect(isOnShoppingTab(shoppingItem({ shoppingItemState: 'not_needed' }), 'to-buy')).toBe(false)
  expect(
    countShoppingTabs([
      shoppingItem({ id: 'a' }),
      shoppingItem({ id: 'b' }),
      shoppingItem({ id: 'c', outOfStockAt: '2030-01-01T00:00:00Z' }),
      shoppingItem({ id: 'd', shoppingItemState: 'not_needed' }),
    ]),
  ).toEqual({ 'to-buy': 3, 'in-cart': 0, 'out-of-stock': 1, 'not-needed': 1 })
})

it('groups by store, named stores alphabetically and the items for any store last, aisles in the order given', () => {
  const groups = groupShoppingItems([
    shoppingItem({ id: '1', storeName: 'Zed Foods', storeKey: 'zedfoods', aisleName: 'Frozen' }),
    shoppingItem({ id: '2', storeName: '', storeKey: '', aisleName: 'Dairy' }),
    shoppingItem({ id: '3', storeName: 'Example Market', storeKey: 'examplemarket', aisleName: 'Produce' }),
    shoppingItem({ id: '4', storeName: 'Example Market', storeKey: 'examplemarket', aisleName: 'Frozen' }),
    shoppingItem({ id: '5', storeName: 'Example Market', storeKey: 'examplemarket', aisleName: 'Produce' }),
  ])
  expect(groups.map((group) => [group.storeName, group.aisleName, group.shoppingItems.map((item) => item.id)])).toEqual(
    [
      ['Example Market', 'Produce', ['3', '5']],
      ['Example Market', 'Frozen', ['4']],
      ['Zed Foods', 'Frozen', ['1']],
      ['', 'Dairy', ['2']],
    ],
  )
})

it('groups the store screen by aisle, with the items for any store last', () => {
  const groups = groupShoppingItemsByAisle([
    shoppingItem({ id: '1', aisleName: 'Frozen' }),
    shoppingItem({ id: '2', storeName: '', storeKey: '', aisleName: 'Frozen' }),
    shoppingItem({ id: '3', aisleName: 'Snacks' }),
  ])
  expect(groups.map((group) => [group.aisleName, group.isAnyStore, group.shoppingItems.map((item) => item.id)])).toEqual([
    ['Frozen', false, ['1']],
    ['Snacks', false, ['3']],
    ['', true, ['2']],
  ])
})

it('writes counts without the places they do not need', () => {
  expect(plainNumber('2.000')).toBe('2')
  expect(plainNumber('0.5')).toBe('0.5')
  expect(plainNumber('')).toBe('')
  expect(quantityText({ neededQuantity: '2.000', quantityUnit: 'lb' })).toBe('2 lb')
  expect(quantityText({ neededQuantity: '3', quantityUnit: '' })).toBe('3')
  expect(isPositiveNumber('2')).toBe(true)
  expect(isPositiveNumber('0')).toBe(false)
  expect(isPositiveNumber('two')).toBe(false)
  expect(isNumber('0')).toBe(true)
  expect(isNumber('')).toBe(false)
})

it('compares store names the way the server does', () => {
  expect(storeKeyOf('Example Market')).toBe('examplemarket')
  expect(storeKeyOf('EXAMPLE-MARKET’s')).toBe('examplemarkets')
  expect(storeKeyOf('---')).toBe('')
})

it('writes addresses that survive a store name with spaces', () => {
  expect(shoppingStoreAddress('Example Market', ['list-1', 'list-2'])).toBe(
    '/shopping/stores/Example%20Market?lists=list-1,list-2',
  )
  expect(shoppingStoreAddress('Example Market')).toBe('/shopping/stores/Example%20Market')
  expect(shoppingListAddress('list-1')).toBe('/shopping/lists/list-1')
  expect(shoppingListAddress('list-1', 'in-cart')).toBe('/shopping/lists/list-1?tab=in-cart')
})
