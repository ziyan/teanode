package mx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/arc"
	"github.com/ziyan/teanode/internal/util/bufferpool"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/smtpc"
)

var deliveryBackoffs = []time.Duration{
	5 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	2 * time.Hour,
	6 * time.Hour,
	24 * time.Hour,
	48 * time.Hour,
}

// kick off generated deliveries
func (self *exchange) goDeliver(ctx context.Context, deliveries []*models.Delivery) {
	if len(deliveries) == 0 {
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()

		// wait for all to finish
		var waitGroup sync.WaitGroup
		defer waitGroup.Wait()

		// delivery all in parallel
		for _, delivery := range deliveries {
			waitGroup.Add(1)
			go func(delivery *models.Delivery) {
				defer deferutil.Recover()
				defer waitGroup.Done()
				// The message is already stored, so a failure here needs no
				// rescue: the retry loop reloads it from storage.
				_ = self.deliver(self.ctx, delivery)
			}(delivery)
		}
	}()
}

// try to deliver one mail
func (self *exchange) deliver(ctx context.Context, delivery *models.Delivery) error {
	// attempt delivery and update status
	delivery.Status = models.DeliveryStatusAttempted
	delivery.Error = ""
	delivery.RetryAt = nil
	defer func() {
		now := time.Now().In(time.Local)

		// track usages
		if delivery.Mail != nil {
			switch delivery.Status {
			case models.DeliveryStatusDelivered:
				self.trackDomainUsage(now, delivery.Mail.DomainID, domainUsage{
					bytesSent:           delivery.Size,
					deliveriesSucceeded: 1,
				})
				if delivery.Mail.CredentialID != "" {
					self.trackCredentialUsage(now, delivery.Mail.CredentialID, credentialUsage{
						bytesSent:           delivery.Size,
						deliveriesSucceeded: 1,
					})
				}
				if delivery.AliasID != "" {
					self.trackAliasUsage(now, delivery.AliasID, aliasUsage{
						bytesSent:           delivery.Size,
						deliveriesSucceeded: 1,
					})
				}
			default:
				self.trackDomainUsage(now, delivery.Mail.DomainID, domainUsage{
					bytesSent:        delivery.Size,
					deliveriesFailed: 1,
				})
				if delivery.Mail.CredentialID != "" {
					self.trackCredentialUsage(now, delivery.Mail.CredentialID, credentialUsage{
						bytesSent:        delivery.Size,
						deliveriesFailed: 1,
					})
				}
				if delivery.AliasID != "" {
					self.trackAliasUsage(now, delivery.AliasID, aliasUsage{
						bytesSent:        delivery.Size,
						deliveriesFailed: 1,
					})
				}
			}
		}

		// retry logic
		if delivery.Status == models.DeliveryStatusAttempted {
			if delivery.Attempts < uint64(len(deliveryBackoffs)) {
				retryAt := now.Add(deliveryBackoffs[delivery.Attempts])
				delivery.RetryAt = &retryAt
			} else {
				delivery.Status = models.DeliveryStatusDropped
			}
		}
		delivery.AttemptedAt = &now
		delivery.Attempts++

		// update database
		if err := self.database.Transaction(func(tx db.Transaction) error {
			_, err := tx.ModifyDelivery(delivery.ID, func(existingDelivery *models.Delivery) error {
				if existingDelivery.CreatedAt.IsZero() {
					return fmt.Errorf("mx: delivery already deleted")
				}
				existingDelivery.Status = delivery.Status
				existingDelivery.Size = delivery.Size
				existingDelivery.Error = delivery.Error
				existingDelivery.Attempts = delivery.Attempts
				existingDelivery.AttemptedAt = delivery.AttemptedAt
				existingDelivery.RetryAt = delivery.RetryAt
				switch delivery.Status {
				case models.DeliveryStatusDelivered:
					existingDelivery.DeliveredAt = &now
				case models.DeliveryStatusDropped:
					existingDelivery.DroppedAt = &now
				}
				return nil
			}, nil)
			return err
		}); err != nil {
			log.Warningf("failed to update delivery %q status to %q in database: %s", delivery.ID, delivery.Status, err)
		}
	}()

	// check data
	if delivery.Mail == nil {
		err := fmt.Errorf("mx: mail missing for delivery %q", delivery.ID)
		delivery.Error = err.Error()
		delivery.Status = models.DeliveryStatusDropped
		return err
	}
	if len(delivery.Mail.Headers) == 0 {
		err := fmt.Errorf("mx: headers missing for delivery %q", delivery.ID)
		delivery.Error = err.Error()
		delivery.Status = models.DeliveryStatusDropped
		return err
	}
	// The domain may have been removed from the configuration since the mail
	// arrived, in which case there is nowhere to send a bounce back to and the
	// delivery cannot be completed.
	if delivery.Mail.Domain == nil {
		err := fmt.Errorf("mx: delivery %q is for a domain that is no longer configured", delivery.ID)
		delivery.Error = err.Error()
		delivery.Status = models.DeliveryStatusDropped
		return err
	}

	// sign dsn address
	sender, err := mailparse.SignAddress("dsn", delivery.ID, delivery.Mail.Domain.Hostname(), self.settings.Secret)
	if err != nil {
		log.Errorf("failed to sign dsn address: %s", err)
		delivery.Error = err.Error()
		delivery.Status = models.DeliveryStatusDropped
		return err
	}

	// add headers
	var deliveryHeaders []string
	if delivery.Kind == models.DeliveryKindForward && delivery.Alias != nil && delivery.Alias.Kind != models.AliasKindMailServer {
		deliveryHeaders = []string{
			mailparse.UnsplitHeader("Delivered-To", delivery.Recipient),
			mailparse.UnsplitHeader("Return-Path", fmt.Sprintf("<%s>", delivery.Mail.Sender)),
		}
	}
	// No X-Forwarding-Service. It named this software and its version on
	// every message that left, which told a recipient what to look up if they
	// wanted a version to attack, and told them nothing they had any use for.
	// It was also wrong half the time: it went on submitted mail too, which is
	// not forwarded by anybody. The Received header already says which host
	// handled the message, which is the part a reader tracing delivery needs.
	var feedbackHeaders []string

	// Feedback-ID, on the mail this server was asked to send and on nothing
	// else.
	//
	// The header lets a receiver group a sender's spam complaints, and the
	// last of its colon-separated fields is the identifier that grouping is
	// by: it has to mean the same sender every time, or there is nothing to
	// group. The domain is that identifier here — stable, already visible in
	// the From header and in the signature, and the identity a receiver's
	// tooling is registered against. Fields can be prefixed later if somebody
	// wants complaints broken down further; the last one must stay the
	// domain.
	//
	// Only on submitted mail. A message being forwarded belongs to whoever
	// wrote it: it is their reputation the complaints attach to, they may
	// have set this header themselves, and one added here would go in front
	// of theirs and take the grouping with it. That is what this used to do,
	// to every forwarded message, with a value that identified nothing.
	//
	// And never over one already present, whoever set it.
	if delivery.Kind == models.DeliveryKindExternal && delivery.Mail.Domain.Domain != "" {
		if mailparse.FindHeaderValue(delivery.Mail.Headers, "Feedback-ID") == "" {
			feedbackHeaders = append(feedbackHeaders,
				mailparse.UnsplitHeader("Feedback-ID", delivery.Mail.Domain.Domain))
		}
	}

	// The ARC chain is sealed as the domain the message arrived for, using
	// that domain's key. A domain with no key cannot seal, and forwarding it
	// unsealed is better than not forwarding it: the receiver simply has less
	// to go on when the original SPF breaks, which is what ARC exists to
	// repair.
	sealSigner, sealSelector, canSeal := self.signerFor(delivery.Mail.Domain)
	sealDomain := signingDomain(delivery.Mail.Domain)

	// add ARC set
	var arcHeaders []string
	if canSeal {
		arcHeaders, err = arc.Seal(mailparse.MergeHeaders(feedbackHeaders, deliveryHeaders, delivery.Mail.Headers), delivery.Mail.Body, &arc.SealOptions{
			Domain:   sealDomain,
			Selector: sealSelector,
			Signer:   sealSigner,
		})
		if err != nil {
			log.Errorf("failed to seal message: %s", err)
			delivery.Error = err.Error()
			delivery.Status = models.DeliveryStatusDropped
			return err
		}
	} else {
		log.Warningf("domain %q has no signing key, so the forwarded message carries no ARC chain", delivery.Mail.Domain.Domain)
	}

	// add a timeout
	ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	switch delivery.Kind {
	case models.DeliveryKindExternal:
		// send mail externally
		size, err := self.sendMail(ctxWithTimeout, sender, delivery.Recipient, mailparse.MergeHeaders(arcHeaders, feedbackHeaders, deliveryHeaders, delivery.Mail.Headers), delivery.Mail.Body)
		if err != nil {
			recordFailure(delivery, err)
			return err
		}
		delivery.Size = size
		delivery.Status = models.DeliveryStatusDelivered
	case models.DeliveryKindForward:
		// forward mail
		if delivery.Alias == nil {
			err := fmt.Errorf("mx: alias missing for delivery %q", delivery.ID)
			delivery.Error = err.Error()
			delivery.Status = models.DeliveryStatusDropped
			return err
		}
		switch delivery.Alias.Kind {
		case models.AliasKindEmail:
			if delivery.Alias.Email != "" {
				// send email
				size, err := self.sendMail(ctxWithTimeout, sender, delivery.Alias.Email, mailparse.MergeHeaders(arcHeaders, feedbackHeaders, deliveryHeaders, delivery.Mail.Headers), delivery.Mail.Body)
				if err != nil {
					recordFailure(delivery, err)
					return err
				}
				delivery.Size = size
				delivery.Status = models.DeliveryStatusDelivered
			}
		case models.AliasKindWebhook:
			if delivery.Alias.Webhook != "" {
				// send webhook
				size, err := self.sendWebhook(ctxWithTimeout, delivery.Alias.Webhook, sender, delivery.Recipient, mailparse.MergeHeaders(arcHeaders, feedbackHeaders, deliveryHeaders, delivery.Mail.Headers), delivery.Mail.Body)
				if err != nil {
					delivery.Error = err.Error()
					return err
				}
				delivery.Size = size
				delivery.Status = models.DeliveryStatusDelivered
			}
		case models.AliasKindMailServer:
			if delivery.Alias.MailServer != nil && delivery.Alias.MailServer.Host != "" {
				// forward mail to specific mail server
				mailServer := delivery.Alias.MailServer
				size, err := self.forwardMail(ctxWithTimeout, sender, delivery.Recipient, mailServer.Host, mailServer.Port, mailServer.Username, mailServer.Password, mailparse.MergeHeaders(arcHeaders, feedbackHeaders, deliveryHeaders, delivery.Mail.Headers), delivery.Mail.Body)
				if err != nil {
					recordFailure(delivery, err)
					return err
				}
				delivery.Size = size
				delivery.Status = models.DeliveryStatusDelivered
			}
		}
	}
	log.Debugf("took %s to deliver %q", time.Since(delivery.Mail.ReceivedAt), delivery.ID)
	return nil
}

func (self *exchange) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if self.settings.SOCKS5Proxy != "" {
		dialer, err := proxy.SOCKS5(network, self.settings.SOCKS5Proxy, nil, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("mx: failed to create socks5 dialer: %w", err)
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return dialer.Dial(network, address)
		}
		return contextDialer.DialContext(ctx, network, address)
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

// deliver by smtp
// servedHere reports whether an MX host names this server.
//
// Equality as well as a suffix match: an MX record normally names one of these
// hosts exactly, and the old check only looked for something beneath the name,
// which an MX record almost never is.
func (self *exchange) servedHere(host string) bool {
	host = strings.ToLower(strings.Trim(host, "."))
	for _, name := range self.settings.MailServers {
		name = strings.ToLower(strings.Trim(name, "."))
		if name == "" {
			continue
		}
		if host == name || strings.HasSuffix(host, "."+name) {
			return true
		}
	}
	return false
}

func (self *exchange) sendMail(ctx context.Context, sender, recipient string, headers []string, body []byte) (uint64, error) {
	if self.settings.DisableSendMail {
		return 0, fmt.Errorf("mx: disabled")
	}

	_, domain := mailparse.SplitAddress(recipient)

	// get a buffer from pool
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()

	// assemble mail data
	if err := mailparse.Unsplit(buffer, body, headers); err != nil {
		return 0, err
	}
	data := buffer.Bytes()

	// A relay takes everything, so there is no MX to look up and no loop to
	// detect: the operator named the destination.
	if relay := self.settings.Relay; relay != nil {
		return self.relayMail(ctx, relay, sender, recipient, data)
	}

	// look up mx records
	mxs, err := self.resolver.LookupMX(ctx, domain)
	if err != nil {
		return 0, err
	}
	for _, mx := range mxs {
		if self.servedHere(mx.Host) {
			return 0, fmt.Errorf("mx: domain %q is using teanode, loop detected", domain)
		}
	}
	for _, mx := range mxs {
		conn, err := self.dialContext(ctx, "tcp", fmt.Sprintf("%s:25", mx.Host))
		if err != nil {
			log.Warningf("failed to dial %q: %s", mx.Host, err)
			continue
		}
		if err := smtpc.Send(ctx, conn, "", "", sender, []string{recipient}, data, &smtpc.Settings{
			Hello:   self.settings.Server,
			Timeout: time.Hour,
		}); err != nil {
			log.Errorf("failed to deliver email to %q: %s", recipient, err)
			return 0, err
		}
		return uint64(len(data)), nil
	}
	return 0, fmt.Errorf("mx: failed to deliver email, no mail server available for %q", domain)
}

// relayMail hands one message to the configured relay.
//
// Unlike delivering to a stranger's MX, this connection is authenticated and
// checked: the operator named the host, so the certificate is verified against
// it and a relay that will not encrypt is refused rather than fallen back
// from. See config.Relay for why.
func (self *exchange) relayMail(ctx context.Context, relay *RelaySettings, sender, recipient string, data []byte) (uint64, error) {
	conn, err := self.dialContext(ctx, "tcp", relay.Address())
	if err != nil {
		return 0, fmt.Errorf("mx: cannot reach the relay at %s: %w", relay.Address(), err)
	}

	if err := smtpc.Send(ctx, conn, relay.Username, relay.Password, sender, []string{recipient}, data, &smtpc.Settings{
		Hello:      self.settings.Server,
		Timeout:    time.Hour,
		TLS:        relay.TLS,
		ServerName: relay.Host,
	}); err != nil {
		log.Errorf("failed to relay email to %q through %s: %s", recipient, relay.Address(), err)
		return 0, err
	}
	return uint64(len(data)), nil
}

// deliver by forwarding smtp
func (self *exchange) forwardMail(ctx context.Context, sender, recipient string, host string, port uint16, username, password string, headers []string, body []byte) (uint64, error) {
	// connect
	if port == 0 {
		port = 25
	}
	endpoint := fmt.Sprintf("%s:%d", host, port)
	conn, err := self.dialContext(ctx, "tcp", endpoint)
	if err != nil {
		log.Errorf("failed to dial %q: %s", endpoint, err)
		return 0, err
	}

	// get a buffer from pool
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()

	// assemble mail data
	if err := mailparse.Unsplit(buffer, body, headers); err != nil {
		return 0, err
	}
	data := buffer.Bytes()

	// send
	if err := smtpc.Send(ctx, conn, username, password, sender, []string{recipient}, data, &smtpc.Settings{
		Hello:   self.settings.Server,
		Timeout: time.Hour,
	}); err != nil {
		log.Errorf("failed to deliver email to %q: %s", recipient, err)
		return 0, err
	}
	return uint64(len(data)), nil
}

// deliver by webhook
func (self *exchange) sendWebhook(ctx context.Context, url, sender, recipient string, headers []string, body []byte) (uint64, error) {
	// get a buffer from pool
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()

	// assemble mail data
	if err := mailparse.Unsplit(buffer, body, headers); err != nil {
		return 0, err
	}

	// construct request
	request, err := http.NewRequestWithContext(ctx, "POST", url, buffer)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")

	// send request
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()

	// check response
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return 0, fmt.Errorf("mx: webhook failed with status code %d", response.StatusCode)
	}
	return uint64(buffer.Len()), nil
}

// periodically re-attempt deliveries
func (self *exchange) deliverOnce(ctx context.Context) error {
	var deliveries []*models.Delivery
	var mails []*models.Mail
	if err := self.database.Transaction(func(tx db.Transaction) error {
		var err error
		deliveries, err = tx.ListDeliveriesToRetry(nil)
		if err != nil {
			return err
		}
		if len(deliveries) == 0 {
			return nil
		}

		// get mails
		mailsMap := make(map[string]*models.Mail)
		for _, delivery := range deliveries {
			mailsMap[delivery.MailID] = nil
		}
		mailIds := make([]string, 0, len(mailsMap))
		for mailId := range mailsMap {
			mailIds = append(mailIds, mailId)
		}
		mails, err = tx.GetMails(mailIds, nil)
		if err != nil {
			return err
		}
		for _, mail := range mails {
			if mail != nil {
				mailsMap[mail.ID] = mail
			}
		}
		for _, delivery := range deliveries {
			delivery.Mail = mailsMap[delivery.MailID]
		}

		// get domains
		// Resolve the domain and alias each row refers to from the
		// configuration. Either may be absent because the operator has since
		// removed it; the delivery attempt reports that rather than crashing.
		domains := map[string]*models.Domain{}
		for _, mail := range mails {
			if mail == nil {
				continue
			}
			domain, ok := domains[mail.DomainID]
			if !ok {
				domain, err = tx.GetDomain(mail.DomainID)
				if err != nil {
					return err
				}
				domains[mail.DomainID] = domain
			}
			mail.Domain = domain
		}
		for _, delivery := range deliveries {
			if delivery.AliasID == "" {
				continue
			}
			alias, err := tx.GetAlias(delivery.AliasID)
			if err != nil {
				return err
			}
			delivery.Alias = alias
		}

		return nil
	}); err != nil {
		return err
	}

	// load mails from s3
	var waitGroup sync.WaitGroup
	for _, mail := range mails {
		if mail == nil {
			continue
		}
		waitGroup.Add(1)
		go func(mail *models.Mail) {
			defer deferutil.Recover()
			defer waitGroup.Done()
			headers, body, err := self.storage.Get(ctx, mail.ID)
			if err != nil {
				log.Warningf("cannot retry the delivery of mail %q, its content is no longer stored: %s", mail.ID, err)
				return
			}
			mail.Headers = headers
			mail.Body = body
		}(mail)
	}
	waitGroup.Wait()

	// delivery all in parallel
	for _, delivery := range deliveries {
		self.waitGroup.Add(1)
		go func(delivery *models.Delivery) {
			defer deferutil.Recover()
			defer self.waitGroup.Done()
			_ = self.deliver(self.ctx, delivery)
		}(delivery)
	}
	return nil
}

// signingDomain is the d= value to put in a signature or an ARC seal: the
// domain, never Hostname().
//
// A receiver verifies either one by looking up <selector>._domainkey.<d>, and
// the key is published at the domain — that is the record the dashboard lists,
// and the one outgoing DKIM signs as. Sealing as mail.example.com points them
// at a name with no key under it, so every seal fails to verify. Nothing here
// notices, because a seal is only ever checked by somebody else.
func signingDomain(domain *models.Domain) string {
	if domain == nil {
		return ""
	}
	return domain.Domain
}

// recordFailure writes what went wrong on the delivery, and decides whether
// it is worth trying again.
//
// A 5xx reply is the other server saying "no, and asking again will not
// change the answer" — RFC 5321's permanent negative completion. Retrying it
// on the backoff schedule anyway was how a message Gmail had refused as
// unsolicited was offered to Gmail four more times over a day, each attempt
// costing this server reputation with the one receiver that matters most.
// Such a delivery is dropped now; the retry schedule is for 4xx and for
// connections that failed before any reply.
func recordFailure(delivery *models.Delivery, err error) {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		delivery.Error = smtpc.CollapseResponse(reply.Msg)
		if isPermanentReply(reply.Code) {
			delivery.Status = models.DeliveryStatusDropped
		}
		return
	}
	delivery.Error = err.Error()
}

// isPermanentReply says whether an SMTP status code means "do not retry".
func isPermanentReply(code int) bool {
	return code >= 500 && code < 600
}
