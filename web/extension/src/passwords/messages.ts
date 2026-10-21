// The messages between the pieces of the extension's Passwords: the content
// script in a page, the service worker, and the extension's own pages. All
// travel on chrome.runtime.sendMessage and tabs.sendMessage, which a web page
// cannot send or hear. The service worker decides what to trust from who
// sent a message (sender.tab, sender.url, sender.frameId), never from what the
// message says about itself.

import type { FieldRole } from './fieldDetection'
import type { LoginSummary } from './vault'

// --- from a content script ---------------------------------------------

export type SuggestionAnswer =
  // Nothing to offer: signed out, Passwords not set up, or nothing for this site.
  | { state: 'none' }
  | { state: 'locked'; host: string }
  | { state: 'logins'; host: string; logins: LoginSummary[] }
  // A new password field while Passwords is open: offer a generated one.
  | { state: 'generate'; host: string }

export interface CapturedLoginMessage {
  type: 'passwords:captured'
  userName: string
  password: string
  isNewPassword: boolean
}

export interface BarOffer {
  // 'unlock': Passwords is locked; the button opens the window to unlock.
  action: 'save' | 'update' | 'unlock'
  host: string
  userName: string
}

export interface FillFieldsMessage {
  type: 'passwords:fill-fields'
  // host is the host the secrets were approved for; the content script fills
  // only if its own frame is still at it.
  host: string
  userName: string
  password: string
  oneTimeCode: string
}

export interface ShowBarMessage {
  type: 'passwords:show-bar'
  offer: BarOffer
}

export type ContentRequest =
  | { type: 'passwords:suggest'; role: FieldRole }
  | { type: 'passwords:fill'; itemId: string }
  | { type: 'passwords:fill-code'; itemId: string }
  | { type: 'passwords:generate' }
  | CapturedLoginMessage
  | { type: 'passwords:page-ready'; hasPasswordField: boolean }
  | { type: 'passwords:bar-answer'; answer: 'save' | 'dismiss' | 'unlock' }
  | { type: 'passwords:open-popup' }
  | { type: 'passwords:passkey'; kind: 'create' | 'get'; options: Record<string, unknown>; isCrossOrigin: boolean }

// --- from the extension's own pages ------------------------------------

export interface FrameTarget {
  tabId: number
  frameId: number
}

export interface PopupState {
  isSignedIn: boolean
  isSetUp: boolean
  isUnlocked: boolean
  serverOrigin: string
  // The page the popup was opened for, when it was opened for one.
  frameHost: string
  topHost: string
  isFrameCrossOrigin: boolean
  hasPendingSave: boolean
  pendingSave: { host: string; userName: string } | null
}

export interface SearchAnswer {
  forThisSite: LoginSummary[]
  others: LoginSummary[]
}

export type PopupRequest =
  | ({ type: 'passwords:state' } & Partial<FrameTarget>)
  | { type: 'passwords:unlock'; secretText: string; isRecoveryKey: boolean }
  | { type: 'passwords:lock' }
  | ({ type: 'passwords:search'; query: string } & Partial<FrameTarget>)
  | { type: 'passwords:copy'; itemId: string; field: 'password' | 'userName' | 'oneTimeCode' }
  | { type: 'passwords:copy-text'; text: string }
  | { type: 'passwords:one-time-code'; itemId: string }
  | ({ type: 'passwords:fill-in-tab'; itemId: string; part: 'login' | 'oneTimeCode' } & FrameTarget)
  | ({ type: 'passwords:save-pending' } & Pick<FrameTarget, 'tabId'>)
  | { type: 'passwords:open-dashboard' }
  | { type: 'passwords:open-popup-window' }
  | { type: 'passwords:prompt-info'; promptId: string }
  | { type: 'passwords:prompt-decide'; promptId: string; decision: 'allow' | 'browser'; choiceCredentialId: string }

export interface PromptInfo {
  kind: 'create' | 'get'
  relyingPartyId: string
  userName: string
  isUnlocked: boolean
  // For a sign-in: the passkeys that can answer, labelled by user name.
  choices: { label: string; credentialId: string }[]
}
