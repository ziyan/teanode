// Package arc implements Authenticated Received Chain (ARC) validation and signing.
package arc

import "github.com/op/go-logging"

var log = logging.MustGetLogger("arc") //nolint:unused

const (
	aarHeaderKey = "ARC-Authentication-Results"
	amsHeaderKey = "ARC-Message-Signature"
	asHeaderKey  = "ARC-Seal"
	arHeaderKey  = "Authentication-Results"
	crlf         = "\r\n"
)
