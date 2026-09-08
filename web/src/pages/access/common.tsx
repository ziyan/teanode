import { useRef, useState } from 'react'

import { graphql } from '../../api'
import { Key, useTranslation } from '../../i18n/i18n'

// What the four access pages share: the shapes the API returns, the queries
// that list them, and the one control they all use — a list of things to
// tick.

export type User = {
  id: string
  username: string
  name?: string
  email?: string
  disabledAt?: string | null
  hasPassword: boolean
  groupIds: string[]
  createdAt: string
}

export type Group = {
  id: string
  name: string
  description?: string
  idpGroup?: string
  userIds: string[]
  roleIds: string[]
  domainIds: string[]
}

export type Role = {
  id: string
  name: string
  description?: string
  permissions: string[]
}

export type PermissionDescription = {
  key: string
  kind: 'server' | 'domain' | 'all-domains'
  widens?: string
}

export type DomainSummary = { id: string; domain: string }

export const USER_FIELDS = '{ id username name email disabledAt hasPassword groupIds createdAt }'
export const GROUP_FIELDS = '{ id name description idpGroup userIds roleIds domainIds }'
export const ROLE_FIELDS = '{ id name description permissions }'

export const LIST_USERS = `query { ListUsers ${USER_FIELDS} }`
export const LIST_GROUPS = `query { ListGroups ${GROUP_FIELDS} }`
export const LIST_ROLES = `query { ListRoles ${ROLE_FIELDS} }`
export const LIST_PERMISSIONS = `query { ListPermissions { key kind widens } }`
export const LIST_DOMAINS = `query { ListDomains { id domain } }`

export async function listUsers(): Promise<User[]> {
  return (await graphql<{ ListUsers: User[] }>(LIST_USERS)).ListUsers
}

export async function listGroups(): Promise<Group[]> {
  return (await graphql<{ ListGroups: Group[] }>(LIST_GROUPS)).ListGroups
}

export async function listRoles(): Promise<Role[]> {
  return (await graphql<{ ListRoles: Role[] }>(LIST_ROLES)).ListRoles
}

export async function listPermissions(): Promise<PermissionDescription[]> {
  return (await graphql<{ ListPermissions: PermissionDescription[] }>(LIST_PERMISSIONS)).ListPermissions
}

export async function listDomains(): Promise<DomainSummary[]> {
  return (await graphql<{ ListDomains: DomainSummary[] }>(LIST_DOMAINS)).ListDomains
}

// CheckList is a set of things to tick: users in a group, roles of a group,
// permissions of a role. Ticks rather than a multi-select, because a list of
// twelve names with checkboxes is readable and a select with twelve options
// held down with a modifier key is not.
export function CheckList<T extends { id: string }>({
  label,
  items,
  selected,
  onChange,
  describe,
  text,
  hint,
  empty,
}: {
  label: string
  items: T[]
  selected: string[]
  onChange: (selected: string[]) => void
  describe: (item: T) => React.ReactNode
  // What a search matches against. Given for the lists that grow — the
  // people on a server, its domains — and left out for the short ones.
  text?: (item: T) => string
  hint?: React.ReactNode
  empty?: Key
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')

  // What was chosen when this list was opened. The order is settled from it
  // rather than from what is chosen now, because a row that jumps to the top
  // the moment it is ticked takes itself out from under the cursor — and
  // ticking five people in a row would reorder the list five times.
  const settled = useRef(new Set(selected))
  const listed = useRef(items)
  if (listed.current !== items) {
    listed.current = items
    settled.current = new Set(selected)
  }

  // A list of a dozen people is a list somebody scrolls past what they came
  // for. Beyond a handful it gets a search, and what is already chosen is
  // kept at the top: a group's membership is what you came to read, and
  // hunting for it among everybody else is the thing this control was
  // getting wrong.
  const searchable = Boolean(text) && items.length > 7
  const wanted = query.trim().toLowerCase()
  const shown = items.filter((item) => !searchable || !wanted || (text?.(item) ?? '').toLowerCase().includes(wanted))
  const ordered = [...shown].sort(
    (left, right) => Number(settled.current.has(right.id)) - Number(settled.current.has(left.id)),
  )
  const chosenHere = items.filter((item) => selected.includes(item.id)).length

  return (
    <fieldset className="check-list">
      <legend>
        {label}
        {chosenHere > 0 && <span className="muted"> · {t('access.checkList.selected', { count: chosenHere })}</span>}
      </legend>
      {hint && <p className="muted">{hint}</p>}
      {searchable && (
        <input
          type="search"
          className="check-list-search"
          placeholder={t('access.checkList.search')}
          aria-label={`${label}: ${t('access.checkList.search')}`}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          // The list sits inside a form whose button saves. Enter here means
          // "I have finished typing the search", not "save".
          onKeyDown={(event) => event.key === 'Enter' && event.preventDefault()}
        />
      )}
      {items.length === 0 && empty && <p className="muted">{t(empty)}</p>}
      {items.length > 0 && ordered.length === 0 && <p className="muted">{t('access.checkList.noMatches')}</p>}
      <div className="check-list-items">
        {ordered.map((item) => {
          const checked = selected.includes(item.id)
          return (
            <label key={item.id} className={checked ? 'check-list-item chosen' : 'check-list-item'}>
              <input
                type="checkbox"
                checked={checked}
                onChange={(event) =>
                  onChange(
                    event.target.checked
                      ? [...selected, item.id]
                      : selected.filter((candidate) => candidate !== item.id),
                  )
                }
              />
              <span>{describe(item)}</span>
            </label>
          )
        })}
      </div>
    </fieldset>
  )
}

// permissionLabel is the human reading of a permission key, with the key
// itself beside it so the two cannot be confused.
export function usePermissionLabel() {
  const { t } = useTranslation()
  return (key: string): string => {
    const translation = `access.permission.${key}` as Key
    const label = t(translation)
    return label === translation ? key : label
  }
}
