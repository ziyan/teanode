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
// without the network.
type Relay struct {
	sealer *Sealer
	apns   *APNs
}

// NewRelay builds a relay around its APNs client, sealing registrations with
// a key made from the client's.
func NewRelay(apns *APNs) (*Relay, error) {
	sealer, err := NewSealer(apns.keyID, apns.key)
	if err != nil {
		return nil, err
	}
	return &Relay{sealer: sealer, apns: apns}, nil
}

// deviceTokenPattern is an APNs device token as an app reports it: hex,
// 32 bytes today, possibly longer later.
var deviceTokenPattern = regexp.MustCompile(`^[0-9a-f]{64,200}$`)

// Register seals a phone's device token, or one Live Activity's push token,
// into the registration it hands its server.
//
// A Live Activity's update is shown by iOS as it arrives, with no extension
// in between to decrypt it, so it crosses Apple and the relay readable. It
// therefore carries no words at all: the relay writes it from one of a few
// states (activityStates), and the app's widget says what each means.
func (self *Relay) Register(deviceToken string, isSandbox bool, pushType string, now time.Time) (string, error) {
	deviceToken = strings.ToLower(strings.TrimSpace(deviceToken))
	if !deviceTokenPattern.MatchString(deviceToken) {
		return "", fmt.Errorf("%w: that is not a device token", ErrRefused)
	}
	if pushType == "" {
		pushType = PushTypeAlert
	}
	if pushType != PushTypeAlert && pushType != PushTypeLiveActivity {
		return "", fmt.Errorf("%w: %q pushes are not relayed", ErrRefused, pushType)
	}
	return self.sealer.Seal(&Registration{
		DeviceToken: deviceToken, IsSandbox: isSandbox, PushType: pushType, CreatedAt: now.UTC(),
	})
}

// Open reads the registration a server presents.
func (self *Relay) Open(pushRegistration string, now time.Time) (*Registration, error) {
	registration, err := self.sealer.Open(pushRegistration, now)
	if err != nil {
		return nil, err
	}
	if registration.PushType != PushTypeAlert && registration.PushType != PushTypeLiveActivity {
		return nil, fmt.Errorf("%w: that registration is for %s pushes, which are not relayed", ErrRefused, registration.PushType)
	}
	return registration, nil
}

// DeliverTo sends a delivery to an opened registration's phone: an alert to
// the phone, or a state to one Live Activity.
func (self *Relay) DeliverTo(ctx context.Context, registration *Registration, delivery *Delivery, now time.Time) error {
	var notification *Notification
	var err error
	if registration.PushType == PushTypeLiveActivity {
		notification, err = delivery.activityNotification(now)
	} else {
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
