import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { Alias, graphql } from '../api'
import { Tag } from '../components/common'
import { Tooltip } from '../components/tooltip'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { PencilIcon, TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { DomainTabProps } from './domainTabs'
import { Select } from '../components/select'

const MAILBOXES = `{ ListAllMailboxes { id name userId username userName } }`

type MailboxSummary = { id: string; name: string; userId: string; username: string; userName?: string }

const CREATE_ALIAS = `
  mutation ($domainId: String!, $pattern: String!, $comment: String, $kind: String!, $email: String, $webhook: String, $mailboxId: String) {
    CreateAlias(domainId: $domainId, aliasParameters: { pattern: $pattern, comment: $comment, kind: $kind, email: $email, webhook: $webhook, mailboxId: $mailboxId }) {
      id
    }
  }`
// A destination left null is left as it is, and the server clears the ones the
// kind does not use. A relay's host, port and password are not sent at all, so
// saving a relay keeps them.
const UPDATE_ALIAS = `
  mutation ($aliasId: String!, $pattern: String!, $comment: String, $kind: String!, $email: String, $webhook: String, $mailboxId: String, $disabled: Boolean) {
    UpdateAlias(aliasId: $aliasId, aliasParameters: { pattern: $pattern, comment: $comment, kind: $kind, email: $email, webhook: $webhook, mailboxId: $mailboxId, disabled: $disabled }) {
      id
    }
  }`
const DELETE_ALIAS = `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`

// What the alias holds for a kind's destination box, if it is of that kind.
function storedDestination(alias: Alias, kind: string) {
  if (alias.kind !== kind) return ''
  return (kind === 'email' ? alias.email : kind === 'webhook' ? alias.webhook : '') ?? ''
}

// Who receives mail for this domain. The most-used page of a forwarding
// server, and the reason it has a tab of its own rather than being the fourth
// screen of a scroll.
export function DomainAliasesTab({ domain, run }: DomainTabProps) {
  const { t } = useTranslation()
  const { domainId } = useParams()
  const toast = useToast()

  const [pattern, setPattern] = useState('')
  const [comment, setComment] = useState('')
  const [kind, setKind] = useState('email')
  const [mailboxId, setMailboxId] = useState('')
  const [isDisabled, setIsDisabled] = useState(false)

  // Whose mailbox an address can deliver into: every mailbox on the server,
  // with its owner's name, for the picker. Loaded once.
  const mailboxes = useQuery(() => graphql<{ ListAllMailboxes: MailboxSummary[] }>(MAILBOXES), [], { refresh: false })
  const mailboxLabel = (id?: string) => {
    const found = mailboxes.data?.ListAllMailboxes.find((mailbox) => mailbox.id === id)
    return found ? `${found.username} · ${found.name}` : (id ?? '')
  }
  const [destination, setDestination] = useState('')
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Alias | null>(null)
  const [deleting, setDeleting] = useState<Alias | null>(null)

  const open = () => {
    setPattern('')
    setComment('')
    setKind('email')
    setDestination('')
    setMailboxId('')
    setIsDisabled(false)
    setAdding(true)
  }

  const edit = (alias: Alias) => {
    setPattern(alias.pattern)
    setComment(alias.comment ?? '')
    setKind(alias.kind)
    setDestination(storedDestination(alias, alias.kind))
    setMailboxId(alias.mailboxId ?? '')
    setIsDisabled(alias.disabled)
    setEditing(alias)
  }

  // An address typed for one kind is not an answer for another: the box starts
  // from what the alias already holds for the kind chosen, and is empty for a
  // kind it never had.
  const changeKind = (nextKind: string) => {
    setKind(nextKind)
    setDestination(editing ? storedDestination(editing, nextKind) : '')
  }

  const aliasName = (alias: Alias) => alias.pattern || t('domain.catchAll')

  // The server keeps the old pattern when it is sent an empty one, so an
  // address that has a pattern cannot be saved with it blanked: it would look
  // saved as a catch-all and stay what it was.
  const isPatternMissing = editing !== null && editing.pattern !== '' && pattern.trim() === ''
  // A kind that delivers somewhere needs to be told where.
  const isDestinationFilled =
    kind === 'mailbox' ? mailboxId !== '' : kind === 'email' || kind === 'webhook' ? destination.trim() !== '' : true
  const canSubmit = !isPatternMissing && isDestinationFilled

  // Only the destination of the chosen kind is sent, so switching kind never
  // carries the old address along.
  const destinations = {
    email: kind === 'email' ? destination.trim() : null,
    webhook: kind === 'webhook' ? destination.trim() : null,
    mailboxId: kind === 'mailbox' ? mailboxId : null,
  }

  const kindOptions = [
    { value: 'mailbox', label: t('domain.kindMailbox') },
    { value: 'email', label: t('domain.kindEmail') },
    { value: 'webhook', label: t('domain.kindWebhook') },
    { value: 'null', label: t('domain.kindDiscard') },
    // A relay cannot be made here, only kept: its host and password are not
    // editable in this dialog, so it is offered only to the alias that is one.
    ...(editing?.kind === 'mailServer' ? [{ value: 'mailServer', label: t('domain.kindRelay') }] : []),
  ]

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
                    {alias.comment && <div className="muted">{alias.comment}</div>}
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
                      <Tooltip label={t('common.edit')}>
                        <button
                          type="button"
                          className="icon-action"
                          aria-label={`${aliasName(alias)}: ${t('common.edit')}`}
                          onClick={() => edit(alias)}
                        >
                          <PencilIcon size={16} />
                        </button>
                      </Tooltip>
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

      {(adding || editing) && (
        <FormDialog
          title={editing ? t('domain.editAlias') : t('domain.addAlias')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          canSubmit={canSubmit}
          onClose={() => {
            setAdding(false)
            setEditing(null)
          }}
          onSubmit={() =>
            void run(async () => {
              if (editing) {
                await graphql(UPDATE_ALIAS, {
                  aliasId: editing.id,
                  pattern: pattern.trim(),
                  comment: comment.trim(),
                  kind,
                  ...destinations,
                  disabled: isDisabled,
                })
                toast.done(t('common.saved'))
                setEditing(null)
                return
              }
              await graphql(CREATE_ALIAS, {
                domainId,
                pattern: pattern.trim(),
                comment: comment.trim(),
                kind,
                ...destinations,
              })
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
          {isPatternMissing && <p className="muted">{t('domain.patternRequired')}</p>}
          <label>
            <span>{t('domain.kind')}</span>
            <Select block value={kind} label={t('domain.kind')} options={kindOptions} onChange={changeKind} />
          </label>
          {kind === 'mailbox' && (
            <label>
              <span>{t('domain.deliverInto')}</span>
              <Select
                block
                value={mailboxId}
                label={t('domain.chooseMailbox')}
                placeholder={t('domain.chooseMailbox')}
                options={(mailboxes.data?.ListAllMailboxes ?? []).map((mailbox) => ({
                  value: mailbox.id,
                  label: `${mailbox.username}${mailbox.userName && mailbox.userName !== mailbox.username ? ` (${mailbox.userName})` : ''} · ${mailbox.name}`,
                }))}
                onChange={setMailboxId}
              />
            </label>
          )}
          {kind === 'mailServer' && editing?.mailServer && (
            <label>
              <span>{t('domain.relay')}</span>
              <input readOnly className="mono" value={`${editing.mailServer.host}:${editing.mailServer.port}`} />
            </label>
          )}
          {kind !== 'null' && kind !== 'mailbox' && kind !== 'mailServer' && (
            <label>
              <span>{kind === 'email' ? t('domain.forwardTo') : t('domain.postTo')}</span>
              <input value={destination} onChange={(event) => setDestination(event.target.value)} />
            </label>
          )}
          <label>
            <span>{t('domain.aliasNote')}</span>
            <input value={comment} onChange={(event) => setComment(event.target.value)} />
          </label>
          {editing && (
            <>
              <label className="checkbox">
                <input type="checkbox" checked={isDisabled} onChange={(event) => setIsDisabled(event.target.checked)} />
                {t('domain.aliasDisable')}
              </label>
              <p className="muted">{t('domain.aliasDisableHint')}</p>
            </>
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
