// Package arc implements Authenticated Received Chain (ARC) validation and signing.
package arc

const (
	aarHeaderKey = "ARC-Authentication-Results"
	amsHeaderKey = "ARC-Message-Signature"
	asHeaderKey  = "ARC-Seal"
	arHeaderKey  = "Authentication-Results"
	crlf         = "\r\n"
)
