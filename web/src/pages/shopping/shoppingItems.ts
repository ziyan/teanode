import { subscribe } from '../../api'
import { uploadFiles } from '../../upload'
import { AGENT_ATTACHMENTS_PATH } from '../finance/financeApi'
import { SHOPPING_EVENTS, ShoppingItem } from './shoppingApi'

// What the Shopping page works out from the items it reads: how they are
// grouped, how a count and a price are written, which tab an item is on, and
// the two things several parts of the page share, the live feed and the
// upload of a photo.

// The most a photo or receipt may weigh, the limit Finance puts on a receipt.
export const MAXIMUM_UPLOAD_BYTES = 10 << 20

export type ShoppingTab = 'to-buy' | 'in-cart' | 'out-of-stock' | 'not-needed'

export const SHOPPING_TABS: ShoppingTab[] = ['to-buy', 'in-cart', 'out-of-stock', 'not-needed']

export function isShoppingTab(value: string | null): value is ShoppingTab {
  return SHOPPING_TABS.includes(value as ShoppingTab)
}

// shoppingTabOf is the tab an item is listed on. An item out of stock is
// still needed, and is listed on its own tab rather than among those to buy.
export function shoppingTabOf(item: ShoppingItem): ShoppingTab {
  if (item.shoppingItemState === 'in_cart') return 'in-cart'
  if (item.shoppingItemState === 'not_needed') return 'not-needed'
  return item.outOfStockAt ? 'out-of-stock' : 'to-buy'
}

export function countShoppingTabs(items: ShoppingItem[]): Record<ShoppingTab, number> {
  const counts: Record<ShoppingTab, number> = { 'to-buy': 0, 'in-cart': 0, 'out-of-stock': 0, 'not-needed': 0 }
  for (const item of items) counts[shoppingTabOf(item)] += 1
  return counts
}

export interface ShoppingItemGroup {
  // Empty for the items that can be got at any store.
  storeName: string
  aisleName: string
  shoppingItems: ShoppingItem[]
}

// groupShoppingItems puts the items of a list under a heading for each store
// and aisle: the named stores in alphabetical order, then the items for any
// store; within a store the aisles in the order the server sent them (as
// they were given), and the items in the order the server sent them.
export function groupShoppingItems(items: ShoppingItem[]): ShoppingItemGroup[] {
  const groups = new Map<string, ShoppingItemGroup>()
  for (const item of items) {
    const key = `${item.storeKey}\u0000${item.aisleName}`
    let group = groups.get(key)
    if (!group) {
      group = { storeName: item.storeName, aisleName: item.aisleName, shoppingItems: [] }
      groups.set(key, group)
    }
    group.shoppingItems.push(item)
  }
  const ordered = [...groups.values()]
  const storeOrder = [...new Set(ordered.map((group) => group.storeName))].sort((first, second) => {
    if (first === '' || second === '') return first === '' ? (second === '' ? 0 : 1) : -1
    return first.localeCompare(second)
  })
  return storeOrder.flatMap((storeName) => ordered.filter((group) => group.storeName === storeName))
}

// groupShoppingItemsByAisle is the store screen's grouping: the aisle is the
// heading, whatever store the item is for, in the order the server sent them.
export function groupShoppingItemsByAisle(
  items: ShoppingItem[],
): { aisleName: string; shoppingItems: ShoppingItem[] }[] {
  const groups = new Map<string, { aisleName: string; shoppingItems: ShoppingItem[] }>()
  for (const item of items) {
    let group = groups.get(item.aisleName)
    if (!group) {
      group = { aisleName: item.aisleName, shoppingItems: [] }
      groups.set(item.aisleName, group)
    }
    group.shoppingItems.push(item)
  }
  return [...groups.values()]
}

// plainNumber writes a number kept as text without the places it does not
// need: "2.000" is 2. Text that is not a number is given back as it is.
export function plainNumber(written: string): string {
  const parsed = Number(written)
  return written.trim() === '' || !Number.isFinite(parsed) ? written : String(parsed)
}

// quantityText is the count needed and its unit: "2 lb", or "3".
export function quantityText(item: Pick<ShoppingItem, 'neededQuantity' | 'quantityUnit'>): string {
  return [plainNumber(item.neededQuantity), item.quantityUnit].filter(Boolean).join(' ')
}

// An item's price per unit as a number, or null when it has none.
export function priceNumber(unitPriceAmount: string): number | null {
  if (unitPriceAmount.trim() === '') return null
  const parsed = Number(unitPriceAmount)
  return Number.isFinite(parsed) ? parsed : null
}

// isPositiveNumber says whether text is a count or a price the server takes.
export function isPositiveNumber(written: string): boolean {
  const parsed = Number(written)
  return written.trim() !== '' && Number.isFinite(parsed) && parsed > 0
}

export function isNumber(written: string): boolean {
  return written.trim() !== '' && Number.isFinite(Number(written)) && Number(written) >= 0
}

// --- the live feed -----------------------------------------------------------

type ShoppingEventListener = (shoppingListId: string) => void

const listeners = new Set<ShoppingEventListener>()
let stopFeed: (() => void) | null = null

// listenToShoppingEvents calls the listener with the id of each list that
// changes, and with an empty id when the feed was down and anything may have:
// the listener then reads again whatever it shows. One feed serves every
// listener, opened with the first and closed with the last.
export function listenToShoppingEvents(listener: ShoppingEventListener): () => void {
  listeners.add(listener)
  if (!stopFeed) {
    stopFeed = subscribe<{ ShoppingEvents: { shoppingListId: string } }>(
      SHOPPING_EVENTS,
      {},
      (data) => listeners.forEach((each) => each(data.ShoppingEvents.shoppingListId)),
      // A feed that ended is covered by the reads a page makes when it opens.
      undefined,
      {
        beforeStart: (isReconnecting) => {
          if (isReconnecting) listeners.forEach((each) => each(''))
        },
      },
    )
  }
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0 && stopFeed) {
      stopFeed()
      stopFeed = null
    }
  }
}

// --- uploads -----------------------------------------------------------------

// uploadAgentAttachment sends a photo or a PDF to the agent's attachments, the
// way Finance sends a receipt, and answers its id.
export async function uploadAgentAttachment(file: File): Promise<string> {
  const uploaded = (await uploadFiles('POST', AGENT_ATTACHMENTS_PATH, [file], () => undefined).promise) as {
    attachments?: { id: string }[]
  } | null
  const agentAttachmentId = uploaded?.attachments?.[0]?.id
  if (!agentAttachmentId) throw new Error('The file was not kept.')
  return agentAttachmentId
}

// --- addresses ---------------------------------------------------------------

export const SHOPPING_PATH = '/shopping'

export const shoppingListAddress = (shoppingListId: string, tab?: ShoppingTab) =>
  `/shopping/lists/${encodeURIComponent(shoppingListId)}${tab && tab !== 'to-buy' ? `?tab=${tab}` : ''}`

export function shoppingStoreAddress(storeName: string, shoppingListIds: string[] = []) {
  const address = `/shopping/stores/${encodeURIComponent(storeName)}`
  return shoppingListIds.length > 0 ? `${address}?lists=${shoppingListIds.map(encodeURIComponent).join(',')}` : address
}

export const shoppingReceiptAddress = (financeReceiptId: string) =>
  `/shopping/receipts/${encodeURIComponent(financeReceiptId)}`

// Where the Finance page opens one of its receipts.
export const financeReceiptAddress = (financeReceiptId: string) =>
  `/finance/receipts?receipt=${encodeURIComponent(financeReceiptId)}`

// storeKeyOf is the form every store name is compared in, as the server
// compares them: lower case, letters and digits only, so "Example Market" and
// "EXAMPLE-MARKET" are one store.
export function storeKeyOf(storeName: string): string {
  return storeName.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '')
}
