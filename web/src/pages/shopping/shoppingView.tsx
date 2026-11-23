import { ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useTranslation } from '../../i18n/i18n'
import { ShoppingItem } from './shoppingApi'
import { ShoppingItemName, ShoppingPicture } from './shoppingParts'

// How the items of a list or a store are laid out: as the table, or as cards
// with big pictures, which is easier to find a product on the shelf by. The
// choice is in the address (view=pictures), so a link opens the same layout,
// and it is remembered in this browser for the next list opened without one.

export type ShoppingView = 'list' | 'pictures'

const STORAGE_KEY = 'teanode.shopping.view'

function isShoppingView(value: string | null): value is ShoppingView {
  return value === 'list' || value === 'pictures'
}

function storedShoppingView(): ShoppingView {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    return isShoppingView(stored) ? stored : 'list'
  } catch {
    return 'list'
  }
}

export function useShoppingView(): [ShoppingView, (shoppingView: ShoppingView) => void] {
  const [parameters, setParameters] = useSearchParams()
  const asked = parameters.get('view')
  const shoppingView = isShoppingView(asked) ? asked : storedShoppingView()
  function chooseShoppingView(chosen: ShoppingView) {
    try {
      window.localStorage.setItem(STORAGE_KEY, chosen)
    } catch {
      // A browser that keeps nothing still shows the layout through the address.
    }
    setParameters(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.set('view', chosen)
        return next
      },
      { replace: true },
    )
  }
  return [shoppingView, chooseShoppingView]
}

export function ShoppingViewSwitch({
  shoppingView,
  onChoose,
}: {
  shoppingView: ShoppingView
  onChoose: (shoppingView: ShoppingView) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="segmented shopping-view-switch" role="group" aria-label={t('shopping.viewSwitch')}>
      {(['list', 'pictures'] as const).map((each) => (
        <button
          key={each}
          type="button"
          className={shoppingView === each ? 'active' : ''}
          aria-pressed={shoppingView === each}
          onClick={() => onChoose(each)}
        >
          {each === 'list' ? t('shopping.viewList') : t('shopping.viewPictures')}
        </button>
      ))}
    </div>
  )
}

// ShoppingItemCard is one item in the pictures view: the picture across the
// top, which opens in the lightbox, then where it is (its store and aisle),
// the check, what it is, and the line under it (how many, the price). The
// actions sit at the foot, the same buttons the table row has.
export function ShoppingItemCard({
  shoppingItem,
  groupName,
  listName,
  check,
  facts,
  actions,
  onOpen,
}: {
  shoppingItem: ShoppingItem
  // The store and aisle the table would put it under. The cards run on in
  // that order without headings, so an aisle of one item does not leave a
  // row of empty space; each card says where it is instead.
  groupName: string
  listName?: string
  // The round check that puts it in the cart or checks it off.
  check: ReactNode
  facts: string[]
  actions: ReactNode
  // Opens the item when the card itself is clicked, not one of its buttons.
  onOpen?: () => void
}) {
  const shownFacts = facts.filter(Boolean)
  return (
    <li
      className={[
        'shopping-card shopping-row',
        shoppingItem.shoppingItemState === 'needed' ? '' : 'is-in-cart',
        onOpen ? 'is-openable' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      onClick={(event) => {
        if (onOpen && !(event.target as HTMLElement).closest('button, a, input, label')) onOpen()
      }}
    >
      <ShoppingPicture item={shoppingItem} pictureSize="card" />
      <div className="shopping-card-body">
        <div className="shopping-card-group-name">{groupName}</div>
        {/* The facts sit under the name, past the check, so the words of a
            card start on one line. */}
        <div className="shopping-card-name">
          {check}
          <div className="shopping-card-words">
            <ShoppingItemName item={shoppingItem} listName={listName} />
            {shownFacts.length > 0 ? <div className="shopping-card-facts">{shownFacts.join(' · ')}</div> : null}
          </div>
        </div>
        <div className="row-actions shopping-card-actions">{actions}</div>
      </div>
    </li>
  )
}
