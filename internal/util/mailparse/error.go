package mailparse

type Error struct {
	statusCode          int
	enhancedStatusCodes string
	message             string
}

func (self *Error) Error() string {
	return self.message
}

func (self *Error) StatusCode() int {
	return self.statusCode
}

func (self *Error) EnhancedStatusCodes() string {
	return self.enhancedStatusCodes
}

func (self *Error) Message() string {
	return self.message
}

func newError(statusCode int, enhancedStatusCodes string, message string) error {
	return &Error{
		statusCode:          statusCode,
		enhancedStatusCodes: enhancedStatusCodes,
		message:             message,
	}
}

var (
	ErrMultipleRecipientDomains = newError(451, "4.1.2", "Please RCPT to the same domain")
	ErrInvalidCredentials       = newError(454, "4.7.0", "Invalid credentials")

	ErrMailBoxUnavailable  = newError(550, "5.1.9", "Mailbox unavailable")
	ErrMailBoxNotActivated = newError(550, "5.1.9", "Mailbox unavailable, service for domain not yet activated")

	ErrDKIMVerificationFailed  = newError(550, "5.7.20", "DKIM verification failed")
	ErrSPFValidationFailed     = newError(550, "5.7.23", "SPF validation failed")
	ErrSPFValidationError      = newError(550, "5.7.24", "SPF validation error")
	ErrInvalidFromHeader       = newError(550, "5.7.26", "Invalid From header")
	ErrInvalidContentType      = newError(550, "5.7.26", "Invalid content type")
	ErrInvalidTransferEncoding = newError(550, "5.7.26", "Invalid transfer encoding")
	ErrDMARCAlignmentFailed    = newError(550, "5.7.26", "DMARC alignment failed")
	ErrVirusDetected           = newError(550, "5.7.26", "Virus detected")
	ErrSpamCheckFailed         = newError(550, "5.7.26", "Spam check failed")
	ErrProhibitedFileExtension = newError(550, "5.7.26", "Prohibited file extension in attachments")
	ErrInvalidFromMX           = newError(550, "5.7.27", "From address has null MX")
	ErrInvalidSenderMX         = newError(550, "5.7.27", "Sender address has null MX")
	ErrARCValidationFailed     = newError(550, "5.7.29", "ARC validation failed")
)
