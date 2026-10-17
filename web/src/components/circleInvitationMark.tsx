import { useOpenCircleInvitationCount } from './circleInvitations'

// CircleInvitationMark is the number of circle invitations waiting for an
// answer, drawn on the corner of the button that leads to them: the account
// button in the rail and, on a phone, the button that opens the rail. Nothing
// when none are waiting. Hidden from screen readers, whose button carries the
// same number in its label.
export function CircleInvitationMark() {
  const openInvitationCount = useOpenCircleInvitationCount()
  if (openInvitationCount === 0) {
    return null
  }
  return (
    <span className="circle-invitation-mark" aria-hidden="true">
      {openInvitationCount > 99 ? '99+' : openInvitationCount}
    </span>
  )
}
