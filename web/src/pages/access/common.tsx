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
  hint,
  empty,
}: {
  label: string
  items: T[]
  selected: string[]
  onChange: (selected: string[]) => void
  describe: (item: T) => React.ReactNode
  hint?: React.ReactNode
  empty?: Key
}) {
  const { t } = useTranslation()
  return (
    <fieldset className="check-list">
      <legend>{label}</legend>
      {hint && <p className="muted">{hint}</p>}
      {items.length === 0 && empty && <p className="muted">{t(empty)}</p>}
      {items.map((item) => {
        const checked = selected.includes(item.id)
        return (
          <label key={item.id} className="check-list-item">
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
