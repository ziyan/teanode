// Package dkim implements DKIM (DomainKeys Identified Mail) signing and verification.
package dkim

import "github.com/op/go-logging"

var log = logging.MustGetLogger("dkim") //nolint:unused

const (
	dkimSignatureHeaderKey = "DKIM-Signature"
	crlf                   = "\r\n"
)
