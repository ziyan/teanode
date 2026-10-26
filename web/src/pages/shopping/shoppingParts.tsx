import { initialOf } from '../../components/avatar'
import { Tag } from '../../components/common'
import { useTranslation } from '../../i18n/i18n'
import { ShoppingItem } from './shoppingApi'

// The pieces of an item that more than one part of the Shopping page draws.

// ShoppingPicture is an item's picture, square, or a neutral square with the
// first letter of its description while it has none.
export function ShoppingPicture({
  item,
  isLarge = false,
}: {
  item: Pick<ShoppingItem, 'picturePath' | 'itemDescription'>
  isLarge?: boolean
}) {
  const className = isLarge ? 'shopping-picture shopping-picture-large' : 'shopping-picture'
  if (item.picturePath) {
    return <img className={className} src={item.picturePath} alt="" loading="lazy" draggable={false} />
  }
  return (
    <span className={`${className} shopping-picture-empty`} aria-hidden="true">
      {initialOf(item.itemDescription)}
    </span>
  )
}

// ShoppingItemName is an item's description with its details under it in
// muted text, and the tags that say what is special about it.
export function ShoppingItemName({ item, listName }: { item: ShoppingItem; listName?: string }) {
  const { t } = useTranslation()
  return (
    <div className="shopping-item-name">
      <div className="shopping-item-description">
        <strong>{item.itemDescription}</strong>
        {item.isStoreBrand ? <Tag value={t('shopping.storeBrand')} /> : null}
        {item.outOfStockAt ? <Tag value={t('shopping.outOfStock')} tone="warn" /> : null}
      </div>
      {item.itemDetails ? <div className="muted">{item.itemDetails}</div> : null}
      {listName ? <div className="muted">{listName}</div> : null}
    </div>
  )
}
