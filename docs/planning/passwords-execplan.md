# Passwords: logins, passkeys, cards and notes, readable only on the person's devices

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

People keep their passwords in a manager (Apple Passwords, Bitwarden, 1Password, or `pass`, the Unix password store). After this plan, TeaNode is that manager, on the person's own server:

- **Passwords** holds logins (user name, password, sites, one-time code secret, passkeys), cards and notes. Everything in it is encrypted on the person's devices before it reaches the server. The server, its operator, a stolen backup and the agent see only encrypted data, except for the items the person lets the agent use.
- The person sets Passwords up once, with a **passphrase** (separate from their TeaNode sign-in) and a **recovery key** they print or save. A device asks for the passphrase once; after that the iPhone opens it with Face ID and Chrome keeps it open for a time the person chooses.
- The **TeaNode Chrome extension** fills logins, offers to save a login after the person signs in, generates passwords, and creates and uses passkeys on websites.
- The **iPhone app** has a Passwords screen and is an **AutoFill provider**: in Settings, General, AutoFill & Passwords the person turns on TeaNode, and Safari, Chrome and other apps offer its logins, passkeys and one-time codes. A passkey sign-in on a computer (the QR code) can use it too.
- The **dashboard** has a Passwords page, and `teanode passwords` does the same from the command line, including importing a `pass` store, and Chrome, Apple Passwords or Bitwarden exports.
- The person can **share** an item with a circle or one person in a circle. The people it reaches can use it (fill, copy, sign in with its passkey); only the owner changes it.
- The person can let **the agent use** one item. The agent then signs in with it in its headless browser or in the person's attached tab: the password, code or passkey is typed in or signed by TeaNode, only on that item's sites, and never shown to the model. A note the agent may use is read by the model.

To see it working at the end: on a development server, set up Passwords on the dashboard with an invented passphrase and save the recovery key. `teanode passwords import pass` on a test store of three invented entries (one with a `login:` line, one with an `otpauth://` line) adds three logins. The Chrome extension, unlocked, fills the login on a local test page served at `https://login.example.test`, offers to save a new one after a test sign-in, and creates then uses a passkey on a local WebAuthn test page. The iPhone simulator (demo data) shows the Passwords screen and the AutoFill list. A second account in the same circle sees a shared login and fills it. Asked "sign in to the test site and read my order", the agent says it may not use that login until the person lets it; after the person turns on "Let the agent use this" on that item, the agent signs in in the headless browser, and its transcript contains no password. A database dump shows no site name, user name or password in plain text.

## Progress

- [x] (2026-10-20) Research: Apple's AutoFill provider rules and APIs, Chrome extension filling and passkeys (Bitwarden's open source extension), end-to-end designs (Bitwarden, 1Password, Proton Pass) and their published weaknesses, the `pass` format, and what exists in the repository. Summarized in Surprises and the Decision Log.
- [x] (2026-10-20) The maintainer answered the open questions (see the Decision Log).
- [x] (2026-10-20) This plan written.
- [ ] Milestone 1: the encryption format with test vectors in Go, TypeScript and Swift; storage, GraphQL, the command line and the `pass` importer.
- [ ] Milestone 2: the dashboard's Passwords page.
- [ ] Milestone 3: the Chrome extension fills, saves and generates.
- [ ] Milestone 4: the Chrome extension creates and uses passkeys.
- [ ] Milestone 5: the iPhone Passwords screen.
- [ ] Milestone 6: the iPhone AutoFill provider.
- [ ] Milestone 7: the agent uses the items it may use, in the headless browser and the attached tab.
- [ ] First use on a development server, walks in Chrome at 1400 and 390 wide, light and dark, the iPhone screenshots.
- [ ] Review rounds until a round finds nothing above low.

## Surprises & Discoveries

- Apple lets a third party app be the system's password and passkey provider through an AutoFill Credential Provider extension (`ASCredentialProviderViewController`, entitlement `com.apple.developer.authentication-services.autofill-credential-provider`). Passwords and passkeys since iOS 17, one-time codes and "text to insert" since iOS 18. Safari and Chrome on the iPhone both use it. Up to three providers can be on at once.
- The extension is killed at about 120 MB (this repository's share extension found the same). It must read a local encrypted copy and never run the passphrase derivation; the app keeps the copy current.
- A passkey made by a provider must be a synced one: Apple returns no device-bound keys from a provider. Flags BE and BS set, sign count 0, attestation `none`.
- A provider gets passkey sign-ins from a computer's QR code (hybrid) from the system, with no code of its own.
- Chrome gives extensions no passkey provider API (`chrome.webAuthenticationProxy` is for remote desktop software and suspends the browser's own WebAuthn). Bitwarden, 1Password and Dashlane replace `navigator.credentials.create` and `.get` from a script in the page's own world at `document_start`, relay to the extension, and fall back to the browser on request. This plan does the same.
- A 2025 study (DOM-based extension clickjacking) showed that a page can make a manager's fill menu invisible and trick a click. Defences: the menu in an extension-owned frame, checks that it is visible and on top when clicked, trusted events only, no fill on page load, and exact host matching by default.
- A 2026 USENIX Security paper attacked Bitwarden, LastPass and Dashlane assuming a malicious server; most attacks came through sharing, key handoffs and unauthenticated encryption. Hence: authenticated encryption with the item's id in the associated data, a person's public key shown as a short safety code, and a warning when a recipient's key changes.
- LastPass's breach exposed site addresses kept in plain text. Here site addresses, names and user names are encrypted like the password; a person's list is small enough for a device to decrypt all of it and match sites locally.
- The repository already seals secrets with AES-256-GCM under the server secret (`internal/util/secretbox`), models shares with a user or circle grantee (`health_share`), and ends shares when a person leaves a circle (`endSharesOutsideCircles`). TeaNode's own passkeys use `go-webauthn` as a relying party, which does not help an authenticator; signing is written here.
- WebCrypto has no Argon2, Apple's CryptoKit has neither Argon2 nor (on Linux, where TeaNodeKit is tested) itself. PBKDF2-SHA256 is native on every surface (WebCrypto, CommonCrypto, Go's x/crypto, `swift-crypto`'s `_CryptoExtras` on Linux).

## Decision Log

- Decision: only the person's devices decrypt Passwords. The server stores encrypted items and wrapped keys.
  Rationale: the maintainer chose it ("only on my device is preferred"); the operator of a family server is often one of its people, and passwords are the worst thing it could lose. A misled agent cannot read what the server cannot read.
  Date/Author: 2026-10-20, the maintainer.
- Decision: a passphrase typed once per device, Face ID on the iPhone after that, a timed unlock in Chrome, and a recovery key made at setup.
  Rationale: the maintainer's choice. Losing both passphrase and recovery key loses Passwords; the only way out is to erase and start again, which the setup screen says.
  Date/Author: 2026-10-20, the maintainer.
- Decision: the agent may use an item only after the person turns on "Let the agent use this" on it. Off for every item by default.
  Rationale: the maintainer's choice, and the maintainer asked that sharing and the agent's headless browser be thought through with end-to-end encryption. The person's device wraps that one item's key for the server's agent key (see below), so the server can open exactly the items the person let it, and nothing else.
  Date/Author: 2026-10-20, the maintainer.
- Decision: the first version has logins (with one-time codes and passkeys), cards and notes.
  Rationale: the maintainer chose cards and notes in the first version.
  Date/Author: 2026-10-20, the maintainer.
- Decision: cards are kept, shown and copied, but nothing fills them: no card filling in the extension and no "text to insert" on the iPhone.
  Rationale: the maintainer: "let's drop the card autofill, it's not often used".
  Date/Author: 2026-10-20, the maintainer.
- Decision: a login's secrets never reach the model. The agent fills a password or one-time code, or signs with a passkey, through the browser by the item's id; TeaNode puts the value in, only on a page whose host matches one of the item's sites.
  Rationale: the agent needs to sign in, not to know the password; a page that talks the model into "type the password here" gets nothing on another host.
  Date/Author: 2026-10-20, this plan.
- Decision: the agent can use a granted item in any run of the person's agent, scheduled runs included, and each use is recorded on the item ("used by the agent on example.com") and in the audit log.
  Rationale: signing in on a schedule (a monthly statement) is the reason to grant an item. The attached tab is still only used while the person watches (`20260910-the-attached-tab-is-the-persons.md`).
  Date/Author: 2026-10-20, this plan.
- Decision: a card cannot be granted to the agent in the first version.
  Rationale: paying stays the person's act; the payment skills already ask call by call (`20261016-a-payment-skill-is-shared-call-by-call.md`).
  Date/Author: 2026-10-20, this plan.
- Decision: no agent reads an item shared with its person. Only the owner can grant an item to their own agent.
  Rationale: the same rule as Health (`20261018-health-is-shared-by-category-as-day-summaries.md`): the owner decides what reaches a model's provider.
  Date/Author: 2026-10-20, this plan.
- Decision: PBKDF2-SHA256 with 600,000 iterations derives the passphrase key; not Argon2id.
  Rationale: native on every surface (see Surprises); Argon2id would need WebAssembly in the dashboard and extension and a C library in the app. The iteration count is stored with the wrapped key so it can be raised later.
  Date/Author: 2026-10-20, this plan.
- Decision: our own clients, not the Bitwarden API.
  Rationale: Vaultwarden shows the Bitwarden API can be served, but it is large, changes with Bitwarden's clients, and has no circles or agent. Bitwarden's JSON export is imported instead.
  Date/Author: 2026-10-20, this plan.
- Decision: recipients use a shared item; only the owner changes it.
  Rationale: one writer per item keeps the encryption simple (the owner's device rewraps on every change) and matches how location and Health are shared. Letting recipients change it would need a new choice here.
  Date/Author: 2026-10-20, this plan.

## Outcomes & Retrospective

Not started.

## Context and Orientation

- `internal/util/secretbox`: AES-256-GCM under a key derived (HKDF-SHA256) from the server secret with a label. The server's agent key below is sealed with it under the label `teanode passwords: agent key-v1`.
- `internal/db/migrations/0245_health.sql`: `health_share` is the shape of a share (`grantee_user_id` or `grantee_circle_id`, exactly one). `internal/db/database_circle.go` `endSharesOutsideCircles` ends shares when a person leaves; this plan adds item shares to it.
- `internal/access/principal.go` `IsPersonsTap`: true for a dashboard session or the iPhone app's token. Setting up, erasing, sharing and granting to the agent need it, as turning on Health's agent switch does. The Chrome extension's token is not a tap; the extension reads and saves items but does not share or grant.
- `internal/browser`: the headless Chrome beside the server (CDP), and the attached tab through the extension (`web/extension/src/background.js`, a websocket to the server). The browser tool lives in `internal/agent/tools/` (`browsing.go` and the browser package).
- `web/extension`: Manifest V3, signs in with `chrome.identity.launchWebAuthFlow` against `/cli`, holds its token in `chrome.storage.local`, has no content scripts yet.
- `ios/project.yml`: targets TeaNode, TeaNodeNotification, TeaNodeShare, TeaNodeWidgets; app group `$(TEANODE_APP_GROUP)`, keychain group `$(TEANODE_KEYCHAIN_GROUP)`. `ios/TeaNodeKit` is the code the app and its extensions share, tested on Linux in Docker (`swift:6.1`).

Words used everywhere: **Passwords** (the feature, Apple's own word), an **item** (one login, card or note), a **login**, a **card**, a **note**, a **passkey** (always inside a login), a **one-time code**, the **passphrase**, the **recovery key**, **share**, **Let the agent use this**. Not "vault", not "entry", not "credential" (which already names a domain's sending credentials).

## The contract every surface shares

### Encryption (`internal/passwords/` in Go, `web/src/passwords/crypto.ts`, `ios/TeaNodeKit/Sources/TeaNodeKit/PasswordsCrypto.swift`)

All three implement this format byte for byte and are pinned by one file of test vectors, `internal/passwords/testdata/vectors.json`, written by a Go test (`go test ./internal/passwords -run TestWriteVectors -update`) and read by the Go, dashboard (vitest) and TeaNodeKit tests. Every function that draws randomness takes it as a parameter in the code the tests reach (a nonce, a key, an ephemeral private key), so the vectors are deterministic.

- **Text to bytes.** Every string is UTF-8. The passphrase is normalized to Unicode NFC first (Go `golang.org/x/text/unicode/norm`, JavaScript `normalize('NFC')`, Swift `precomposedStringWithCanonicalMapping`). Base64 is standard with padding (RFC 4648 section 4).
- **Sealed value.** AES-256-GCM, a 12 byte random nonce, the 16 byte tag appended to the ciphertext, and associated data given as text. Written as the JSON object `{"version":1,"nonce":"<base64>","ciphertext":"<base64 of ciphertext and tag>"}`. A sealed value is stored in a `text` column as that JSON.
- **Account key.** 32 random bytes.
- **Passphrase key.** PBKDF2-HMAC-SHA256(NFC passphrase, salt of 16 random bytes, 600000 iterations, 32 bytes). The salt and count are stored.
- **Wrap and proof keys.** From a 32 byte secret (the passphrase key, or the recovery key's bytes): `wrap = HKDF-SHA256(ikm: secret, salt: empty, info: "teanode passwords wrap v1", 32 bytes)` and `proof = HKDF-SHA256(ikm: secret, salt: empty, info: "teanode passwords proof v1", 32 bytes)`. The proof is sent as base64; the server stores base64 of SHA-256(proof bytes) and compares in constant time.
- **Account key wrapped.** Sealed with `wrap`, associated data `account-key`.
- **Recovery key.** 32 random bytes, written as RFC 4648 base32 without padding (52 upper case letters and digits 2 to 7) in 13 groups of 4 joined by single spaces. Read back ignoring ASCII spaces, tabs, line breaks and hyphens and the case of A to Z. Anything else is refused, in every client: a padding sign `=`, a letter some languages change when they upper-case it (a sharp s becomes SS, a long s becomes S), a non-breaking space, and a last letter whose unused bits are not zero. `testdata/vectors.json` lists the texts every client must accept (`recoveryKey.acceptedTexts`) and refuse (`recoveryKeyInvalid`).
- **Person key pair.** P-256 ECDH. The public key is the 65 byte uncompressed point (`04 || X || Y`) in base64. The private key is the 32 byte scalar, sealed with the account key, associated data `private-key`. (WebCrypto imports it as a JWK built from the scalar and the public point.)
- **Safety code.** SHA-256 of the 65 public key bytes; bytes 0 to 9 taken as five big-endian 16 bit numbers, each modulo 10000, written as four digits with leading zeros, joined by spaces: `0412 9930 0007 5121 8800`.
- **Item key.** 32 random bytes per item. Sealed for the owner with the account key, associated data `item-key:<item id>`.
- **Item fields.** The fields JSON (below) sealed with the item key, associated data `item:<item id>:<item kind>`.
- **Sealed for a key pair** (a recipient, or the server's agent key), sent by the item's owner. Make an ephemeral P-256 key pair; `ephemeralShared` = the 32 byte X coordinate of ECDH(ephemeral private, recipient public) and `ownerShared` = the X coordinate of ECDH(owner's person private key, recipient public); `key = HKDF-SHA256(ikm: ephemeralShared || ownerShared, salt: the recipient's 65 public key bytes, info: "teanode passwords share v1", 32 bytes)`; seal the item key with it, associated data `share:<item id>:<owner id>:<recipient id>`, where the recipient id is the recipient's user id, or `agent` for the server's agent key. The recipient computes the same two secrets from its private key, the ephemeral public key and the owner's public key, so only a holder of the owner's private key can make a share that opens: the server cannot plant an item "shared by" somebody. Written as `{"version":1,"ephemeralPublicKey":"<base64 65 bytes>","nonce":"<base64>","ciphertext":"<base64>"}`.
- **Known keys.** Each device keeps the public keys of the people it has shared with or received from (trust on first use), and the server is never trusted to change one: a device seals an item key only for a recipient whose key it already knows or that the person has just confirmed by its safety code, opens a shared item only with the owner key it knows, and shows both safety codes when a key changes, sealing nothing until the person confirms. Changing who an item is shared with sends keys only for newly reached, confirmed recipients; existing recipients keep the key they have.
- **Passphrase rules.** The iteration count must be between 600000 and 10000000 on every client and on the server, and a passphrase has at least 12 characters.
- **Server agent key.** One P-256 key pair per server; the private scalar sealed by `secretbox` with the label `teanode passwords: agent key-v1`.
- **One-time codes.** RFC 6238 from an `otpauth://totp/...` URI: `secret` (base32, case-insensitive, padding optional), `algorithm` SHA1 (default), SHA256 or SHA512, `digits` 6 (default) or 8, `period` 30 (default).
- **Passkeys** (ES256 only, COSE algorithm -7). The private key is PKCS#8 DER in base64 inside the login's fields. Registration: a 16 byte random credential id; authenticator data = SHA-256(relying party id) (32) || flags || sign count 0 (4 bytes big-endian) || AAGUID (16) || credential id length (2 bytes big-endian) || credential id || the COSE public key (CBOR map `{1: 2, 3: -7, -1: 1, -2: X, -3: Y}` with canonical key order 1, 3, -1, -2, -3); flags `0x5D` (UP, UV, BE, BS, AT) when the person was verified, `0x59` when not; attestation object = CBOR map `{"fmt": "none", "attStmt": {}, "authData": <bytes>}` in that key order. Sign-in: authenticator data = SHA-256(relying party id) || flags (`0x1D` verified, `0x19` not) || sign count 0; the signature is ECDSA P-256 SHA-256 over authenticator data || client data hash, DER encoded. TeaNode's AAGUID is the fixed 16 bytes `5445414e-4f44-4550-4153-53574f524453` (the ASCII of "TEANODEPASSWORDS").


### Item fields (inside the encryption)

- Login: `name`, `userName`, `password`, `sites` (list of URLs), `oneTimeCodeUri` (`otpauth://`), `notes`, `passkeys` (each `credentialId`, `relyingPartyId`, `userHandle`, `userName`, `userDisplayName`, `privateKey` (PKCS#8, ES256), `createdAt`), `folder`.
- Card: `name`, `cardholderName`, `number`, `expiryMonth`, `expiryYear`, `securityCode`, `brand`, `notes`, `folder`.
- Note: `name`, `text`, `folder`.

### Storage (migration `0251_passwords`, with `.reverse.sql` and a reverse test like its neighbours)

- `password_setup`: `user_id` (primary key), `account_key_by_passphrase`, `passphrase_salt`, `passphrase_iteration_count`, `passphrase_proof_hash`, `account_key_by_recovery_key`, `recovery_key_proof_hash`, `public_key`, `private_key_by_account_key`, `lock_after_minutes`, `created_at`, `updated_at`.
- `password_item`: `id` (UUID), `owner_user_id`, `item_kind` (`login`, `card`, `note`), `encrypted_fields`, `item_key_by_account_key`, `revision` (bigint, raised on each save), `is_usable_by_agent`, `item_key_by_agent_key` (null unless usable by the agent), `last_used_by_agent_at`, `deleted_at` (kept 30 days, then removed), `created_at`, `updated_at`.
- `password_share`: `id`, `item_id`, `owner_user_id`, `grantee_user_id` or `grantee_circle_id` (exactly one), `created_at`.
- `password_share_key`: `item_id`, `recipient_user_id`, `item_key_by_recipient_key`, `recipient_public_key` (the key it was wrapped for), `created_at`; primary key (`item_id`, `recipient_user_id`). A circle share has a row per member with Passwords set up; a member without one, or who joined later, is listed as waiting until an owner's device adds their row.
- `password_item_version`: `item_id`, `revision` (the one the item had before the save), `encrypted_fields`, `item_key_by_account_key`, `replaced_at`; primary key (`item_id`, `revision`). A save that replaces an item first copies what it replaces here; the newest 20 versions of an item are kept, for 30 days (the hourly sweep that removes deleted items removes older versions). Nothing in the API reads these rows: they are for an operator to put back from the database after a bad save.
- `password_agent_key`: one row, `public_key`, `private_key_sealed`.
- Permission `passwords:use`.

### GraphQL (`internal/api/v1api/apigraph/passwords.go`)

Every operation needs `passwords:use`; a person without it is told "not found". Every sealed value is a `String` holding the JSON of the format above, and the server checks its shape on every input (version 1, a 12 byte nonce, a ciphertext of at least 16 bytes, canonical base64 with no other spelling and no extra field; a key sealed for a key pair also has an `ephemeralPublicKey` of 65 bytes starting with `04`; public keys, salts and hashes are canonical base64 too), and every identifier is a `String` (the schema has no ID type). A revision is an `Int`. An item's `id` is a lower case UUID made by the device, because the encryption names it (the server refuses upper case, and the database checks it too).

Queries:

- `ReadPasswordsSetup`: `isSetUp`, `passphraseSalt`, `passphraseIterationCount`, `publicKey` (null until set up), `lockAfterMinutes`, `itemCount`.
- `ListPasswordItems(sinceRevision: Int)`: `items` (the person's own, deleted ones with `deletedAt`, and those shared with them for which a key sealed for their current public key is stored, whose `revision` is above `sinceRevision`), `itemIds` (every item they can open now, so a device drops what is gone; an id there that a device does not hold means it asks again from the start), `revision` (the highest visible, 0 with none). The listing reads under a shared advisory lock on the revision lock below, so a change that has not committed is waited for and none can commit between its statements: the revision it answers is never above one that is not visible, and a device that continues from it skips nothing. An item's `itemKey` is the owner's key sealed by the account key, or for a recipient their own key sealed for their public key, which they open with the item's `ownerPublicKey` (the owner's current public key from `password_setup`, on every item view, `String!`; a device compares it with the key it knows for that owner before using it) and the item's `ownerUserId`; `shares` is the owner's list with each share's `recipients` (name, `publicKey`, `hasKey`) and is empty for anybody else.
- `ListPasswordRecipients(granteeUserId, granteeCircleId)`: the person (in a circle with the caller) or the members of a circle the caller is in, without the caller, who may use Passwords, with the public key each set up (null if none). `hasKey` is false here, since no item is asked about. A person or circle the caller has no reach to is "not found", as in Health.
- `ListWaitingPasswordShares`: for the caller's own items, each recipient a share reaches who has set up Passwords and has no key sealed for their current public key (`itemId`, `recipient`).
- `ReadPasswordsAgentKey`: the base64 public key (65 bytes) of the server's agent key, made on first use. The private scalar is sealed under the secretbox label `teanode passwords: agent key-v1`.

Mutations (a "tap" is `IsPersonsTap`: the dashboard or the iPhone app, not a command line token, with the `api.TapRequiredPhrase` refusal):

- `SetUpPasswords(input: PasswordsSetupInput)` (tap; refused with "Passwords is already set up."): `accountKeyByPassphrase`, `passphraseSalt`, `passphraseIterationCount`, `passphraseProofHash`, `accountKeyByRecoveryKey`, `recoveryKeyProofHash`, `publicKey` (checked to be a P-256 point), `privateKeyByAccountKey`. `passphraseIterationCount` must be from 600000 to 10000000 here and in `ChangePasswordsPassphrase`.
- `UnlockPasswords(proof, isRecoveryKey)`: `accountKeyWrapped` (by the passphrase, or by the recovery key with `isRecoveryKey`), `privateKeyByAccountKey`, `publicKey`. Wrong proofs say "That passphrase is not right." or "That recovery key is not right.", are limited to 10 in a row for each person on an instance (30 an hour given back, in memory like the sign-in limits; a try is taken before the proof is checked and given back when the proof is right, so proofs sent at the same moment cannot all be judged before the first wrong one is counted), and are written to the audit log without the proof.
- `ChangePasswordsPassphrase(input: PasswordsPassphraseChangeInput)` (tap; `proof` of the old passphrase, or of the recovery key with `isRecoveryKey`) and `ReplacePasswordsRecoveryKey(input: PasswordsRecoveryKeyReplacementInput)` (tap; `proof` of the passphrase). Both answer the setup.
- `UpdatePasswordsSettings(lockAfterMinutes)` (1 to 10080), `ErasePasswords` (tap; answers true).
- `SavePasswordItem(input: PasswordItemInput)`: `id`, `itemKind`, `encryptedFields`, `itemKey`, `baseRevision` (null for a new item). Owner only. A `baseRevision` that is not the current one is refused with "This item changed on another device. Open it again."; the kind cannot change; a deleted item cannot be saved.
- `DeletePasswordItem(id)`, `RestorePasswordItem(id)`: a delete keeps the item 30 days, ends the keys sealed for its recipients and the agent's use; a restore leaves the recipients waiting.
- `SetPasswordItemShares(input: PasswordItemSharesInput)`: `itemId`, `grantees` (`granteeUserId` or `granteeCircleId`), `keys` (`recipientUserId`, `itemKeyByRecipientKey`, `recipientPublicKey`). It replaces the item's shares; a tap is needed when it adds a grantee (keeping or narrowing needs none). A key's recipient must be reached by a grantee, else "not found"; its `recipientPublicKey` must equal their current one, else "That person's key changed since it was read." Keys of people no longer reached are deleted. Nothing is stored when it is refused.
- `AddPasswordShareKeys(keys: [PasswordItemShareKeyInput!]!)`: the same keys with an `itemId`, for recipients already reached (the waiting ones); no tap, since it widens nothing; answers how many were stored.
- `SetPasswordItemUsableByAgent(input: PasswordItemAgentUseInput)`: `itemId`, `isUsableByAgent`, `itemKeyByAgentKey` (needed to turn it on). Turning it on needs a tap; turning it off does not. A card is refused with "A card cannot be used by the agent."

Revisions come from one sequence, drawn under an advisory lock held until commit so that numbers are committed in the order they are drawn and a device that has seen N has seen everything below it. A save, a delete, a restore, a change of shares or keys, a change of the agent's use, the agent using an item, and a person leaving the circle that made a share possible each give the affected items the next number. A mutation that is refused after writing something would be committed with the rest of the request, so the ones that can refuse late (shares and keys) run under a savepoint. Items deleted more than 30 days ago are removed by the hourly sweep that removes expired sessions and tokens (`authenticator.Scavenge`). The audit log has `passwords` (set up, passphrase and recovery key changes, unlock failures, erase) and `password_item` (saved, deleted, restored, shares changed, keys added, agent use changed, used by the agent) events, with ids, kinds, revisions and grantees, never a sealed value, key or hash.

### The agent (`internal/agent/tools/passwords/`)

- `passwords` tool, actions: `list` (the items the person let the agent use: name, kind, sites, user name; never a password, code or card number), `read_note` (a note the agent may use), `generate` (a new password, never stored by the agent).
- The browser tool gains `fill_login` (item id, the user name and password fields by snapshot reference), `fill_one_time_code` (item id, field reference) and passkey sign-in: in the headless browser through CDP's WebAuthn domain (a virtual authenticator holding only that passkey, removed after the sign-in); in the attached tab by asking the extension through the attached tab's existing `act`/`result` pair (#612's protocol), with the actions `fill_login`, `fill_one_time_code` and `passkey_sign_in` whose arguments the server builds; the extension's `PROTOCOL` goes to 5 and the server checks it as it checks casting for 4. While the person has taken over the tab, these are refused like every other acting action (only reading actions run), so the agent never fills while the person holds the tab; a fill the person starts from the extension is theirs. The content scripts never detach the debugger the agent and the live view share.
- Each refuses an item not usable by the agent ("Turn on Let the agent use this on the item first"), a page whose host does not match the item's sites, and a card.

### The command line (`internal/client/passwords.go`, `internal/cmd/passwords.go`)

`teanode passwords setup | unlock | list | show | copy | add | edit | delete | share | unshare | agent | generate | import pass | import csv | export`. It asks for the passphrase (never in an argument or environment variable; `--passphrase-stdin` for scripts), at least 12 characters when it makes one. `setup` asks the server whether this connection may set up (it sends an empty setup, which the server refuses for want of a tap before it looks at the input) before it asks for a passphrase. `unlock` keeps the opened keys in a file readable by its owner only, in `$XDG_RUNTIME_DIR/teanode` when that is set and in the configuration directory otherwise, for the lock time but never more than 8 hours; a file that others could read or change, that is a link, or that belongs to somebody else is refused, and an expired one is removed. Each profile keeps the public keys of the people it shared with or received from in `passwords-keys-<profile>.json` in the configuration directory (also mode 0600): `share`, `share --waiting` and an item shared with the person seal or open only for a key in that file or one confirmed now by its safety code, with both codes shown on a change, a refusal without a terminal unless `--force`; `unshare` sends no keys. `copy` clears the clipboard after the time given, on Ctrl-C and when asked to end, and only if the clipboard still holds the secret (read back where the clipboard program can). Changing the passphrase or the recovery key makes the old one stop opening Passwords on this server; it does not make a new account key, so a copy of the database from before still opens with the old one. `import pass` runs `gpg` on the person's own machine, reads the first line as the password, `login:`, `user:`, `username:`, `email:`, `url:` lines and an `otpauth://` line, and keeps the rest as notes; the path becomes the folder and name; the path is given to gpg after `--`. `import csv` reads Chrome's, Apple Passwords' and Bitwarden's CSV exports.

### The dashboard (`web/src/pages/passwords/`)

An unlock screen, the list (search, folders, kinds), an item with its fields hidden until shown, copy buttons, the one-time code ticking, the editor with a generator, Share (circles and people, safety codes, waiting recipients), Let the agent use this, Recently deleted, and a Settings tab (passphrase, recovery key, lock time, import, erase). Locked again after the lock time or on reload.

### The Chrome extension (`web/extension/src/passwords/`)

- Unlock in the popup. The account key is kept in `chrome.storage.session` and dropped after the lock time.
- The popup lists the logins for the current tab's host first; Fill, copy, one-time code, open the dashboard.
- A suggestion under a focused user name or password field, drawn in an extension-owned frame; fill only on a trusted click while the frame is visible and on top. Never fills on page load, into a hidden field, or into a frame on another host without asking.
- A keyboard shortcut and a context menu entry fill too.
- After a sign-in form is sent with a password that is not saved, offers to save or update it.
- Passkeys: a script in the page's own world at `document_start` replaces `navigator.credentials.create` and `.get` for `publicKey` requests, relays them to the extension, which checks the relying party id against the page's host (same registrable domain), creates or signs (ES256, attestation `none`, flags UP, UV, BE, BS), and offers "Use the browser instead".
- Cards are not filled; the popup shows and copies them.
- New permissions: content scripts on all sites; the listing and the privacy policy say why.

### The iPhone

- A Passwords screen (list, item, editor, share, agent, settings) and setup with the recovery key, reached from the tab bar and Search like the other parts (#605).
- A new target `TeaNodeAutoFill` (an `ASCredentialProviderViewController`) with the AutoFill entitlement on the app and the extension, `ProvidesPasswords`, `ProvidesPasskeys`, `ProvidesOneTimeCodes`, `ShowsConfigurationUI`. Cards and notes are not offered by AutoFill; the Passwords screen shows and copies them.
- The app keeps an encrypted copy of the items in the app group container and the account key in the shared keychain group, readable only after Face ID (`.biometryCurrentSet`). The extension reads both, never the network, and never derives the passphrase key.
- The app updates `ASCredentialIdentityStore` (site, user name, passkey ids; no secrets) after each sync, so QuickType suggestions appear.
- Demo data and `shoot` lines for every screen and the AutoFill list.

### Tests

The vectors in three languages; storage and GraphQL with a database (a stale revision refused, a share ended by leaving the circle, a grantee without Passwords waiting, the agent key opening only granted items); the importer on invented `pass` and CSV files; the tool refusing ungranted items, other hosts and cards; the extension's host matching and relying party checks; TeaNodeKit on Linux.

## Plan of Work

Milestones as listed in Progress, in that order; each ends with its tests passing. Subagents (at most eight, Sonnet) build from this plan; every diff is read, built and tested here before it is committed. One pull request carries this plan and all of it.

## Validation and Acceptance

The walk in Purpose, done on a development server, plus: a database dump grepped for an invented site, user name and password finds none; the extension's suggestion does not fill when its frame is made transparent by the page; the agent's transcript for the sign-in contains no password; the iPhone screenshots in light, dark and largest text.

## Idempotence and Recovery

Saving sends the revision it was based on, so a retry or two devices racing cannot overwrite silently. Import skips an item whose kind, name, folder, first site, user name and a SHA-256 of its password (or note text) are all the same as one that exists, and names what it skipped; an item that differs in any of them is kept as a second item. A save that replaces an item keeps the version it replaced for 30 days (`password_item_version`, newest 20), so a bad overwrite can be put back from the database; there is no API to restore a version, since the keys sealed for recipients and the agent belong to the item key of one version. Erasing Passwords is the only recovery when both passphrase and recovery key are lost; it says so before it asks.

## Interfaces and Dependencies

Go: `crypto/ecdh`, `crypto/aes`, `crypto/cipher`, `golang.org/x/crypto/pbkdf2` and `hkdf` (already vendored). Dashboard and extension: WebCrypto only. iPhone: CryptoKit and CommonCrypto; TeaNodeKit on Linux: `swift-crypto` and `_CryptoExtras`. Apple: the AutoFill Credential Provider capability on `com.teanode.app` and a new App ID for the extension (`com.teanode.app.autofill`), both added through the App Store Connect API before the first macOS run; CI signs automatically with its one kept development certificate (`ios/Scripts/revoke-stale-certificates.py` is no longer needed), so the new target needs no change to the signing steps beyond what Xcode's automatic signing does, which the first run checks. The extension and browser work builds on #612 (the attached tab's protocol 4 and take over), after it merges. No new server dependency.
