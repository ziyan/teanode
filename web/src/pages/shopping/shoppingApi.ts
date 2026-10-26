import { graphql } from '../../api'

// The shopping lists' documents and the shapes they return. The names are the
// server's (internal/api/v1api/apigraph/shopping.go), and every part of the
// Shopping page goes through here so that a field is spelled once.

export type ShoppingItemState = 'needed' | 'in_cart' | 'not_needed'

export interface ShoppingList {
  id: string
  userId: string
  listName: string
  ownerName: string
  ownerUsername: string
  // Only the owner shares a list further or deletes it.
  isOwner: boolean
  // Whether anybody besides the owner has it.
  isShared: boolean
  // Whether the caller is reminded about it on arriving at a store.
  isReminded: boolean
  neededCount: number
  inCartCount: number
  notNeededCount: number
  updatedAt: string
}

export interface ShoppingItem {
  id: string
  shoppingListId: string
  listName: string
  itemDescription: string
  itemDetails: string
  // Empty for an item that can be got at any store.
  storeName: string
  storeKey: string
  isStoreBrand: boolean
  aisleName: string
  aisleRank: number
  itemRank: number
  // A number written as text; the count needed.
  neededQuantity: string
  quantityUnit: string
  // The latest price of one, a number written as text; empty when unknown.
  unitPriceAmount: string
  currencyCode: string
  shoppingItemState: ShoppingItemState
  cartedAt: string | null
  cartedByName: string
  cartedByUserId: string
  // Set while the item is out of stock; the item is still needed.
  outOfStockAt: string | null
  outOfStockByName: string
  outOfStockStoreName: string
  lastBoughtAt: string | null
  // Where the picture is served from, a full path under /api/v1; empty when
  // the item has none.
  picturePath: string
  pictureSource: string
  picturePageUrl: string
  pictureSearchedAt: string | null
  updatedAt: string
}

export interface ShoppingTrip {
  id: string
  userId: string
  shopperName: string
  shopperUsername: string
  storeName: string
  storeKey: string
  shoppingListIds: string[]
  startedAt: string
  finishedAt: string | null
}

export interface ShoppingStore {
  storeName: string
  storeKey: string
  neededCount: number
  isReminded: boolean
}

export interface ShoppingStoreList {
  shoppingListId: string
  listName: string
  neededCount: number
  isIncluded: boolean
}

export interface ShoppingStoreView {
  storeName: string
  isReminded: boolean
  shoppingItems: ShoppingItem[]
  shoppingLists: ShoppingStoreList[]
  shoppingTrips: ShoppingTrip[]
}

export interface ShoppingItemPrice {
  id: string
  shoppingItemId: string
  storeName: string
  unitPriceAmount: string
  currencyCode: string
  quantity: string
  quantityUnit: string
  observedOn: string
  // 'receipt' or 'person'.
  priceSource: string
  financeReceiptId: string
}

export interface ShoppingListShare {
  id: string
  shoppingListId: string
  granteeUserId: string
  granteeCircleId: string
  granteeName: string
  granteeUsername: string
  granteeCircleName: string
}

// An item as the model read it from pasted text or a picture, and the item
// already on the list that adding it would merge into.
export interface ShoppingImportItem {
  itemDescription: string
  itemDetails: string
  neededQuantity: string
  quantityUnit: string
  aisleName: string
  storeName: string
  isStoreBrand: boolean
  mergedShoppingItemId: string
  mergedShoppingItemState: string
  mergedNeededQuantity: string
}

// One item to add, as AddShoppingItems takes it.
export interface ShoppingItemEntry {
  itemDescription: string
  itemDetails?: string
  storeName?: string
  isStoreBrand?: boolean
  aisleName?: string
  neededQuantity?: string
  quantityUnit?: string
  unitPriceAmount?: string
  currencyCode?: string
}

export type ReceiptMatchState = 'applied' | 'proposed' | 'dismissed'

export interface ShoppingReceiptMatch {
  id: string
  financeReceiptId: string
  financeReceiptLineId: string
  shoppingItemId: string
  receiptMatchState: ReceiptMatchState
  matchedQuantity: string
}

export interface ShoppingReceiptLine {
  financeReceiptLineId: string
  lineNumber: number
  description: string
  quantity: string
  lineAmount: string
  shoppingReceiptMatches: { shoppingReceiptMatch: ShoppingReceiptMatch; shoppingItem: ShoppingItem | null }[]
}

export interface ShoppingReceipt {
  financeReceiptId: string
  merchantName: string
  purchasedOn: string
  totalAmount: string
  currencyCode: string
  shoppingReceiptLines: ShoppingReceiptLine[]
  stillNeededItems: ShoppingItem[]
}

export const SHOPPING_LIST_FIELDS = `id userId listName ownerName ownerUsername isOwner isShared isReminded
  neededCount inCartCount notNeededCount updatedAt`

export const SHOPPING_ITEM_FIELDS = `id shoppingListId listName itemDescription itemDetails storeName storeKey
  isStoreBrand aisleName aisleRank itemRank neededQuantity quantityUnit unitPriceAmount currencyCode
  shoppingItemState cartedAt cartedByName cartedByUserId outOfStockAt outOfStockByName outOfStockStoreName
  lastBoughtAt picturePath pictureSource picturePageUrl pictureSearchedAt updatedAt`

export const SHOPPING_TRIP_FIELDS = `id userId shopperName shopperUsername storeName storeKey shoppingListIds
  startedAt finishedAt`

// The most items one read of a list answers; a longer list is read in parts.
export const SHOPPING_PAGE_SIZE = 500

export const SHOPPING_EVENTS = `subscription { ShoppingEvents { shoppingListId } }`

const LIST_SHOPPING_LISTS = `query { ListShoppingLists { ${SHOPPING_LIST_FIELDS} } }`

const GET_SHOPPING_LIST = `query ($shoppingListId: String!, $offset: Int, $pageSize: Int) {
  GetShoppingList(shoppingListId: $shoppingListId, offset: $offset, pageSize: $pageSize) {
    totalCount
    shoppingList { ${SHOPPING_LIST_FIELDS} }
    shoppingItems { ${SHOPPING_ITEM_FIELDS} }
    shoppingTrips { ${SHOPPING_TRIP_FIELDS} }
  }
}`

const LIST_SHOPPING_STORES = `query { ListShoppingStores { storeName storeKey neededCount isReminded } }`

const LIST_SHOPPING_STORE_ITEMS = `query ($storeName: String!, $shoppingListIds: [String!]) {
  ListShoppingStoreItems(storeName: $storeName, shoppingListIds: $shoppingListIds) {
    storeName
    isReminded
    shoppingItems { ${SHOPPING_ITEM_FIELDS} }
    shoppingLists { shoppingListId listName neededCount isIncluded }
    shoppingTrips { ${SHOPPING_TRIP_FIELDS} }
  }
}`

const LIST_SHOPPING_ITEM_PRICES = `query ($shoppingItemId: String!) {
  ListShoppingItemPrices(shoppingItemId: $shoppingItemId) {
    id shoppingItemId storeName unitPriceAmount currencyCode quantity quantityUnit observedOn priceSource
    financeReceiptId
  }
}`

const LIST_SHOPPING_LIST_SHARES = `query ($shoppingListId: String!) {
  ListShoppingListShares(shoppingListId: $shoppingListId) {
    id shoppingListId granteeUserId granteeCircleId granteeName granteeUsername granteeCircleName
  }
}`

const PREVIEW_SHOPPING_IMPORT = `query ($shoppingListId: String, $importText: String, $agentAttachmentIds: [String!]) {
  PreviewShoppingImport(shoppingListId: $shoppingListId, importText: $importText, agentAttachmentIds: $agentAttachmentIds) {
    shoppingItems {
      itemDescription itemDetails neededQuantity quantityUnit aisleName storeName isStoreBrand
      mergedShoppingItemId mergedShoppingItemState mergedNeededQuantity
    }
  }
}`

const LIST_SHOPPING_RECEIPT_MATCHES = `query ($financeReceiptId: String!) {
  ListShoppingReceiptMatches(financeReceiptId: $financeReceiptId) {
    financeReceiptId merchantName purchasedOn totalAmount currencyCode
    shoppingReceiptLines {
      financeReceiptLineId lineNumber description quantity lineAmount
      shoppingReceiptMatches {
        shoppingReceiptMatch {
          id financeReceiptId financeReceiptLineId shoppingItemId receiptMatchState matchedQuantity
        }
        shoppingItem { ${SHOPPING_ITEM_FIELDS} }
      }
    }
    stillNeededItems { ${SHOPPING_ITEM_FIELDS} }
  }
}`

export async function listShoppingLists(signal?: AbortSignal): Promise<ShoppingList[]> {
  const answer = await graphql<{ ListShoppingLists: ShoppingList[] | null }>(LIST_SHOPPING_LISTS, {}, signal)
  return answer.ListShoppingLists ?? []
}

export interface ShoppingListContent {
  shoppingList: ShoppingList
  shoppingItems: ShoppingItem[]
  shoppingTrips: ShoppingTrip[]
}

// getShoppingList reads a whole list, in parts of SHOPPING_PAGE_SIZE items
// when it is longer than one. The tabs count what is in each state, which
// needs every item; the page then pages what it shows.
export async function getShoppingList(shoppingListId: string, signal?: AbortSignal): Promise<ShoppingListContent> {
  const shoppingItems: ShoppingItem[] = []
  let first: ShoppingListContent | null = null
  for (;;) {
    const answer = await graphql<{
      GetShoppingList: {
        totalCount: number
        shoppingList: ShoppingList
        shoppingItems: ShoppingItem[] | null
        shoppingTrips: ShoppingTrip[] | null
      } | null
    }>(GET_SHOPPING_LIST, { shoppingListId, offset: shoppingItems.length, pageSize: SHOPPING_PAGE_SIZE }, signal)
    const page = answer.GetShoppingList
    if (!page) throw new Error('not found')
    const pageItems = page.shoppingItems ?? []
    shoppingItems.push(...pageItems)
    first ??= { shoppingList: page.shoppingList, shoppingItems, shoppingTrips: page.shoppingTrips ?? [] }
    if (pageItems.length === 0 || shoppingItems.length >= page.totalCount) return first
  }
}

export async function listShoppingStores(signal?: AbortSignal): Promise<ShoppingStore[]> {
  const answer = await graphql<{ ListShoppingStores: ShoppingStore[] | null }>(LIST_SHOPPING_STORES, {}, signal)
  return answer.ListShoppingStores ?? []
}

export async function listShoppingStoreItems(
  storeName: string,
  shoppingListIds: string[],
  signal?: AbortSignal,
): Promise<ShoppingStoreView> {
  const answer = await graphql<{ ListShoppingStoreItems: ShoppingStoreView }>(
    LIST_SHOPPING_STORE_ITEMS,
    { storeName, shoppingListIds: shoppingListIds.length > 0 ? shoppingListIds : null },
    signal,
  )
  const view = answer.ListShoppingStoreItems
  return {
    ...view,
    shoppingItems: view.shoppingItems ?? [],
    shoppingLists: view.shoppingLists ?? [],
    shoppingTrips: view.shoppingTrips ?? [],
  }
}

export async function listShoppingItemPrices(
  shoppingItemId: string,
  signal?: AbortSignal,
): Promise<ShoppingItemPrice[]> {
  const answer = await graphql<{ ListShoppingItemPrices: ShoppingItemPrice[] | null }>(
    LIST_SHOPPING_ITEM_PRICES,
    { shoppingItemId },
    signal,
  )
  return answer.ListShoppingItemPrices ?? []
}

export async function listShoppingListShares(
  shoppingListId: string,
  signal?: AbortSignal,
): Promise<ShoppingListShare[]> {
  const answer = await graphql<{ ListShoppingListShares: ShoppingListShare[] | null }>(
    LIST_SHOPPING_LIST_SHARES,
    { shoppingListId },
    signal,
  )
  return answer.ListShoppingListShares ?? []
}

export async function previewShoppingImport(
  shoppingListId: string | null,
  importText: string,
  agentAttachmentIds: string[],
): Promise<ShoppingImportItem[]> {
  const answer = await graphql<{ PreviewShoppingImport: { shoppingItems: ShoppingImportItem[] | null } | null }>(
    PREVIEW_SHOPPING_IMPORT,
    {
      shoppingListId,
      importText: importText.trim() || null,
      agentAttachmentIds: agentAttachmentIds.length > 0 ? agentAttachmentIds : null,
    },
  )
  return answer.PreviewShoppingImport?.shoppingItems ?? []
}

export async function listShoppingReceiptMatches(
  financeReceiptId: string,
  signal?: AbortSignal,
): Promise<ShoppingReceipt | null> {
  const answer = await graphql<{ ListShoppingReceiptMatches: ShoppingReceipt | null }>(
    LIST_SHOPPING_RECEIPT_MATCHES,
    { financeReceiptId },
    signal,
  )
  const receipt = answer.ListShoppingReceiptMatches
  if (!receipt) return null
  return {
    ...receipt,
    shoppingReceiptLines: (receipt.shoppingReceiptLines ?? []).map((line) => ({
      ...line,
      shoppingReceiptMatches: line.shoppingReceiptMatches ?? [],
    })),
    stillNeededItems: receipt.stillNeededItems ?? [],
  }
}

// --- changes ---------------------------------------------------------------

export async function createShoppingList(listName: string): Promise<ShoppingList> {
  const answer = await graphql<{ CreateShoppingList: ShoppingList }>(
    `mutation ($listName: String!) { CreateShoppingList(listName: $listName) { ${SHOPPING_LIST_FIELDS} } }`,
    { listName },
  )
  return answer.CreateShoppingList
}

export async function renameShoppingList(shoppingListId: string, listName: string): Promise<void> {
  await graphql(
    `
      mutation ($shoppingListId: String!, $listName: String!) {
        RenameShoppingList(shoppingListId: $shoppingListId, listName: $listName) {
          id
        }
      }
    `,
    { shoppingListId, listName },
  )
}

export async function deleteShoppingList(shoppingListId: string): Promise<void> {
  await graphql(
    `
      mutation ($shoppingListId: String!) {
        DeleteShoppingList(shoppingListId: $shoppingListId)
      }
    `,
    {
      shoppingListId,
    },
  )
}

export async function shareShoppingList(
  shoppingListId: string,
  grantee: { granteeUserId: string } | { granteeCircleId: string },
): Promise<void> {
  await graphql(
    `
      mutation ($shoppingListId: String!, $granteeUserId: String, $granteeCircleId: String) {
        ShareShoppingList(
          shoppingListId: $shoppingListId
          granteeUserId: $granteeUserId
          granteeCircleId: $granteeCircleId
        ) {
          id
        }
      }
    `,
    { shoppingListId, ...grantee },
  )
}

export async function unshareShoppingList(shoppingListShareId: string): Promise<void> {
  await graphql(
    `
      mutation ($shoppingListShareId: String!) {
        UnshareShoppingList(shoppingListShareId: $shoppingListShareId)
      }
    `,
    { shoppingListShareId },
  )
}

export async function setShoppingListReminder(shoppingListId: string, isReminded: boolean): Promise<void> {
  await graphql(
    `
      mutation ($shoppingListId: String!, $isReminded: Boolean!) {
        SetShoppingListReminder(shoppingListId: $shoppingListId, isReminded: $isReminded)
      }
    `,
    { shoppingListId, isReminded },
  )
}

export async function setShoppingStoreReminder(storeName: string, isReminded: boolean): Promise<void> {
  await graphql(
    `
      mutation ($storeName: String!, $isReminded: Boolean!) {
        SetShoppingStoreReminder(storeName: $storeName, isReminded: $isReminded)
      }
    `,
    { storeName, isReminded },
  )
}

export async function addShoppingItems(
  shoppingListId: string,
  shoppingItems: ShoppingItemEntry[],
): Promise<{ isMerged: boolean; shoppingItem: ShoppingItem | null }[]> {
  const answer = await graphql<{
    AddShoppingItems: { isMerged: boolean; shoppingItem: ShoppingItem | null }[] | null
  }>(
    `mutation ($shoppingListId: String!, $shoppingItems: [ShoppingItemEntryInput!]!) {
       AddShoppingItems(shoppingListId: $shoppingListId, shoppingItems: $shoppingItems) {
         isMerged
         shoppingItem { ${SHOPPING_ITEM_FIELDS} }
       }
     }`,
    { shoppingListId, shoppingItems },
  )
  return answer.AddShoppingItems ?? []
}

export interface ShoppingItemChange {
  itemDescription?: string
  itemDetails?: string
  storeName?: string
  isStoreBrand?: boolean
  aisleName?: string
  neededQuantity?: string
  quantityUnit?: string
}

export async function updateShoppingItem(shoppingItemId: string, change: ShoppingItemChange): Promise<void> {
  await graphql(
    `
      mutation (
        $shoppingItemId: String!
        $itemDescription: String
        $itemDetails: String
        $storeName: String
        $isStoreBrand: Boolean
        $aisleName: String
        $neededQuantity: String
        $quantityUnit: String
      ) {
        UpdateShoppingItem(
          shoppingItemId: $shoppingItemId
          itemDescription: $itemDescription
          itemDetails: $itemDetails
          storeName: $storeName
          isStoreBrand: $isStoreBrand
          aisleName: $aisleName
          neededQuantity: $neededQuantity
          quantityUnit: $quantityUnit
        ) {
          id
        }
      }
    `,
    { shoppingItemId, ...change },
  )
}

export async function removeShoppingItems(shoppingItemIds: string[]): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemIds: [String!]!) {
        RemoveShoppingItems(shoppingItemIds: $shoppingItemIds)
      }
    `,
    {
      shoppingItemIds,
    },
  )
}

export async function checkShoppingItem(shoppingItemId: string, isChecked: boolean): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!, $isChecked: Boolean!) {
        CheckShoppingItem(shoppingItemId: $shoppingItemId, isChecked: $isChecked) {
          id
        }
      }
    `,
    { shoppingItemId, isChecked },
  )
}

export async function setShoppingItemNeed(
  shoppingItemId: string,
  isNeeded: boolean,
  neededQuantity?: string,
): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!, $isNeeded: Boolean!, $neededQuantity: String) {
        SetShoppingItemNeed(shoppingItemId: $shoppingItemId, isNeeded: $isNeeded, neededQuantity: $neededQuantity) {
          id
        }
      }
    `,
    { shoppingItemId, isNeeded, neededQuantity },
  )
}

export async function setShoppingItemOutOfStock(
  shoppingItemId: string,
  isOutOfStock: boolean,
  storeName?: string,
): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!, $isOutOfStock: Boolean!, $storeName: String) {
        SetShoppingItemOutOfStock(shoppingItemId: $shoppingItemId, isOutOfStock: $isOutOfStock, storeName: $storeName) {
          id
        }
      }
    `,
    { shoppingItemId, isOutOfStock, storeName },
  )
}

export async function recordShoppingItemPrice(
  shoppingItemId: string,
  unitPriceAmount: string,
  storeName: string,
  currencyCode: string,
): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!, $unitPriceAmount: String!, $storeName: String, $currencyCode: String) {
        RecordShoppingItemPrice(
          shoppingItemId: $shoppingItemId
          unitPriceAmount: $unitPriceAmount
          storeName: $storeName
          currencyCode: $currencyCode
        ) {
          id
        }
      }
    `,
    { shoppingItemId, unitPriceAmount, storeName: storeName || null, currencyCode: currencyCode || null },
  )
}

export async function findShoppingItemPicture(shoppingItemId: string): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!) {
        FindShoppingItemPicture(shoppingItemId: $shoppingItemId)
      }
    `,
    {
      shoppingItemId,
    },
  )
}

export async function setShoppingItemPicture(shoppingItemId: string, agentAttachmentId: string | null): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!, $agentAttachmentId: String) {
        SetShoppingItemPicture(shoppingItemId: $shoppingItemId, agentAttachmentId: $agentAttachmentId) {
          id
        }
      }
    `,
    { shoppingItemId, agentAttachmentId },
  )
}

export async function startShopping(storeName: string, shoppingListIds: string[]): Promise<ShoppingTrip> {
  const answer = await graphql<{ StartShopping: ShoppingTrip }>(
    `mutation ($storeName: String, $shoppingListIds: [String!]!) {
       StartShopping(storeName: $storeName, shoppingListIds: $shoppingListIds) { ${SHOPPING_TRIP_FIELDS} }
     }`,
    { storeName, shoppingListIds },
  )
  return answer.StartShopping
}

export async function finishShopping(shoppingTripId: string): Promise<void> {
  await graphql(
    `
      mutation ($shoppingTripId: String!) {
        FinishShopping(shoppingTripId: $shoppingTripId) {
          id
        }
      }
    `,
    { shoppingTripId },
  )
}

export async function matchShoppingReceipt(financeReceiptId: string): Promise<void> {
  await graphql(
    `
      mutation ($financeReceiptId: String!) {
        MatchShoppingReceipt(financeReceiptId: $financeReceiptId)
      }
    `,
    {
      financeReceiptId,
    },
  )
}

export async function decideShoppingReceiptMatch(shoppingReceiptMatchId: string, isApplied: boolean): Promise<void> {
  await graphql(
    `
      mutation ($shoppingReceiptMatchId: String!, $isApplied: Boolean!) {
        DecideShoppingReceiptMatch(shoppingReceiptMatchId: $shoppingReceiptMatchId, isApplied: $isApplied) {
          id
        }
      }
    `,
    { shoppingReceiptMatchId, isApplied },
  )
}

export async function matchShoppingReceiptLine(shoppingItemId: string, financeReceiptLineId: string): Promise<void> {
  await graphql(
    `
      mutation ($shoppingItemId: String!, $financeReceiptLineId: String!) {
        MatchShoppingReceiptLine(shoppingItemId: $shoppingItemId, financeReceiptLineId: $financeReceiptLineId) {
          id
        }
      }
    `,
    { shoppingItemId, financeReceiptLineId },
  )
}

export async function undoShoppingReceipt(financeReceiptId: string): Promise<void> {
  await graphql(
    `
      mutation ($financeReceiptId: String!) {
        UndoShoppingReceipt(financeReceiptId: $financeReceiptId)
      }
    `,
    {
      financeReceiptId,
    },
  )
}

export async function addShoppingItemFromReceiptLine(
  financeReceiptLineId: string,
  shoppingListId: string,
): Promise<void> {
  await graphql(
    `
      mutation ($financeReceiptLineId: String!, $shoppingListId: String!) {
        AddShoppingItemFromReceiptLine(financeReceiptLineId: $financeReceiptLineId, shoppingListId: $shoppingListId) {
          id
        }
      }
    `,
    { financeReceiptLineId, shoppingListId },
  )
}
