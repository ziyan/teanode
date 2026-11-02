import { PhoneIcon } from '../components/icons'
import { useTranslation } from '../i18n/i18n'
import { useChatCalls, useGoingChatCall } from './chatCallContext'

// ChatCallBanner is the line at the top of a chat that has a call going: how
// many are on it, and a button to join. It is shown to every member of the
// chat, whether or not the call rang them, and goes away while the person is
// on the call and when the call ends.
export function ChatCallBanner({ conversationId }: { conversationId: string }) {
  const { plural } = useTranslation()
  const { t } = useTranslation()
  const call = useGoingChatCall(conversationId)
  const { activeCall, isBusy, joinCall } = useChatCalls()
  if (!call || call.isAskerOn || activeCall?.callId === call.id) return null
  const onCount = call.members.filter((member) => member.isOn).length
  return (
    <div className="call-banner" role="status">
      <PhoneIcon size={16} />
      <span className="call-banner-words">
        {plural(onCount, { one: 'calls.bannerOne', other: 'calls.bannerOther' })}
      </span>
      <button type="button" className="primary" disabled={isBusy} onClick={() => void joinCall(call.id, false)}>
        {t('calls.join')}
      </button>
    </div>
  )
}
