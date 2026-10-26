import { formatMoney } from '../../components/common'
import { PencilIcon, TrashIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { formatDay } from '../finance/financeApi'
import { ShoppingItem } from './shoppingApi'
import { groupShoppingItems, priceNumber, quantityText, ShoppingTab } from './shoppingItems'
import { ShoppingItemName, ShoppingPicture } from './shoppingParts'

// The items of one tab of a list, as a table grouped under a heading for each
// store and aisle ("Example Market · Frozen"), the items that can be got
// anywhere under "Any store". A table stays a table on a phone: it scrolls
// sideways inside its box.

export function ShoppingItemTable({
  shoppingItems,
  tab,
  isBusy,
  onCheck,
  onEdit,
  onRemove,
  onNeedAgain,
}: {
  shoppingItems: ShoppingItem[]
  tab: ShoppingTab
  isBusy: boolean
  onCheck: (shoppingItem: ShoppingItem, isChecked: boolean) => void
  onEdit: (shoppingItem: ShoppingItem) => void
  onRemove: (shoppingItem: ShoppingItem) => void
  onNeedAgain: (shoppingItem: ShoppingItem) => void
}) {
  const { t } = useTranslation()
  const groups = groupShoppingItems(shoppingItems)
  const columnCount = 7
  return (
    <div className="table-wrap">
      <table className="shopping-table">
        <thead>
          <tr>
            <th className="shrink">
              <span className="visually-hidden">{t('shopping.checkColumn')}</span>
            </th>
            <th className="shrink">
              <span className="visually-hidden">{t('shopping.pictureColumn')}</span>
            </th>
            <th>{t('shopping.itemColumn')}</th>
            <th>{t('shopping.need')}</th>
            <th>{t('shopping.unitPrice')}</th>
            <th>{t('shopping.lastBought')}</th>
            <th className="shrink">
              <span className="visually-hidden">{t('shopping.actionsColumn')}</span>
            </th>
          </tr>
        </thead>
        {groups.map((group) => (
          <tbody key={`${group.storeName}\u0000${group.aisleName}`}>
            <tr className="shopping-group-row">
              <th colSpan={columnCount} scope="colgroup">
                {[group.storeName || t('shopping.anyStore'), group.aisleName].filter(Boolean).join(' · ')}
              </th>
            </tr>
            {group.shoppingItems.map((shoppingItem) => (
              <tr
                key={shoppingItem.id}
                className={shoppingItem.shoppingItemState === 'in_cart' ? 'shopping-row is-in-cart' : 'shopping-row'}
              >
                <td className="shrink">
                  {tab === 'not-needed' ? null : (
                    <input
                      type="checkbox"
                      checked={shoppingItem.shoppingItemState === 'in_cart'}
                      disabled={isBusy}
                      aria-label={t('shopping.checkItem', { itemDescription: shoppingItem.itemDescription })}
                      onChange={(event) => onCheck(shoppingItem, event.target.checked)}
                    />
                  )}
                </td>
                <td className="shrink">
                  <ShoppingPicture item={shoppingItem} />
                </td>
                <td className="wrap">
                  <ShoppingItemName item={shoppingItem} />
                </td>
                <td>{quantityText(shoppingItem)}</td>
                <td>
                  {priceNumber(shoppingItem.unitPriceAmount) === null
                    ? ''
                    : formatMoney(Number(shoppingItem.unitPriceAmount), shoppingItem.currencyCode)}
                </td>
                <td>{shoppingItem.lastBoughtAt ? formatDay(shoppingItem.lastBoughtAt) : ''}</td>
                <td className="shrink">
                  <div className="row-actions">
                    {tab === 'not-needed' ? (
                      <button
                        type="button"
                        className="button-quiet"
                        disabled={isBusy}
                        onClick={() => onNeedAgain(shoppingItem)}
                      >
                        {t('shopping.needAgain')}
                      </button>
                    ) : null}
                    <Tooltip label={t('shopping.editItem')}>
                      <button
                        type="button"
                        className="icon-action"
                        aria-label={`${shoppingItem.itemDescription}: ${t('shopping.editItem')}`}
                        onClick={() => onEdit(shoppingItem)}
                      >
                        <PencilIcon size={16} />
                      </button>
                    </Tooltip>
                    <Tooltip label={t('common.remove')}>
                      <button
                        type="button"
                        className="icon-action danger"
                        aria-label={`${shoppingItem.itemDescription}: ${t('common.remove')}`}
                        disabled={isBusy}
                        onClick={() => onRemove(shoppingItem)}
                      >
                        <TrashIcon size={16} />
                      </button>
                    </Tooltip>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        ))}
      </table>
    </div>
  )
}
