package mx

import (
	"errors"
	"fmt"
	"net/textproto"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A 5xx reply is permanent and the delivery is dropped; a 4xx, or a failure
// with no reply at all, stays on the retry schedule. The message Gmail had
// refused as unsolicited was retried four times before this distinction
// existed.
func TestAPermanentRefusalIsNotRetried(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status models.DeliveryStatus
	}{
		{"550 from the receiver", &textproto.Error{Code: 550, Msg: "5.7.1 Gmail has detected that this message is likely unsolicited mail"}, models.DeliveryStatusDropped},
		{"wrapped 554", fmt.Errorf("mx: %w", &textproto.Error{Code: 554, Msg: "5.7.1 rejected"}), models.DeliveryStatusDropped},
		{"451 try later", &textproto.Error{Code: 451, Msg: "4.7.1 greylisted"}, models.DeliveryStatusAttempted},
		{"no reply at all", errors.New("dial tcp: connection refused"), models.DeliveryStatusAttempted},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			delivery := &models.Delivery{Status: models.DeliveryStatusAttempted}
			recordFailure(delivery, testCase.err)
			if delivery.Status != testCase.status {
				t.Errorf("status = %q, want %q", delivery.Status, testCase.status)
			}
			if delivery.Error == "" {
				t.Errorf("the error was not recorded")
			}
		})
	}
}
