import { useState } from 'react'

import { Passkey, PasskeyCeremony, PasskeyPolicy, graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { RelativeTime, hasTime } from '../../components/relativeTime'
import { useQuery } from '../../components/useQuery'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { cancelled, createCredential, isPasskeySupported } from '../../passkeys'
import { useTranslation } from '../../i18n/i18n'

const PASSKEYS = `
  {
    GetPasskeyPolicy { enabled maximumPerUser }
    ListPasskeys { id name createdAt usedAt ip backupEligible backupState transports }
  }`

const BEGIN = `mutation { BeginPasskeyRegistration { ceremonyId options } }`

const FINISH = `
  mutation ($ceremonyId: String!, $response: String!, $name: String) {
    FinishPasskeyRegistration(ceremonyId: $ceremonyId, response: $response, name: $name) { id name }
  }`

const RENAME = `
  mutation ($passkeyId: String!, $name: String!) {
    RenamePasskey(passkeyId: $passkeyId, name: $name) { id name }
  }`

const REMOVE = `mutation ($passkeyId: String!) { DeletePasskey(passkeyId: $passkeyId) }`

type Response = { GetPasskeyPolicy: PasskeyPolicy; ListPasskeys: Passkey[] }

// The authenticators that can sign in as this account.
//
// Registering one is a ceremony rather than a form: the name is asked for
// first, then the browser takes over and the person touches something. Asking
// afterwards would mean a credential exists on their authenticator before
// they have said whether to keep it.
export function PasskeysPage() {
  const { t } = useTranslation()
  const { data, error, loading, reload } = useQuery(() => graphql<Response>(PASSKEYS), [])

  const [supported] = useState(isPasskeySupported)
  const [naming, setNaming] = useState(false)
  const [name, setName] = useState('')
  const [renaming, setRenaming] = useState<Passkey | null>(null)
  const [removing, setRemoving] = useState<Passkey | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      await reload()
    } catch (caught) {
      if (!cancelled(caught)) {
        setProblem(caught instanceof Error ? caught.message : t('passkeys.failed'))
      }
    } finally {
      setBusy(false)
    }
  }

  async function register(chosen: string) {
    const ceremony = await graphql<{ BeginPasskeyRegistration: PasskeyCeremony }>(BEGIN)
    const response = await createCredential(ceremony.BeginPasskeyRegistration.options)
    await graphql(FINISH, {
      ceremonyId: ceremony.BeginPasskeyRegistration.ceremonyId,
      response,
      name: chosen,
    })
  }

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }

  const policy = data?.GetPasskeyPolicy
  const passkeys = data?.ListPasskeys ?? []
  const full = policy ? passkeys.length >= policy.maximumPerUser : false

  return (
    <>
      <SettingsSection
        description={t('passkeys.intro')}
        action={
          <button
            className="primary"
            type="button"
            disabled={busy || !supported || !policy?.enabled || full}
            onClick={() => {
              setName('')
              setNaming(true)
            }}
          >
            {t('passkeys.add')}
          </button>
        }
      >
        {/* Said rather than left to be discovered by a button that does
            nothing. Each of these is a different problem with a different
            answer. */}
        {!policy?.enabled && <p className="muted">{t('passkeys.disabled')}</p>}
        {policy?.enabled && !supported && <p className="muted">{t('passkeys.unsupported')}</p>}
        {policy?.enabled && supported && full && (
          <p className="muted">{t('passkeys.full', { count: policy.maximumPerUser })}</p>
        )}

        {problem && <p className="error">{problem}</p>}

        {passkeys.length === 0 ? (
          <SettingsEmpty>{t('passkeys.empty')}</SettingsEmpty>
        ) : (
          passkeys.map((passkey) => (
            <SettingsRow
              key={passkey.id}
              title={passkey.name}
              badge={
                // Whether this credential is synced to other devices, which
                // is the difference between a passkey in a password manager
                // and one on a single security key — and decides what losing
                // the device costs.
                passkey.backupState ? (
                  <Tag value={t('passkeys.synced')} />
                ) : (
                  <Tag value={t('passkeys.thisDevice')} />
                )
              }
              subtitle={
                <>
                  {t('passkeys.registered', { time: formatTime(passkey.createdAt) })}
                  {' · '}
                  {hasTime(passkey.usedAt) ? (
                    <>
                      {t('passkeys.lastUsed')} <RelativeTime value={passkey.usedAt} />
                      {passkey.ip ? ` (${passkey.ip})` : ''}
                    </>
                  ) : (
                    t('passkeys.neverUsed')
                  )}
                </>
              }
              actions={
                <>
                  <button
                    className="link"
                    type="button"
                    onClick={() => {
                      setName(passkey.name)
                      setRenaming(passkey)
                    }}
                  >
                    {t('passkeys.rename')}
                  </button>
                  <button className="link danger" type="button" onClick={() => setRemoving(passkey)}>
                    {t('passkeys.remove')}
                  </button>
                </>
              }
            />
          ))
        )}
      </SettingsSection>

      {naming && (
        <FormDialog
          title={t('passkeys.add')}
          submitLabel={t('passkeys.continue')}
          busy={busy}
          error={problem}
          canSubmit={name.trim().length > 0}
          onClose={() => {
            setNaming(false)
            setProblem(null)
          }}
          onSubmit={() => {
            const chosen = name.trim()
            setNaming(false)
            void run(() => register(chosen))
          }}
        >
          <p className="muted">{t('passkeys.addExplained')}</p>
          <label>
            <span>{t('passkeys.name')}</span>
            <input
              value={name}
              maxLength={64}
              onChange={(event) => setName(event.target.value)}
              placeholder={t('passkeys.namePlaceholder')}
            />
          </label>
        </FormDialog>
      )}

      {renaming && (
        <FormDialog
          title={t('passkeys.rename')}
          submitLabel={t('common.save')}
          busy={busy}
          error={problem}
          canSubmit={name.trim().length > 0}
          onClose={() => setRenaming(null)}
          onSubmit={() => {
            const passkeyId = renaming.id
            const chosen = name.trim()
            setRenaming(null)
            void run(() => graphql(RENAME, { passkeyId, name: chosen }))
          }}
        >
          <label>
            <span>{t('passkeys.name')}</span>
            <input value={name} maxLength={64} onChange={(event) => setName(event.target.value)} />
          </label>
        </FormDialog>
      )}

      {removing && (
        <ConfirmDialog
          title={t('passkeys.removeTitle')}
          body={t('passkeys.removeBody', { name: removing.name })}
          confirmLabel={t('passkeys.remove')}
          busy={busy}
          onConfirm={() => {
            const passkeyId = removing.id
            setRemoving(null)
            void run(() => graphql(REMOVE, { passkeyId }))
          }}
          onClose={() => setRemoving(null)}
        />
      )}
    </>
  )
}
