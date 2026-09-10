import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { ChevronDownIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'

const EVENTS = `
  query ($resourceType: String, $first: Int, $offset: Int) {
    ListAuditEvents(resourceType: $resourceType, first: $first, offset: $offset) {
      total
      events {
        id createdAt actorKind actorLabel sourceIp instance
        resourceType resourceId resourceLabel resourceLink action before after
      }
    }
  }`

type AuditEvent = {
  id: string
  createdAt: string
  actorKind: string
  actorLabel?: string
  sourceIp?: string
  instance?: string
  resourceType: string
  resourceId: string
  // What the thing is called, and where it is, resolved by the server: an id
  // says who changed what and nothing about which one.
  resourceLabel?: string
  resourceLink?: string
  action: 'create' | 'update' | 'delete'
  before?: Record<string, unknown> | null
  after?: Record<string, unknown> | null
}

// changedFields is what actually differs between the two sides of an update,
// which is nearly always two or three of thirty. Printing both rows in full
// left the reader to diff them by eye.
function changedFields(
  before: Record<string, unknown> | null | undefined,
  after: Record<string, unknown> | null | undefined,
): string[] {
  const keys = new Set([...Object.keys(before ?? {}), ...Object.keys(after ?? {})])
  return [...keys].filter((key) => JSON.stringify(before?.[key]) !== JSON.stringify(after?.[key])).sort()
}

// A value as a line of text. Objects and lists are printed as JSON, because
// that is what they are; a missing value says so rather than showing nothing.
function describe(value: unknown, missing: string): string {
  if (value === undefined || value === null || value === '') {
    return missing
  }
  if (typeof value === 'string') {
    return value
  }
  return JSON.stringify(value)
}

const PAGE = 50

const RESOURCE_TYPES = [
  '',
  'user',
  'group',
  'role',
  'domain',
  'alias',
  'credential',
  'mailbox',
  'mailbox_address',
  'mailbox_app_password',
  'token',
  'passkey',
  'configuration',
]

// AuditTab is the log of administrative changes, newest first: who, what,
// and the row before and after.
export function AuditTab() {
  const { t } = useTranslation()
  // What the log is filtered to is in the address: this is the page somebody
  // comes to in order to find one event, and finding it is no use if what
  // found it cannot be sent to anybody or returned to.
  const [parameters, setParameters] = useSearchParams()
  const resourceType = parameters.get('resource') ?? ''
  const chooseResource = (kind: string) => setParameters(kind ? { resource: kind } : {})

  // How much has been asked for stays local. "Show more" is not a filter,
  // and a back button that shrank the list again would be a strange thing to
  // have built.
  const [limit, setLimit] = useState(PAGE)
  const [open, setOpen] = useState<string | null>(null)
  const { data, error, loading } = useQuery(
    () =>
      graphql<{ ListAuditEvents: { total: number; events: AuditEvent[] } }>(EVENTS, {
        resourceType: resourceType || null,
        first: limit,
        offset: 0,
      }),
    [resourceType, limit],
  )

  const page = data?.ListAuditEvents
  const events = page?.events ?? []

  return (
    <SettingsSection description={t('access.audit.intro')}>
      <p className="audit-filter">
        <span className="muted">{t('access.audit.filter')}</span>
        <Select
          label={t('access.audit.filter')}
          value={resourceType}
          onChange={chooseResource}
          options={RESOURCE_TYPES.map((candidate) => ({
            value: candidate,
            label: candidate === '' ? t('access.audit.everything') : candidate,
          }))}
        />
      </p>
      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}
      {page && events.length === 0 && <SettingsEmpty>{t('access.audit.empty')}</SettingsEmpty>}

      {events.map((event) => (
        <SettingsRow
          key={event.id}
          title={
            <>
              {event.actorLabel || event.actorKind} <span className="muted">{t(`access.audit.${event.action}`)}</span>{' '}
              {event.resourceType}{' '}
              {/* What the thing is called, and a way to it. The id is what
                  the row stores and the one thing nobody can read, so it is
                  the fallback rather than the answer — and it is still on the
                  page for anybody who wants to search for it. */}
              {event.resourceLabel ? (
                <Tooltip label={event.resourceId}>
                  {event.resourceLink ? (
                    <Link to={event.resourceLink}>{event.resourceLabel}</Link>
                  ) : (
                    <span>{event.resourceLabel}</span>
                  )}
                </Tooltip>
              ) : (
                <span className="mono muted">{event.resourceId}</span>
              )}
            </>
          }
          badge={
            <Tag
              value={event.action}
              tone={event.action === 'delete' ? 'bad' : event.action === 'create' ? 'good' : undefined}
            />
          }
          subtitle={
            <>
              <div>
                {formatTime(event.createdAt)}
                {event.sourceIp ? ` · ${event.sourceIp}` : ''}
                {event.instance ? ` · ${event.instance}` : ''}
              </div>
              {open === event.id &&
                (changedFields(event.before, event.after).length === 0 ? (
                  <p className="muted">{t('access.audit.noChange')}</p>
                ) : (
                  /* A table, because it is one: a column of fields and a
                     column for each side of the change. Laid out as cards
                     across the width, five fields came out in five places
                     and the eye had to find each one before it could read
                     it. Scrolls sideways on a phone rather than folding a
                     row of three columns into six lines. */
                  <div className="audit-diff">
                    <table className="audit-table">
                      <thead>
                        <tr>
                          <th>{t('access.audit.field')}</th>
                          <th>{t('access.audit.before')}</th>
                          <th>{t('access.audit.after')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {changedFields(event.before, event.after).map((field) => (
                          <tr key={field}>
                            <th scope="row">{field}</th>
                            <td className="audit-before">{describe(event.before?.[field], t('access.audit.unset'))}</td>
                            <td>{describe(event.after?.[field], t('access.audit.unset'))}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ))}
            </>
          }
          actions={
            <div className="row-actions">
              <Tooltip label={open === event.id ? t('access.audit.hide') : t('access.audit.show')}>
                <button
                  className="icon-action"
                  type="button"
                  aria-expanded={open === event.id}
                  aria-label={open === event.id ? t('access.audit.hide') : t('access.audit.show')}
                  onClick={() => setOpen(open === event.id ? null : event.id)}
                >
                  {/* One arrow, turned. Swapping it for a different arrow
                      redraws the button at the moment it is pressed, which
                      reads as a flicker; turning the one that is there shows
                      the same thing happening, and shows it happening. */}
                  <ChevronDownIcon size={16} className="chevron" />
                </button>
              </Tooltip>
            </div>
          }
        />
      ))}

      {page && page.total > events.length && (
        <p>
          <button className="link" type="button" onClick={() => setLimit(limit + PAGE)}>
            {t('access.audit.more', { shown: events.length, total: page.total })}
          </button>
        </p>
      )}
    </SettingsSection>
  )
}
