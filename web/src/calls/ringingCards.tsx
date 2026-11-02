import { Avatar } from '../components/avatar'
import { PhoneHangUpIcon, PhoneIcon, VideoIcon } from '../components/icons'
import { useTranslation } from '../i18n/i18n'
import { useChatCalls } from './chatCallContext'
import { ringingCalls } from './chatCallStore'

// RingingCards are the calls ringing the person, at the top of the window on
// whatever page is open: who is calling, in which chat, and the three things
// that can be done. A card goes away the moment the call stops ringing them,
// whether they answered it, said no, answered on another device, or the
// caller gave up.
//
// There is no ring sound: nothing else in the dashboard makes one, and a tab
// that is not in front has no say over the speakers.
export function RingingCards() {
  const { t } = useTranslation()
  const { calls, activeCall, isBusy, joinCall, declineCall } = useChatCalls()
  const ringing = ringingCalls(calls)
  if (ringing.length === 0) return null
  return (
    <div className="call-rings" role="region" aria-label={t('calls.incomingRegion')}>
      {ringing.map((call) => {
        const caller = call.members.find((member) => member.userId === call.startedByUserId)
        const callerName = call.startedByName || caller?.name || t('chat.someone')
        return (
          <div key={call.id} className="call-ring" role="group" aria-label={t('calls.isCalling', { name: callerName })}>
            <div className="call-ring-who">
              <Avatar name={callerName} avatarUrl={caller?.avatarUrl} colorIndex={caller?.avatarColorIndex} size={40} />
              <div className="call-ring-words">
                <strong>{t('calls.isCalling', { name: callerName })}</strong>
                {call.isGroup ? <span className="muted">{call.chatTitle}</span> : null}
                {activeCall ? <span className="muted">{t('calls.answerLeavesCurrent')}</span> : null}
              </div>
            </div>
            <div className="call-ring-actions">
              <button type="button" className="primary" disabled={isBusy} onClick={() => void joinCall(call.id, false)}>
                <PhoneIcon size={14} />
                {t('calls.answer')}
              </button>
              <button type="button" disabled={isBusy} onClick={() => void joinCall(call.id, true)}>
                <VideoIcon size={14} />
                {t('calls.answerWithVideo')}
              </button>
              <button type="button" className="danger" disabled={isBusy} onClick={() => void declineCall(call.id)}>
                <PhoneHangUpIcon size={14} />
                {t('calls.decline')}
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}
