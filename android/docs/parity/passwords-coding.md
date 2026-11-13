# Parity: passwords and coding

What the iPhone app does in Passwords and Coding (and the autofill that goes with Passwords), and where the Android app
stands. Status is `done`, `partly` (what is missing is said) or `not ported` (why). Logic is in `android/kit`
(`com.teanode.kit.passwords`, `com.teanode.kit.coding`), screens in `android/app` (`com.teanode.app.passwords`,
`com.teanode.app.coding`), demo data in `PasswordsDemo.kt` and `CodingDemo.kt`, pictures from `PasswordsScreenshotTest`,
`CodingScreenshotTest` and `DevicePasswordRowScreenshotTest`.

Not verified on a phone or a server: the fingerprint prompt and the Keystore key behind it
(`AndroidPasswordsKeyStore`), the autofill service and its authentication screen (`PasswordsAutofill.kt`), the clipboard
expiry, `FLAG_SECURE`, the file picker of the coding composer, and every call to a real server. They are written against
the platform's APIs and the iPhone app's protocol; none could be run here. Everything else runs against the kit's
stand-in server (`FakePasswordsServer`), which keeps only sealed text, in the JVM tests and in demo mode.

## Kit: Passwords (ios/TeaNodeKit/Sources/TeaNodeKit)

| iOS file | Android | Status |
| --- | --- | --- |
| PasswordsCrypto.swift | PasswordsCrypto.kt (JCA: AES-GCM, PBKDF2, HMAC, ECDH; HKDF and the P-256 public key by hand) | done, byte for byte against internal/passwords/testdata/vectors.json |
| PasswordsFields.swift | PasswordsFields.kt | done (the strict access record reading included) |
| PasswordsAPI.swift | PasswordsApi.kt (the documents copied as they are, the views, the inputs, `ConnectionPasswordsApi`) | done |
| PasswordsSession.swift | PasswordsSession.kt | partly: setup, unlock (passphrase, recovery key, kept key), sync, list, save, delete, restore, purge, earlier versions and restoring one, the agent switch (on and off with a new item key), a new key, passphrase and recovery key changes, lock time, erase, the record signature and its replay check. Not ported: sharing (`setShares`, recipients, waiting shares, `completeWaitingShares`, `finishSharing`, `finishPlan`), key change notices and codes compared, circle notices, the automatic re-key with its cooldown, the items an automatic action leaves for the person |
| PasswordsCache.swift | PasswordsCache.kt | partly: one document and one pins file, written whole (the Android autofill service runs in the app's own process, so there are no per-item files, no file coordination and no memory-limited loading); no sealed summary file |
| PasswordsPinStore.swift | PasswordsCache.kt (`PasswordsPins`, `PasswordsItemMemory`) | partly: the pins live in a second file of the app's private storage, not a keychain group; the record version and mac memory is ported, the sealed-for and circle memory is stored but unused (no sharing) |
| PasswordsKeyStore.swift | PasswordsKeyStore.kt (`AndroidPasswordsKeyStore`, `MemoryPasswordsKeyStore`) | partly: the Android Keystore key behind the framework biometric prompt, not verified on a phone |
| PasswordsLockPolicy.swift | PasswordsLockPolicy.kt | done |
| PasswordsGenerator.swift | PasswordsGenerator.kt (and the passphrase strength) | done |
| PasswordsOneTimeCode.swift | PasswordsOneTimeCode.kt | done, against internal/passwords/testdata/onetimecode.json |
| PasswordsPasskey.swift | PasswordsPasskey.kt | done (the authenticator as functions); the Credential Manager provider that would use it is not ported |
| PasswordsSiteMatch.swift | PasswordsSiteMatch.kt (and the identity records) | done, against internal/passwords/testdata/sitematch.json |
| PasswordsFields drafts (PasswordsEditorViews.swift `PasswordsDraft`) | PasswordsDraft.kt | done |
| PasswordsWords.swift (the filters, the line under a name, search, formats) | PasswordsFormat.kt | done |
| PasswordsDisplayText.swift | none | not ported: it works around SwiftUI hyphenating a long value; Compose breaks a long word without adding characters |
| PasswordsLocation.swift | PasswordsGenerator.kt (`PasswordsSignInEnd`), PasswordsAutofill.kt (`PasswordsAutofillPointer`) | partly: the file naming is the app's own; the rule of what a sign-in end forgets is ported |
| PasswordsAutoFillSummary.swift | none | not ported: the iPhone's extension lists logins from a sealed summary under a memory limit; the Android service opens the copy directly |
| DevicePasswords.swift | DevicePasswords.kt | done (`SyncCredential` and `MailAppPassword` implement `DevicePassword` in the Settings part) |
| Herdr.swift | Herdr.kt (the sessions, turns, agents tree, screen, the calls, `CodingQuestion` with its headline, the list grouping) | done |
| CodingSetup.swift | CodingSetup.kt | done |
| TerminalText.swift (used by the coding screen) | coding/TerminalText.kt (the Chats part has its own in `com.teanode.kit.chats`) | done |
| Operations.swift `answerCodingQuestion` | Herdr.kt `Connection.answerCodingQuestion` | done |

## Kit tests (ios/TeaNodeKit/Tests/TeaNodeKitTests)

| iOS file | Android | Status |
| --- | --- | --- |
| PasswordsCryptoTests | PasswordsCryptoTest (30) | done |
| PasswordsVectorsTests | PasswordsVectorsTest (11, reading the Go package's vectors.json) | done |
| PasswordsGeneratorTests | PasswordsGeneratorTest (7) | done |
| PasswordsLockPolicyTests | PasswordsLockPolicyTest (4) | done |
| PasswordsOneTimeCodeTests | PasswordsOneTimeCodeTest (8) | done |
| PasswordsPasskeyTests | PasswordsPasskeyTest (11) | done |
| PasswordsSiteMatchTests | PasswordsSiteMatchTest (10) | done |
| PasswordsDisplayTextTests | none | not ported (see PasswordsDisplayText) |
| PasswordsSessionTests | PasswordsSessionTest | partly: the tests of setup, unlock, sync, save, delete, restore, settings, erase, the server's sentences; none of sharing |
| PasswordsCacheTests | PasswordsSessionTest (merge, revision, pins, damaged and newer copies) | partly: no per-item files, no summary file |
| PasswordsPinTests, PasswordsReplayTests | PasswordsSessionTest (what a device remembers only goes up, a replayed record is not trusted, a dropped copy leaves the pins) | partly: nothing about sealing for people |
| PasswordsRecordTests | PasswordsSessionTest (the signed record of a new item, an edit keeps it, an older record is a replay, the agent seal) | partly |
| PasswordsRestoreAndPlanTests | PasswordsSessionTest (restoring a version, a version not listed is refused) | partly: no finish plan |
| PasswordsAccessTests, PasswordsAccessFlowTests, PasswordsAnswerChecksTests, PasswordsRekeyTests, PasswordsSharingFollowUpTests | none | not ported (sharing and the automatic re-key) |
| PasswordsAutoFillSummaryTests | none | not ported (no summary) |
| PasswordsFakeServer | FakePasswordsServer.kt (one person, no shares) | partly |
| DevicePasswordsTests | DevicePasswordsTest (3) | done |
| HerdrTests | HerdrTest (24, with the headline tests, the answer and the list grouping) | done |
| CodingSetupTests | CodingSetupTest (5) | done |
| CodingQuestionHeadlineTests | HerdrTest | done |
| TerminalTextTests | TerminalTextTest (4) | done |
| none | PasswordsFormatTest (6), PasswordsDraftTest (4) | new, for the logic that was in the iPhone's screens |

## Screens: Passwords (ios/TeaNode/Sources)

| iOS | Android | Status |
| --- | --- | --- |
| PasswordsModel.swift | PasswordsModel.kt | partly: the phases (loading, failed, not set up, recovery key, locked, unlocked), unlock three ways, locking (lock time, the phone's screen turning off), sync, every item change, settings. Not ported: the sharing parts, the notices of changed keys and circle shares, the app group and the credential identity store |
| PasswordsViews.swift: the stack, intro, list, filter menu, new item menu, options menu, search, copy menu, recently deleted, row | PasswordsSection.kt, PasswordsItemScreen.kt (`PasswordsDeletedScreen`), PasswordsNavigation.kt | done except the finish sharing, key change and circle notices rows, and the app switcher cover (a `FLAG_SECURE` window while open covers the recents picture instead) |
| PasswordsSetupViews.swift: set up with strength, recovery key (copy, share, saved), biometric next time, unlock (passphrase, recovery key, fingerprint) | PasswordsSection.kt | done |
| PasswordsItemViews.swift: the fields (reveal, copy), the one-time code counting down, sites, passkeys, notes, folder, the agent switch, a new key retry, the unchecked and replayed sharing notice, earlier versions row, delete, restore, delete forever | PasswordsItemScreen.kt | partly: no Share row, no finish sharing, no "server also lists" notice |
| PasswordsEditorViews.swift: the editor for three kinds, rebase after a change elsewhere, the generator | PasswordsEditorScreen.kt | done |
| PasswordsSettingsViews.swift: lock time, biometrics, change passphrase (with the recovery key), replace recovery key, safety code, autofill, erase | PasswordsSettingsScreen.kt | done; the "confirm it is the person" step before erase is the confirmation dialog only (the system's device credential check is not ported) |
| PasswordsVersionViews.swift: the list, one version read-only, restore | PasswordsItemScreen.kt (`PasswordsVersionsScreen`, `PasswordsVersionScreen`) | done |
| PasswordsSharingViews.swift (the share screen, who gets the item, compare codes, finish sharing) | none | not ported: the sharing half of the session is not ported |
| PasswordsDemo.swift | PasswordsDemo.kt (the stand-in server set up by an invented person's other device, with a login that has a one-time code and a passkey, an agent login, a card, a note, an item with an earlier version and a deleted item) | done for what is ported |
| PasswordsIdentityStore.swift (the system's credential identity store) | none | not needed on Android: the autofill service finds logins when asked |
| PasswordsWords.swift (the texts) | PasswordsModel.problemText, `passwords_strings.xml` | done |
| DevicePasswordRow.swift | DevicePasswordRow.kt | done |

## Autofill (ios/TeaNodeAutoFill)

| iOS | Android | Status |
| --- | --- | --- |
| CredentialProviderViewController.swift, AutoFillRequest.swift | PasswordsAutofill.kt (`PasswordsAutofillService`, `AutofillStructureParser`) | partly: an `AutofillService` that offers a locked entry for a web page with a password field |
| AutoFillModel.swift, AutoFillViews.swift | PasswordsAutofill.kt (`PasswordsAutofillActivity`) | partly: asks for the fingerprint, opens the copy with no network, fills the logins whose site fits (the exact origin first, then related hosts), at most ten. No list to choose from, no search, no one-time code fill, no passkeys (the Credential Manager provider), nothing for native apps, and nothing without the key kept behind the fingerprint |
| AutoFillIndex.swift | none | not needed (see PasswordsAutoFillSummary) |

## Screens: Coding

| iOS | Android | Status |
| --- | --- | --- |
| CodingViews.swift: the model (the list kept when a computer does not answer), the list by computer and workspace, the state tags, polling, the session (turns, agents, screen), the transcript with earlier turns, the tool call detail, the answer card, the composer, the actions menu (new tab, show on computer, tell me, close), the open sheet | CodingModel.kt, CodingSection.kt, CodingSessionScreen.kt, CodingMarkdown.kt | partly: the composer adds files from the system picker (photos and files), not the camera or a pasted picture; the agent's words are drawn by a small Markdown renderer (paragraphs, lists, headings, code, bold, tables as columns), not the Chats part's; the session shows no computer as a subtitle; a notification opening a session on its question is the notifications part's |
| CodingAgentsViews.swift | CodingAgentsScreen.kt | done |
| CodingSetupViews.swift: reporting hooks, what a session sees, `CodingTool` menu | CodingSetupScreens.kt, CodingSection.kt | done |
| CodeScanner.swift | the foundation's scanner | not this part's |

## Demo cases

Passwords: not set up (intro), the passphrase screen empty and typed, the recovery key after setup, locked, locked with the
fingerprint kept, a wrong passphrase, the list (light, dark, largest text, filter menu, new item menu, a search, a search
with no match), a login with a one-time code and a passkey (hidden and shown password, largest text), an agent login, a
card, a note, a deleted item, recently deleted, an item that is not here, earlier versions, the three editors, the
generator, the settings open and locked. Coding: the list (light, dark, largest text, the menu), a session asking, working
and idle (light, dark, largest text), the actions menu, a session no longer open, the agents tree, the screen, the reporting
hooks (before and after installing), what a session sees, the open screen, the device password rows.

## Not verified

The fingerprint and Keystore, autofill, `FLAG_SECURE`, the clipboard expiry, the system picker, the screen-off receiver,
the real server (only the kit's stand-in), and push. The strings are in English, Chinese and Japanese from the iPhone app's
catalog (the words that name the iPhone say phone here); the autofill words are new.

## Second round

Done: the computer shows in the session title bar; the coding composer adds a camera picture (through the chat's file
provider) and a pasted picture, besides photos and files; autofill fills a one-time code field (hints `smsOTPCode`,
`oneTimeCode`) with the code of the moment, and the datasets it returns are the list the system shows to choose from.
Looked at 12 more pictures (largest text, dark, filter menu, versions, deleted, locked) and made the question card's
heading wrap to two lines.

Not done, and why:
- Sharing (the share screen, `setShares`, waiting shares, `finishSharing`, key change notices, the Share row, the shared
  stand-in server and the access, rekey and answer-check tests): a port of about 1,300 lines of session logic and its
  tests, which are the security core (a key sealed for the wrong person is a leak). It needs its own round and its own
  review; it was not started rather than half done.
- The sealed autofill summary: the iPhone needs it because its extension runs under a memory limit and must not open the
  whole copy. The Android service runs in the app's process and opens only the logins it fills, so the summary has no
  job here. Porting it would add a file and its tests for nothing.
- Passkeys through the Credential Manager provider: needs `androidx.credentials` (a new dependency in the shared build
  files, Android 14 `CredentialProviderService`) and cannot be run without a phone; the authenticator in the kit
  (`PasswordsPasskey`) is ready for it.
- About 40 pictures are still unopened.
