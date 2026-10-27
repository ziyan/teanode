import { initialOf } from '../../components/avatar'
import { Tag } from '../../components/common'
import { ZoomablePicture } from '../../components/lightbox'
import { useTranslation } from '../../i18n/i18n'
import { ShoppingItem } from './shoppingApi'

// The pieces of an item that more than one part of the Shopping page draws.

// ShoppingPicture is an item's picture, square, or a neutral square with the
// first letter of its description while it has none. A picture opens in the
// lightbox, to see the product close up; the letter is not a picture and
// opens nothing.
export function ShoppingPicture({
  item,
  pictureSize = 'small',
}: {
  item: Pick<ShoppingItem, 'picturePath' | 'itemDescription'>
  // small in a table row, large in the item dialog, card across the top of
  // a card in the pictures view.
  pictureSize?: 'small' | 'large' | 'card'
}) {
  const { t } = useTranslation()
  const className = PICTURE_CLASS_NAMES[pictureSize]
  if (item.picturePath) {
    return (
      <ZoomablePicture
        source={item.picturePath}
        name={item.itemDescription}
        imageClassName={className}
        openTitle={t('shopping.openPicture')}
      />
    )
  }
  return (
    <span className={`${className} shopping-picture-empty`} aria-hidden="true">
      {initialOf(item.itemDescription)}
    </span>
  )
}

const PICTURE_CLASS_NAMES = {
  small: 'shopping-picture',
  large: 'shopping-picture shopping-picture-large',
  card: 'shopping-picture shopping-picture-card',
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
