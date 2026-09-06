package mx

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net"
	"net/netip"
	"net/textproto"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
	"github.com/ziyan/teanode/internal/util/arc"
	"github.com/ziyan/teanode/internal/util/authres"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/dmarc"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/spf"
)

// see https://support.google.com/mail/answer/6590
var blockedExtensions = map[string]bool{
	".ade":         true,
	".adp":         true,
	".apk":         true,
	".appx":        true,
	".appxbundle":  true,
	".bat":         true,
	".cab":         true,
	".chm":         true,
	".cmd":         true,
	".com":         true,
	".cpl":         true,
	".dll":         true,
	".dmg":         true,
	".ex":          true,
	".ex_":         true,
	".exe":         true,
	".hta":         true,
	".htm":         true,
	".html":        true,
	".ins":         true,
	".iso":         true,
	".isp":         true,
	".jar":         true,
	".js":          true,
	".jse":         true,
	".lib":         true,
	".lnk":         true,
	".mde":         true,
	".msc":         true,
	".msi":         true,
	".msix":        true,
	".msixbundle":  true,
	".msp":         true,
	".mst":         true,
	".nsh":         true,
	".pif":         true,
	".ps1":         true,
	".scr":         true,
	".sct":         true,
	".shb":         true,
	".sys":         true,
	".teanodetest": true,
	".vb":          true,
	".vbe":         true,
	".vbs":         true,
	".vxd":         true,
	".wsc":         true,
	".wsf":         true,
	".wsh":         true,
}

func (self *exchange) formatReceivedHeader(envelope *mailparse.Envelope) string {
	comment := "insecure"
	if envelope.TLS != nil {
		comment = fmt.Sprintf("version=%s cipher=%s", getTlsVersion(envelope.TLS), getTlsCipherSuite(envelope.TLS))
	}
	rdns := envelope.RDNS
	if rdns == "" {
		rdns = "unknown"
	}

	// What the sender called itself. A message submitted over the API never
	// said: there is no greeting in an HTTP request, and the header came out
	// as "from  (unknown [address])" — a from clause with nothing in it,
	// which is not what RFC 5321 describes and reads as a bug to anybody
	// looking at the source. The address literal is the honest answer, and is
	// a form the grammar allows.
	hello := envelope.Hello
	if strings.TrimSpace(hello) == "" {
		hello = fmt.Sprintf("[%s]", envelope.IP)
	}

	return mailparse.UnsplitHeader("Received", strings.Join([]string{
		fmt.Sprintf("from %s (%s [%s])", hello, rdns, envelope.IP),
		fmt.Sprintf(" by %s (%s) id %s", self.receivedBy(envelope), self.settings.Service, envelope.ID),
		fmt.Sprintf(" for <%s>", envelope.Recipients[0]),
		fmt.Sprintf(" (%s);", comment),
		fmt.Sprintf(" %s", envelope.ReceivedAt.Format("Mon, 02 Jan 2006 15:04:05 -0700 (MST)")),
	}, "\r\n"))
}

func (self *exchange) formatAuthenticationResultsHeader(envelope *mailparse.Envelope, results []authres.Result) string {
	return mailparse.UnsplitHeader("Authentication-Results", authres.Format(self.receivedBy(envelope), results))
}

// receivedBy is the name to report as the host that received a message: the
// one the sender actually reached.
//
// It used to be server.name for every message, which is one name for a server
// that answers to many. A sender that looked up example.net, was given
// mx.example.net and connected to it was then told, in the message it had just
// delivered, that it had reached a host in somebody else's domain. That is
// both wrong and the last place the association between one operator's domains
// was still written down, after the DNS stopped saying it.
//
// The sender's own statement first. A TLS client says which name it is trying
// to reach before the handshake completes, and that is exactly the question
// being answered here. Almost every sender uses TLS and almost all of those
// send the name.
//
// Failing that, the recipient's domain. Mail for someone@example.net was
// delivered by a sender that looked up example.net's MX, so the name it used
// is the one that domain publishes — the same derivation the DNS panel shows,
// so the two cannot disagree. A message to recipients in two served domains
// has no single answer and falls through.
//
// Failing that, the server's own name, which is what a message for a domain
// this server does not serve gets. Such a message is refused anyway.
func (self *exchange) receivedBy(envelope *mailparse.Envelope) string {
	if envelope != nil && envelope.TLS != nil && envelope.TLS.ServerName != "" {
		return envelope.TLS.ServerName
	}

	if envelope != nil {
		configuration := self.config.Current()
		name := ""
		for _, recipient := range envelope.Recipients {
			_, recipientDomain := mailparse.SplitAddress(recipient)
			domain := configuration.FindDomain(recipientDomain)
			if domain == nil {
				return self.settings.Server
			}
			host := configuration.MailHostFor(domain)
			if name != "" && !strings.EqualFold(name, host) {
				return self.settings.Server
			}
			name = host
		}
		if name != "" {
			return name
		}
	}

	return self.settings.Server
}

func (self *exchange) formatReceivedSpfHeader(results []authres.Result, envelope *mailparse.Envelope) string {
	for _, result := range results {
		spfResult, ok := result.(*authres.SPFResult)
		if !ok {
			continue
		}
		switch spfResult.Value {
		case authres.ResultPass:
			return mailparse.UnsplitHeader("Received-SPF", fmt.Sprintf("pass (%s: domain of %s designates %s as permitted sender) client-ip=%s;", self.settings.Server, spfResult.From, envelope.IP, envelope.IP))
		default:
			return mailparse.UnsplitHeader("Received-SPF", fmt.Sprintf("%s (%s: domain of %s does not designate %s as permitted sender) client-ip=%s;", spfResult.Value, self.settings.Server, spfResult.From, envelope.IP, envelope.IP))
		}
	}
	return mailparse.UnsplitHeader("Received-SPF", fmt.Sprintf("none (%s: domain of %s does not designate %s as permitted sender) client-ip=%s;", self.settings.Server, envelope.Sender, envelope.IP, envelope.IP))
}

func getTlsVersion(tlsConnectionState *tls.ConnectionState) string {
	if tlsConnectionState != nil {
		switch tlsConnectionState.Version {
		case tls.VersionTLS10:
			return "TLS1_0"
		case tls.VersionTLS11:
			return "TLS1_1"
		case tls.VersionTLS12:
			return "TLS1_2"
		case tls.VersionTLS13:
			return "TLS1_3"
		}
		return "TLS"
	}
	return ""
}

func getTlsCipherSuite(tlsConnectionState *tls.ConnectionState) string {
	if tlsConnectionState != nil {
		return tls.CipherSuiteName(tlsConnectionState.CipherSuite)
	}
	return ""
}

type authenticatorReturn struct {
	authenticationResults models.AuthenticationResults
	results               []authres.Result
	err                   error
}

type authenticator struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan authenticatorReturn
	workers int
}

func newAuthenticator(ctx context.Context) *authenticator {
	self := &authenticator{
		done: make(chan authenticatorReturn),
	}
	self.ctx, self.cancel = context.WithCancel(ctx)
	return self
}

func (self *authenticator) stop() {
	self.cancel()
}

func (self *authenticator) do(f func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error)) {
	self.workers++
	go func() {
		defer deferutil.Recover()
		authenticationResults, results, err := f(self.ctx)
		self.done <- authenticatorReturn{authenticationResults, results, err}
	}()
}

func (self *authenticator) wait() (models.AuthenticationResults, []authres.Result, error) {
	var err error
	var authenticationResults models.AuthenticationResults
	var results []authres.Result
	for self.workers > 0 {
		returnValue := <-self.done
		self.workers--
		if returnValue.authenticationResults.FromMX != nil {
			authenticationResults.FromMX = returnValue.authenticationResults.FromMX
		}
		if returnValue.authenticationResults.SenderMX != nil {
			authenticationResults.SenderMX = returnValue.authenticationResults.SenderMX
		}
		if returnValue.authenticationResults.DMARC != nil {
			authenticationResults.DMARC = returnValue.authenticationResults.DMARC
		}
		if returnValue.authenticationResults.SPF != nil {
			authenticationResults.SPF = returnValue.authenticationResults.SPF
		}
		if returnValue.authenticationResults.DKIMs != nil {
			authenticationResults.DKIMs = returnValue.authenticationResults.DKIMs
		}
		if returnValue.authenticationResults.ARC != nil {
			authenticationResults.ARC = returnValue.authenticationResults.ARC
		}
		if returnValue.authenticationResults.Antivirus != nil {
			authenticationResults.Antivirus = returnValue.authenticationResults.Antivirus
		}
		if returnValue.authenticationResults.SpamFilter != nil {
			authenticationResults.SpamFilter = returnValue.authenticationResults.SpamFilter
		}
		if returnValue.authenticationResults.ContentFilter != nil {
			authenticationResults.ContentFilter = returnValue.authenticationResults.ContentFilter
		}
		results = append(results, returnValue.results...)
		if returnValue.err != nil {
			// cancel() // cancel the others
			if err == nil {
				err = returnValue.err
			}
			authenticationResults.Errors = append(authenticationResults.Errors, returnValue.err.Error())
		}
	}
	return authenticationResults, results, err
}

func (self *exchange) checkFromMx(authenticator *authenticator, from string) {
	authenticator.do(func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error) {
		_, fromDomain := mailparse.SplitAddress(from)
		mxs, _ := self.resolver.LookupMX(ctx, fromDomain)
		if len(mxs) == 0 {
			// if there is no mx record for the original from domain
			// try tld plus 1, some domain like to send no-reply email from a subdomain
			if effectiveDomain, err := publicsuffix.EffectiveTLDPlusOne(fromDomain); err == nil && effectiveDomain != fromDomain {
				fromDomain = effectiveDomain
				mxs, _ = self.resolver.LookupMX(ctx, fromDomain)
			}
		}
		mailServers := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			mailServers = append(mailServers, mx.Host)
		}
		authenticationResults := models.AuthenticationResults{
			FromMX: &models.MXResult{
				Domain:      fromDomain,
				MailServers: mailServers,
			},
		}
		if len(mailServers) == 0 {
			return authenticationResults, nil, mailparse.ErrInvalidFromMX
		}
		return authenticationResults, nil, nil
	})
}

func (self *exchange) checkSenderMx(authenticator *authenticator, envelope *mailparse.Envelope) {
	authenticator.do(func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error) {
		_, senderDomain := mailparse.SplitAddress(envelope.Sender)
		mxs, _ := self.resolver.LookupMX(ctx, senderDomain)
		mailServers := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			mailServers = append(mailServers, mx.Host)
		}
		authenticationResults := models.AuthenticationResults{
			SenderMX: &models.MXResult{
				Domain:      senderDomain,
				MailServers: mailServers,
			},
		}
		if len(mailServers) == 0 {
			return authenticationResults, nil, mailparse.ErrInvalidSenderMX
		}
		return authenticationResults, nil, nil
	})
}

func (self *exchange) checkDmarcSpfDkim(authenticator *authenticator, from string, envelope *mailparse.Envelope, headers []string, body []byte) {
	authenticator.do(func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error) {
		_, fromDomain := mailparse.SplitAddress(from)
		_, senderDomain := mailparse.SplitAddress(envelope.Sender)

		var waitGroup sync.WaitGroup

		// dmarc
		var dmarcPolicy *dmarc.Discovery
		var dmarcErr error
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			// Discover rather than Lookup: a sender using a subdomain, which
			// is most bulk senders, publishes nothing at that name and is
			// governed by the organizational domain above it.
			dmarcPolicy, dmarcErr = dmarc.Discover(ctx, fromDomain, &dmarc.LookupOptions{
				Resolver: self.resolver,
			})
		}()

		// spf
		var spfResult spf.Result
		var spfErr error
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			spfResult, spfErr = spf.Check(ctx, envelope.IP, senderDomain, envelope.Sender, &spf.CheckOptions{
				Resolver: self.resolver,
			})
		}()

		// dkim
		var dkimResults []*dkim.Verification
		var dkimErr error
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			dkimResults, dkimErr = dkim.Verify(ctx, headers, body, self.resolver)
		}()

		// wait for all
		waitGroup.Wait()

		// construct result
		authenticationResults := models.AuthenticationResults{
			SPF: &models.SPFResult{
				Domain: senderDomain,
				IP:     envelope.IP.String(),
				Result: string(spfResult),
			},
			DKIMs: make([]*models.DKIMResult, 0, len(dkimResults)),
		}
		for _, dkimResult := range dkimResults {
			authenticationResults.DKIMs = append(authenticationResults.DKIMs, &models.DKIMResult{
				Result:     string(dkimResult.Result),
				Domain:     dkimResult.Domain,
				Selector:   dkimResult.Selector,
				Identifier: dkimResult.Identifier,
			})
		}
		dmarcResult := authres.ResultNone
		if dmarcPolicy != nil && dmarcPolicy.Record != nil {
			dmarcRecord := dmarcPolicy.Record
			var spfPassedAndAligned, dkimPassedAndAligned bool
			switch spfResult {
			case spf.ResultPass:
				switch dmarcRecord.SPFAlignment {
				case dmarc.AlignmentStrict:
					if senderDomain == fromDomain {
						spfPassedAndAligned = true
					}
				case dmarc.AlignmentRelaxed:
					if senderDomain == fromDomain || strings.HasSuffix(senderDomain, "."+fromDomain) {
						spfPassedAndAligned = true
					}
				}
			}
			for _, dkimResult := range dkimResults {
				if dkimResult.Result != dkim.ResultPass {
					continue
				}
				switch dmarcRecord.DKIMAlignment {
				case dmarc.AlignmentStrict:
					if dkimResult.Domain == fromDomain {
						dkimPassedAndAligned = true
					}
				case dmarc.AlignmentRelaxed:
					if dkimResult.Domain == fromDomain || strings.HasSuffix(dkimResult.Domain, "."+fromDomain) {
						dkimPassedAndAligned = true
					}
				}
			}
			if spfPassedAndAligned || dkimPassedAndAligned {
				dmarcResult = authres.ResultPass
			} else {
				dmarcResult = authres.ResultFail
			}
			authenticationResults.DMARC = &models.DMARCResult{
				Domain: fromDomain,
				// The policy that actually applies, which for a record found
				// above the sender is its subdomain policy.
				Policy:          string(dmarcPolicy.Policy()),
				SubdomainPolicy: string(dmarcRecord.SubdomainPolicy),
				DKIMAlignment:   string(dmarcRecord.DKIMAlignment),
				SPFAlignment:    string(dmarcRecord.SPFAlignment),
				Result:          string(dmarcResult),
			}
			// Named, so a reader of the stored results can tell a sender's own
			// policy from one inherited from the domain above it.
			if dmarcPolicy.Organizational {
				authenticationResults.DMARC.PolicyDomain = dmarcPolicy.Domain
			}
			log.Debugf("found dmarc record for %q at %q: %v, spfPassedAndAligned = %v, dkimPassedAndAligned = %v", fromDomain, dmarcPolicy.Domain, dmarcRecord, spfPassedAndAligned, dkimPassedAndAligned)
		}

		// check for errors
		if spfErr != nil {
			log.Errorf("spf check for ip %q, domain %q, sender %q, failed with result %q: %s", envelope.IP, senderDomain, envelope.Sender, spfResult, spfErr)
			return authenticationResults, nil, mailparse.ErrSPFValidationError
		}
		// A verification error is not a failed signature. It means the key
		// could not be fetched or read — a resolver hiccup, a malformed
		// record at the signer's end — and RFC 6376 §6.1.2 says that makes
		// the signature unverifiable, not the message rejectable. Refusing
		// here with a permanent 550 bounced a school's mail sent through
		// SparkPost, with SPF passing, over a key lookup. Logged, recorded as
		// no signature verdict, and left to DMARC and the spam filter.
		if dkimErr != nil {
			log.Warningf("could not verify dkim for sender %q: %s", envelope.Sender, dkimErr)
		}
		if dmarcErr != nil {
			log.Errorf("failed to verify dmarc: %s", dmarcErr)
			return authenticationResults, nil, mailparse.ErrDMARCAlignmentFailed
		}

		// only error spf or dkim if dmarc didn't pass
		switch dmarcResult {
		case authres.ResultPass:
		case authres.ResultFail:
			return authenticationResults, nil, mailparse.ErrDMARCAlignmentFailed
		case authres.ResultNone:
			switch spfResult {
			case spf.ResultTempError, spf.ResultPermError:
				log.Errorf("spf check for ip %q, domain %q, sender %q, failed with result %q", envelope.IP, senderDomain, envelope.Sender, spfResult)
				return authenticationResults, nil, mailparse.ErrSPFValidationError
			case spf.ResultFail:
				log.Errorf("spf check for ip %q, domain %q, sender %q, failed with result %q", envelope.IP, senderDomain, envelope.Sender, spfResult)
				return authenticationResults, nil, mailparse.ErrSPFValidationFailed
			}
			if err := dkimVerdict(dkimResults, spfResult); err != nil {
				return authenticationResults, nil, err
			}
		}

		// construct results
		results := make([]authres.Result, 0, len(dkimResults)+2)
		results = append(results, &authres.SPFResult{
			Value: authres.ResultValue(spfResult),
			From:  senderDomain,
			Helo:  envelope.Hello,
		})
		for _, dkimResult := range dkimResults {
			results = append(results, &authres.DKIMResult{
				Value:      authres.ResultValue(dkimResult.Result),
				Domain:     dkimResult.Domain,
				Identifier: dkimResult.Identifier,
			})
		}
		results = append(results, &authres.DMARCResult{
			Value: dmarcResult,
			From:  fromDomain,
		})
		return authenticationResults, results, nil
	})
}

func (self *exchange) checkArc(authenticator *authenticator, headers []string, body []byte) {
	authenticator.do(func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error) {
		authenticationResults := models.AuthenticationResults{
			ARC: &models.ARCResult{
				Result: "fail",
			},
		}
		arcResult, err := arc.Validate(ctx, headers, body, self.resolver)
		if err != nil {
			log.Errorf("failed to verify arc: %s", err)
			return authenticationResults, nil, mailparse.ErrARCValidationFailed
		}
		authenticationResults.ARC.Result = string(arcResult.Status)
		authenticationResults.ARC.Instances = arcResult.Instances
		results := make([]authres.Result, 0, 1)
		switch arcResult.Status {
		case arc.StatusPass:
			results = append(results, &authres.ARCResult{
				Value: authres.ResultPass,
			})
		}
		return authenticationResults, results, nil
	})
}

func (self *exchange) checkContent(authenticator *authenticator, headers []string, body []byte) {
	authenticator.do(func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error) {
		unsafeExtensionsMap := make(map[string]bool)
		if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
			// check extension in Content-Type
			if contentType := header.Get("Content-Type"); contentType != "" {
				_, contentTypeParameters, _ := mime.ParseMediaType(contentType)
				if extension := strings.ToLower(filepath.Ext(contentTypeParameters["name"])); blockedExtensions[extension] {
					unsafeExtensionsMap[extension] = true
				}
			}

			// check extension in Content-Disposition
			if contentDisposition := header.Get("Content-Disposition"); contentDisposition != "" {
				_, contentDispositionParameters, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
				if extension := strings.ToLower(filepath.Ext(contentDispositionParameters["filename"])); blockedExtensions[extension] {
					unsafeExtensionsMap[extension] = true
				}
			}
			return nil
		}); err != nil {
			return models.AuthenticationResults{}, nil, err
		}
		unsafeExtensions := make([]string, 0, len(unsafeExtensionsMap))
		for unsafeExtension := range unsafeExtensionsMap {
			unsafeExtensions = append(unsafeExtensions, unsafeExtension)
		}
		authenticationResults := models.AuthenticationResults{
			ContentFilter: &models.ContentFilterResult{
				UnsafeExtensions: unsafeExtensions,
			},
		}
		if len(unsafeExtensions) > 0 {
			return authenticationResults, nil, mailparse.ErrProhibitedFileExtension
		}
		return authenticationResults, nil, nil
	})
}

func (self *exchange) checkVirus(authenticator *authenticator, headers []string, body []byte) {
	if self.clamav == nil {
		return
	}
	authenticator.do(func(ctx context.Context) (models.AuthenticationResults, []authres.Result, error) {
		virusesMap := make(map[string]bool)
		if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
			switch header.Get("Content-Transfer-Encoding") {
			case "base64":
				reader = base64.NewDecoder(base64.StdEncoding, reader)
			}

			// check content for virus
			start := time.Now()
			virus, err := self.clamav.Scan(ctx, reader)
			if err != nil {
				log.Warningf("failed to scan virus: %s", err)
				return nil
			}
			log.Debugf("clamav: took %s to scan for virus, result is %q", time.Since(start), virus)
			if virus != "" {
				virusesMap[virus] = true
			}
			return nil
		}); err != nil {
			return models.AuthenticationResults{}, nil, err
		}
		viruses := make([]string, 0, len(virusesMap))
		for virus := range virusesMap {
			viruses = append(viruses, virus)
		}
		authenticationResults := models.AuthenticationResults{
			Antivirus: &models.AntivirusResult{
				Viruses: viruses,
			},
		}
		if len(viruses) > 0 {
			return authenticationResults, nil, mailparse.ErrVirusDetected
		}
		return authenticationResults, nil, nil
	})
}

// checkSpam scores the message, after the other checks have finished.
//
// It runs on its own rather than alongside them, because the built-in filter
// reads what they established: SPF, DKIM, DMARC and ARC are inputs to the
// score, and while the checks all ran together those answers did not exist
// yet at this point. The cost of giving up that concurrency is small — the
// built-in filter does no lookups of its own — and the alternative is scoring
// a message while ignoring what the server just learned about it.
//
// A filter that fails does not reject mail. Spam scoring is advisory, and a
// broken scorer that bounced messages would be worse than no scorer at all,
// so the error is logged and the message goes on unscored.
func (self *exchange) checkSpam(
	ctx context.Context,
	envelope *mailparse.Envelope,
	mail *models.Mail,
	scoreThreshold float64,
) error {
	if self.spamFilter == nil {
		return nil
	}

	start := time.Now()
	message := &spamfilter.Message{
		Headers:        mail.Headers,
		Body:           mail.Body,
		Authentication: &mail.AuthenticationResults,
		ReverseName:    envelope.RDNS,
		Location:       envelope.Location,
		HelloName:      envelope.Hello,
		ServerName:     self.config.Current().Server.Name,
		Authenticated:  envelope.CredentialID != "",
	}
	if envelope.IP != nil {
		if address, ok := netip.AddrFromSlice(envelope.IP); ok {
			message.RemoteAddress = address.Unmap()
		}
	}

	spamResult, err := self.spamFilter.Check(ctx, message)
	if err != nil {
		log.Warningf("failed to check spam: %s", err)
		return nil
	}
	if spamResult == nil {
		return nil
	}
	log.Debugf("spam filter: took %s to check for spam: score = %f, symbols = %v",
		time.Since(start), spamResult.Score, spamResult.Symbols)

	spamResult.Result = "pass"
	if spamResult.Score > scoreThreshold {
		spamResult.Result = "fail"
	}
	mail.AuthenticationResults.SpamFilter = spamResult

	if spamResult.Score > scoreThreshold {
		return mailparse.ErrSpamCheckFailed
	}
	return nil
}

func (self *exchange) checkIp(ctx context.Context, ip net.IP, timeout time.Duration) string {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addresses, err := self.resolver.LookupAddr(ctxWithTimeout, ip.String())
	if err != nil {
		log.Warningf("reverse dns lookup for %q failed: %s", ip, err)
		return ""
	}
	for _, address := range addresses {
		resolutions, err := self.resolver.LookupIPAddr(ctxWithTimeout, address)
		if err != nil {
			log.Warningf("failed to resolve domain %q: %s", address, err)
			continue
		}
		for _, resolution := range resolutions {
			if ip.Equal(resolution.IP) {
				return address
			}
		}
	}
	return ""
}

func (self *exchange) matchAliases(domain *config.Domain, recipientAlias string, mail *models.Mail) ([]*models.Delivery, error) {
	aliases := self.config.Current().MatchAliases(domain, recipientAlias)
	if len(aliases) == 0 {
		return nil, nil
	}
	recipient := mailparse.UnsplitAddress(recipientAlias, domain.Domain)

	// find all recipient addresses
	var deliveries []*models.Delivery
	for _, alias := range aliases {
		self.trackAliasUsage(mail.ReceivedAt, alias.ID, aliasUsage{
			bytesReceived: mail.Size,
			mailsAccepted: 1,
		})
		var deliver bool
		switch alias.Kind {
		case config.AliasKindEmail:
			deliver = alias.Email != ""
		case config.AliasKindWebhook:
			deliver = alias.Webhook != ""
		case config.AliasKindMailServer:
			deliver = alias.MailServer != nil && alias.MailServer.Host != ""
		}
		if deliver {
			deliveries = append(deliveries, &models.Delivery{
				MailID:    mail.ID,
				Mail:      mail,
				AliasID:   alias.ID,
				Alias:     alias,
				Recipient: recipient,
				Kind:      models.DeliveryKindForward,
			})
		}
	}
	return deliveries, nil
}

// dkimVerdict decides, for a sender with no DMARC policy, whether the DKIM
// results alone are grounds to refuse the message.
//
// They almost never are. Mail through a forwarder or a relay carries two
// signatures — the original, broken in transit, and the relay's, intact —
// and refusing on the first non-passing one bounced ten legitimate messages
// through Apple's private relay in six days, all with SPF passing and one
// valid signature each. RFC 6376 §6.3: verifiers should not reject a message
// solely on a failed signature.
//
// So a message is refused here only when it carries signatures, none of them
// verify, and SPF did not pass either — the one combination where nothing at
// all vouches for it.
func dkimVerdict(results []*dkim.Verification, spfResult spf.Result) error {
	if len(results) == 0 || spfResult == spf.ResultPass {
		return nil
	}
	for _, result := range results {
		if result.Result == dkim.ResultPass {
			return nil
		}
	}
	return mailparse.ErrDKIMVerificationFailed
}
