import { useMemo, useRef, useState } from 'react'
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
  listShoppingStoreItems,
  setShoppingItemOutOfStock,
  setShoppingStoreReminder,
  ShoppingItem,
} from './shoppingApi'
import { ShoppingCartDialog, ShoppingResumeRow, useReceiptReader } from './shoppingCart'
import { ShoppingItemDialog } from './shoppingItemDialog'
import { groupShoppingItemsByAisle, isInOwnCart, quantityText, storeKeyOf } from './shoppingItems'
import { ShoppingPaneBack, useShoppingLayout } from './shoppingLayout'
import { ShoppingItemCard, ShoppingViewSwitch, useShoppingView } from './shoppingView'
import { ShoppingCheck, ShoppingItemName, ShoppingPicture } from './shoppingParts'
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
  const { reloadShoppingLists, resumableTrip } = useShoppingLayout()
  const [parameters, setParameters] = useSearchParams()
  const listsParameter = parameters.get('lists') ?? ''
  const askedListIds = useMemo(() => (listsParameter ? listsParameter.split(',') : []), [listsParameter])
  const view = useQuery(() => listShoppingStoreItems(storeName, askedListIds), [storeName, listsParameter], {
    refresh: false,
  })
  const [isBusy, setIsBusy] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [isCartOpen, setIsCartOpen] = useState(false)
  const receiptInput = useRef<HTMLInputElement>(null)
  const readReceipt = useReceiptReader()

  const shoppingLists = view.data?.shoppingLists ?? []
  useShoppingEvents((changedListId) => {
    if (changedListId === '' || shoppingLists.some((each) => each.shoppingListId === changedListId)) {
      void view.reload(true)
    }
  })
  useBreadcrumbDetail(storeName)

  const shoppingItems = useMemo(() => view.data?.shoppingItems ?? [], [view.data])
  const includedLists = shoppingLists.filter((each) => each.isIncluded)
  // What is in a cart, anybody's, goes last under its own heading, so what is
  // left to find stays on top.
  const cartItems = useMemo(() => shoppingItems.filter((item) => item.shoppingItemState === 'in_cart'), [shoppingItems])
  const groups = useMemo(
    () => groupShoppingItemsByAisle(shoppingItems.filter((item) => item.shoppingItemState !== 'in_cart')),
    [shoppingItems],
  )
  const storeKey = storeKeyOf(storeName)
  const openTrips = (view.data?.shoppingTrips ?? []).filter(
    (trip) => trip.finishedAt === null && trip.storeKey === storeKey,
  )
  const ownTrip = openTrips.find((trip) => trip.userId === session.userId) ?? null
  const ownCartCount = shoppingItems.filter((item) => isInOwnCart(item, session.userId)).length
  const resumeHere = resumableTrip && resumableTrip.storeKey === storeKey ? resumableTrip : null
  const [shoppingView, chooseShoppingView] = useShoppingView()
  const otherTrips = openTrips.filter((trip) => trip.userId !== session.userId)
  const editingItem = shoppingItems.find((each) => each.id === editing) ?? null

  async function uploadReceipt(file: File | undefined) {
    if (!file) return
    setIsBusy(true)
    try {
      if (await readReceipt(file)) toast.done(t('shopping.receiptReading'))
    } finally {
      setIsBusy(false)
    }
  }

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
  const sections = [
    ...groups.map((group) => ({
      key: group.isAnyStore ? '\u0000any-store' : group.aisleName,
      heading: group.isAnyStore ? t('shopping.storeAnyStoreSection') : group.aisleName || t('shopping.noAisle'),
      shoppingItems: group.shoppingItems,
    })),
    ...(cartItems.length > 0
      ? [{ key: '\u0000in-cart', heading: t('shopping.tabInCart'), shoppingItems: cartItems }]
      : []),
  ]
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
              Start. Done opens the cart; it is the primary button only once
              something is in the person's own cart. */}
          {ownTrip || ownCartCount > 0 ? (
            <button
              type="button"
              className={ownCartCount > 0 ? 'primary' : ''}
              disabled={isBusy}
              onClick={() => setIsCartOpen(true)}
            >
              {ownCartCount > 0 ? t('shopping.doneShoppingCount', { count: ownCartCount }) : t('shopping.doneShopping')}
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
          <button type="button" disabled={isBusy} onClick={() => receiptInput.current?.click()}>
            {t('shopping.uploadReceipt')}
          </button>
          <input
            ref={receiptInput}
            type="file"
            accept="image/*,application/pdf"
            hidden
            aria-label={t('shopping.uploadReceipt')}
            onChange={(event) => {
              void uploadReceipt(event.target.files?.[0])
              event.target.value = ''
            }}
          />
        </div>

        {resumeHere ? (
          <ShoppingResumeRow
            shoppingTrip={resumeHere}
            onResumed={() => {
              reloadShoppingLists()
              void view.reload(true)
            }}
          />
        ) : null}

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
          <ul className="shopping-cards">
            {sections.flatMap((section) =>
              section.shoppingItems.map((shoppingItem) => (
                <ShoppingItemCard
                  key={shoppingItem.id}
                  shoppingItem={shoppingItem}
                  groupName={section.heading}
                  onOpen={() => setEditing(shoppingItem.id)}
                  listName={isListNamed ? shoppingItem.listName : undefined}
                  check={
                    <StoreItemCheck
                      shoppingItem={shoppingItem}
                      isBusy={isBusy}
                      onCheck={(isChecked) => void act(() => checkShoppingItem(shoppingItem.id, isChecked, storeName))}
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
              )),
            )}
          </ul>
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
              {sections.map((section) => (
                <tbody key={section.key}>
                  <tr className="shopping-group-row">
                    <th colSpan={5} scope="colgroup">
                      {section.heading}
                    </th>
                  </tr>
                  {section.shoppingItems.map((shoppingItem) => (
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
      {isCartOpen ? (
        <ShoppingCartDialog
          ownTrip={ownTrip}
          shoppingListId={null}
          storeName={storeName}
          onChanged={() => {
            reloadShoppingLists()
            void view.reload(true)
          }}
          onFinished={() => {
            setIsCartOpen(false)
            reloadShoppingLists()
            void view.reload(true)
          }}
          onClose={() => setIsCartOpen(false)}
        />
      ) : null}
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
    <tr
      className={
        shoppingItem.shoppingItemState === 'in_cart'
          ? 'shopping-row is-in-cart is-openable'
          : 'shopping-row is-openable'
      }
      onClick={(event) => {
        if (!(event.target as HTMLElement).closest('button, a, input, label')) onEdit()
      }}
    >
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

// StoreItemCheck is the round check that puts an item in the cart at the
// store.
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
    <ShoppingCheck
      isChecked={shoppingItem.shoppingItemState === 'in_cart'}
      isDisabled={isBusy}
      label={t('shopping.checkItem', { itemDescription: shoppingItem.itemDescription })}
      onChange={onCheck}
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
