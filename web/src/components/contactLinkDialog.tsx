import { useState } from 'react'

import { graphql } from '../api'
import { ContactPicker, contactChoiceLabel, type ContactChoice } from './contactPicker'
import { SET_CONTACT_ACCOUNT } from './contactEditor'
import { FormDialog } from './dialog'
import { personName } from './personLabel'
import { useQuery } from './useQuery'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

// LinkContactDialog says which of the person's contacts is somebody in their
// circles, from the person's card: the other way round from the address
// book's, where a contact is given the person. It never makes a contact. The
// contacts that look like the person (their address is the person's) are
// offered first. Only the person who links sees the link; nobody is told.

const SUGGESTIONS = `query { ListContactAccountSuggestions { addressBookId contact { id } account { userId } } }`

export function LinkContactDialog({
  account,
  onLinked,
  onClose,
}: {
  account: { userId: string; name: string; username: string }
  onLinked: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const accountName = personName(account)
  const [chosen, setChosen] = useState<ContactChoice | null>(null)
  const [isBusy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  // Asked once; a failure costs only the order, so nothing is said about it.
  const suggestions = useQuery(
    async () => {
      const answer = await graphql<{
        ListContactAccountSuggestions:
          { addressBookId: string; contact: { id: string }; account: { userId: string } }[] | null
      }>(SUGGESTIONS).catch(() => null)
      return new Set(
        (answer?.ListContactAccountSuggestions ?? [])
          .filter((suggestion) => suggestion.account.userId === account.userId)
          .map((suggestion) => suggestion.contact.id),
      )
    },
    [account.userId],
    { refresh: false },
  )
  const isSuggested = (choice: ContactChoice) => suggestions.data?.has(choice.id) === true

  const link = async () => {
    if (!chosen) return
    setBusy(true)
    setProblem(null)
    try {
      await graphql(SET_CONTACT_ACCOUNT, {
        addressBookId: chosen.addressBookId,
        contactId: chosen.id,
        accountUserId: account.userId,
      })
      toast.done(t('addressBook.accountLinked', { name: contactChoiceLabel(chosen), account: accountName }))
      onLinked()
      onClose()
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : t('addressBook.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('person.linkContactTitle', { name: accountName })}
      submitLabel={t('addressBook.suggestionLink')}
      busy={isBusy}
      error={problem}
      canSubmit={chosen !== null}
      onSubmit={() => void link()}
      onClose={onClose}
    >
      <p className="muted">{t('person.linkContactHint', { name: accountName })}</p>
      <ContactPicker
        chosen={chosen}
        isLinkedLeftOut
        detailOf={(choice) => (isSuggested(choice) ? t('person.linkContactSuggested', { name: accountName }) : '')}
        rankOf={(choice) => Number(isSuggested(choice))}
        onChoose={setChosen}
        onFailed={(failure) => setProblem(failure instanceof Error ? failure.message : t('addressBook.failed'))}
      />
    </FormDialog>
  )
}
