import { useMemo, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'

import { useBreadcrumbDetail } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import { InboxOffIcon, PencilIcon, RestoreIcon } from '../../components/icons'
import { SettingsEmpty } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { useSession } from '../../session'
import {
  checkShoppingItem,
  finishShopping,
  listShoppingStoreItems,
  setShoppingItemOutOfStock,
  setShoppingStoreReminder,
  ShoppingItem,
} from './shoppingApi'
import { ShoppingItemDialog } from './shoppingItemDialog'
import { groupShoppingItemsByAisle, quantityText, storeKeyOf } from './shoppingItems'
import { ShoppingPaneBack, useShoppingLayout } from './shoppingLayout'
import { ShoppingCardGroup, ShoppingItemCard, ShoppingViewSwitch, useShoppingView } from './shoppingView'
import { ShoppingItemName, ShoppingPicture } from './shoppingParts'
import { useShoppingEvents } from './useShoppingEvents'

// The store screen: what is needed at one store across the lists the person
// reaches, by aisle, as it is walked. The lists are buttons; the ones switched
// on are in the address (lists=), and none in the address means every list
// that has something here. Checking an item off shows on everybody's screen at
// once, and an item the store has none of is marked, for everybody.

export function ShoppingStorePage() {
  const { storeName = '' } = useParams()
  const { t } = useTranslation()
  const toast = useToast()
  const session = useSession()
  const { reloadShoppingLists } = useShoppingLayout()
  const [parameters, setParameters] = useSearchParams()
  const listsParameter = parameters.get('lists') ?? ''
  const askedListIds = useMemo(() => (listsParameter ? listsParameter.split(',') : []), [listsParameter])
  const view = useQuery(() => listShoppingStoreItems(storeName, askedListIds), [storeName, listsParameter], {
    refresh: false,
  })
  const [isBusy, setIsBusy] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)

  const shoppingLists = view.data?.shoppingLists ?? []
  useShoppingEvents((changedListId) => {
    if (changedListId === '' || shoppingLists.some((each) => each.shoppingListId === changedListId)) {
      void view.reload(true)
    }
  })
  useBreadcrumbDetail(storeName)

  const shoppingItems = useMemo(() => view.data?.shoppingItems ?? [], [view.data])
  const includedLists = shoppingLists.filter((each) => each.isIncluded)
  const groups = useMemo(() => groupShoppingItemsByAisle(shoppingItems), [shoppingItems])
  const storeKey = storeKeyOf(storeName)
  const openTrips = (view.data?.shoppingTrips ?? []).filter(
    (trip) => trip.finishedAt === null && trip.storeKey === storeKey,
  )
  const ownTrip = openTrips.find((trip) => trip.userId === session.userId) ?? null
  const hasOwnCart = shoppingItems.some(
    (item) => item.shoppingItemState === 'in_cart' && item.cartedByUserId === session.userId,
  )
  const [shoppingView, chooseShoppingView] = useShoppingView()
  const otherTrips = openTrips.filter((trip) => trip.userId !== session.userId)
  const editingItem = shoppingItems.find((each) => each.id === editing) ?? null

  async function act(work: () => Promise<unknown>, done?: string) {
    setIsBusy(true)
    try {
      await work()
      if (done) toast.done(done)
      reloadShoppingLists()
      await view.reload(true)
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  // Switching a list on or off writes the lists that are on into the address;
  // all of them on is no list in the address. The last one on stays on, since
  // a store screen of no list is a blank page.
  function toggleList(shoppingListId: string) {
    const nextIds = shoppingLists
      .filter((each) => (each.shoppingListId === shoppingListId ? !each.isIncluded : each.isIncluded))
      .map((each) => each.shoppingListId)
    if (nextIds.length === 0) return
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        if (nextIds.length === shoppingLists.length) written.delete('lists')
        else written.set('lists', nextIds.join(','))
        return written
      },
      { replace: true },
    )
  }

  if (view.loading && !view.data) {
    return (
      <>
        <ErrorMessage error={view.error} />
        <Loading />
      </>
    )
  }

  const isListNamed = includedLists.length > 1
  return (
    <>
      <ErrorMessage error={view.error} />
      <section className="shopping-list-card" aria-label={storeName}>
        <div className="shopping-list-head">
          <ShoppingPaneBack />
          <div className="shopping-list-summary">
            {otherTrips.map((trip) => (
              <p key={trip.id} className="shopping-trip-line">
                {t('shopping.tripOther', { shopperName: trip.shopperName || trip.shopperUsername, storeName })}
              </p>
            ))}
            {ownTrip ? <p className="shopping-trip-line">{t('shopping.tripSelf', { storeName })}</p> : null}
          </div>
        </div>

        <div className="page-actions shopping-list-actions">
          {/* The first item put in the cart starts the trip, so there is no
              Start; Done buys the cart and needs no receipt. */}
          {ownTrip || hasOwnCart ? (
            <button
              type="button"
              className="primary"
              disabled={isBusy}
              onClick={() => void act(() => finishShopping(ownTrip?.id), t('shopping.shoppingDone'))}
            >
              {t('shopping.doneShopping')}
            </button>
          ) : null}
          <button
            type="button"
            disabled={isBusy}
            onClick={() =>
              void act(
                () => setShoppingStoreReminder(storeName, !view.data?.isReminded),
                view.data?.isReminded
                  ? t('shopping.storeReminderOff', { storeName })
                  : t('shopping.storeReminderOn', { storeName }),
              )
            }
          >
            {view.data?.isReminded ? t('shopping.dontRemindAtStore') : t('shopping.remindAtStore')}
          </button>
        </div>

        {shoppingLists.length > 0 || shoppingItems.length > 0 ? (
          <div className="shopping-view-bar">
            {shoppingLists.length > 0 ? (
              <div className="shopping-list-toggles" role="group" aria-label={t('shopping.listsHeading')}>
                {shoppingLists.map((each) => (
                  <button
                    key={each.shoppingListId}
                    type="button"
                    className={each.isIncluded ? 'shopping-list-toggle is-included' : 'shopping-list-toggle'}
                    aria-pressed={each.isIncluded}
                    disabled={isBusy}
                    onClick={() => toggleList(each.shoppingListId)}
                  >
                    {each.listName}
                    <span className="tab-count">{each.neededCount.toLocaleString()}</span>
                  </button>
                ))}
              </div>
            ) : null}
            {shoppingItems.length > 0 ? (
              <ShoppingViewSwitch shoppingView={shoppingView} onChoose={chooseShoppingView} />
            ) : null}
          </div>
        ) : null}

        {shoppingItems.length === 0 ? (
          <SettingsEmpty>{t('shopping.storeEmpty', { storeName })}</SettingsEmpty>
        ) : shoppingView === 'pictures' ? (
          <div className="shopping-card-groups">
            {groups.map((group) => (
              <ShoppingCardGroup
                key={group.isAnyStore ? '\u0000any-store' : group.aisleName}
                heading={
                  group.isAnyStore ? t('shopping.storeAnyStoreSection') : group.aisleName || t('shopping.noAisle')
                }
              >
                {group.shoppingItems.map((shoppingItem) => (
                  <ShoppingItemCard
                    key={shoppingItem.id}
                    shoppingItem={shoppingItem}
                    listName={isListNamed ? shoppingItem.listName : undefined}
                    check={
                      <StoreItemCheck
                        shoppingItem={shoppingItem}
                        isBusy={isBusy}
                        onCheck={(isChecked) =>
                          void act(() => checkShoppingItem(shoppingItem.id, isChecked, storeName))
                        }
                      />
                    }
                    facts={[t('shopping.needCount', { quantity: quantityText(shoppingItem) })]}
                    actions={
                      <StoreItemActions
                        shoppingItem={shoppingItem}
                        isBusy={isBusy}
                        onOutOfStock={() =>
                          void act(() =>
                            setShoppingItemOutOfStock(shoppingItem.id, !shoppingItem.outOfStockAt, storeName),
                          )
                        }
                        onEdit={() => setEditing(shoppingItem.id)}
                      />
                    }
                  />
                ))}
              </ShoppingCardGroup>
            ))}
          </div>
        ) : (
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
                  <th className="shrink">
                    <span className="visually-hidden">{t('shopping.actionsColumn')}</span>
                  </th>
                </tr>
              </thead>
              {groups.map((group) => (
                <tbody key={group.isAnyStore ? '\u0000any-store' : group.aisleName}>
                  <tr className="shopping-group-row">
                    <th colSpan={5} scope="colgroup">
                      {group.isAnyStore ? t('shopping.storeAnyStoreSection') : group.aisleName || t('shopping.noAisle')}
                    </th>
                  </tr>
                  {group.shoppingItems.map((shoppingItem) => (
                    <StoreItemRow
                      key={shoppingItem.id}
                      shoppingItem={shoppingItem}
                      isListNamed={isListNamed}
                      isBusy={isBusy}
                      onCheck={(isChecked) => void act(() => checkShoppingItem(shoppingItem.id, isChecked, storeName))}
                      onOutOfStock={() =>
                        void act(() =>
                          setShoppingItemOutOfStock(shoppingItem.id, !shoppingItem.outOfStockAt, storeName),
                        )
                      }
                      onEdit={() => setEditing(shoppingItem.id)}
                    />
                  ))}
                </tbody>
              ))}
            </table>
          </div>
        )}
      </section>
      {editingItem ? (
        <ShoppingItemDialog
          target={{ shoppingItem: editingItem }}
          onSaved={() => {
            reloadShoppingLists()
            void view.reload(true)
          }}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </>
  )
}

function StoreItemRow({
  shoppingItem,
  isListNamed,
  isBusy,
  onCheck,
  onOutOfStock,
  onEdit,
}: {
  shoppingItem: ShoppingItem
  isListNamed: boolean
  isBusy: boolean
  onCheck: (isChecked: boolean) => void
  onOutOfStock: () => void
  onEdit: () => void
}) {
  return (
    <tr className={shoppingItem.shoppingItemState === 'in_cart' ? 'shopping-row is-in-cart' : 'shopping-row'}>
      <td className="shrink">
        <StoreItemCheck shoppingItem={shoppingItem} isBusy={isBusy} onCheck={onCheck} />
      </td>
      <td className="shrink">
        <ShoppingPicture item={shoppingItem} />
      </td>
      <td className="wrap">
        <ShoppingItemName item={shoppingItem} listName={isListNamed ? shoppingItem.listName : undefined} />
      </td>
      <td>{quantityText(shoppingItem)}</td>
      <td className="shrink">
        <div className="row-actions">
          <StoreItemActions shoppingItem={shoppingItem} isBusy={isBusy} onOutOfStock={onOutOfStock} onEdit={onEdit} />
        </div>
      </td>
    </tr>
  )
}

// StoreItemCheck is the box that puts an item in the cart at the store.
function StoreItemCheck({
  shoppingItem,
  isBusy,
  onCheck,
}: {
  shoppingItem: ShoppingItem
  isBusy: boolean
  onCheck: (isChecked: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <input
      type="checkbox"
      checked={shoppingItem.shoppingItemState === 'in_cart'}
      disabled={isBusy}
      aria-label={t('shopping.checkItem', { itemDescription: shoppingItem.itemDescription })}
      onChange={(event) => onCheck(event.target.checked)}
    />
  )
}

// StoreItemActions are an item's buttons at the store: out of stock (or in
// stock again) and edit.
function StoreItemActions({
  shoppingItem,
  isBusy,
  onOutOfStock,
  onEdit,
}: {
  shoppingItem: ShoppingItem
  isBusy: boolean
  onOutOfStock: () => void
  onEdit: () => void
}) {
  const { t } = useTranslation()
  const outOfStockLabel = shoppingItem.outOfStockAt ? t('shopping.backInStock') : t('shopping.markOutOfStock')
  return (
    <>
      <Tooltip label={outOfStockLabel}>
        <button
          type="button"
          className="icon-action"
          aria-label={`${shoppingItem.itemDescription}: ${outOfStockLabel}`}
          disabled={isBusy}
          onClick={onOutOfStock}
        >
          {shoppingItem.outOfStockAt ? <RestoreIcon size={16} /> : <InboxOffIcon size={16} />}
        </button>
      </Tooltip>
      <Tooltip label={t('shopping.editItem')}>
        <button
          type="button"
          className="icon-action"
          aria-label={`${shoppingItem.itemDescription}: ${t('shopping.editItem')}`}
          onClick={onEdit}
        >
          <PencilIcon size={16} />
        </button>
      </Tooltip>
    </>
  )
}
