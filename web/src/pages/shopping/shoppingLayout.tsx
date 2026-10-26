import { useEffect, useRef, useState } from 'react'
import { Navigate, NavLink, Outlet, useNavigate, useOutletContext } from 'react-router-dom'

import { ErrorMessage, Loading } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { PlusIcon } from '../../components/icons'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { createShoppingList, listShoppingLists, ShoppingList } from './shoppingApi'
import { listenToShoppingEvents, shoppingListAddress } from './shoppingItems'

// The Shopping page: the person's lists on the left (their own first, then
// the ones shared with them) and what is open beside them, a list, a store or
// a receipt, each with a path of its own. Every part of it is in the address,
// so Back, a reload and a copied link land in the same place.
//
// The lists are read here once, for every part, and read again when the live
// feed says a list changed.

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
  const lists = useQuery(() => listShoppingLists(), [], { refresh: false })
  const [isNaming, setIsNaming] = useState(false)

  const reloadRef = useRef(lists.reload)
  reloadRef.current = lists.reload
  useEffect(() => listenToShoppingEvents(() => void reloadRef.current(true)), [])

  const shoppingLists = lists.data ?? []
  const ownLists = shoppingLists.filter((shoppingList) => shoppingList.isOwner)
  const sharedLists = shoppingLists.filter((shoppingList) => !shoppingList.isOwner)
  const context: ShoppingLayoutContext = {
    shoppingLists,
    isLoading: lists.loading && !lists.data,
    reloadShoppingLists: () => void lists.reload(true),
  }

  return (
    <div className="shopping-layout">
      <ErrorMessage error={lists.error} />
      <nav className="shopping-lists card" aria-label={t('shopping.listsHeading')}>
        <div className="shopping-lists-head">
          <h3>{t('shopping.listsHeading')}</h3>
          <Tooltip label={t('shopping.newList')}>
            <button
              type="button"
              className="icon-action"
              aria-label={t('shopping.newList')}
              onClick={() => setIsNaming(true)}
            >
              <PlusIcon size={16} />
            </button>
          </Tooltip>
        </div>
        {lists.loading && !lists.data ? <Loading /> : null}
        {lists.data && shoppingLists.length === 0 ? (
          <p className="muted shopping-lists-empty">{t('shopping.noLists')}</p>
        ) : null}
        <ShoppingListLinks shoppingLists={ownLists} />
        {sharedLists.length > 0 ? (
          <>
            <h4 className="shopping-lists-subheading">{t('shopping.sharedWithYou')}</h4>
            <ShoppingListLinks shoppingLists={sharedLists} />
          </>
        ) : null}
      </nav>
      <div className="shopping-main">
        <Outlet context={context} />
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
    </div>
  )
}

function ShoppingListLinks({ shoppingLists }: { shoppingLists: ShoppingList[] }) {
  const { t, plural } = useTranslation()
  if (shoppingLists.length === 0) return null
  return (
    <ul className="shopping-list-links">
      {shoppingLists.map((shoppingList) => (
        <li key={shoppingList.id}>
          <NavLink
            to={shoppingListAddress(shoppingList.id)}
            className={({ isActive }) => (isActive ? 'shopping-list-link is-current' : 'shopping-list-link')}
          >
            <span className="shopping-list-link-name">{shoppingList.listName}</span>
            <span className="muted shopping-list-link-detail">
              {[
                plural(shoppingList.neededCount, { one: 'shopping.toBuyOne', other: 'shopping.toBuyOther' }),
                shoppingList.isOwner
                  ? shoppingList.isShared
                    ? t('shopping.sharedMark')
                    : ''
                  : t('shopping.sharedBy', { name: shoppingList.ownerName || shoppingList.ownerUsername }),
              ]
                .filter(Boolean)
                .join(' · ')}
            </span>
          </NavLink>
        </li>
      ))}
    </ul>
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

// ShoppingHome is /shopping on its own: the first list when there is one, and
// otherwise the offer to make one.
export function ShoppingHome() {
  const { t } = useTranslation()
  const { shoppingLists, isLoading, reloadShoppingLists } = useShoppingLayout()
  const [isNaming, setIsNaming] = useState(false)
  const navigate = useNavigate()
  if (isLoading) return null
  if (shoppingLists.length > 0) {
    return <Navigate replace to={shoppingListAddress(shoppingLists[0].id)} />
  }
  return (
    <div className="card">
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
