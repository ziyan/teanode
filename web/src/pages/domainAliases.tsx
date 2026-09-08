import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { Alias, graphql } from '../api'
import { Tag } from '../components/common'
import { Tooltip } from '../components/tooltip'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { DomainTabProps } from './domainTabs'

const MAILBOXES = `{ ListAllMailboxes { id name userId username userName } }`

type MailboxSummary = { id: string; name: string; userId: string; username: string; userName?: string }

const CREATE_ALIAS = `
  mutation ($domainId: String!, $pattern: String!, $kind: String!, $email: String, $webhook: String, $mailboxId: String) {
    CreateAlias(domainId: $domainId, aliasParameters: { pattern: $pattern, kind: $kind, email: $email, webhook: $webhook, mailboxId: $mailboxId }) {
      id
    }
  }`
const DELETE_ALIAS = `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`

// Who receives mail for this domain. The most-used page of a forwarding
// server, and the reason it has a tab of its own rather than being the fourth
// screen of a scroll.
export function DomainAliasesTab({ domain, run }: DomainTabProps) {
  const { t } = useTranslation()
  const { domainId } = useParams()

  const [pattern, setPattern] = useState('')
  const [kind, setKind] = useState('email')
  const [mailboxId, setMailboxId] = useState('')

  // Whose mailbox an address can deliver into: every mailbox on the server,
  // with its owner's name, for the picker. Loaded once.
  const mailboxes = useQuery(() => graphql<{ ListAllMailboxes: MailboxSummary[] }>(MAILBOXES), [], { refresh: false })
  const mailboxLabel = (id?: string) => {
    const found = mailboxes.data?.ListAllMailboxes.find((mailbox) => mailbox.id === id)
    return found ? `${found.username} · ${found.name}` : (id ?? '')
  }
  const [destination, setDestination] = useState('')
  const [adding, setAdding] = useState(false)
  const [deleting, setDeleting] = useState<Alias | null>(null)

  const open = () => {
    setPattern('')
    setDestination('')
    setAdding(true)
  }

  const aliasName = (alias: Alias) => alias.pattern || t('domain.catchAll')

  return (
    <>
      <SettingsSection
        card
        title={t('domain.aliasesTitle')}
        description={t('domain.aliasesIntro')}
        action={
          <button className="primary" type="button" onClick={open}>
            {t('domain.addAlias')}
          </button>
        }
      >
        {domain.aliases.length === 0 ? (
          <SettingsEmpty>{t('domain.aliasesEmpty')}</SettingsEmpty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>{t('domain.pattern')}</th>
                <th>{t('domain.goesTo')}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {domain.aliases.map((alias) => (
                <tr key={alias.id}>
                  <td className="mono">
                    {alias.pattern || <span className="muted">{t('domain.catchAll')}</span>}
                    {alias.disabled && ' '}
                    {alias.disabled && <Tag value={t('domain.disabled')} />}
                  </td>
                  <td>
                    {alias.kind === 'email' && alias.email}
                    {alias.kind === 'webhook' && <span className="mono">{alias.webhook}</span>}
                    {alias.kind === 'mailServer' && alias.mailServer && (
                      <span className="mono">
                        {alias.mailServer.host}:{alias.mailServer.port}
                      </span>
                    )}
                    {alias.kind === 'null' && <span className="muted">{t('domain.discarded')}</span>}
                    {alias.kind === 'mailbox' && (
                      <span>
                        {t('domain.deliveredInto')} {mailboxLabel(alias.mailboxId)}
                      </span>
                    )}
                  </td>
                  <td className="shrink">
                    <div className="row-actions">
                      <Tooltip label={t('common.remove')}>
                        <button
                          type="button"
                          className="icon-action danger"
                          aria-label={`${aliasName(alias)}: ${t('common.remove')}`}
                          onClick={() => setDeleting(alias)}
                        >
                          <TrashIcon size={16} />
                        </button>
                      </Tooltip>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </SettingsSection>

      {adding && (
        <FormDialog
          title={t('domain.addAlias')}
          submitLabel={t('common.create')}
          onClose={() => setAdding(false)}
          onSubmit={() =>
            void run(async () => {
              await graphql(CREATE_ALIAS, {
                domainId,
                pattern,
                kind,
                email: kind === 'email' ? destination : null,
                webhook: kind === 'webhook' ? destination : null,
                mailboxId: kind === 'mailbox' ? mailboxId : null,
              })
              setPattern('')
              setDestination('')
              setAdding(false)
            })
          }
        >
          <label>
            <span>{t('domain.pattern')}</span>
            {/* Left blank it is a catch-all, which is not guessable from an
              empty box, so the placeholder says so. */}
            <input
              value={pattern}
              onChange={(event) => setPattern(event.target.value)}
              placeholder={t('domain.patternPlaceholder')}
            />
          </label>
          <label>
            <span>{t('domain.kind')}</span>
            <select value={kind} onChange={(event) => setKind(event.target.value)}>
              <option value="mailbox">{t('domain.kindMailbox')}</option>
              <option value="email">{t('domain.kindEmail')}</option>
              <option value="webhook">{t('domain.kindWebhook')}</option>
              <option value="null">{t('domain.kindDiscard')}</option>
            </select>
          </label>
          {kind === 'mailbox' && (
            <label>
              <span>{t('domain.deliverInto')}</span>
              <select value={mailboxId} onChange={(event) => setMailboxId(event.target.value)} required>
                <option value="">{t('domain.chooseMailbox')}</option>
                {(mailboxes.data?.ListAllMailboxes ?? []).map((mailbox) => (
                  <option key={mailbox.id} value={mailbox.id}>
                    {mailbox.username}
                    {mailbox.userName && mailbox.userName !== mailbox.username ? ` (${mailbox.userName})` : ''} ·{' '}
                    {mailbox.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {kind !== 'null' && kind !== 'mailbox' && (
            <label>
              <span>{kind === 'email' ? t('domain.forwardTo') : t('domain.postTo')}</span>
              <input value={destination} onChange={(event) => setDestination(event.target.value)} />
            </label>
          )}
        </FormDialog>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('domain.aliasRemove')}
          body={t('domain.aliasRemoveConfirm', { name: aliasName(deleting) })}
          confirmLabel={t('common.remove')}
          onConfirm={() => {
            const aliasId = deleting.id
            setDeleting(null)
            void run(() => graphql(DELETE_ALIAS, { aliasId }))
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
