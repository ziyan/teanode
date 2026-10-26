import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { ErrorMessage, formatMoney } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { formatDay } from '../finance/financeApi'
import {
  addShoppingItems,
  findShoppingItemPicture,
  listShoppingItemPrices,
  listShoppingStores,
  recordShoppingItemPrice,
  removeShoppingItems,
  setShoppingItemOutOfStock,
  setShoppingItemPicture,
  ShoppingItem,
  ShoppingItemChange,
  ShoppingItemPrice,
  updateShoppingItem,
} from './shoppingApi'
import {
  financeReceiptAddress,
  isNumber,
  isPositiveNumber,
  MAXIMUM_UPLOAD_BYTES,
  plainNumber,
  priceNumber,
  uploadAgentAttachment,
} from './shoppingItems'
import { ShoppingPicture } from './shoppingParts'

// The dialog that makes an item or changes one: what it is, where to get it,
// how many are needed and what one costs. Changing one also shows its picture
// (to find another or to upload one's own) and what it has cost over time.
// Saving changes only what was edited; a changed price is remembered as a new
// price seen today rather than written over the old ones.

export type ShoppingItemDialogTarget = { shoppingListId: string; shoppingItem: null } | { shoppingItem: ShoppingItem }

export function ShoppingItemDialog({
  target,
  onSaved,
  onClose,
}: {
  target: ShoppingItemDialogTarget
  // Called after anything was changed, so that the page can read again.
  onSaved: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const item = target.shoppingItem
  const stores = useQuery(() => listShoppingStores(), [], { refresh: false })
  const [itemDescription, setItemDescription] = useState(item?.itemDescription ?? '')
  const [itemDetails, setItemDetails] = useState(item?.itemDetails ?? '')
  const [storeName, setStoreName] = useState(item?.storeName ?? '')
  const [isStoreBrand, setIsStoreBrand] = useState(item?.isStoreBrand ?? false)
  const [aisleName, setAisleName] = useState(item?.aisleName ?? '')
  const [neededQuantity, setNeededQuantity] = useState(item ? plainNumber(item.neededQuantity) : '1')
  const [quantityUnit, setQuantityUnit] = useState(item?.quantityUnit ?? '')
  const [unitPriceAmount, setUnitPriceAmount] = useState(item ? plainNumber(item.unitPriceAmount) : '')
  const [isBusy, setIsBusy] = useState(false)
  const [isRemoving, setIsRemoving] = useState(false)
  const pictureInput = useRef<HTMLInputElement>(null)

  const canSubmit =
    itemDescription.trim() !== '' &&
    isPositiveNumber(neededQuantity) &&
    (unitPriceAmount.trim() === '' || isNumber(unitPriceAmount))

  // run is one action of the dialog: busy while it works, a toast for how it
  // went. The dialog closes when it succeeded and said so.
  async function run(work: () => Promise<string>, shouldClose = true) {
    setIsBusy(true)
    try {
      toast.done(await work())
      onSaved()
      if (shouldClose) onClose()
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function save() {
    if (!item) {
      const shoppingListId = 'shoppingListId' in target ? target.shoppingListId : ''
      await run(async () => {
        const added = await addShoppingItems(shoppingListId, [
          {
            itemDescription: itemDescription.trim(),
            itemDetails: itemDetails.trim(),
            storeName: storeName.trim(),
            isStoreBrand,
            aisleName: aisleName.trim(),
            neededQuantity: neededQuantity.trim(),
            quantityUnit: quantityUnit.trim(),
            unitPriceAmount: unitPriceAmount.trim() || undefined,
          },
        ])
        return added[0]?.isMerged
          ? t('shopping.itemMerged', { itemDescription: itemDescription.trim() })
          : t('shopping.itemAdded', { itemDescription: itemDescription.trim() })
      })
      return
    }
    await run(async () => {
      const change: ShoppingItemChange = {}
      if (itemDescription.trim() !== item.itemDescription) change.itemDescription = itemDescription.trim()
      if (itemDetails.trim() !== item.itemDetails) change.itemDetails = itemDetails.trim()
      if (storeName.trim() !== item.storeName) change.storeName = storeName.trim()
      if (isStoreBrand !== item.isStoreBrand) change.isStoreBrand = isStoreBrand
      if (aisleName.trim() !== item.aisleName) change.aisleName = aisleName.trim()
      if (Number(neededQuantity) !== Number(item.neededQuantity)) change.neededQuantity = neededQuantity.trim()
      if (quantityUnit.trim() !== item.quantityUnit) change.quantityUnit = quantityUnit.trim()
      if (Object.keys(change).length > 0) await updateShoppingItem(item.id, change)
      const price = unitPriceAmount.trim()
      if (price !== '' && priceNumber(price) !== priceNumber(item.unitPriceAmount)) {
        await recordShoppingItemPrice(item.id, price, change.storeName ?? item.storeName, item.currencyCode)
      }
      return t('shopping.itemSaved', { itemDescription: itemDescription.trim() })
    })
  }

  async function chooseFile(file: File | undefined) {
    if (!item || !file) return
    if (file.size > MAXIMUM_UPLOAD_BYTES) {
      toast.failed(t('shopping.fileTooLarge', { name: file.name, size: String(MAXIMUM_UPLOAD_BYTES >> 20) }))
      return
    }
    await run(async () => {
      await setShoppingItemPicture(item.id, await uploadAgentAttachment(file))
      return t('shopping.pictureChanged')
    }, false)
  }

  if (isRemoving && item) {
    return (
      <ConfirmDialog
        title={t('shopping.removeTitle', { itemDescription: item.itemDescription })}
        body={t('shopping.removeBody')}
        confirmLabel={t('common.remove')}
        busy={isBusy}
        onConfirm={() =>
          void run(async () => {
            await removeShoppingItems([item.id])
            return t('shopping.itemRemoved', { itemDescription: item.itemDescription })
          })
        }
        onClose={() => setIsRemoving(false)}
      />
    )
  }

  return (
    <FormDialog
      wide
      title={item ? t('shopping.editItemTitle') : t('shopping.addItemTitle')}
      submitLabel={item ? t('common.save') : t('shopping.addItem')}
      busy={isBusy}
      canSubmit={canSubmit}
      onSubmit={() => void save()}
      onClose={onClose}
      otherAction={
        item ? (
          <>
            <button
              type="button"
              disabled={isBusy}
              onClick={() =>
                void run(async () => {
                  await setShoppingItemOutOfStock(item.id, !item.outOfStockAt)
                  return item.outOfStockAt
                    ? t('shopping.backInStockDone', { itemDescription: item.itemDescription })
                    : t('shopping.outOfStockDone', { itemDescription: item.itemDescription })
                })
              }
            >
              {item.outOfStockAt ? t('shopping.backInStock') : t('shopping.markOutOfStock')}
            </button>
            <button type="button" className="danger" disabled={isBusy} onClick={() => setIsRemoving(true)}>
              {t('common.remove')}
            </button>
          </>
        ) : undefined
      }
    >
      {item ? (
        <div className="shopping-picture-field">
          <ShoppingPicture item={item} isLarge />
          <div className="shopping-picture-buttons">
            <button
              type="button"
              disabled={isBusy}
              onClick={() =>
                void run(async () => {
                  await findShoppingItemPicture(item.id)
                  return t('shopping.pictureSearching')
                }, false)
              }
            >
              {t('shopping.findAnotherPicture')}
            </button>
            <button type="button" disabled={isBusy} onClick={() => pictureInput.current?.click()}>
              {t('shopping.uploadPhoto')}
            </button>
            <input
              ref={pictureInput}
              type="file"
              accept="image/*"
              hidden
              aria-label={t('shopping.uploadPhoto')}
              onChange={(event) => {
                void chooseFile(event.target.files?.[0])
                event.target.value = ''
              }}
            />
          </div>
        </div>
      ) : null}

      <label>
        {t('shopping.itemDescription')}
        <input value={itemDescription} maxLength={200} onChange={(event) => setItemDescription(event.target.value)} />
      </label>
      <label>
        {t('shopping.itemDetails')}
        <textarea
          rows={2}
          value={itemDetails}
          placeholder={t('shopping.itemDetailsPlaceholder')}
          onChange={(event) => setItemDetails(event.target.value)}
        />
      </label>
      <div className="row">
        <label>
          {t('shopping.store')}
          <input
            value={storeName}
            list="shopping-store-choices"
            placeholder={t('shopping.storePlaceholder')}
            onChange={(event) => setStoreName(event.target.value)}
          />
          <datalist id="shopping-store-choices">
            {(stores.data ?? []).map((store) => (
              <option key={store.storeKey} value={store.storeName} />
            ))}
          </datalist>
        </label>
        <label>
          {t('shopping.aisle')}
          <input value={aisleName} maxLength={120} onChange={(event) => setAisleName(event.target.value)} />
        </label>
      </div>
      <label className="checkbox">
        <input type="checkbox" checked={isStoreBrand} onChange={(event) => setIsStoreBrand(event.target.checked)} />
        <span>{t('shopping.isStoreBrand')}</span>
      </label>
      <div className="row">
        <label>
          {t('shopping.need')}
          <input
            inputMode="decimal"
            value={neededQuantity}
            onChange={(event) => setNeededQuantity(event.target.value)}
          />
        </label>
        <label>
          {t('shopping.unit')}
          <input
            value={quantityUnit}
            maxLength={40}
            placeholder={t('shopping.unitPlaceholder')}
            onChange={(event) => setQuantityUnit(event.target.value)}
          />
        </label>
        <label>
          {t('shopping.unitPrice')}
          <input
            inputMode="decimal"
            value={unitPriceAmount}
            onChange={(event) => setUnitPriceAmount(event.target.value)}
          />
        </label>
      </div>

      {item ? <PriceHistory shoppingItemId={item.id} /> : null}
    </FormDialog>
  )
}

// PriceHistory is what an item has cost: a line of the prices over time when
// there are two or more in one currency, and a table of every price seen.
function PriceHistory({ shoppingItemId }: { shoppingItemId: string }) {
  const { t } = useTranslation()
  const prices = useQuery(() => listShoppingItemPrices(shoppingItemId), [shoppingItemId], { refresh: false })
  const list = prices.data ?? []
  return (
    <section className="shopping-prices" aria-label={t('shopping.priceOverTime')}>
      <ErrorMessage error={prices.error} />
      <h4>{t('shopping.priceOverTime')}</h4>
      {prices.data && list.length === 0 ? <p className="muted">{t('shopping.noPrices')}</p> : null}
      <PriceChart prices={list} />
      <PriceRange prices={list} />
      {list.length > 0 ? (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>{t('shopping.priceDay')}</th>
                <th>{t('shopping.priceStore')}</th>
                <th>{t('shopping.unitPrice')}</th>
                <th>{t('shopping.priceSource')}</th>
              </tr>
            </thead>
            <tbody>
              {list.map((price) => (
                <tr key={price.id}>
                  <td>{formatDay(price.observedOn)}</td>
                  <td>{price.storeName || t('shopping.anyStore')}</td>
                  <td>{formatMoney(Number(price.unitPriceAmount), price.currencyCode)}</td>
                  <td>
                    {price.priceSource === 'receipt' && price.financeReceiptId ? (
                      <Link to={financeReceiptAddress(price.financeReceiptId)}>{t('shopping.priceFromReceipt')}</Link>
                    ) : price.priceSource === 'receipt' ? (
                      t('shopping.priceFromReceipt')
                    ) : (
                      t('shopping.priceByHand')
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  )
}

// PriceRange says in words what the line shows: the lowest and the highest
// price and the days they span, which the line alone does not.
function PriceRange({ prices }: { prices: ShoppingItemPrice[] }) {
  const { t } = useTranslation()
  const points = pricePoints(prices)
  if (points.length === 0) return null
  const amounts = points.map((point) => Number(point.price.unitPriceAmount))
  const currencyCode = points[0].price.currencyCode
  return (
    <p className="muted shopping-price-range">
      {t('shopping.priceRange', {
        lowest: formatMoney(Math.min(...amounts), currencyCode),
        highest: formatMoney(Math.max(...amounts), currencyCode),
        from: formatDay(points[0].price.observedOn),
        to: formatDay(points[points.length - 1].price.observedOn),
      })}
    </p>
  )
}

const CHART_WIDTH = 320
const CHART_HEIGHT = 80
const CHART_PADDING = 8

// pricePoints are the prices of the currency of the newest, oldest first,
// drawn in a box CHART_WIDTH wide. Prices in another currency are left out of
// the line (they stay in the table), since one scale cannot hold two.
export function pricePoints(prices: ShoppingItemPrice[]): { x: number; y: number; price: ShoppingItemPrice }[] {
  if (prices.length === 0) return []
  const newest = [...prices].sort((first, second) => second.observedOn.localeCompare(first.observedOn))[0]
  const sameCurrency = prices
    .filter((price) => price.currencyCode === newest.currencyCode && priceNumber(price.unitPriceAmount) !== null)
    .sort((first, second) => first.observedOn.localeCompare(second.observedOn))
  if (sameCurrency.length < 2) return []
  const amounts = sameCurrency.map((price) => Number(price.unitPriceAmount))
  const lowest = Math.min(...amounts)
  const highest = Math.max(...amounts)
  const spread = highest - lowest || 1
  return sameCurrency.map((price, index) => ({
    price,
    x: CHART_PADDING + (index / (sameCurrency.length - 1)) * (CHART_WIDTH - 2 * CHART_PADDING),
    y:
      CHART_HEIGHT -
      CHART_PADDING -
      ((Number(price.unitPriceAmount) - lowest) / spread) * (CHART_HEIGHT - 2 * CHART_PADDING),
  }))
}

function PriceChart({ prices }: { prices: ShoppingItemPrice[] }) {
  const { t } = useTranslation()
  const points = pricePoints(prices)
  if (points.length === 0) return null
  const first = points[0].price
  const last = points[points.length - 1].price
  return (
    <svg
      className="shopping-price-chart"
      viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
      role="img"
      aria-label={t('shopping.priceChartLabel', {
        from: formatMoney(Number(first.unitPriceAmount), first.currencyCode),
        to: formatMoney(Number(last.unitPriceAmount), last.currencyCode),
      })}
    >
      <polyline points={points.map((point) => `${point.x},${point.y}`).join(' ')} />
      {points.map((point) => (
        <circle key={point.price.id} cx={point.x} cy={point.y} r={3}>
          <title>{`${formatDay(point.price.observedOn)}: ${formatMoney(Number(point.price.unitPriceAmount), point.price.currencyCode)}`}</title>
        </circle>
      ))}
    </svg>
  )
}
