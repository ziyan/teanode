// What the Shopping tests share: the shapes the server answers with, and the
// providers and routes a page is drawn in.

import { render } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import type { ShoppingItem, ShoppingList, ShoppingTrip } from './shoppingApi'
import { ShoppingHome, ShoppingLayout } from './shoppingLayout'
import { ShoppingListPage } from './shoppingList'
import { ShoppingReceiptPage } from './shoppingReceipt'
import { ShoppingStorePage } from './shoppingStore'

export function shoppingList(overrides: Partial<ShoppingList> = {}): ShoppingList {
  return {
    id: 'list-1',
    userId: 'person-a',
    listName: 'Groceries',
    ownerName: 'Alex',
    ownerUsername: 'alex',
    isOwner: true,
    isShared: false,
    isReminded: true,
    neededCount: 3,
    inCartCount: 0,
    notNeededCount: 1,
    updatedAt: '2030-01-01T00:00:00Z',
    ...overrides,
  }
}

export function shoppingItem(overrides: Partial<ShoppingItem> = {}): ShoppingItem {
  return {
    id: 'item-1',
    shoppingListId: 'list-1',
    listName: 'Groceries',
    itemDescription: 'Peas',
    itemDetails: '',
    storeName: 'Example Market',
    storeKey: 'examplemarket',
    isStoreBrand: false,
    aisleName: 'Frozen',
    aisleRank: 0,
    itemRank: 0,
    neededQuantity: '2',
    quantityUnit: 'bags',
    unitPriceAmount: '1.50',
    currencyCode: 'USD',
    shoppingItemState: 'needed',
    cartedAt: null,
    cartedByName: '',
    cartedByUserId: '',
    outOfStockAt: null,
    outOfStockByName: '',
    outOfStockStoreName: '',
    lastBoughtAt: null,
    picturePath: '',
    pictureSource: '',
    picturePageUrl: '',
    pictureSearchedAt: null,
    updatedAt: '2030-01-01T00:00:00Z',
    ...overrides,
  }
}

export function shoppingTrip(overrides: Partial<ShoppingTrip> = {}): ShoppingTrip {
  return {
    id: 'trip-1',
    userId: 'person-b',
    shopperName: 'Sam',
    shopperUsername: 'sam',
    storeName: 'Example Market',
    storeKey: 'examplemarket',
    shoppingListIds: ['list-1'],
    startedAt: '2030-01-01T10:00:00Z',
    finishedAt: null,
    ...overrides,
  }
}

// The last of a list the test knows is not empty.
export function lastOf<T>(list: T[]): T {
  return list[list.length - 1]
}

// The calls made with a document that mentions the operation.
export function callsOf(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

export function renderShopping(entry: string, permissions = ['shopping:use']) {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'alex',
            passkeysEnabled: false,
            permissions: { everywhere: permissions, byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route element={<ShoppingLayout />}>
                <Route path="/shopping" element={<ShoppingHome />} />
                <Route path="/shopping/lists/:shoppingListId" element={<ShoppingListPage />} />
                <Route path="/shopping/stores/:storeName" element={<ShoppingStorePage />} />
                <Route path="/shopping/receipts/:financeReceiptId" element={<ShoppingReceiptPage />} />
              </Route>
            </Routes>
            <Where />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

// renderInProviders draws one component, a dialog say, with the words, the
// toasts, the session and a router around it.
export function renderInProviders(element: React.ReactNode, permissions = ['shopping:use']) {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'alex',
            passkeysEnabled: false,
            permissions: { everywhere: permissions, byDomain: [] },
          }}
        >
          <MemoryRouter>{element}</MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}
