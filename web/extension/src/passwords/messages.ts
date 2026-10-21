// The messages between the pieces of the extension's Passwords: the content
// script in a page, the service worker, and the extension's own pages. All
// travel on chrome.runtime.sendMessage and tabs.sendMessage, which a web page
// cannot send or hear. The service worker decides what to trust from who
// sent a message (sender.tab, sender.url, sender.frameId), never from what the
// message says about itself.

import type { FieldRole } from './fieldDetection'
import type { LoginSummary, OwnerKeyChangeAnswer } from './openedKeys'

// --- from a content script ---------------------------------------------

export type SuggestionAnswer =
  // Nothing to offer: signed out, Passwords not set up, or nothing for this site.
  | { suggestionState: 'none' }
  | { suggestionState: 'locked'; host: string }
  | { suggestionState: 'logins'; host: string; logins: LoginSummary[] }
  // A new password field while Passwords is open: offer a generated one.
  | { suggestionState: 'generate'; host: string }

export interface CapturedLoginMessage {
  type: 'passwords:captured'
  userName: string
  password: string
  isNewPassword: boolean
}

export interface BarOffer {
  // 'unlock': Passwords is locked; the button opens the window to unlock.
  offerAction: 'save' | 'update' | 'unlock'
  host: string
  userName: string
}

export interface FillFieldsMessage {
  type: 'passwords:fill-fields'
  // origin is the origin (scheme, host and port) the secrets were approved
  // for; the content script fills only if its own frame is still at it.
  origin: string
  // isForeignActionConfirmed: the person was told the form sends to another
  // host than the page's own and said to fill anyway.
  isForeignActionConfirmed: boolean
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
  | { type: 'passwords:bar-answer'; barAnswer: 'save' | 'dismiss' | 'unlock' }
  | { type: 'passwords:open-popup' }
  | { type: 'passwords:passkey-cancel' }
  | {
      type: 'passwords:passkey'
      ceremonyKind: 'create' | 'get'
      options: Record<string, unknown>
      isCrossOrigin: boolean
    }

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

// OtherSiteQuestion is what the window asks before it fills a login into a
// page the login is not saved for ("Fill <name> into <site>? It is saved for
// <its sites>."). The answer to it is sent back with the origin, so that a
// page that moved in the meantime is not filled on the strength of a yes
// given for another.
export interface OtherSiteQuestion {
  loginName: string
  // origin is the origin (scheme, host and port) of the page the window would
  // fill into, written whole so that a different scheme or port is seen.
  origin: string
  // savedSites are the origins the login is saved for, written the same way;
  // none when it has no site.
  savedSites: string[]
  // isPageEncrypted is false for a plain-text page that is not a local one.
  isPageEncrypted: boolean
}

export interface FillInTabAnswer {
  filledCount: number
  foreignActionHost: string
  // otherSite is set instead of filling when the login is not saved for the
  // page and the person has not said yes to that yet.
  otherSite?: OtherSiteQuestion
}

export interface SearchAnswer {
  forThisSite: LoginSummary[]
  others: LoginSummary[]
  // ownerKeyChangedCount is how many shared logins were left out because
  // their owner's key is not the one this device kept.
  ownerKeyChangedCount: number
  // ownerKeyChanges are those owners, with the safety code before and now, for
  // the window to ask whether the new key matches.
  ownerKeyChanges: OwnerKeyChangeAnswer[]
}

export type PopupRequest =
  | ({ type: 'passwords:state' } & Partial<FrameTarget>)
  | { type: 'passwords:unlock'; secretText: string; isRecoveryKey: boolean }
  | { type: 'passwords:lock' }
  | ({ type: 'passwords:search'; query: string } & Partial<FrameTarget>)
  | { type: 'passwords:copy'; itemId: string; copiedField: 'password' | 'userName' | 'oneTimeCode' }
  | { type: 'passwords:copy-text'; text: string }
  | { type: 'passwords:one-time-code'; itemId: string }
  | ({
      type: 'passwords:fill-in-tab'
      itemId: string
      fillPart: 'login' | 'oneTimeCode'
      isForeignActionConfirmed?: boolean
      // confirmedOtherSiteOrigin is the origin the person said yes to filling
      // a login that is not saved for it into (see OtherSiteQuestion).
      confirmedOtherSiteOrigin?: string
    } & FrameTarget)
  | ({ type: 'passwords:save-pending' } & Pick<FrameTarget, 'tabId'>)
  | { type: 'passwords:confirm-owner-key'; ownerUserId: string; newPublicKey: string }
  | { type: 'passwords:open-dashboard' }
  | { type: 'passwords:open-popup-window' }
  | { type: 'passwords:prompt-info'; promptId: string }
  | {
      type: 'passwords:prompt-decide'
      promptId: string
      promptDecision: 'allow' | 'browser'
      choiceCredentialId: string
    }

export interface PromptInfo {
  ceremonyKind: 'create' | 'get'
  relyingPartyId: string
  // pageOrigin is the origin of the page that asked, which can be another
  // than the relying party id names (a page at a host under it).
  pageOrigin: string
  userName: string
  isUnlocked: boolean
  // isVerificationNeeded: the site insists on the person proving who they are
  // and they have not typed the passphrase lately.
  isVerificationNeeded: boolean
  // For a sign-in: the passkeys that can answer, labelled by user name.
  // sharedByText is "Shared by <owner name>" for a passkey in a login somebody
  // else owns, else empty.
  choices: { label: string; credentialId: string; sharedByText: string }[]
}
