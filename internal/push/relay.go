package push

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Relay is what a relay does, whether asked over HTTP by another server
// (internal/api/v1api/apipush) or by its own server, which sends to itself
// without the network. It reaches iPhones through APNs, Android phones
// through Firebase Cloud Messaging, or both; a platform it has no client for
// is refused.
type Relay struct {
	sealer *Sealer
	apns   *APNs
	fcm    *FCM
}

// NewRelay builds a relay around its APNs client, its FCM client, or both;
// the one it does not have is nil. Registrations are sealed with a key made
// from the APNs client's key when there is one, so that a relay that gains
// Android keeps every registration the iPhones hold, and from the FCM
// client's otherwise.
func NewRelay(apns *APNs, fcm *FCM) (*Relay, error) {
	var sealer *Sealer
	var err error
	switch {
	case apns != nil:
		sealer, err = NewSealer(apns.keyID, apns.key)
	case fcm != nil:
		sealer, err = NewSealerFromSecret(fcm.keyID(), fcm.secret())
	default:
		return nil, fmt.Errorf("push: a relay needs an APNs key, a Firebase service account, or both")
	}
	if err != nil {
		return nil, err
	}
	return &Relay{sealer: sealer, apns: apns, fcm: fcm}, nil
}

// deviceTokenPattern is an APNs device token as an app reports it: hex,
// 32 bytes today, possibly longer later.
var deviceTokenPattern = regexp.MustCompile(`^[0-9a-f]{64,200}$`)

// registrationTokenPattern is an FCM registration token as an app reports
// it: a long string of letters, digits, hyphens, underscores and a colon.
// Firebase does not publish its length; they are about 160 characters today.
var registrationTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_:-]{50,512}$`)

// Register seals a phone's device token, one Live Activity's push token, or
// the token PushKit gives the app for incoming calls, into the registration
// it hands its server. An Android phone gives its FCM registration token with
// platform android, for its ordinary notifications (alert) and, as the same
// token, to be rung (voip); an empty platform is an iPhone.
//
// A Live Activity's update is shown by iOS as it arrives, with no extension
// in between to decrypt it, so it crosses Apple and the relay readable. It
// therefore carries no words at all: the relay writes it from one of a few
// states (activityStates), and the app's widget says what each means. A VoIP
// push is handed to the app, which decrypts it with its own key like any
// other, so it carries the sealed content and nothing else. An Android
// phone's messages are all that way: data only, sealed.
func (self *Relay) Register(deviceToken string, isSandbox bool, pushType, platform string, now time.Time) (string, error) {
	if platform == "" {
		platform = PlatformIOS
	}
	if pushType == "" {
		pushType = PushTypeAlert
	}
	if !isRelayedPushType(pushType) {
		return "", fmt.Errorf("%w: %q pushes are not relayed", ErrRefused, pushType)
	}
	registration := &Registration{PushType: pushType, CreatedAt: now.UTC()}
	deviceToken = strings.TrimSpace(deviceToken)
	switch platform {
	case PlatformIOS:
		if self.apns == nil {
			return "", fmt.Errorf("%w: this relay does not serve iPhones", ErrRefused)
		}
		deviceToken = strings.ToLower(deviceToken)
		if !deviceTokenPattern.MatchString(deviceToken) {
			return "", fmt.Errorf("%w: that is not a device token", ErrRefused)
		}
		registration.IsSandbox = isSandbox
	case PlatformAndroid:
		if self.fcm == nil {
			return "", fmt.Errorf("%w: this relay does not serve Android phones", ErrRefused)
		}
		if pushType == PushTypeLiveActivity {
			return "", fmt.Errorf("%w: Live Activities are the iPhone's", ErrRefused)
		}
		if !registrationTokenPattern.MatchString(deviceToken) {
			return "", fmt.Errorf("%w: that is not a registration token", ErrRefused)
		}
		// Only the phones registered before Android was served have no
		// platform, so Android is always written out.
		registration.Platform = PlatformAndroid
	default:
		return "", fmt.Errorf("%w: %q is not a platform; the platforms are %s and %s", ErrRefused, platform, PlatformIOS, PlatformAndroid)
	}
	registration.DeviceToken = deviceToken
	return self.sealer.Seal(registration)
}

// isRelayedPushType says whether the relay sends this kind of push.
func isRelayedPushType(pushType string) bool {
	return pushType == PushTypeAlert || pushType == PushTypeLiveActivity || pushType == PushTypeVoIP
}

// Open reads the registration a server presents.
func (self *Relay) Open(pushRegistration string, now time.Time) (*Registration, error) {
	registration, err := self.sealer.Open(pushRegistration, now)
	if err != nil {
		return nil, err
	}
	if !isRelayedPushType(registration.PushType) {
		return nil, fmt.Errorf("%w: that registration is for %s pushes, which are not relayed", ErrRefused, registration.PushType)
	}
	switch registration.ResolvedPlatform() {
	case PlatformIOS:
		if self.apns == nil {
			return nil, fmt.Errorf("%w: this relay does not serve iPhones", ErrRefused)
		}
	case PlatformAndroid:
		if self.fcm == nil {
			return nil, fmt.Errorf("%w: this relay does not serve Android phones", ErrRefused)
		}
	default:
		return nil, fmt.Errorf("%w: that registration is for a platform the relay does not know", ErrRefused)
	}
	return registration, nil
}

// DeliverTo sends a delivery to an opened registration's phone: an alert to
// the phone, a state to one Live Activity, or a ring to the phone's VoIP
// token; on Android a data message for the app to draw, high priority and of
// the call kind when it rings.
func (self *Relay) DeliverTo(ctx context.Context, registration *Registration, delivery *Delivery, now time.Time) error {
	if registration.ResolvedPlatform() == PlatformAndroid {
		message, err := delivery.fcmMessage(registration.PushType, now)
		if err != nil {
			return err
		}
		return self.fcm.Send(ctx, registration.DeviceToken, message)
	}
	var notification *Notification
	var err error
	switch registration.PushType {
	case PushTypeLiveActivity:
		notification, err = delivery.activityNotification(now)
	case PushTypeVoIP:
		notification, err = delivery.voipNotification(now)
	default:
		notification, err = delivery.notification(now)
	}
	if err != nil {
		return err
	}
	return self.apns.Send(ctx, registration.DeviceToken, registration.IsSandbox, notification)
}

// Sender is how a server sends a notification for one of its phones: to the
// relay over HTTP, or to itself when it is the relay.
type Sender interface {
	Send(ctx context.Context, pushRegistration string, delivery *Delivery) error
}

// Send lets a server that is the relay send to itself.
func (self *Relay) Send(ctx context.Context, pushRegistration string, delivery *Delivery) error {
	now := time.Now()
	registration, err := self.Open(pushRegistration, now)
	if err != nil {
		return err
	}
	return self.DeliverTo(ctx, registration, delivery, now)
}
