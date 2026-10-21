import { useCallback } from 'react'

import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { passwordsSession } from './passwordsSession'

// useRekeyFailedToast gives the function that says an item could not be given
// a new key after somebody was taken off it. The removal stands; the toast
// offers to try again, and says so again if that fails too. The item stays
// marked in the session until a new key is made, so a later save or opening
// the item tries as well.
export function useRekeyFailedToast(): (itemId: string) => void {
  const { t } = useTranslation()
  const toast = useToast()
  return useCallback(
    function say(itemId: string) {
      toast.failed(t('passwords.rekeyFailed'), {
        label: t('common.tryAgain'),
        run: async () => {
          try {
            await passwordsSession.giveNewKey(itemId)
            toast.done(t('passwords.rekeyDone'))
          } catch {
            say(itemId)
          }
        },
      })
    },
    [t, toast],
  )
}
