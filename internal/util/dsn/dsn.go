// Package dsn provides Delivery Status Notification (DSN) generation.
package dsn

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

var log = logging.MustGetLogger("dsn")

const crlf = "\r\n"

type Action string

const (
	ActionFailed    Action = "failed"
	ActionDelayed   Action = "delayed"
	ActionDelivered Action = "delivered"
	ActionRelayed   Action = "relayed"
	ActionExpanded  Action = "expanded"
)

type DeliveryStatus struct {
	// per message fields
	OriginalEnvelopeID string     `json:"originalEnvelopeId,omitempty"`
	ReportingMTA       string     `json:"reportingMta,omitempty"`
	DSNGateway         string     `json:"dsnGateway,omitempty"`
	ReceivedFromMTA    string     `json:"receivedFromMta,omitempty"`
	ArrivalDate        *time.Time `json:"arrivalDate,omitempty"`

	Extensions map[string]string `json:"extensions,omitempty"`

	// per recipient fields
	RecipientStatuses []RecipientStatus `json:"recipientStatuses,omitempty"`
}

type RecipientStatus struct {
	OriginalRecipient string     `json:"originalRecipient,omitempty"`
	FinalRecipient    string     `json:"finalRecipient,omitempty"`
	Action            Action     `json:"action,omitempty"`
	Status            string     `json:"status,omitempty"`
	RemoteMTA         string     `json:"remoteMta,omitempty"`
	DiagnosticCode    string     `json:"diagnosticCode,omitempty"`
	LastAttemptDate   *time.Time `json:"lastAttemptDate,omitempty"`
	FinalLogID        string     `json:"finalLogId,omitempty"`
	WillRetryUntil    *time.Time `json:"willRetryUntil,omitempty"`

	Extensions map[string]string `json:"extensions,omitempty"`
}

func Parse(reader io.Reader) (*DeliveryStatus, error) {
	headerGroups, err := parseHeaderGroups(reader)
	if err != nil {
		return nil, err
	}
	var deliveryStatus DeliveryStatus
	for _, headers := range headerGroups {
		recipientStatus := RecipientStatus{}
		hasRecipientStatus := false
		for _, header := range headers {
			key, value := mailparse.SplitHeader(header)
			if strings.EqualFold(key, "Original-Envelope-ID") {
				deliveryStatus.OriginalEnvelopeID = strings.TrimSpace(value)
			} else if strings.EqualFold(key, "Reporting-MTA") {
				deliveryStatus.ReportingMTA = parseName(value)
			} else if strings.EqualFold(key, "DSN-Gateway") {
				deliveryStatus.DSNGateway = parseName(value)
			} else if strings.EqualFold(key, "Received-From-MTA") {
				deliveryStatus.ReceivedFromMTA = parseName(value)
			} else if strings.EqualFold(key, "Arrival-Date") {
				deliveryStatus.ArrivalDate = parseDate(value)
			} else if strings.EqualFold(key, "Original-Recipient") {
				if originalRecipient := parseEmail(value); originalRecipient != "" {
					recipientStatus.OriginalRecipient = originalRecipient
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Final-Recipient") {
				if finalRecipient := parseEmail(value); finalRecipient != "" {
					recipientStatus.FinalRecipient = finalRecipient
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Action") {
				if action := parseAction(value); action != "" {
					recipientStatus.Action = action
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Status") {
				if status := parseStatus(value); status != "" {
					recipientStatus.Status = status
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Remote-MTA") {
				if remoteMta := parseName(value); remoteMta != "" {
					recipientStatus.RemoteMTA = remoteMta
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Diagnostic-Code") {
				if diagnosticCode := parseName(value); diagnosticCode != "" {
					recipientStatus.DiagnosticCode = diagnosticCode
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Last-Attempt-Date") {
				if lastAttemptDate := parseDate(value); lastAttemptDate != nil {
					recipientStatus.LastAttemptDate = lastAttemptDate
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Final-Log-ID") {
				if finalLogId := strings.TrimSpace(value); finalLogId != "" {
					recipientStatus.FinalLogID = finalLogId
					hasRecipientStatus = true
				}
			} else if strings.EqualFold(key, "Will-Retry-Until") {
				if willRetryUntil := parseDate(value); willRetryUntil != nil {
					recipientStatus.WillRetryUntil = willRetryUntil
					hasRecipientStatus = true
				}
			} else {
				if value := strings.TrimSpace(value); value != "" {
					if hasRecipientStatus {
						if recipientStatus.Extensions == nil {
							recipientStatus.Extensions = make(map[string]string)
						}
						recipientStatus.Extensions[textproto.CanonicalMIMEHeaderKey(key)] = value
					} else {
						if deliveryStatus.Extensions == nil {
							deliveryStatus.Extensions = make(map[string]string)
						}
						deliveryStatus.Extensions[textproto.CanonicalMIMEHeaderKey(key)] = value
					}
				}
			}
		}
		if hasRecipientStatus {
			deliveryStatus.RecipientStatuses = append(deliveryStatus.RecipientStatuses, recipientStatus)
		}
	}
	if len(deliveryStatus.RecipientStatuses) == 0 {
		return nil, fmt.Errorf("dsn: no recipient status found")
	}
	return &deliveryStatus, nil
}

func parseHeaderGroups(reader io.Reader) ([][]string, error) {
	bufferedReader := bufio.NewReader(reader)
	text := textproto.NewReader(bufferedReader)
	var headerGroups [][]string
	var headers []string
	for {
		l, err := text.ReadLine()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("dsn: failed to read header: %w", err)
		}
		if len(l) == 0 {
			if len(headers) > 0 {
				headerGroups = append(headerGroups, headers)
				headers = nil
			}
		} else if len(headers) > 0 && (l[0] == ' ' || l[0] == '\t') {
			// this is a continuation line
			headers[len(headers)-1] += l + crlf
		} else {
			headers = append(headers, l+crlf)
		}
	}
	if len(headers) > 0 {
		headerGroups = append(headerGroups, headers)
	}
	return headerGroups, nil
}

func parseDate(value string) *time.Time {
	if t, err := dateparse.ParseAny(value); err == nil {
		return &t
	}
	return nil
}

func parseAction(value string) Action {
	switch value {
	case "failed", "delayed", "delivered", "relayed", "expanded":
		return Action(value)
	}
	return Action("")
}

func parseEmail(value string) string {
	parts := strings.SplitN(value, ";", 2)
	if len(parts) != 2 {
		log.Warningf("unable to parse email field %q", value)
		return ""
	}
	if strings.ToLower(strings.TrimSpace(parts[0])) != "rfc822" {
		log.Warningf("unable to parse email field %q", value)
		return ""
	}
	address, err := mailparse.ParseAddress(strings.TrimSpace(parts[1]))
	if err != nil {
		log.Warningf("unable to parse email field %q: %s", value, err)
		return ""
	}
	return address
}

func parseName(value string) string {
	parts := strings.SplitN(value, ";", 2)
	if len(parts) != 2 {
		log.Warningf("unable to parse field %q", value)
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func parseStatus(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
