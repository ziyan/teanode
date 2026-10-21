import { useMemo } from 'react'

import { ConfirmDialog } from '../../components/dialog'
import { useTranslation } from '../../i18n/i18n'
import { passwordsSession, usePasswordsSession } from './passwordsSession'

// FinishSharingDialog says who finishing the sharing of some items gives a key
// before anything is sealed: the people and circles by name for each set of
// items, and the agent. Nothing is given until the person confirms.
export function FinishSharingDialog({
  itemIds,
  busy,
  onConfirm,
  onClose,
}: {
  itemIds: string[]
  busy?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const { t, plural } = useTranslation()
  const { items } = usePasswordsSession()
  // The items are read again when they change, so the words follow what would
  // be sealed now.
  const plan = useMemo(
    () => passwordsSession.planFinishSharing(itemIds),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [itemIds, items],
  )
  return (
    <ConfirmDialog
      destructive={false}
      title={t('passwords.finishConfirmTitle')}
      body={
        <>
          <p className="muted">{t('passwords.finishConfirmBody')}</p>
          <ul className="passwords-sites">
            {plan.groups.map((group) => {
              const names = group.people
                .map((person) =>
                  person.isCircle
                    ? plural(
                        person.personCount,
                        { one: 'passwords.finishWhoCircleOne', other: 'passwords.finishWhoCircle' },
                        { name: person.name },
                      )
                    : person.name,
                )
                .join(', ')
              return (
                <li key={names}>
                  {plural(
                    group.itemCount,
                    { one: 'passwords.finishConfirmPeopleOne', other: 'passwords.finishConfirmPeople' },
                    { names },
                  )}
                </li>
              )
            })}
            {plan.agentItemCount > 0 && (
              <li>
                {plural(plan.agentItemCount, {
                  one: 'passwords.finishConfirmAgentOne',
                  other: 'passwords.finishConfirmAgent',
                })}
              </li>
            )}
            {plan.onlyYouItemCount > 0 && (
              <li>
                {plural(plan.onlyYouItemCount, {
                  one: 'passwords.finishConfirmOnlyYouOne',
                  other: 'passwords.finishConfirmOnlyYou',
                })}
              </li>
            )}
          </ul>
        </>
      }
      confirmLabel={t('passwords.finishConfirmButton')}
      busy={busy}
      onClose={onClose}
      onConfirm={onConfirm}
    />
  )
}
