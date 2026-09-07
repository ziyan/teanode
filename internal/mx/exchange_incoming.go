package mx

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func (self *exchange) handleIncoming(ctx context.Context, tx db.Transaction, envelope *mailparse.Envelope) ([]*models.Delivery, error) {
	// figure out the domain name and aliases
	_, recipientDomain := mailparse.SplitAddress(envelope.Recipients[0])
	recipientAliases := make([]string, 0, len(envelope.Recipients))
	for _, recipient := range envelope.Recipients {
		alias, domain := mailparse.SplitAddress(recipient)
		if domain != recipientDomain {
			return nil, mailparse.ErrMultipleRecipientDomains
		}
		recipientAliases = append(recipientAliases, alias)
	}

	// extract some important headers
	from, err := mailparse.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "From")))
	if err != nil {
		return nil, mailparse.ErrInvalidFromHeader
	}
	subject := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Subject"))
	messageId := mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(envelope.Headers, "Message-ID"))

	// find the domain in the configuration. A domain that is not configured is
	// not served here, which is what "mailbox unavailable" means. Whether its
	// DNS records are correct is reported in the dashboard rather than being a
	// condition of accepting mail; see the decision record on advisory
	// verification.
	domain, err := tx.GetDomainByName(recipientDomain)
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
		Kind:           models.MailKindIncoming,
	}

	// authenticate the mail using spf, dkim and arc
	if err := self.authenticateIncoming(ctx, envelope, mail, domain); err != nil {
		log.Warningf("failed to authenticate incoming mail %q, rejecting: %s", envelope.ID, err)

		// try to save mail before exiting
		if _, err := tx.CreateMail(mail, nil); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}

		// Keep what was refused. The row alone says a message was rejected
		// and why; the message itself is what answers whether the verdict
		// was right, which is the question somebody actually has when they
		// go looking. It arrived in full — the refusal happens after DATA —
		// so there is nothing to gain by throwing it away, and a false
		// positive becomes impossible to investigate if we do.
		self.store(ctx, mail)

		// track usages
		self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
			bytesReceived: envelope.Size,
			mailsRejected: 1,
		})

		// also track alias usages
		for _, recipientAlias := range recipientAliases {
			for _, alias := range self.matchingAliases(domain, recipientAlias) {
				self.trackAliasUsage(envelope.ReceivedAt, alias.ID, aliasUsage{
					bytesReceived: envelope.Size,
					mailsRejected: 1,
				})
			}
		}
		return nil, err
	}

	// save mail and commit
	if _, err := tx.CreateMail(mail, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// track usages
	self.trackDomainUsage(envelope.ReceivedAt, domain.ID, domainUsage{
		bytesReceived: envelope.Size,
		mailsAccepted: 1,
	})

	// resolve each alias
	var deliveries []*models.Delivery
	for _, recipientAlias := range recipientAliases {
		matchedDeliveries, err := self.matchAliases(domain, recipientAlias, mail)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, matchedDeliveries...)
	}

	// insert into database
	return tx.CreateDeliveries(deliveries, nil)
}

func (self *exchange) authenticateIncoming(ctx context.Context, envelope *mailparse.Envelope, mail *models.Mail, domain *models.Domain) error {
	start := time.Now()
	mail.Status = models.MailStatusRejected

	// prepare parallel checks
	authenticator := newAuthenticator(ctx)
	defer authenticator.stop()

	self.checkFromMx(authenticator, mail.From)
	self.checkSenderMx(authenticator, envelope)
	self.checkDmarcSpfDkim(authenticator, mail.From, envelope, mail.Headers, mail.Body)
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
		self.formatReceivedSpfHeader(results, envelope),
	}
	mail.Headers = mailparse.MergeHeaders(additionalHeaders, mail.Headers)

	// change status
	mail.Status = models.MailStatusAccepted

	log.Debugf("took %s to authenticate incoming mail %q", time.Since(start), envelope.ID)
	return nil
}
