import { formatMoney } from '../../components/common'
import { PencilIcon, PlusIcon, RestoreIcon, TrashIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { useSession } from '../../session'
import { formatDay } from '../finance/financeApi'
import { ShoppingItem } from './shoppingApi'
import { groupShoppingItems, isInOtherCart, priceNumber, quantityText, ShoppingTab } from './shoppingItems'
import { ShoppingCheck, ShoppingItemName, ShoppingPicture } from './shoppingParts'
import { ShoppingItemCard } from './shoppingView'

// The items of one tab of a list, as a table grouped under a heading for each
// store and aisle ("Example Market · Frozen"), the items that can be got
// anywhere under "Any store". A table stays a table on a phone: it scrolls
// sideways inside its box. Given onAddAt, each heading has a button that adds
// an item for its store and aisle without typing them.

export function ShoppingItemTable({
  shoppingItems,
  tab,
  isBusy,
  onCheck,
  onEdit,
  onRemove,
  onNeedAgain,
  onAddAt,
}: {
  shoppingItems: ShoppingItem[]
  tab: ShoppingTab
  isBusy: boolean
  onCheck: (shoppingItem: ShoppingItem, isChecked: boolean) => void
  onEdit: (shoppingItem: ShoppingItem) => void
  onRemove: (shoppingItem: ShoppingItem) => void
  onNeedAgain: (shoppingItem: ShoppingItem) => void
  onAddAt?: (storeName: string, aisleName: string) => void
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
                <span className="shopping-group-heading">
                  {groupHeading(group.storeName, group.aisleName, t)}
                  {onAddAt ? (
                    <Tooltip label={t('shopping.addItemHere')}>
                      <button
                        type="button"
                        className="icon-action"
                        aria-label={`${groupHeading(group.storeName, group.aisleName, t)}: ${t('shopping.addItemHere')}`}
                        disabled={isBusy}
                        onClick={() => onAddAt(group.storeName, group.aisleName)}
                      >
                        <PlusIcon size={16} />
                      </button>
                    </Tooltip>
                  ) : null}
                </span>
              </th>
            </tr>
            {group.shoppingItems.map((shoppingItem) => (
              <tr
                key={shoppingItem.id}
                className={`${rowClassName(shoppingItem)} is-openable`}
                onClick={(event) => openFromRow(event, () => onEdit(shoppingItem))}
              >
                <td className="shrink">
                  <ListItemCheck shoppingItem={shoppingItem} onCheck={onCheck} />
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
    <ul className="shopping-cards">
      {groupShoppingItems(shoppingItems).flatMap((group) =>
        group.shoppingItems.map((shoppingItem) => (
          <ShoppingItemCard
            key={shoppingItem.id}
            shoppingItem={shoppingItem}
            groupName={groupHeading(group.storeName, group.aisleName, t)}
            check={<ListItemCheck shoppingItem={shoppingItem} onCheck={onCheck} />}
            onOpen={() => onEdit(shoppingItem)}
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
        )),
      )}
    </ul>
  )
}

function priceText(shoppingItem: ShoppingItem): string {
  return priceNumber(shoppingItem.unitPriceAmount) === null
    ? ''
    : formatMoney(Number(shoppingItem.unitPriceAmount), shoppingItem.currencyCode)
}

// rowClassName is a row's classes: crossed out while the item is in a cart or
// checked off, in the same height as one that is not.
function rowClassName(shoppingItem: ShoppingItem): string {
  return shoppingItem.shoppingItemState === 'needed' ? 'shopping-row' : 'shopping-row is-in-cart'
}

// openFromRow opens an item when the click is on the row itself and not on a
// button, link or check in it, so the whole row is a target.
function openFromRow(event: React.MouseEvent, open: () => void) {
  if ((event.target as HTMLElement).closest('button, a, input, label')) return
  open()
}

// ListItemCheck is the round check on a list. An item to buy is checked off,
// or put in the cart while the person shops; a checked one (in a cart or
// checked off) is put back. An item in somebody else's cart cannot be touched.
function ListItemCheck({
  shoppingItem,
  onCheck,
}: {
  shoppingItem: ShoppingItem
  onCheck: (shoppingItem: ShoppingItem, isChecked: boolean) => void
}) {
  const { t } = useTranslation()
  const session = useSession()
  const label =
    shoppingItem.shoppingItemState === 'not_needed'
      ? t('shopping.uncheckItem', { itemDescription: shoppingItem.itemDescription })
      : t('shopping.checkItem', { itemDescription: shoppingItem.itemDescription })
  return (
    <ShoppingCheck
      isChecked={shoppingItem.shoppingItemState !== 'needed'}
      isDisabled={isInOtherCart(shoppingItem, session.userId)}
      label={label}
      onChange={(isChecked) => onCheck(shoppingItem, isChecked)}
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

// groupHeading names a group of items: its store, or "Any store", and its
// aisle.
function groupHeading(storeName: string, aisleName: string, t: ReturnType<typeof useTranslation>['t']): string {
  return [storeName || t('shopping.anyStore'), aisleName].filter(Boolean).join(' · ')
}
