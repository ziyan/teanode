import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { graphql } from '../api'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { TrashIcon } from '../components/icons'
import { SecretDialog, SettingsEmpty, SettingsSection } from '../components/settingsList'
import { useTranslation } from '../i18n/i18n'
import { DomainTabProps } from './domainTabs'

const CREATE_CREDENTIAL = `
  mutation ($domainId: String!, $comment: String) {
    CreateCredential(domainId: $domainId, credentialParameters: { comment: $comment }) {
      username password host port
    }
  }`
const DELETE_CREDENTIAL = `mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }`

type NewCredential = { username: string; password: string; host: string; port: string }
type Credential = { id: string; comment?: string | null }

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

  const [created, setCreated] = useState<NewCredential | null>(null)
  const [comment, setComment] = useState('')
  const [adding, setAdding] = useState(false)
  const [deleting, setDeleting] = useState<Credential | null>(null)

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
                  <td>{credential.comment || <span className="muted">{t('domain.credentialNoNote')}</span>}</td>
                  <td className="mono muted">{credential.id}</td>
                  <td className="shrink">
                    <div className="row-actions">
                      <button
                        type="button"
                        className="icon-action danger"
                        aria-label={`${credential.comment || credential.id}: ${t('common.remove')}`}
                        title={t('common.remove')}
                        onClick={() => setDeleting(credential)}
                      >
                        <TrashIcon size={16} />
                      </button>
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
            <input value={comment} onChange={(event) => setComment(event.target.value)} placeholder="laptop" />
          </label>
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
