import { Link } from 'react-router-dom'

import { Tag } from '../../components/common'
import { Column, DataTable } from '../../components/dataTable'
import { RestoreIcon } from '../../components/icons'
import { RelativeTime } from '../../components/relativeTime'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'
import { ItemKind } from '../../passwords/fields'
import { PasswordItem } from './passwordsProtocol'

// The list of items: a DataTable, where the whole row opens the item. Search,
// the kind and the folder narrow it before it gets here; the page, the search
// and the filters are in the address.

export type ItemFilter = {
  search: string
  itemKind: ItemKind | ''
  folder: string
  // isDeleted lists the items in Recently deleted instead of the others.
  isDeleted: boolean
}

// The folder filter's choice for items kept in no folder.
export const NO_FOLDER = '\u0000none'

export const KIND_WORDS: Record<ItemKind, Key> = {
  login: 'passwords.kindLogin',
  card: 'passwords.kindCard',
  note: 'passwords.kindNote',
}

export const KIND_PLURAL_WORDS: Record<ItemKind, Key> = {
  login: 'passwords.kindLogins',
  card: 'passwords.kindCards',
  note: 'passwords.kindNotes',
}

// siteHost is a site as it is listed: the host, without the scheme and path.
export function siteHost(site: string): string {
  try {
    return new URL(site).host || site
  } catch {
    return site
  }
}

// searchableText is what a search looks in: the name, the user name, the sites,
// the folder and a card's holder. Never a password, a code or a note's text.
function searchableText(item: PasswordItem): string {
  if (item.itemKind === 'login') {
    return [item.fields.name, item.fields.userName, item.fields.folder, ...item.fields.sites].join('\n')
  }
  if (item.itemKind === 'card') {
    return [item.fields.name, item.fields.cardholderName, item.fields.brand, item.fields.folder].join('\n')
  }
  return [item.fields.name, item.fields.folder].join('\n')
}

export function filterItems(items: PasswordItem[], filter: ItemFilter): PasswordItem[] {
  const words = filter.search.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return items.filter((item) => {
    if ((item.deletedAt !== null) !== filter.isDeleted) return false
    if (filter.itemKind !== '' && item.itemKind !== filter.itemKind) return false
    if (filter.folder === NO_FOLDER && item.fields.folder !== '') return false
    if (filter.folder !== '' && filter.folder !== NO_FOLDER && item.fields.folder !== filter.folder) return false
    const text = searchableText(item).toLowerCase()
    return words.every((word) => text.includes(word))
  })
}

// foldersOf are the folders the items use, once each.
export function foldersOf(items: PasswordItem[]): string[] {
  return [...new Set(items.map((item) => item.fields.folder).filter((folder) => folder !== ''))].sort((first, second) =>
    first.localeCompare(second),
  )
}

// itemSummary is the second column: a login's user name, a card's brand and
// last digits.
export function itemSummary(item: PasswordItem): string {
  if (item.itemKind === 'login') return item.fields.userName
  if (item.itemKind === 'card') {
    const lastDigits = item.fields.number.replace(/\D/g, '').slice(-4)
    return [item.fields.brand, lastDigits ? `•••• ${lastDigits}` : ''].filter(Boolean).join(' ')
  }
  return ''
}

export function PasswordsList({
  items,
  hasAnyItems,
  filter,
  addressOf,
  onRestore,
}: {
  // items are the items already filtered.
  items: PasswordItem[]
  // hasAnyItems says whether there are items at all, filtered or not, so an
  // empty list can say "no items match" or "no items yet".
  hasAnyItems: boolean
  filter: ItemFilter
  // addressOf is the search part of the address that opens an item.
  addressOf: (itemId: string) => string
  onRestore: (item: PasswordItem) => void
}) {
  const { t, plural } = useTranslation()

  const nameColumn: Column<PasswordItem> = {
    key: 'name',
    header: t('passwords.columnName'),
    truncate: true,
    value: (item) => item.fields.name || t('passwords.unreadableItem'),
    render: (item) =>
      filter.isDeleted ? (
        item.fields.name || t('passwords.unreadableItem')
      ) : (
        <Link to={{ search: addressOf(item.id) }}>{item.fields.name || t('passwords.unreadableItem')}</Link>
      ),
  }
  const marksColumn: Column<PasswordItem> = {
    key: 'marks',
    header: '',
    render: (item) => (
      <span className="passwords-marks">
        {filter.isDeleted ? (
          item.isOwnedByMe && (
            <Tooltip label={t('passwords.restore')}>
              <button
                type="button"
                className="icon-action"
                aria-label={`${item.fields.name}: ${t('passwords.restore')}`}
                onClick={() => onRestore(item)}
              >
                <RestoreIcon size={16} />
              </button>
            </Tooltip>
          )
        ) : (
          <>
            {item.isSharedWithMe && <Tag value={t('passwords.tagSharedBy', { name: item.ownerName })} />}
            {item.isOwnedByMe && item.shares.length > 0 && <Tag value={t('passwords.tagShared')} />}
            {item.isUsableByAgent && <Tag value={t('passwords.tagAgent')} tone="good" />}
          </>
        )}
      </span>
    ),
  }
  const columns: Column<PasswordItem>[] = filter.isDeleted
    ? [
        nameColumn,
        {
          key: 'deleted',
          header: t('passwords.columnDeleted'),
          render: (item) => item.deletedAt && <RelativeTime value={item.deletedAt} />,
        },
        {
          key: 'kind',
          header: t('passwords.columnKind'),
          optional: true,
          render: (item) => <span className="muted">{t(KIND_WORDS[item.itemKind])}</span>,
        },
        marksColumn,
      ]
    : [
        nameColumn,
        {
          key: 'detail',
          header: t('passwords.columnDetail'),
          truncate: true,
          value: (item) => itemSummary(item),
          render: (item) => <span className="muted">{itemSummary(item)}</span>,
        },
        {
          key: 'site',
          header: t('passwords.columnSite'),
          optional: true,
          truncate: true,
          value: (item) =>
            item.itemKind === 'login' && item.fields.sites.length > 0 ? siteHost(item.fields.sites[0]) : '',
          render: (item) => (
            <span className="muted">
              {item.itemKind === 'login' && item.fields.sites.length > 0 ? siteHost(item.fields.sites[0]) : ''}
            </span>
          ),
        },
        marksColumn,
      ]

  return (
    <DataTable
      columns={columns}
      rows={items}
      rowKey={(item) => item.id}
      rowLink={filter.isDeleted ? undefined : (item) => addressOf(item.id)}
      emptyMessage={t(
        filter.isDeleted ? 'passwords.deletedEmpty' : hasAnyItems ? 'passwords.noMatches' : 'passwords.listEmpty',
      )}
      countLabel={(itemCount) =>
        plural(itemCount, { one: 'passwords.itemCountOne', other: 'passwords.itemCount' }, { count: itemCount })
      }
    />
  )
}
