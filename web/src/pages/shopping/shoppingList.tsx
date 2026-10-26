import { useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { useBreadcrumbDetail } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PencilIcon, PlusIcon, TrashIcon } from '../../components/icons'
import { Pager, useKeepPageInRange, usePageInAddress } from '../../components/pager'
import { SettingsEmpty } from '../../components/settingsList'
import { TabItem, Tabs } from '../../components/tabs'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { useSession } from '../../session'
import { READ_RECEIPT } from '../finance/financeApi'
import {
  checkShoppingItem,
  deleteShoppingList,
  finishShopping,
  getShoppingList,
  listShoppingListShares,
  listShoppingStores,
  removeShoppingItems,
  renameShoppingList,
  setShoppingItemNeed,
  ShoppingItem,
  ShoppingList,
  ShoppingTrip,
} from './shoppingApi'
import { ShoppingImportDialog } from './shoppingImportDialog'
import { ShoppingItemDialog, ShoppingItemDialogTarget } from './shoppingItemDialog'
import { ShoppingItemGrid, ShoppingItemTable } from './shoppingItemTable'
import {
  countShoppingTabs,
  isPositiveNumber,
  isShoppingTab,
  MAXIMUM_UPLOAD_BYTES,
  plainNumber,
  SHOPPING_PATH,
  shoppingListAddress,
  shoppingStoreAddress,
  isOnShoppingTab,
  ShoppingTab,
  uploadAgentAttachment,
} from './shoppingItems'
import { ShoppingPaneBack, useShoppingLayout } from './shoppingLayout'
import { ShoppingViewSwitch, useShoppingView } from './shoppingView'
import { ShoppingShareDialog } from './shoppingShareDialog'
import { useShoppingEvents } from './useShoppingEvents'

// One list: who it is shared with, who is shopping it now, the buttons that
// act on it, and its items in four tabs (the tab is in the address, as is the
// page of a long tab). The items are read whole, so each tab can say how many
// it holds, and are paged here. The open list is read again when the live feed
// names it.

const TAB_LABELS: Record<ShoppingTab, Key> = {
  'to-buy': 'shopping.tabToBuy',
  'in-cart': 'shopping.tabInCart',
  'out-of-stock': 'shopping.tabOutOfStock',
  'not-needed': 'shopping.tabNotNeeded',
}

const EMPTY_TAB_WORDS: Record<ShoppingTab, Key> = {
  'to-buy': 'shopping.emptyToBuy',
  'in-cart': 'shopping.emptyInCart',
  'out-of-stock': 'shopping.emptyOutOfStock',
  'not-needed': 'shopping.emptyNotNeeded',
}

type Opened =
  | { dialogKind: 'item'; target: ShoppingItemDialogTarget }
  | { dialogKind: 'import' }
  | { dialogKind: 'share' }
  | { dialogKind: 'rename' }
  | { dialogKind: 'deleteList' }
  | { dialogKind: 'removeItem'; shoppingItem: ShoppingItem }
  | { dialogKind: 'needAgain'; shoppingItem: ShoppingItem }
  | { dialogKind: 'shopAtStore' }

export function ShoppingListPage() {
  const { shoppingListId = '' } = useParams()
  const { t, plural } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const session = useSession()
  const { reloadShoppingLists } = useShoppingLayout()
  const [parameters, setParameters] = useSearchParams()
  const content = useQuery(() => getShoppingList(shoppingListId), [shoppingListId], { refresh: false })
  const [opened, setOpened] = useState<Opened | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const receiptInput = useRef<HTMLInputElement>(null)

  const askedTab = parameters.get('tab')
  const tab: ShoppingTab = isShoppingTab(askedTab) ? askedTab : 'to-buy'
  const { pageIndex, pageSize, offset, setPageIndex, setPageSize } = usePageInAddress({ defaultPageSize: 50 })

  useShoppingEvents((changedListId) => {
    if (changedListId === '' || changedListId === shoppingListId) void content.reload(true)
  })

  const shoppingList = content.data?.shoppingList ?? null
  const shoppingItems = useMemo(() => content.data?.shoppingItems ?? [], [content.data])
  const counts = useMemo(() => countShoppingTabs(shoppingItems), [shoppingItems])
  const tabItems = useMemo(() => shoppingItems.filter((item) => isOnShoppingTab(item, tab)), [shoppingItems, tab])
  const shownItems = tabItems.slice(offset, offset + pageSize)
  useKeepPageInRange(pageIndex, pageSize, content.data ? tabItems.length : null, setPageIndex)
  useBreadcrumbDetail(shoppingList?.listName)

  // Who a list the person owns is shared with, by name, for the line under
  // the page's heading. Only the owner may read the shares.
  const shares = useQuery(
    () =>
      shoppingList?.isOwner && shoppingList.isShared ? listShoppingListShares(shoppingList.id) : Promise.resolve([]),
    [shoppingList?.id, shoppingList?.isOwner, shoppingList?.isShared],
    { refresh: false },
  )

  const openTrips = (content.data?.shoppingTrips ?? []).filter((trip) => trip.finishedAt === null)
  const ownTrip = openTrips.find((trip) => trip.userId === session.userId) ?? null
  const hasOwnCart = shoppingItems.some(
    (item) => item.shoppingItemState === 'in_cart' && item.cartedByUserId === session.userId,
  )
  const [shoppingView, chooseShoppingView] = useShoppingView()

  async function reloadAll() {
    reloadShoppingLists()
    await Promise.all([content.reload(true), shares.reload(true)])
  }

  // act runs one change, with the page busy meanwhile and a toast if it fails.
  async function act(work: () => Promise<unknown>, done?: string): Promise<boolean> {
    setIsBusy(true)
    try {
      await work()
      if (done) toast.done(done)
      await reloadAll()
      return true
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  async function uploadReceipt(file: File | undefined) {
    if (!file) return
    if (file.size > MAXIMUM_UPLOAD_BYTES) {
      toast.failed(t('shopping.fileTooLarge', { name: file.name, size: String(MAXIMUM_UPLOAD_BYTES >> 20) }))
      return
    }
    setIsBusy(true)
    try {
      const agentAttachmentId = await uploadAgentAttachment(file)
      await graphql(READ_RECEIPT, { agentAttachmentId })
      toast.done(t('shopping.receiptReading'))
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  function selectTab(next: string) {
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        if (next === 'to-buy') written.delete('tab')
        else written.set('tab', next)
        written.delete('page')
        return written
      },
      { replace: true },
    )
  }

  const tabs: TabItem[] = (Object.keys(TAB_LABELS) as ShoppingTab[]).map((id) => ({
    id,
    label: TAB_LABELS[id],
    count: counts[id],
  }))

  if (content.loading && !content.data) {
    return (
      <>
        <ErrorMessage error={content.error} />
        <Loading />
      </>
    )
  }
  if (!shoppingList) {
    return (
      <>
        <ErrorMessage error={content.error} />
        <SettingsEmpty>{t('shopping.listNotFound')}</SettingsEmpty>
      </>
    )
  }

  const sharedWith = (shares.data ?? []).map((share) =>
    share.granteeCircleId
      ? t('shopping.shareEveryoneIn', { name: share.granteeCircleName })
      : share.granteeName || share.granteeUsername,
  )
  const ownerLine = shoppingList.isOwner
    ? shoppingList.isShared
      ? sharedWith.length > 0
        ? t('shopping.sharedWith', { names: sharedWith.join(', ') })
        : t('shopping.sharedMark')
      : t('shopping.notShared')
    : t('shopping.sharedBy', { name: shoppingList.ownerName || shoppingList.ownerUsername })

  return (
    <>
      <ErrorMessage error={content.error} />
      <section className="shopping-list-card" aria-label={shoppingList.listName}>
        <div className="shopping-list-head">
          <ShoppingPaneBack />
          <div className="shopping-list-summary">
            <p className="muted">{ownerLine}</p>
            {openTrips.map((trip) => (
              <p key={trip.id} className="shopping-trip-line">
                {tripLine(trip, trip.userId === session.userId, t)}
              </p>
            ))}
          </div>
          <div className="row-actions head-icons">
            <Tooltip label={t('common.rename')}>
              <button
                type="button"
                className="icon-action"
                aria-label={`${shoppingList.listName}: ${t('common.rename')}`}
                onClick={() => setOpened({ dialogKind: 'rename' })}
              >
                <PencilIcon size={16} />
              </button>
            </Tooltip>
            {shoppingList.isOwner ? (
              <Tooltip label={t('shopping.deleteList')}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={`${shoppingList.listName}: ${t('shopping.deleteList')}`}
                  onClick={() => setOpened({ dialogKind: 'deleteList' })}
                >
                  <TrashIcon size={16} />
                </button>
              </Tooltip>
            ) : null}
          </div>
        </div>

        <div className="page-actions shopping-list-actions">
          {/* There is no starting: the first item put in the cart starts
              the person's trip and tells the others. Done shows once there
              is something to buy, and needs no receipt. */}
          {ownTrip || hasOwnCart ? (
            <button
              type="button"
              disabled={isBusy}
              onClick={() => void act(() => finishShopping(ownTrip?.id), t('shopping.shoppingDone'))}
            >
              {t('shopping.doneShopping')}
            </button>
          ) : null}
          {ownTrip?.storeName ? (
            <Link className="button" to={shoppingStoreAddress(ownTrip.storeName, ownTrip.shoppingListIds)}>
              {t('shopping.openStore', { storeName: ownTrip.storeName })}
            </Link>
          ) : (
            <button type="button" disabled={isBusy} onClick={() => setOpened({ dialogKind: 'shopAtStore' })}>
              {t('shopping.shopAtStore')}
            </button>
          )}
          <button type="button" disabled={isBusy} onClick={() => setOpened({ dialogKind: 'import' })}>
            {t('shopping.import')}
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
          <button type="button" disabled={isBusy} onClick={() => setOpened({ dialogKind: 'share' })}>
            {shoppingList.isOwner ? t('shopping.share') : t('shopping.reminders')}
          </button>
          <button
            type="button"
            className="primary"
            disabled={isBusy}
            onClick={() =>
              setOpened({ dialogKind: 'item', target: { shoppingListId: shoppingList.id, shoppingItem: null } })
            }
          >
            <PlusIcon size={16} /> {t('shopping.addItem')}
          </button>
        </div>

        <div className="shopping-tabs-row">
          <Tabs items={tabs} active={tab} onSelect={selectTab} />
          <ShoppingViewSwitch shoppingView={shoppingView} onChoose={chooseShoppingView} />
        </div>

        {tabItems.length === 0 ? (
          <SettingsEmpty>{t(EMPTY_TAB_WORDS[tab])}</SettingsEmpty>
        ) : shoppingView === 'pictures' ? (
          <ShoppingItemGrid
            shoppingItems={shownItems}
            tab={tab}
            isBusy={isBusy}
            onCheck={(shoppingItem, isChecked) => void act(() => checkShoppingItem(shoppingItem.id, isChecked))}
            onEdit={(shoppingItem) => setOpened({ dialogKind: 'item', target: { shoppingItem } })}
            onRemove={(shoppingItem) => setOpened({ dialogKind: 'removeItem', shoppingItem })}
            onNeedAgain={(shoppingItem) => setOpened({ dialogKind: 'needAgain', shoppingItem })}
          />
        ) : (
          <ShoppingItemTable
            shoppingItems={shownItems}
            tab={tab}
            isBusy={isBusy}
            onCheck={(shoppingItem, isChecked) => void act(() => checkShoppingItem(shoppingItem.id, isChecked))}
            onEdit={(shoppingItem) => setOpened({ dialogKind: 'item', target: { shoppingItem } })}
            onRemove={(shoppingItem) => setOpened({ dialogKind: 'removeItem', shoppingItem })}
            onNeedAgain={(shoppingItem) => setOpened({ dialogKind: 'needAgain', shoppingItem })}
          />
        )}
        {tabItems.length > pageSize || pageIndex > 0 ? (
          <div className="list-foot">
            <span />
            <Pager
              pageIndex={pageIndex}
              pageSize={pageSize}
              shownCount={shownItems.length}
              totalCount={tabItems.length}
              onPageIndex={setPageIndex}
              onPageSize={setPageSize}
            />
          </div>
        ) : null}
      </section>

      {opened?.dialogKind === 'item' ? (
        <ShoppingItemDialog
          target={itemDialogTarget(opened.target, shoppingItems)}
          onSaved={() => void reloadAll()}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'import' ? (
        <ShoppingImportDialog
          shoppingList={shoppingList}
          onAdded={(addedToListId) => {
            void reloadAll()
            if (addedToListId !== shoppingList.id) navigate(shoppingListAddress(addedToListId))
          }}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'share' ? (
        <ShoppingShareDialog
          shoppingList={shoppingList}
          onChanged={() => void reloadAll()}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'rename' ? (
        <RenameListDialog
          shoppingList={shoppingList}
          isBusy={isBusy}
          onRename={async (listName) => {
            const isDone = await act(() => renameShoppingList(shoppingList.id, listName), t('shopping.listRenamed'))
            if (isDone) setOpened(null)
          }}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'deleteList' ? (
        <ConfirmDialog
          title={t('shopping.deleteListTitle', { listName: shoppingList.listName })}
          body={plural(
            shoppingItems.length,
            { one: 'shopping.deleteListBodyOne', other: 'shopping.deleteListBodyOther' },
            { listName: shoppingList.listName },
          )}
          confirmLabel={t('shopping.deleteList')}
          busy={isBusy}
          onConfirm={() =>
            void (async () => {
              const isDone = await act(
                () => deleteShoppingList(shoppingList.id),
                t('shopping.listDeleted', { listName: shoppingList.listName }),
              )
              if (isDone) navigate(SHOPPING_PATH, { replace: true })
            })()
          }
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'removeItem' ? (
        <ConfirmDialog
          title={t('shopping.removeTitle', { itemDescription: opened.shoppingItem.itemDescription })}
          body={t('shopping.removeBody')}
          confirmLabel={t('common.remove')}
          busy={isBusy}
          onConfirm={() =>
            void (async () => {
              const isDone = await act(
                () => removeShoppingItems([opened.shoppingItem.id]),
                t('shopping.itemRemoved', { itemDescription: opened.shoppingItem.itemDescription }),
              )
              if (isDone) setOpened(null)
            })()
          }
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'needAgain' ? (
        <NeedAgainDialog
          shoppingItem={opened.shoppingItem}
          isBusy={isBusy}
          onNeedAgain={async (neededQuantity) => {
            const isDone = await act(
              () => setShoppingItemNeed(opened.shoppingItem.id, true, neededQuantity),
              t('shopping.neededAgain', { itemDescription: opened.shoppingItem.itemDescription }),
            )
            if (isDone) setOpened(null)
          }}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {opened?.dialogKind === 'shopAtStore' ? (
        <ShopAtStoreDialog
          shoppingList={shoppingList}
          onChosen={(storeName) => navigate(shoppingStoreAddress(storeName, [shoppingList.id]))}
          onClose={() => setOpened(null)}
        />
      ) : null}
    </>
  )
}

// itemDialogTarget is what the item dialog is opened on, with the newest read
// of the item, so a picture that arrived while the dialog is open shows in it.
function itemDialogTarget(target: ShoppingItemDialogTarget, shoppingItems: ShoppingItem[]): ShoppingItemDialogTarget {
  if (!target.shoppingItem) return target
  const openedItemId = target.shoppingItem.id
  return { shoppingItem: shoppingItems.find((each) => each.id === openedItemId) ?? target.shoppingItem }
}

// tripLine says who is shopping and where, from the shopper's side or the
// reader's.
function tripLine(trip: ShoppingTrip, isOwn: boolean, t: ReturnType<typeof useTranslation>['t']): string {
  const shopperName = trip.shopperName || trip.shopperUsername
  if (isOwn) {
    return trip.storeName ? t('shopping.tripSelf', { storeName: trip.storeName }) : t('shopping.tripSelfNoStore')
  }
  return trip.storeName
    ? t('shopping.tripOther', { shopperName, storeName: trip.storeName })
    : t('shopping.tripOtherNoStore', { shopperName })
}

function RenameListDialog({
  shoppingList,
  isBusy,
  onRename,
  onClose,
}: {
  shoppingList: ShoppingList
  isBusy: boolean
  onRename: (listName: string) => Promise<void>
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [listName, setListName] = useState(shoppingList.listName)
  return (
    <FormDialog
      title={t('shopping.renameList')}
      submitLabel={t('common.save')}
      busy={isBusy}
      canSubmit={listName.trim() !== '' && listName.trim() !== shoppingList.listName}
      onSubmit={() => void onRename(listName.trim())}
      onClose={onClose}
    >
      <label>
        {t('shopping.listName')}
        <input value={listName} maxLength={120} onChange={(event) => setListName(event.target.value)} />
      </label>
    </FormDialog>
  )
}

// NeedAgainDialog asks how many are needed when an item that was bought, or
// not wanted, goes back on the list to buy.
function NeedAgainDialog({
  shoppingItem,
  isBusy,
  onNeedAgain,
  onClose,
}: {
  shoppingItem: ShoppingItem
  isBusy: boolean
  onNeedAgain: (neededQuantity: string) => Promise<void>
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [neededQuantity, setNeededQuantity] = useState(plainNumber(shoppingItem.neededQuantity) || '1')
  return (
    <FormDialog
      title={t('shopping.needAgainTitle', { itemDescription: shoppingItem.itemDescription })}
      submitLabel={t('shopping.needAgain')}
      busy={isBusy}
      canSubmit={isPositiveNumber(neededQuantity)}
      onSubmit={() => void onNeedAgain(neededQuantity.trim())}
      onClose={onClose}
    >
      <label>
        {shoppingItem.quantityUnit
          ? t('shopping.needWithUnit', { unit: shoppingItem.quantityUnit })
          : t('shopping.need')}
        <input inputMode="decimal" value={neededQuantity} onChange={(event) => setNeededQuantity(event.target.value)} />
      </label>
    </FormDialog>
  )
}

// ShopAtStoreDialog picks a store and opens its screen with this list on it.
// Nothing is started: the first item put in the cart there does that.
function ShopAtStoreDialog({
  shoppingList,
  onChosen,
  onClose,
}: {
  shoppingList: ShoppingList
  onChosen: (storeName: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const stores = useQuery(() => listShoppingStores(), [], { refresh: false })
  const [storeName, setStoreName] = useState('')

  return (
    <FormDialog
      title={t('shopping.shopAtStore')}
      submitLabel={t('shopping.openStoreButton')}
      canSubmit={storeName.trim() !== ''}
      onSubmit={() => onChosen(storeName.trim())}
      onClose={onClose}
    >
      <ErrorMessage error={stores.error} />
      <p className="muted">{t('shopping.shopAtStoreHint', { listName: shoppingList.listName })}</p>
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
    </FormDialog>
  )
}
