import { useEffect, useMemo, useState } from 'react'

import { useBreadcrumbDetail } from '../../components/breadcrumb'
import { ConfirmDialog } from '../../components/dialog'
import { LockIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { ITEM_KINDS, ItemKind } from '../../passwords/fields'
import { useFailureToast } from './passwordsErrors'
import { PasswordsEditor } from './passwordsEditor'
import { PasswordsItemView } from './passwordsItemView'
import {
  filterItems,
  foldersOf,
  ItemFilter,
  KIND_PLURAL_WORDS,
  KIND_WORDS,
  NO_FOLDER,
  PasswordsList,
} from './passwordsList'
import { PasswordItem } from './passwordsProtocol'
import { passwordsSession, usePasswordsSession } from './passwordsSession'

// The unlocked Items tab: the list with its search and filters, or one item,
// with the editor over either. Which item, the editor, the kind and Recently
// deleted are in the address. The search text and the folder name are
// decrypted values, so they stay in this component's state: the address ends up
// in browser history and in the server's request logs.

export type ChangeParameters = (changes: Record<string, string | null>, options?: { replace?: boolean }) => void

function isItemKind(text: string | null): text is ItemKind {
  return (ITEM_KINDS as readonly (string | null)[]).includes(text)
}

export function PasswordsItems({
  parameters,
  changeParameters,
}: {
  parameters: URLSearchParams
  changeParameters: ChangeParameters
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const snapshot = usePasswordsSession()
  const [isChoosingKind, setIsChoosingKind] = useState(false)
  const [search, setSearch] = useState('')
  const [folder, setFolder] = useState('')

  const filter: ItemFilter = {
    search,
    itemKind: isItemKind(parameters.get('kind')) ? (parameters.get('kind') as ItemKind) : '',
    folder,
    isDeleted: parameters.get('deleted') === '1',
  }
  const itemId = parameters.get('item')
  const newKind = parameters.get('new')
  const item = itemId ? (snapshot.items.find((candidate) => candidate.id === itemId) ?? null) : null
  const isEditing = parameters.get('edit') === '1' && item !== null && item.isOwnedByMe && !item.isUnreadable

  // The trail and the tab title name the kind ("Login"), never the item: a
  // title is kept in the browser's history and sync, and the extension sends
  // the title of the tab it is attached to to the server.
  useBreadcrumbDetail(item ? t(KIND_WORDS[item.itemKind]) : null)

  // A link to an item that is gone (deleted for good, or no longer shared) is
  // taken out of the address once the items are read, and the person is told.
  const isItemGone = itemId !== null && snapshot.hasLoadedItems && item === null
  useEffect(() => {
    if (!isItemGone) return
    toast.failed(t('passwords.itemNotFound'))
    changeParameters({ item: null, edit: null }, { replace: true })
    // Once per link found stale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isItemGone, itemId])

  const folders = useMemo(
    () => foldersOf(snapshot.items.filter((candidate) => candidate.isOwnedByMe)),
    [snapshot.items],
  )
  const shownItems = useMemo(
    () => filterItems(snapshot.items, filter),
    // The filter is built from the address and the state each render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [snapshot.items, filter.search, filter.itemKind, filter.folder, filter.isDeleted],
  )

  function addressOf(openedItemId: string): string {
    const written = new URLSearchParams(parameters)
    written.set('item', openedItemId)
    written.delete('page')
    return `?${written.toString()}`
  }

  function lockNow() {
    passwordsSession.lock()
    toast.done(t('passwords.lockedNow'))
  }

  async function restore(restored: PasswordItem) {
    try {
      await passwordsSession.restoreItem(restored.id)
      toast.done(t('passwords.itemRestored', { name: restored.fields.name }))
    } catch (failure) {
      reportFailure(failure, t('passwords.restoreFailed'))
    }
  }

  const kindOptions = [
    { value: '', label: t('passwords.kindAll') },
    ...ITEM_KINDS.map((itemKind) => ({ value: itemKind, label: t(KIND_PLURAL_WORDS[itemKind]) })),
  ]
  const folderOptions = [
    { value: '', label: t('passwords.folderAll') },
    { value: NO_FOLDER, label: t('passwords.folderNone') },
    ...folders.map((folder) => ({ value: folder, label: folder })),
  ]

  return (
    <>
      {item === null && (
        <SettingsSection card>
          <div className="passwords-toolbar">
            <input
              type="search"
              className="passwords-search"
              aria-label={t('passwords.search')}
              placeholder={t('passwords.search')}
              value={filter.search}
              onChange={(event) => setSearch(event.target.value)}
            />
            <Select
              value={filter.itemKind}
              label={t('passwords.kindFilter')}
              options={kindOptions}
              onChange={(value) => changeParameters({ kind: value || null, page: null })}
            />
            {folders.length > 0 && (
              <Select
                value={filter.folder}
                label={t('passwords.folderFilter')}
                options={folderOptions}
                onChange={setFolder}
              />
            )}
          </div>
          <div className="page-actions">
            <button type="button" className="primary" onClick={() => setIsChoosingKind(true)}>
              {t('passwords.new')}
            </button>
            <button
              type="button"
              aria-pressed={filter.isDeleted}
              onClick={() => changeParameters({ deleted: filter.isDeleted ? null : '1', page: null })}
            >
              {t('passwords.recentlyDeleted')}
            </button>
            <button type="button" onClick={lockNow}>
              <LockIcon size={16} /> {t('passwords.lock')}
            </button>
          </div>
          {filter.isDeleted && <p className="muted">{t('passwords.deletedBody')}</p>}
          {!snapshot.hasLoadedItems ? (
            <p className="muted">{t('common.loading')}</p>
          ) : (
            <PasswordsList
              items={shownItems}
              hasAnyItems={snapshot.items.some((candidate) => (candidate.deletedAt !== null) === filter.isDeleted)}
              filter={filter}
              addressOf={addressOf}
              onRestore={(restored) => void restore(restored)}
            />
          )}
        </SettingsSection>
      )}

      {item !== null && (
        <PasswordsItemView
          key={item.id}
          item={item}
          onBack={() => changeParameters({ item: null, edit: null })}
          onEdit={() => changeParameters({ edit: '1' })}
        />
      )}

      {isChoosingKind && (
        <ConfirmDialog
          title={t('passwords.newTitle')}
          destructive={false}
          body={
            <div className="passwords-new-kinds">
              {ITEM_KINDS.map((itemKind) => (
                <button
                  key={itemKind}
                  type="button"
                  onClick={() => {
                    setIsChoosingKind(false)
                    changeParameters({ new: itemKind, item: null, edit: null })
                  }}
                >
                  {t(KIND_WORDS[itemKind])}
                </button>
              ))}
            </div>
          }
          onClose={() => setIsChoosingKind(false)}
        />
      )}

      {isItemKind(newKind) && (
        <PasswordsEditor
          itemKind={newKind}
          existing={null}
          folders={folders}
          onSaved={(saved) => changeParameters({ new: null, item: saved.id })}
          onClose={() => changeParameters({ new: null })}
        />
      )}
      {isEditing && item !== null && (
        <PasswordsEditor
          key={item.id}
          itemKind={item.itemKind}
          existing={item}
          folders={folders}
          onSaved={() => changeParameters({ edit: null })}
          onClose={() => changeParameters({ edit: null })}
        />
      )}
    </>
  )
}
