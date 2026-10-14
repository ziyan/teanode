import { useState } from 'react'

import { graphql } from '../../api'
import { Tag } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

type Phone = {
  id: string
  phoneName: string
  phoneModel: string
  phoneModelName: string
  notificationKinds: string[]
  canBeNotified: boolean
  lastSeenAt?: string | null
  lastDeliveredAt?: string | null
  lastDeliveryError: string
}

const PHONE_FIELDS =
  'id phoneName phoneModel phoneModelName notificationKinds canBeNotified lastSeenAt lastDeliveredAt lastDeliveryError'

// The documents the Phones section of Devices sends, by operation, which TestPhoneParity
// reads to check the dashboard offers what the command line does.
const PHONE_DOCUMENTS = {
  ListAgentPhones: `query { ListAgentPhones { phones { ${PHONE_FIELDS} } isPushEnabled notificationKinds } }`,
  UpdateAgentPhone: `mutation ($phoneId: String!, $phoneName: String, $notificationKinds: [String!]) { UpdateAgentPhone(phoneId: $phoneId, phoneName: $phoneName, notificationKinds: $notificationKinds) { id } }`,
  RemoveAgentPhone: `mutation ($phoneId: String!) { RemoveAgentPhone(phoneId: $phoneId) }`,
  TestAgentPhone: `mutation ($phoneId: String!) { TestAgentPhone(phoneId: $phoneId) { id } }`,
  CreateOAuthConnectCode: `mutation { CreateOAuthConnectCode { connectLink } }`,
}

// PhonesPage is the TeaNode app section of the Devices page. It lists the phones the TeaNode app is connected on: what each is
// told about, whether notifications reach it, and a way to connect another.
// Connecting itself is the Connect page, which shows the code. A phone is an
// app connected as the person, so it is listed under Apps too; here is what
// only a phone has.
export function PhonesPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(
    () =>
      graphql<{ ListAgentPhones: { phones: Phone[]; isPushEnabled: boolean; notificationKinds: string[] } }>(
        PHONE_DOCUMENTS.ListAgentPhones,
        {},
      ),
    [],
    { refresh: false },
  )
  const [busy, setBusy] = useState<string | null>(null)
  const [editing, setEditing] = useState<{ phone: Phone; phoneName: string; notificationKinds: string[] } | null>(null)
  const [removing, setRemoving] = useState<Phone | null>(null)
  const [problem, setProblem] = useState<string | null>(null)
  if (error) return null
  const list = data?.ListAgentPhones
  const phones = list?.phones ?? []
  const kinds = list?.notificationKinds ?? []
  const kindName = (kind: string) => {
    switch (kind) {
      case 'message':
        return t('phones.kind.message')
      case 'approval':
        return t('phones.kind.approval')
      case 'question':
        return t('phones.kind.question')
      case 'answer':
        return t('phones.kind.answer')
    }
    return kind
  }
  const act = async (phone: Phone, run: () => Promise<unknown>, done: string) => {
    setBusy(phone.id)
    try {
      await run()
      toast.done(done)
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(null)
    }
  }
  const stateOf = (phone: Phone) => {
    if (!phone.canBeNotified) return <Tag value={t('phones.notRegistered')} tone="warn" />
    if (phone.lastDeliveryError) return <Tag value={t('phones.lastFailed')} tone="bad" />
    return <Tag value={t('phones.notified')} tone="good" />
  }
  return (
    <>
      <SettingsSection
        description={list && !list.isPushEnabled ? t('phones.pushOff') : t('phones.hint')}
        action={
          <a className="button" href="/connect">
            {t('phones.connect')}
          </a>
        }
      >
        {phones.length === 0 ? <SettingsEmpty>{t('phones.none')}</SettingsEmpty> : null}
        {phones.map((phone) => {
          // What a phone is told about, said as that: it can do everything
          // the person can, and this is only which notifications reach it.
          const told =
            phone.notificationKinds.length === 0 || phone.notificationKinds.length === kinds.length
              ? t('phones.notificationsAll')
              : t('phones.notificationsSome', { kinds: phone.notificationKinds.map(kindName).join(', ') })
          const detail = [phone.phoneModelName, phone.canBeNotified ? told : '']
          if (phone.lastDeliveryError) detail.push(phone.lastDeliveryError)
          return (
            <SettingsRow
              key={phone.id}
              title={phone.phoneName || t('phones.unnamed')}
              badge={stateOf(phone)}
              subtitle={detail.filter(Boolean).join(' · ')}
              actions={
                <>
                  <button
                    type="button"
                    disabled={busy === phone.id || !phone.canBeNotified}
                    onClick={() =>
                      void act(
                        phone,
                        () => graphql(PHONE_DOCUMENTS.TestAgentPhone, { phoneId: phone.id }),
                        t('phones.tested', { name: phone.phoneName }),
                      )
                    }
                  >
                    {t('phones.test')}
                  </button>
                  <button
                    type="button"
                    disabled={busy === phone.id}
                    onClick={() => {
                      setProblem(null)
                      setEditing({ phone, phoneName: phone.phoneName, notificationKinds: phone.notificationKinds })
                    }}
                  >
                    {t('common.edit')}
                  </button>
                  <button type="button" disabled={busy === phone.id} onClick={() => setRemoving(phone)}>
                    {t('common.remove')}
                  </button>
                </>
              }
            />
          )
        })}
      </SettingsSection>
      {editing !== null ? (
        <FormDialog
          title={t('phones.editTitle')}
          submitLabel={t('common.save')}
          busy={busy === editing.phone.id}
          error={problem}
          canSubmit={editing.phoneName.trim() !== ''}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            setBusy(editing.phone.id)
            graphql(PHONE_DOCUMENTS.UpdateAgentPhone, {
              phoneId: editing.phone.id,
              phoneName: editing.phoneName,
              notificationKinds: editing.notificationKinds,
            })
              .then(async () => {
                setEditing(null)
                toast.done(t('phones.saved'))
                await reload()
              })
              .catch((caught: unknown) => setProblem(messageOf(caught)))
              .finally(() => setBusy(null))
          }}
        >
          <label className="field">
            <span>{t('phones.name')}</span>
            <input
              value={editing.phoneName}
              onChange={(event) => setEditing({ ...editing, phoneName: event.target.value })}
            />
          </label>
          <fieldset className="field">
            <legend>{t('phones.toldAbout')}</legend>
            <p className="muted field-hint">{t('phones.toldAboutHint')}</p>
            {kinds.map((kind) => (
              <label key={kind} className="checkbox">
                <input
                  type="checkbox"
                  checked={editing.notificationKinds.includes(kind)}
                  onChange={(event) =>
                    setEditing({
                      ...editing,
                      notificationKinds: event.target.checked
                        ? [...editing.notificationKinds, kind]
                        : editing.notificationKinds.filter((other) => other !== kind),
                    })
                  }
                />
                {kindName(kind)}
              </label>
            ))}
          </fieldset>
        </FormDialog>
      ) : null}
      {removing !== null ? (
        <ConfirmDialog
          title={t('phones.removeTitle')}
          body={t('phones.removeBody', { name: removing.phoneName })}
          confirmLabel={t('common.remove')}
          destructive
          busy={busy === removing.id}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            void act(
              removing,
              () => graphql(PHONE_DOCUMENTS.RemoveAgentPhone, { phoneId: removing.id }),
              t('phones.removed', { name: removing.phoneName }),
            ).then(() => setRemoving(null))
          }}
        />
      ) : null}
    </>
  )
}
