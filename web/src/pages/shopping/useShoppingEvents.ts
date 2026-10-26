import { useEffect, useRef } from 'react'

import { listenToShoppingEvents } from './shoppingItems'

// useShoppingEvents calls the handler with the id of each list that changes,
// and with an empty id when the live feed was down and anything may have: the
// page then reads again what it shows.
export function useShoppingEvents(onEvent: (shoppingListId: string) => void) {
  const handler = useRef(onEvent)
  handler.current = onEvent
  useEffect(() => listenToShoppingEvents((shoppingListId) => handler.current(shoppingListId)), [])
}
