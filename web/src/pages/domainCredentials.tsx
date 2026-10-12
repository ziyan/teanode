import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { Credential, graphql } from '../api'
import { Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { Tooltip } from '../components/tooltip'
import { PencilIcon, TrashIcon } from '../components/icons'
import { SecretDialog, SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { DomainTabProps } from './domainTabs'

const CREATE_CREDENTIAL = `
  mutation ($domainId: String!, $comment: String) {
    CreateCredential(domainId: $domainId, credentialParameters: { comment: $comment }) {
      username password host port
    }
  }`
// A blank alias is sent as an empty string, which lifts the restriction: the
// server only leaves it alone when the field is left out.
const UPDATE_CREDENTIAL = `
  mutation ($credentialId: String!, $comment: String, $alias: String, $disabled: Boolean) {
    UpdateCredential(credentialId: $credentialId, credentialParameters: { comment: $comment, alias: $alias, disabled: $disabled }) {
      id
    }
  }`
const DELETE_CREDENTIAL = `mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }`

type NewCredential = { username: string; password: string; host: string; port: string }

// Who may send through this domain: one username and password per device,
// shown once when it is made and never again.
//
// The username is the credential's own identifier rather than an address,
// because nobody reads mail with it — a machine relays through this server
// with it, and the server checks the password's signature without a lookup.
// A mailbox's app password is the other way round: a person types it into a
// mail program that has already asked for their address.
export function DomainCredentialsTab({ domain, run }: DomainTabProps) {
  const { t } = useTranslation()
  const { domainId } = useParams()
  const toast = useToast()

  const [created, setCreated] = useState<NewCredential | null>(null)
  const [comment, setComment] = useState('')
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Credential | null>(null)
  const [sendAs, setSendAs] = useState('')
  const [isDisabled, setIsDisabled] = useState(false)
  const [deleting, setDeleting] = useState<Credential | null>(null)

  const edit = (credential: Credential) => {
    setComment(credential.comment ?? '')
    setSendAs(credential.alias ?? '')
    setIsDisabled(credential.disabled)
    setEditing(credential)
  }

  return (
    <>
      <SettingsSection
        card
        title={t('domain.credentialsTitle')}
        description={t('domain.credentialsIntro', { domain: domain.domain })}
        action={
          <button
            className="primary"
            type="button"
            onClick={() => {
              setComment('')
              setAdding(true)
            }}
          >
            {t('domain.createCredential')}
          </button>
        }
      >
        {domain.credentials.length === 0 ? (
          <SettingsEmpty>{t('domain.credentialsEmpty')}</SettingsEmpty>
        ) : (
          <table>
            <tbody>
              {domain.credentials.map((credential) => (
                <tr key={credential.id}>
                  <td>
                    {credential.comment || <span className="muted">{t('domain.credentialNoNote')}</span>}
                    {credential.disabled && ' '}
                    {credential.disabled && <Tag value={t('domain.disabled')} />}
                    {credential.alias && (
                      <div className="muted">{t('domain.credentialSendsAs', { alias: credential.alias })}</div>
                    )}
                  </td>
                  <td className="mono muted">{credential.id}</td>
                  <td className="shrink">
                    <div className="row-actions">
                      <Tooltip label={t('common.edit')}>
                        <button
                          type="button"
                          className="icon-action"
                          aria-label={`${credential.comment || credential.id}: ${t('common.edit')}`}
                          onClick={() => edit(credential)}
                        >
                          <PencilIcon size={16} />
                        </button>
                      </Tooltip>
                      <Tooltip label={t('common.remove')}>
                        <button
                          type="button"
                          className="icon-action danger"
                          aria-label={`${credential.comment || credential.id}: ${t('common.remove')}`}
                          onClick={() => setDeleting(credential)}
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
          title={t('domain.createCredential')}
          submitLabel={t('common.create')}
          onClose={() => setAdding(false)}
          onSubmit={() =>
            void run(async () => {
              const result = await graphql<{ CreateCredential: NewCredential }>(CREATE_CREDENTIAL, {
                domainId,
                comment,
              })
              setCreated(result.CreateCredential)
              setComment('')
              setAdding(false)
            })
          }
        >
          <label>
            <span>{t('domain.credentialNote')}</span>
            <input
              value={comment}
              onChange={(event) => setComment(event.target.value)}
              placeholder={t('domain.credentialNotePlaceholder')}
            />
          </label>
        </FormDialog>
      )}

      {editing && (
        <FormDialog
          title={t('domain.editCredential')}
          submitLabel={t('common.save')}
          onClose={() => setEditing(null)}
          onSubmit={() =>
            void run(async () => {
              await graphql(UPDATE_CREDENTIAL, {
                credentialId: editing.id,
                comment: comment.trim(),
                alias: sendAs.trim(),
                disabled: isDisabled,
              })
              toast.done(t('common.saved'))
              setEditing(null)
            })
          }
        >
          <label>
            <span>{t('domain.credentialNote')}</span>
            <input
              value={comment}
              onChange={(event) => setComment(event.target.value)}
              placeholder={t('domain.credentialNotePlaceholder')}
            />
          </label>
          <label>
            <span>{t('domain.credentialSendAs')}</span>
            <input
              className="mono"
              value={sendAs}
              onChange={(event) => setSendAs(event.target.value)}
              placeholder={t('domain.credentialSendAsPlaceholder')}
            />
          </label>
          <p className="muted">{t('domain.credentialSendAsHint', { domain: domain.domain })}</p>
          <label className="checkbox">
            <input type="checkbox" checked={isDisabled} onChange={(event) => setIsDisabled(event.target.checked)} />
            {t('domain.credentialDisable')}
          </label>
          <p className="muted">{t('domain.credentialDisableHint')}</p>
          <label>
            <span>{t('domain.credentialUsername')}</span>
            <input readOnly className="mono" value={editing.id} />
          </label>
          <p className="muted">{t('domain.credentialPasswordGone')}</p>
        </FormDialog>
      )}

      {created && (
        <SecretDialog
          title={t('domain.credentialSaveNow')}
          intro={t('domain.credentialShownOnce')}
          secret={created.password}
          extra={
            <table className="detail">
              <tbody>
                <tr>
                  <td className="shrink muted">{t('domain.credentialServer')}</td>
                  <td className="mono">
                    {t('domain.credentialServerValue', { host: created.host, port: created.port })}
                  </td>
                </tr>
                <tr>
                  <td className="shrink muted">{t('domain.credentialUsername')}</td>
                  <td className="mono">{created.username}</td>
                </tr>
              </tbody>
            </table>
          }
          onDone={() => setCreated(null)}
        />
      )}

      {deleting && (
        <ConfirmDialog
          title={t('domain.credentialRemove')}
          body={t('domain.credentialRemoveConfirm', { name: deleting.comment || deleting.id })}
          confirmLabel={t('common.remove')}
          onConfirm={() => {
            const credentialId = deleting.id
            setDeleting(null)
            void run(() => graphql(DELETE_CREDENTIAL, { credentialId }))
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
