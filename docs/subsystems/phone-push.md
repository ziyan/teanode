# Phones and push notifications

How a notification reaches the TeaNode app on a person's phone, iPhone or
Android, without the relay in the middle being able to read it.

`internal/push/`, `internal/api/v1api/apipush/`, `internal/agent/phone_notifier.go`,
`internal/api/v1api/apigraph/agent_phone.go`, `internal/models/phone.go`,
`internal/db/database_phone.go`.

## The parts

A phone is a row of `agent_phone`, one per install, keyed on the OAuth client
the app connected as. The app registers itself after it connects
(`RegisterAgentPhone`) and again each time it starts. The row holds the phone's
name and model, its **platform** (`phonePlatform`: `ios` or `android`, `ios`
when the app says nothing, which is every app from before Android), the phone's
sealed registrations, and its payload key.

Neither Apple nor Google delivers to an app except from a credential of the
account that publishes it, which no TeaNode server but the publisher's holds.
So the phone asks a **relay** (a TeaNode server with `agent.push.relay` on) to
register its push token, and gets a *sealed registration* back: the token, its
platform and push type, and the time, encrypted under a key only the relay has.
The phone hands that to its own server. The relay keeps nothing. To notify the
phone the server sends the sealed registration with a *delivery* to its relay
(`agent.push.relayUrl`, or itself when it is the relay), which opens it and
passes the delivery to APNs or to Firebase Cloud Messaging.

The delivery is already encrypted. The phone makes a 256-bit key when it
registers and gives it to its server, which seals it with the server secret.
The server encrypts each notification's content with it (AES-256-GCM,
`push.EncryptContent`); the relay, Apple and Google see only the ciphertext,
a hashed thread identifier and a hashed collapse identifier.

## Platforms

`push.RegisterRequest.platform` is `ios` (the default) or `android`. The
sealed registration carries it, so the relay never has to be told again, and a
registration sealed before Android was served has none and is an iPhone's.

| | iPhone | Android |
| --- | --- | --- |
| Token the phone gives | APNs device token (hex) | FCM registration token |
| Delivered by | APNs, HTTP/2, ES256 key | FCM HTTP v1, RS256 service account |
| Ordinary notification | `alert` registration: an `aps` alert the notification service extension rewrites | `alert` registration: a data-only message `{sealed, pushKind: "notification", threadId}`, high priority, collapse key from the card |
| Ring for a call | `voip` registration, a PushKit push | `voip` registration, the same FCM token: a data-only message `{sealed, pushKind: "call"}`, high priority, no collapse key |
| Live Activities | `liveactivity` registrations | refused |

An Android message has no `notification` block. A message with one is drawn by
the system with the relay's words, which would have to be readable; a data
message wakes the app, which decrypts `sealed` and draws the notification
itself, or for `pushKind: "call"` shows an incoming call with a full-screen
intent. Lifetimes are the iPhone's: an hour for a notification (a server may
ask for less, never more than a day), sixty seconds for a ring (never more than
two minutes), as the FCM `ttl`.

The phone's own registrations from one FCM token are two sealed registrations,
`alert` and `voip`, sent as `pushRegistration` and `voipPushRegistration`. So
`AgentPhone.CanRing` and the ring-then-alert fallback of
`PhoneNotifier.RingPhones` work unchanged for Android.

## What Firebase answers

`push.FCM` maps the HTTP v1 API's refusals to the errors a caller acts on:

- `UNREGISTERED`, `SENDER_ID_MISMATCH`, and `INVALID_ARGUMENT` whose message
  names the registration token: `ErrGone`. The relay answers `gone`, the
  sending server clears that registration, and the app registers again the next
  time it starts.
- Any other `INVALID_ARGUMENT`, a quota or other refusal a retry will not change:
  `ErrRefused`.
- `UNAUTHENTICATED`, `THIRD_PARTY_AUTH_ERROR`, 429 and 5xx: a plain error, so
  the relay answers `unreached` and the server tries again later. An
  authentication refusal also drops the cached access token.

The access token is traded for with a signed assertion (RS256, the service
account's `client_email`, the `firebase.messaging` scope), cached, and renewed
five minutes before it expires. Only the standard library signs it.

## Sealing key

A relay with an APNs key seals registrations under a key made from it, as it
always did, so adding Android to a relay leaves every iPhone's registration
valid. A relay with only a Firebase service account seals under the service
account's private key, with its `private_key_id` written in front. Replacing
that credential makes the relay answer old registrations as gone, and each
phone registers again when its app next starts.

## Configuration

`agent.push.relay.fcmServiceAccount` is the service account's JSON file, a
secret (redacted everywhere the APNs key is). The relay needs the APNs key, the
service account, or both; it answers `refused` to a registration for a platform
it has no credential for. See `docs/reference/deployment.md` for how an
operator sets it up.

## The app updates

`GetAppUpdate(updateChannel)` tells the app a newer build exists
(`internal/appupdate`). The channels are `testflight` and `appstore` for the
iPhone app and `android` for the Android app. The server reads, at most every
thirty minutes, a file the distribution script publishes as a release asset of
`ziyan/teanode`, `android-update-channel/android.json`:

    {"version": "1.4.0", "build": "42", "installUrl": "https://..."}

`build` may be a string or a number. `installUrl` must be https on Firebase's
tester distribution (`appdistribution.firebase.google.com` or
`appdistribution.firebase.dev`) or Google Play; an Android feed that leads
anywhere else is refused, as is one that leads an iPhone to Google. The same
`upgrade.enabled` switch that stops the server looking for its own releases
stops this.
