import { formatMoney } from '../../components/common'
import { PencilIcon, RestoreIcon, TrashIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { formatDay } from '../finance/financeApi'
import { ShoppingItem } from './shoppingApi'
import { groupShoppingItems, priceNumber, quantityText, ShoppingTab } from './shoppingItems'
import { ShoppingItemName, ShoppingPicture } from './shoppingParts'
import { ShoppingCardGroup, ShoppingItemCard } from './shoppingView'

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
                  <ListItemCheck shoppingItem={shoppingItem} tab={tab} isBusy={isBusy} onCheck={onCheck} />
                </td>
                <td className="shrink">
                  <ShoppingPicture item={shoppingItem} />
                </td>
                <td className="wrap">
                  <ShoppingItemName item={shoppingItem} />
                </td>
                {/* A count needed means nothing for an item not needed now. */}
                <td>{shoppingItem.shoppingItemState === 'not_needed' ? '' : quantityText(shoppingItem)}</td>
                <td>{priceText(shoppingItem)}</td>
                <td>{shoppingItem.lastBoughtAt ? formatDay(shoppingItem.lastBoughtAt) : ''}</td>
                <td className="shrink">
                  <div className="row-actions">
                    <ListItemActions
                      shoppingItem={shoppingItem}
                      tab={tab}
                      isBusy={isBusy}
                      onEdit={onEdit}
                      onRemove={onRemove}
                      onNeedAgain={onNeedAgain}
                    />
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

// ShoppingItemGrid is the same items as cards with big pictures, grouped under
// the same headings, for the pictures view.
export function ShoppingItemGrid({
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
  return (
    <div className="shopping-card-groups">
      {groupShoppingItems(shoppingItems).map((group) => (
        <ShoppingCardGroup
          key={`${group.storeName}\u0000${group.aisleName}`}
          heading={[group.storeName || t('shopping.anyStore'), group.aisleName].filter(Boolean).join(' · ')}
        >
          {group.shoppingItems.map((shoppingItem) => (
            <ShoppingItemCard
              key={shoppingItem.id}
              shoppingItem={shoppingItem}
              check={<ListItemCheck shoppingItem={shoppingItem} tab={tab} isBusy={isBusy} onCheck={onCheck} />}
              facts={[
                shoppingItem.shoppingItemState === 'not_needed'
                  ? ''
                  : t('shopping.needCount', { quantity: quantityText(shoppingItem) }),
                priceText(shoppingItem),
                shoppingItem.lastBoughtAt ? t('shopping.boughtOn', { day: formatDay(shoppingItem.lastBoughtAt) }) : '',
              ]}
              actions={
                <ListItemActions
                  shoppingItem={shoppingItem}
                  tab={tab}
                  isBusy={isBusy}
                  isCompact
                  onEdit={onEdit}
                  onRemove={onRemove}
                  onNeedAgain={onNeedAgain}
                />
              }
            />
          ))}
        </ShoppingCardGroup>
      ))}
    </div>
  )
}

function priceText(shoppingItem: ShoppingItem): string {
  return priceNumber(shoppingItem.unitPriceAmount) === null
    ? ''
    : formatMoney(Number(shoppingItem.unitPriceAmount), shoppingItem.currencyCode)
}

// ListItemCheck is the box that puts an item in the cart; a bought item on
// Not needed has none.
function ListItemCheck({
  shoppingItem,
  tab,
  isBusy,
  onCheck,
}: {
  shoppingItem: ShoppingItem
  tab: ShoppingTab
  isBusy: boolean
  onCheck: (shoppingItem: ShoppingItem, isChecked: boolean) => void
}) {
  const { t } = useTranslation()
  if (tab === 'not-needed') return null
  return (
    <input
      type="checkbox"
      checked={shoppingItem.shoppingItemState === 'in_cart'}
      disabled={isBusy}
      aria-label={t('shopping.checkItem', { itemDescription: shoppingItem.itemDescription })}
      onChange={(event) => onCheck(shoppingItem, event.target.checked)}
    />
  )
}

// ListItemActions are an item's buttons on a list: Need again on Not needed,
// edit, and remove. On a card Need again is an icon like the other two, so
// the three fit on one line of a card's width.
function ListItemActions({
  shoppingItem,
  tab,
  isBusy,
  isCompact = false,
  onEdit,
  onRemove,
  onNeedAgain,
}: {
  shoppingItem: ShoppingItem
  tab: ShoppingTab
  isBusy: boolean
  isCompact?: boolean
  onEdit: (shoppingItem: ShoppingItem) => void
  onRemove: (shoppingItem: ShoppingItem) => void
  onNeedAgain: (shoppingItem: ShoppingItem) => void
}) {
  const { t } = useTranslation()
  return (
    <>
      {tab === 'not-needed' && isCompact ? (
        <Tooltip label={t('shopping.needAgain')}>
          <button
            type="button"
            className="icon-action"
            aria-label={`${shoppingItem.itemDescription}: ${t('shopping.needAgain')}`}
            disabled={isBusy}
            onClick={() => onNeedAgain(shoppingItem)}
          >
            <RestoreIcon size={16} />
          </button>
        </Tooltip>
      ) : null}
      {tab === 'not-needed' && !isCompact ? (
        <button type="button" className="button-quiet" disabled={isBusy} onClick={() => onNeedAgain(shoppingItem)}>
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
    </>
  )
}
