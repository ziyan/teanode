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
| PasswordsSession.swift | PasswordsSession.kt, PasswordsSharing.kt (the recipient, key trust, waiting share, key change, circle notice, completed shares and finish plan types) | done, including sharing: `recipients`, `setShares`, `waitingShares`, `completeWaitingShares`, `finishSharing`, `finishPlan`, `itemIdsNeedingYou`, `itemIdsWithLostAgentCopy`, `giveNewKeysToMarkedItems`, the origin-aware `rekey` (choice, request, automatic, restore) with its ten minute wait and the limit of twenty a session, `keyChanges`, `circleShareNotices`, `markCompared`. The session takes a lock around each public call, so the calls that call each other (finish sharing, set shares, rekey, sync) use unlocked inner functions. Not ported: the on-demand loading and the autofill summary (the Android service runs in the app's process) |
| PasswordsCache.swift | PasswordsCache.kt | partly: one document and one pins file, written whole (the Android autofill service runs in the app's own process, so there are no per-item files, no file coordination and no memory-limited loading); no sealed summary file |
| PasswordsPinStore.swift | PasswordsCache.kt (`PasswordsPins`, `PasswordsItemMemory`, `PasswordsSealedChange`) | done except the keychain: the pins live in a second file of the app's private storage; item memories follow the item id and not the user id, a changed key is recorded when none was known, comparing codes clears the notice, `merging` and `sealing` are ported |
| PasswordsKeyStore.swift | PasswordsKeyStore.kt (`AndroidPasswordsKeyStore`, `MemoryPasswordsKeyStore`) | partly: the Android Keystore key behind the framework biometric prompt, not verified on a phone |
| PasswordsLockPolicy.swift | PasswordsLockPolicy.kt | done |
| PasswordsGenerator.swift | PasswordsGenerator.kt (and the passphrase strength) | done |
| PasswordsOneTimeCode.swift | PasswordsOneTimeCode.kt | done, against internal/passwords/testdata/onetimecode.json |
| PasswordsPasskey.swift | PasswordsPasskey.kt, PasswordsPasskeyRequests.kt | done (the authenticator as functions, and the mapping between the Credential Manager's JSON and the authenticator, tested without a phone) |
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
| PasswordsSessionTests | PasswordsSessionTest (34), PasswordsSharingSessionTest (16) | done: the setup, unlock, sync, save, delete, restore, settings, erase and sentences tests, and every sharing test (recipients, key trust, waiting shares, key changes, comparing codes, the opener for autofill), plus two tests that a key sealed for the wrong person opens nothing |
| PasswordsCacheTests | PasswordsSessionTest (merge, revision, damaged and newer copies), PasswordsCachePinsTest (3: pinned keys apart from the copy, an unreadable pins file never written over, erase keeping the pins) | done except the per-item files, the summary file and the keychain legacy migration tests (no such thing here) |
| PasswordsPinTests | PasswordsPinTest (3) | done |
| PasswordsReplayTests | PasswordsReplayTest (7) | done (the file test uses the file storage in place of `PasswordsLocation`) |
| PasswordsRecordTests | PasswordsRecordTest (14) | done |
| PasswordsRestoreAndPlanTests | PasswordsRestoreAndPlanTest (9) | done |
| PasswordsAccessTests | PasswordsAccessTest (18) | done |
| PasswordsAccessFlowTests | PasswordsAccessFlowTest (18) | done |
| PasswordsAnswerChecksTests | PasswordsAnswerChecksTest (23) | done |
| PasswordsRekeyTests | PasswordsRekeyTest (6) | done |
| PasswordsSharingFollowUpTests | PasswordsSharingFollowUpTest (8) | done |
| none | PasswordsPasskeyRequestsTest (15) | new, for the Credential Manager mapping |
| PasswordsAutoFillSummaryTests | none | not ported (no summary) |
| PasswordsFakeServer | FakePasswordsServer.kt (several people, circles, shares and their keys, waiting shares, too much shared, `setCircle`, `replacePublicKey`, `replaceShareKey`, the rekey refusal and count; each person's connection is `api(userId)`) | done |
| DevicePasswordsTests | DevicePasswordsTest (3) | done |
| HerdrTests | HerdrTest (24, with the headline tests, the answer and the list grouping) | done |
| CodingSetupTests | CodingSetupTest (5) | done |
| CodingQuestionHeadlineTests | HerdrTest | done |
| TerminalTextTests | TerminalTextTest (4) | done |
| none | PasswordsFormatTest (6), PasswordsDraftTest (4) | new, for the logic that was in the iPhone's screens |

## Screens: Passwords (ios/TeaNode/Sources)

| iOS | Android | Status |
| --- | --- | --- |
| PasswordsModel.swift | PasswordsModel.kt | done except the app group and the credential identity store: the phases (loading, failed, not set up, recovery key, locked, unlocked), unlock three ways, locking (lock time, the phone's screen turning off), sync (which now gives marked items a new key and waiting people their keys), every item change, settings, and the sharing state: key changes, circle notices, items needing the person, items held back, the finish plan and the finish of one or every item with its progress |
| PasswordsViews.swift: the stack, intro, list, filter menu, new item menu, options menu, search, copy menu, recently deleted, row | PasswordsSection.kt, PasswordsItemScreen.kt (`PasswordsDeletedScreen`), PasswordsNavigation.kt, PasswordsSharingScreens.kt (the notices and the finish sharing rows at the top of the list) | done except the app switcher cover (a `FLAG_SECURE` window while open covers the recents picture instead) |
| PasswordsSetupViews.swift: set up with strength, recovery key (copy, share, saved), biometric next time, unlock (passphrase, recovery key, fingerprint) | PasswordsSection.kt | done |
| PasswordsItemViews.swift: the fields (reveal, copy), the one-time code counting down, sites, passkeys, notes, folder, the agent switch, a new key retry (and the held-back wording), finish sharing with its question, the unchecked and replayed sharing notice and "Set sharing again", the Share row with who it is shared with, the "server also lists" notice, earlier versions row, delete, restore, delete forever | PasswordsItemScreen.kt | done |
| PasswordsEditorViews.swift: the editor for three kinds, rebase after a change elsewhere, the generator | PasswordsEditorScreen.kt | done |
| PasswordsSettingsViews.swift: lock time, biometrics, change passphrase (with the recovery key), replace recovery key, safety code, autofill, erase | PasswordsSettingsScreen.kt | done; the "confirm it is the person" step before erase is the confirmation dialog only (the system's device credential check is not ported) |
| PasswordsVersionViews.swift: the list, one version read-only, restore | PasswordsItemScreen.kt (`PasswordsVersionsScreen`, `PasswordsVersionScreen`) | done |
| PasswordsSharingViews.swift (the share screen, who gets the item, compare codes, key change and circle notice rows) | PasswordsSharingScreens.kt (`PasswordsShareScreen`, `PasswordsRecipientRow`, `PasswordsCompareCodesSheet`, `PasswordsKeyChangeNotice`, `PasswordsCircleShareNoticeRow`, the finish sharing question and rows) | done; the person and circle picker is the finance area's `ShareTargetPicker`, which reads the same people and circles. No "Open Circles" link: this app has no circles screen yet |
| PasswordsVersionViews.swift: the finish sharing dialog | PasswordsSharingScreens.kt (`PasswordsFinishSharingDialog`) | done |
| PasswordsDemo.swift | PasswordsDemo.kt (the stand-in server set up by an invented person's other device, with a login that has a one-time code and a passkey, an agent login, a card, a note, an item with an earlier version and a deleted item) | done for what is ported |
| PasswordsIdentityStore.swift (the system's credential identity store) | none | not needed on Android: the autofill service finds logins when asked |
| PasswordsWords.swift (the texts) | PasswordsModel.problemText, `passwords_strings.xml` | done |
| DevicePasswordRow.swift | DevicePasswordRow.kt | done |

## Autofill (ios/TeaNodeAutoFill)

| iOS | Android | Status |
| --- | --- | --- |
| CredentialProviderViewController.swift, AutoFillRequest.swift | PasswordsAutofill.kt (`PasswordsAutofillService`, `AutofillStructureParser`) | partly: an `AutofillService` that offers a locked entry for a web page with a password field |
| AutoFillModel.swift, AutoFillViews.swift | PasswordsAutofill.kt (`PasswordsAutofillActivity`), PasswordsCredentialProvider.kt (`PasswordsCredentialProviderService`, `PasswordsCredentialActivity`) | partly: asks for the fingerprint, opens the copy with no network, fills the logins whose site fits (the exact origin first, then related hosts), at most ten, and a one-time code; passkeys through the Credential Manager on Android 14 and later (a locked entry, then the passkeys that fit, signing with the one picked, and making a passkey into the login for that user and site, saved to the server before the website gets it). No search, nothing for native apps, nothing without the key kept behind the fingerprint |
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

Third round: the passkey provider has never run on a phone or an emulator (needs Android 14, a screen lock and a website or app that asks for a passkey), the privileged browser list (`assets/passwords_passkey_privileged_apps.json`, Google's published list) is only read by the platform call, and the sharing flows run only against the kit's stand-in server (the real server's sentences and limits are assumed from the iPhone's code).

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

## Third round

Done: sharing (the kit port with every iPhone sharing test, 141 more kit tests for Passwords than before, 268 in all; the share screen, recipients with their safety codes, comparing codes, the waiting and "too much shared" wording, finish sharing for one item or every item with its question and progress, key change and circle notices, the Share row, the unchecked and replayed notices); passkeys through the Credential Manager (a `CredentialProviderService` and an activity, with the request and response mapping tested in the kit); the pictures.

The shared build files changed for passkeys: `gradle/libs.versions.toml` (`credentials = "1.3.0"`, `androidx-credentials`) and `app/build.gradle.kts` (`implementation(libs.androidx.credentials)`).

Porting the tests found three bugs in the kit that the earlier rounds had: an access record whose `isUsableByAgent` was the text "true" was accepted, a changed key was not recorded for a person this device had never seen, and comparing codes did not clear the changed-key notice. A test, a fourth difference, showed that what a device remembers about an item must follow the item id and not the user id the server names; the pins were reading another person's pins as empty.

Not done: the progress row while "Finish sharing" runs for every item has no picture (it needs a running session); the finish question and the compare codes sheet are drawn in the page since a dialog or a sheet is a window of its own.
