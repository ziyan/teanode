import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'

const EVENTS = `
  query ($resourceType: String, $first: Int, $offset: Int) {
    ListAuditEvents(resourceType: $resourceType, first: $first, offset: $offset) {
      total
      events { id createdAt actorKind actorLabel sourceIp instance resourceType resourceId action before after }
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
  action: 'create' | 'update' | 'delete'
  before?: unknown
  after?: unknown
}

const PAGE = 50

const RESOURCE_TYPES = ['', 'user', 'group', 'role', 'domain', 'alias', 'credential', 'mailbox', 'mailbox_address', 'mailbox_app_password', 'token', 'passkey', 'configuration']

// AuditTab is the log of administrative changes, newest first: who, what,
// and the row before and after.
export function AuditTab() {
  const { t } = useTranslation()
  const [resourceType, setResourceType] = useState('')
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
    <SettingsSection
      description={t('access.audit.intro')}
      action={
        <select value={resourceType} onChange={(event) => setResourceType(event.target.value)}>
          {RESOURCE_TYPES.map((candidate) => (
            <option key={candidate} value={candidate}>
              {candidate === '' ? t('access.audit.everything') : candidate}
            </option>
          ))}
        </select>
      }
    >
      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}
      {page && events.length === 0 && <SettingsEmpty>{t('access.audit.empty')}</SettingsEmpty>}

      {events.map((event) => (
        <SettingsRow
          key={event.id}
          title={
            <>
              {event.actorLabel || event.actorKind} <span className="muted">{t(`access.audit.${event.action}`)}</span>{' '}
              {event.resourceType} <span className="mono muted">{event.resourceId}</span>
            </>
          }
          badge={<Tag value={event.action} tone={event.action === 'delete' ? 'bad' : event.action === 'create' ? 'good' : undefined} />}
          subtitle={
            <>
              <div>
                {formatTime(event.createdAt)}
                {event.sourceIp ? ` · ${event.sourceIp}` : ''}
                {event.instance ? ` · ${event.instance}` : ''}
              </div>
              {open === event.id && (
                <div className="audit-diff">
                  {event.before !== undefined && event.before !== null && (
                    <div>
                      <div className="muted">{t('access.audit.before')}</div>
                      <pre>{JSON.stringify(event.before, null, 2)}</pre>
                    </div>
                  )}
                  {event.after !== undefined && event.after !== null && (
                    <div>
                      <div className="muted">{t('access.audit.after')}</div>
                      <pre>{JSON.stringify(event.after, null, 2)}</pre>
                    </div>
                  )}
                </div>
              )}
            </>
          }
          actions={
            <button className="link" type="button" onClick={() => setOpen(open === event.id ? null : event.id)}>
              {open === event.id ? t('access.audit.hide') : t('access.audit.show')}
            </button>
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
