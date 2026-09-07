package mx

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/dsn"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func (self *exchange) handleDsn(ctx context.Context, tx db.Transaction, envelope *mailparse.Envelope) ([]*models.Delivery, error) {
	// extract some important headers
	from, _ := mailparse.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "From")))
	subject := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Subject"))
	messageId := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Message-ID"))

	// parse dsn
	deliveryStatus, err := self.parseDeliveryStatus(envelope.Headers, envelope.Body)
	if err != nil {
		return nil, err
	}
	if deliveryStatus != nil && len(deliveryStatus.RecipientStatuses) > 1 {
		log.Warningf("mx: there are %d recipient statuses in the dsn, only considering the first one, and ignoring the rest", len(deliveryStatus.RecipientStatuses))
	}

	// look up the delivery that bounced and update
	originalDelivery, err := tx.GetDelivery(envelope.SpecialID, nil)
	if err != nil {
		return nil, err
	}
	if originalDelivery == nil {
		return nil, mailparse.ErrMailBoxUnavailable
	}

	// look up the original mail
	originalMail, err := tx.GetMail(originalDelivery.MailID, nil)
	if err != nil {
		return nil, err
	}
	if originalMail == nil {
		return nil, mailparse.ErrMailBoxUnavailable
	}

	// look up the domain the bounced mail was for
	domain, err := tx.GetDomain(originalMail.DomainID)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, mailparse.ErrMailBoxUnavailable
	}

	// add Received header
	receivedHeader := self.formatReceivedHeader(envelope)

	// combine the headers
	headers := mailparse.MergeHeaders([]string{
		receivedHeader,
	}, envelope.Headers)

	// prepare mail
	mail := &models.Mail{
		DomainID:       domain.ID,
		Domain:         domain,
		DeliveryID:     originalDelivery.ID,
		Delivery:       originalDelivery,
		EnvelopeID:     envelope.ID,
		Hello:          envelope.Hello,
		IP:             envelope.IP.String(),
		RDNS:           envelope.RDNS,
		TLSVersion:     getTlsVersion(envelope.TLS),
		TLSCipherSuite: getTlsCipherSuite(envelope.TLS),
		Location:       envelope.Location,
		Sender:         envelope.Sender,
		Recipients:     envelope.Recipients,
		MessageID:      messageId,
		From:           from,
		Subject:        subject,
		Headers:        headers,
		Body:           envelope.Body,
		Size:           envelope.Size,
		ReceivedAt:     envelope.ReceivedAt,
		Kind:           models.MailKindDSN,
	}

	// authenticate the mail using spf, dkim and arc
	if err := self.authenticateDsn(ctx, envelope, mail, domain); err != nil {
		log.Warningf("failed to authenticate special mail %q, rejecting: %s", envelope.ID, err)

		// try to save mail before exiting
		if _, err := tx.CreateMail(mail, nil); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}

		// track usages
		self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
			bytesReceived: envelope.Size,
			mailsRejected: 1,
		})

		return nil, err
	}

	// find original mail that bounced and update its status
	if _, err := tx.ModifyDelivery(originalDelivery.ID, func(delivery *models.Delivery) error {
		if delivery.CreatedAt.IsZero() {
			return mailparse.ErrMailBoxUnavailable
		}
		delivery.Status = models.DeliveryStatusDelivered
		delivery.Error = ""
		delivery.NotifiedAt = &envelope.ReceivedAt
		if deliveryStatus != nil {
			switch deliveryStatus.RecipientStatuses[0].Action {
			case dsn.ActionFailed:
				delivery.Status = models.DeliveryStatusFailed
				delivery.Error = deliveryStatus.RecipientStatuses[0].DiagnosticCode
			case dsn.ActionDelayed:
				delivery.Status = models.DeliveryStatusDelayed
				delivery.Error = deliveryStatus.RecipientStatuses[0].DiagnosticCode
			}
			delivery.DeliveryStatuses = append(delivery.DeliveryStatuses, deliveryStatus)
		}
		return nil
	}, nil); err != nil {
		return nil, err
	}

	// add delivered to header because we own the dsn mailbox internally
	// so additional forward will not create loop
	mail.Headers = mailparse.MergeHeaders([]string{
		mailparse.UnsplitHeader("Delivered-To", envelope.Recipients[0]),
		mailparse.UnsplitHeader("Return-Path", fmt.Sprintf("<%s>", envelope.Sender)),
	}, mail.Headers)

	// save mail and commit
	if _, err := tx.CreateMail(mail, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// track usages
	self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
		bytesReceived:     envelope.Size,
		deliveriesBounced: 1,
	})
	if originalMail.CredentialID != "" {
		self.trackCredentialUsage(envelope.ReceivedAt, originalMail.CredentialID, credentialUsage{
			bytesReceived:     envelope.Size,
			deliveriesBounced: 1,
		})
	}
	if originalDelivery.AliasID != "" {
		self.trackAliasUsage(envelope.ReceivedAt, originalDelivery.AliasID, aliasUsage{
			bytesReceived:     envelope.Size,
			deliveriesBounced: 1,
		})
	}

	// forward boucned mail
	var deliveries []*models.Delivery
	switch originalMail.Kind {
	case models.MailKindIncoming:
		// protect forwarding address privacy, do not forward bounce for incoming
	case models.MailKindOutgoing:
		// track usage
		self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
			bytesReceived: envelope.Size,
			mailsAccepted: 1,
		})

		// forward the dns mail
		recipientAlias, _ := mailparse.SplitAddress(originalMail.Sender)
		matchedDeliveries, err := self.matchAliases(tx, domain, recipientAlias, mail)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, matchedDeliveries...)
	}
	return tx.CreateDeliveries(deliveries, nil)
}

func (self *exchange) authenticateDsn(ctx context.Context, envelope *mailparse.Envelope, mail *models.Mail, domain *models.Domain) error {
	start := time.Now()
	mail.Status = models.MailStatusRejected

	// prepare parallel checks
	authenticator := newAuthenticator(ctx)
	defer authenticator.stop()

	if envelope.Sender != "" {
		self.checkSenderMx(authenticator, envelope)
	}
	self.checkArc(authenticator, mail.Headers, mail.Body)
	self.checkVirus(authenticator, mail.Headers, mail.Body)
	self.checkContent(authenticator, mail.Headers, mail.Body)

	// wait for all
	authenticationResults, results, err := authenticator.wait()
	mail.AuthenticationResults = authenticationResults
	if err != nil {
		return err
	}

	// then score, which reads what those checks established
	if err := self.checkSpam(ctx, envelope, mail, domain.SpamThreshold()); err != nil {
		return err
	}

	// add headers
	additionalHeaders := []string{
		self.formatAuthenticationResultsHeader(envelope, results),
	}
	mail.Headers = mailparse.MergeHeaders(additionalHeaders, mail.Headers)

	// change status
	mail.Status = models.MailStatusAccepted

	log.Debugf("took %s to authenticate dsn mail %q", time.Since(start), envelope.ID)
	return nil
}

func (self *exchange) parseDeliveryStatus(headers []string, body []byte) (*dsn.DeliveryStatus, error) {
	var deliveryStatus *dsn.DeliveryStatus
	if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			return mailparse.ErrInvalidContentType
		}
		switch mediaType {
		case "message/delivery-status":
			deliveryStatus, err = dsn.Parse(reader)
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return deliveryStatus, nil
}
