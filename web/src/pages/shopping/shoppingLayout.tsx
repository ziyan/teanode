import { useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate, useOutletContext } from 'react-router-dom'

import { ErrorMessage, Loading } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { ArrowLeftIcon, PlusIcon } from '../../components/icons'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { createShoppingList, listShoppingLists, listShoppingStores, ShoppingList, ShoppingStore } from './shoppingApi'
import { listenToShoppingEvents, SHOPPING_PATH, shoppingListAddress, shoppingStoreAddress } from './shoppingItems'

// The Shopping page, drawn as the mailbox is: the person's lists (their own
// first, then the ones shared with them) and the stores their items are for
// in the column on the left, and what is open beside them, a list, a store or
// a receipt, in the pane, each with a path of its own. Every part of it is in
// the address, so Back, a reload and a copied link land in the same place.
// Where the two do not fit side by side, one shows at a time: the column at
// /shopping, the pane once something is open, with a way back to the column.
//
// The lists and stores are read here once, for every part, and read again
// when the live feed says a list changed.

export interface ShoppingLayoutContext {
  shoppingLists: ShoppingList[]
  // Whether the first answer is still outstanding.
  isLoading: boolean
  // Reads the lists again, quietly.
  reloadShoppingLists: () => void
}

export function useShoppingLayout(): ShoppingLayoutContext {
  return useOutletContext<ShoppingLayoutContext>()
}

export function ShoppingLayout() {
  const session = useSession()
  const { t } = useTranslation()
  if (!hasPermission(session.permissions, 'shopping:use')) {
    return (
      <SettingsSection card title={t('shopping.title')}>
        <SettingsEmpty>{t('shopping.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <ShoppingLayoutContent key={session.userId ?? ''} />
}

function ShoppingLayoutContent() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const lists = useQuery(() => listShoppingLists(), [], { refresh: false })
  const stores = useQuery(() => listShoppingStores(), [], { refresh: false })
  const [isNaming, setIsNaming] = useState(false)

  const reloadRef = useRef(() => {
    void lists.reload(true)
    void stores.reload(true)
  })
  reloadRef.current = () => {
    void lists.reload(true)
    void stores.reload(true)
  }
  useEffect(() => listenToShoppingEvents(() => reloadRef.current()), [])

  const shoppingLists = lists.data ?? []
  const ownLists = shoppingLists.filter((shoppingList) => shoppingList.isOwner)
  const sharedLists = shoppingLists.filter((shoppingList) => !shoppingList.isOwner)
  const shoppingStores = stores.data ?? []
  const context: ShoppingLayoutContext = {
    shoppingLists,
    isLoading: lists.loading && !lists.data,
    reloadShoppingLists: () => reloadRef.current(),
  }
  // Something is open in the pane at every address below /shopping itself.
  const isReading = location.pathname.replace(/\/+$/, '') !== SHOPPING_PATH

  return (
    <>
      <ErrorMessage error={lists.error} />
      <div className="mailbox-frame">
        <div className={['mailbox', 'shopping-panes', isReading ? 'reading' : ''].filter(Boolean).join(' ')}>
          <nav className="mailbox-list" aria-label={t('shopping.listsHeading')}>
            <div className="list-toolbar shopping-list-toolbar">
              <button type="button" className="primary" onClick={() => setIsNaming(true)}>
                <PlusIcon size={16} /> {t('shopping.newList')}
              </button>
            </div>
            <div className="mailbox-rows">
              {lists.loading && !lists.data ? <Loading /> : null}
              {lists.data && shoppingLists.length === 0 ? (
                <p className="mailbox-placeholder">{t('shopping.noLists')}</p>
              ) : null}
              <ShoppingListRows shoppingLists={ownLists} />
              {sharedLists.length > 0 ? (
                <>
                  <h4 className="shopping-rows-heading">{t('shopping.sharedWithYou')}</h4>
                  <ShoppingListRows shoppingLists={sharedLists} />
                </>
              ) : null}
              {shoppingStores.length > 0 ? (
                <>
                  <h4 className="shopping-rows-heading">{t('shopping.storesHeading')}</h4>
                  <ShoppingStoreRows shoppingStores={shoppingStores} />
                </>
              ) : null}
            </div>
          </nav>
          <div className="mailbox-pane shopping-pane">
            <Outlet context={context} />
          </div>
        </div>
      </div>
      {isNaming ? (
        <NewListDialog
          onCreated={(shoppingList) => {
            setIsNaming(false)
            context.reloadShoppingLists()
            navigate(shoppingListAddress(shoppingList.id))
          }}
          onClose={() => setIsNaming(false)}
        />
      ) : null}
    </>
  )
}

function rowClassName({ isActive }: { isActive: boolean }): string {
  return isActive ? 'shopping-row-link active' : 'shopping-row-link'
}

function ShoppingListRows({ shoppingLists }: { shoppingLists: ShoppingList[] }) {
  const { t } = useTranslation()
  if (shoppingLists.length === 0) return null
  return (
    <ul className="shopping-rows">
      {shoppingLists.map((shoppingList) => (
        <li key={shoppingList.id}>
          <NavLink to={shoppingListAddress(shoppingList.id)} className={rowClassName}>
            <span className="shopping-row-name">{shoppingList.listName}</span>
            <span className="shopping-row-count">{shoppingList.neededCount.toLocaleString()}</span>
            <span className="shopping-row-meta">
              {shoppingList.isOwner
                ? shoppingList.isShared
                  ? t('shopping.sharedMark')
                  : t('shopping.notShared')
                : t('shopping.sharedBy', { name: shoppingList.ownerName || shoppingList.ownerUsername })}
            </span>
          </NavLink>
        </li>
      ))}
    </ul>
  )
}

// ShoppingStoreRows are the stores the person's items are for, each opening
// the store's screen, with how many items are needed there.
function ShoppingStoreRows({ shoppingStores }: { shoppingStores: ShoppingStore[] }) {
  return (
    <ul className="shopping-rows">
      {shoppingStores.map((shoppingStore) => (
        <li key={shoppingStore.storeKey}>
          <NavLink to={shoppingStoreAddress(shoppingStore.storeName)} className={rowClassName}>
            <span className="shopping-row-name">{shoppingStore.storeName}</span>
            <span className="shopping-row-count">{shoppingStore.neededCount.toLocaleString()}</span>
          </NavLink>
        </li>
      ))}
    </ul>
  )
}

// ShoppingPaneBack is the way back to the column of lists, for the width
// where the column is not beside the pane; with both in view it is hidden.
export function ShoppingPaneBack() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  return (
    <Tooltip label={t('shopping.backToLists')}>
      <button
        type="button"
        className="icon-button shopping-pane-back"
        aria-label={t('shopping.backToLists')}
        onClick={() => navigate(SHOPPING_PATH)}
      >
        <ArrowLeftIcon size={16} />
      </button>
    </Tooltip>
  )
}

// NewListDialog names a list and makes it.
function NewListDialog({
  onCreated,
  onClose,
}: {
  onCreated: (shoppingList: ShoppingList) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [listName, setListName] = useState('')
  const [isBusy, setIsBusy] = useState(false)

  async function submit() {
    setIsBusy(true)
    try {
      const shoppingList = await createShoppingList(listName.trim())
      toast.done(t('shopping.listCreated', { listName: shoppingList.listName }))
      onCreated(shoppingList)
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('shopping.newList')}
      submitLabel={t('common.create')}
      busy={isBusy}
      canSubmit={listName.trim() !== ''}
      onSubmit={() => void submit()}
      onClose={onClose}
    >
      <label>
        {t('shopping.listName')}
        <input
          value={listName}
          maxLength={120}
          placeholder={t('shopping.listNamePlaceholder')}
          onChange={(event) => setListName(event.target.value)}
        />
      </label>
    </FormDialog>
  )
}

// ShoppingHome is /shopping on its own: in the pane, a line saying to choose
// a list or a store, and with no lists yet the offer to make one.
export function ShoppingHome() {
  const { t } = useTranslation()
  const { shoppingLists, isLoading, reloadShoppingLists } = useShoppingLayout()
  const [isNaming, setIsNaming] = useState(false)
  const navigate = useNavigate()
  if (isLoading) return null
  if (shoppingLists.length > 0) {
    return (
      <div className="mailbox-pane-placeholder">
        <span>{t('shopping.choose')}</span>
      </div>
    )
  }
  return (
    <div className="shopping-home">
      <h3>{t('shopping.title')}</h3>
      <p className="muted">{t('shopping.intro')}</p>
      <div className="page-actions">
        <button type="button" className="primary" onClick={() => setIsNaming(true)}>
          <PlusIcon size={16} /> {t('shopping.newList')}
        </button>
      </div>
      {isNaming ? (
        <NewListDialog
          onCreated={(shoppingList) => {
            reloadShoppingLists()
            navigate(shoppingListAddress(shoppingList.id))
          }}
          onClose={() => setIsNaming(false)}
        />
      ) : null}
    </div>
  )
}
