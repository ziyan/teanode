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
// top, which opens in the lightbox, then the check, what it is, and the line
// under it (how many, the price). The actions sit at the foot, the same
// buttons the table row has.
export function ShoppingItemCard({
  shoppingItem,
  listName,
  check,
  facts,
  actions,
}: {
  shoppingItem: ShoppingItem
  listName?: string
  // The checkbox that puts it in the cart, or nothing where an item cannot
  // be checked (a bought item on Not needed).
  check: ReactNode
  facts: string[]
  actions: ReactNode
}) {
  const shownFacts = facts.filter(Boolean)
  return (
    <li
      className={
        shoppingItem.shoppingItemState === 'in_cart'
          ? 'shopping-card shopping-row is-in-cart'
          : 'shopping-card shopping-row'
      }
    >
      <ShoppingPicture item={shoppingItem} pictureSize="card" />
      <div className="shopping-card-body">
        <div className="shopping-card-name">
          {check}
          <ShoppingItemName item={shoppingItem} listName={listName} />
        </div>
        {shownFacts.length > 0 ? <div className="shopping-card-facts">{shownFacts.join(' · ')}</div> : null}
        <div className="row-actions shopping-card-actions">{actions}</div>
      </div>
    </li>
  )
}

// ShoppingCardGroup is a run of cards under the heading of its store and
// aisle, as the table puts a band across itself.
export function ShoppingCardGroup({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <section className="shopping-card-group">
      <h4>{heading}</h4>
      <ul className="shopping-cards">{children}</ul>
    </section>
  )
}
