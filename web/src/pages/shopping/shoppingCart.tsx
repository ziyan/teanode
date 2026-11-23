import { useRef, useState } from 'react'
import { NavLink } from 'react-router-dom'

import { ErrorMessage, formatMoney, Loading } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { useSession } from '../../session'
import {
  checkShoppingItem,
  finishShopping,
  getShoppingList,
  listShoppingStoreItems,
  resumeShopping,
  ShoppingItem,
  ShoppingTrip,
} from './shoppingApi'
import {
  cartEstimate,
  isInOwnCart,
  lineAmount,
  MAXIMUM_UPLOAD_BYTES,
  quantityText,
  readShoppingReceipt,
  shoppingListAddress,
  shoppingStoreAddress,
} from './shoppingItems'
import { ShoppingCheck, ShoppingPicture } from './shoppingParts'

// The parts of the shopping flow that more than one screen draws: the cart
// that Done shopping opens, the row that offers to resume a trip finished by
// mistake, and the row that says the person is shopping.

// loadOwnCart reads the items in the person's own cart: across the lists of
// their trip when it is at a store, else at the store or on the list the page
// shows.
export async function loadOwnCart(
  userId: string | undefined,
  ownTrip: ShoppingTrip | null,
  shoppingListId: string | null,
  storeName: string | null,
): Promise<ShoppingItem[]> {
  let shoppingItems: ShoppingItem[] = []
  if (ownTrip?.storeName) {
    shoppingItems = (await listShoppingStoreItems(ownTrip.storeName, ownTrip.shoppingListIds)).shoppingItems
  } else if (storeName) {
    shoppingItems = (await listShoppingStoreItems(storeName, [])).shoppingItems
  } else if (shoppingListId) {
    shoppingItems = (await getShoppingList(shoppingListId)).shoppingItems
  }
  return shoppingItems.filter((item) => isInOwnCart(item, userId))
}

// useReceiptReader answers a function that sends a receipt to be read, after
// checking its size. It answers whether the receipt went; a failure is said
// in a toast.
export function useReceiptReader(): (file: File | undefined) => Promise<boolean> {
  const { t } = useTranslation()
  const toast = useToast()
  return async (file) => {
    if (!file) return false
    if (file.size > MAXIMUM_UPLOAD_BYTES) {
      toast.failed(t('shopping.fileTooLarge', { name: file.name, size: String(MAXIMUM_UPLOAD_BYTES >> 20) }))
      return false
    }
    try {
      await readShoppingReceipt(file)
      return true
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
      return false
    }
  }
}

// ShoppingCartDialog is what Done shopping opens: the items in the person's
// own cart, each with a check that takes it out, what they should come to,
// and the two ways to finish. Closing it keeps shopping.
export function ShoppingCartDialog({
  ownTrip,
  shoppingListId,
  storeName,
  onChanged,
  onFinished,
  onClose,
}: {
  ownTrip: ShoppingTrip | null
  // The list or the store the page shows, for a cart read with no trip at a
  // store.
  shoppingListId: string | null
  storeName: string | null
  // An item was taken out of the cart.
  onChanged: () => void
  onFinished: () => void
  onClose: () => void
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const session = useSession()
  const readReceipt = useReceiptReader()
  const cart = useQuery(
    () => loadOwnCart(session.userId, ownTrip, shoppingListId, storeName),
    [ownTrip?.id, shoppingListId, storeName],
    {
      refresh: false,
    },
  )
  const [takenOutIds, setTakenOutIds] = useState<string[]>([])
  const [isBusy, setIsBusy] = useState(false)
  const [isConfirming, setIsConfirming] = useState(false)
  const receiptInput = useRef<HTMLInputElement>(null)

  const cartItems = (cart.data ?? []).filter((item) => !takenOutIds.includes(item.id))
  const isListNamed = new Set(cartItems.map((item) => item.shoppingListId)).size > 1
  const estimate = cartEstimate(cartItems)
  const itemsText = plural(cartItems.length, { one: 'shopping.cartCountOne', other: 'shopping.cartCountOther' })
  const summary = estimate
    ? t('shopping.cartSummary', { items: itemsText, amount: formatMoney(estimate.amount, estimate.currencyCode) })
    : itemsText

  async function takeOut(shoppingItem: ShoppingItem) {
    setTakenOutIds((previous) => [...previous, shoppingItem.id])
    try {
      await checkShoppingItem(shoppingItem.id, false)
      onChanged()
    } catch (caught) {
      setTakenOutIds((previous) => previous.filter((each) => each !== shoppingItem.id))
      toast.failure(caught, t('shopping.failed'))
    }
  }

  async function finish(file: File | undefined) {
    setIsBusy(true)
    try {
      if (file && !(await readReceipt(file))) return
      await finishShopping(ownTrip?.id)
      toast.done(file ? t('shopping.shoppingDoneReceipt') : t('shopping.shoppingDone'))
      onFinished()
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
      setIsConfirming(false)
    }
  }

  return (
    <>
      <ConfirmDialog
        title={t('shopping.cartTitle')}
        body={
          <div className="shopping-cart">
            <p className="muted">{cart.data ? summary : ''}</p>
            <ErrorMessage error={cart.error} />
            {cart.loading && !cart.data ? <Loading /> : null}
            {cart.data && cartItems.length === 0 ? <p className="muted">{t('shopping.cartEmpty')}</p> : null}
            <ul className="shopping-cart-items">
              {cartItems.map((item) => {
                const amount = lineAmount(item)
                return (
                  <li key={item.id} className="shopping-cart-item">
                    <ShoppingCheck
                      isChecked
                      label={t('shopping.takeOutOfCart', { itemDescription: item.itemDescription })}
                      onChange={() => void takeOut(item)}
                    />
                    <ShoppingPicture item={item} />
                    <div className="shopping-cart-words">
                      <strong>{item.itemDescription}</strong>
                      <span className="muted">
                        {[quantityText(item), isListNamed ? item.listName : ''].filter(Boolean).join(' · ')}
                      </span>
                    </div>
                    <span className="shopping-cart-price">
                      {amount === null ? '' : formatMoney(amount, item.currencyCode)}
                    </span>
                  </li>
                )
              })}
            </ul>
            <input
              ref={receiptInput}
              type="file"
              accept="image/*,application/pdf"
              hidden
              aria-label={t('shopping.receiptFile')}
              onChange={(event) => {
                void finish(event.target.files?.[0])
                event.target.value = ''
              }}
            />
          </div>
        }
        confirmLabel={t('shopping.uploadTheReceipt')}
        destructive={false}
        busy={isBusy}
        wide
        otherAction={
          <button type="button" disabled={isBusy} onClick={() => setIsConfirming(true)}>
            {t('shopping.finishWithoutReceipt')}
          </button>
        }
        onConfirm={() => receiptInput.current?.click()}
        onClose={() => {
          if (!isConfirming) onClose()
        }}
      />
      {isConfirming ? (
        <ConfirmDialog
          title={t('shopping.finishWithoutReceiptTitle')}
          body={
            cartItems.length === 0
              ? t('shopping.finishWithoutReceiptBodyEmpty')
              : plural(cartItems.length, {
                  one: 'shopping.finishWithoutReceiptBodyOne',
                  other: 'shopping.finishWithoutReceiptBodyOther',
                })
          }
          confirmLabel={t('shopping.finish')}
          destructive={false}
          busy={isBusy}
          onConfirm={() => void finish(undefined)}
          onClose={() => setIsConfirming(false)}
        />
      ) : null}
    </>
  )
}

// timeOfDay writes the moment a trip finished the way the person reads a
// clock.
function timeOfDay(finishedAt: string): string {
  return new Date(finishedAt).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// ShoppingResumeRow offers to open a trip finished by mistake again, with
// what it bought back in the cart.
export function ShoppingResumeRow({
  shoppingTrip,
  onResumed,
}: {
  shoppingTrip: ShoppingTrip
  onResumed: (shoppingTrip: ShoppingTrip) => void
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)
  const time = shoppingTrip.finishedAt ? timeOfDay(shoppingTrip.finishedAt) : ''

  async function resume() {
    setIsBusy(true)
    try {
      await resumeShopping(shoppingTrip.id)
      onResumed(shoppingTrip)
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <div className="shopping-notice">
      <div className="shopping-notice-words">
        <strong>
          {shoppingTrip.storeName
            ? t('shopping.resumeLine', { storeName: shoppingTrip.storeName, time })
            : t('shopping.resumeLineNoStore', { time })}
        </strong>
        <span className="muted">
          {plural(shoppingTrip.boughtItemCount, {
            one: 'shopping.resumeItemsOne',
            other: 'shopping.resumeItemsOther',
          })}
        </span>
      </div>
      <button type="button" className="primary" disabled={isBusy} onClick={() => void resume()}>
        {t('shopping.resume')}
      </button>
    </div>
  )
}

// ShoppingOwnTripRow says the person is shopping, and opens the store.
export function ShoppingOwnTripRow({ shoppingTrip, cartCount }: { shoppingTrip: ShoppingTrip; cartCount: number }) {
  const { t, plural } = useTranslation()
  const address = shoppingTrip.storeName
    ? shoppingStoreAddress(shoppingTrip.storeName, shoppingTrip.shoppingListIds)
    : shoppingListAddress(shoppingTrip.shoppingListIds[0] ?? '')
  const inCart = plural(cartCount, { one: 'shopping.inYourCartOne', other: 'shopping.inYourCartOther' })
  return (
    <NavLink to={address} className="shopping-notice shopping-notice-link">
      <span className="shopping-notice-words">
        <strong>
          {shoppingTrip.storeName
            ? [t('shopping.shoppingAt', { storeName: shoppingTrip.storeName }), inCart].join(' · ')
            : t('shopping.tripSelfNoStore')}
        </strong>
      </span>
    </NavLink>
  )
}
