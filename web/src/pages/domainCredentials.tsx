import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { graphql } from '../api'
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

// Who may send through this domain: one username and password per device,
// shown once when it is made and never again.
export function DomainCredentialsTab({ domain, run }: DomainTabProps) {
  const { t } = useTranslation()
  const { domainId } = useParams()

  const [created, setCreated] = useState<NewCredential | null>(null)
  const [comment, setComment] = useState('')

  return (
    <div className="card">
      <h3>{t('domain.credentialsTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('domain.credentialsIntro', { domain: domain.domain })}
      </p>
      {created && (
        <div className="banner">
          <strong>{t('domain.credentialSaveNow')}</strong>
          <table>
            <tbody>
              <tr>
                <td className="shrink">{t('domain.credentialServer')}</td>
                <td className="mono">
                  {t('domain.credentialServerValue', { host: created.host, port: created.port })}
                </td>
              </tr>
              <tr>
                <td className="shrink">{t('domain.credentialUsername')}</td>
                <td className="mono">{created.username}</td>
              </tr>
              <tr>
                <td className="shrink">{t('domain.credentialPassword')}</td>
                <td className="mono">{created.password}</td>
              </tr>
            </tbody>
          </table>
          <button className="link" onClick={() => setCreated(null)}>
            {t('common.done')}
          </button>
        </div>
      )}
      <table>
        <tbody>
          {domain.credentials.map((credential) => (
            <tr key={credential.id}>
              <td>{credential.comment || <span className="muted">{t('domain.credentialNoNote')}</span>}</td>
              <td className="mono muted">{credential.id}</td>
              <td className="shrink">
                <button onClick={() => void run(() => graphql(DELETE_CREDENTIAL, { credentialId: credential.id }))}>
                  {t('common.remove')}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <form
        className="row"
        style={{ marginTop: 12 }}
        onSubmit={(event) => {
          event.preventDefault()
          void run(async () => {
            const result = await graphql<{ CreateCredential: NewCredential }>(CREATE_CREDENTIAL, {
              domainId,
              comment,
            })
            setCreated(result.CreateCredential)
            setComment('')
          })
        }}
      >
        <label style={{ margin: 0 }}>
          <span>{t('domain.credentialNote')}</span>
          <input value={comment} onChange={(event) => setComment(event.target.value)} placeholder="laptop" />
        </label>
        <div className="shrink">
          <button className="primary" type="submit">
            {t('domain.createCredential')}
          </button>
        </div>
      </form>
    </div>
  )
}
