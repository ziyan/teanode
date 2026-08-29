package dsn_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/util/dsn"
)

func testDsn(t *testing.T, name, content string, recipientStatuses int) {
	t.Helper()

	deliveryStatus, err := dsn.Parse(bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("failed to parse %s dsn: %s", name, err)
	}
	raw, _ := json.Marshal(deliveryStatus)
	t.Logf("%s dsn: %s", name, string(raw))
	if len(deliveryStatus.RecipientStatuses) != recipientStatuses {
		t.Fatalf("unexpected number of recipient statuses in %s dsn: %d", name, len(deliveryStatus.RecipientStatuses))
	}
}

// The fixtures below that name universities and 1990s hosts are the examples
// from RFC 3464 appendix D, quoted verbatim so that what is parsed here is
// what the specification says a report looks like. The rest are shaped like
// reports a running server receives, with every address rewritten to the
// reserved documentation domains.

const simpleDsn = `Reporting-MTA: dns; cs.utk.edu

Original-Recipient: rfc822;louisl@larry.slip.umd.edu
Final-Recipient: rfc822;louisl@larry.slip.umd.edu
Action: failed
Status: 4.0.0
Diagnostic-Code: smtp; 426 connection timed out
Last-Attempt-Date: Thu, 7 Jul 1994 17:15:49 -0400
`

func TestParseSimple(t *testing.T) {
	t.Parallel()
	testDsn(t, "simple", simpleDsn, 1)
}

const multiDsn = `Reporting-MTA: dns; cs.utk.edu

Original-Recipient: rfc822;arathib@vnet.ibm.com
Final-Recipient: rfc822;arathib@vnet.ibm.com
Action: failed
Status: 5.0.0 (permanent failure)
Diagnostic-Code: smtp;  550 'arathib@vnet.IBM.COM' is not a
registered gateway user
Remote-MTA: dns; vnet.ibm.com

Original-Recipient: rfc822;johnh@hpnjld.njd.hp.com
Final-Recipient: rfc822;johnh@hpnjld.njd.hp.com
Action: delayed
Status: 4.0.0 (hpnjld.njd.jp.com: host name lookup failure)

Original-Recipient: rfc822;wsnell@sdcc13.ucsd.edu
Final-Recipient: rfc822;wsnell@sdcc13.ucsd.edu
Action: failed
Status: 5.0.0
Diagnostic-Code: smtp; 550 user unknown
Remote-MTA: dns; sdcc13.ucsd.edu
`

func TestParseMulti(t *testing.T) {
	t.Parallel()
	testDsn(t, "multi", multiDsn, 3)
}

const gatewayDsn = `Reporting-MTA: mailbus; SYS30

Final-Recipient: unknown; nair_s
Status: 5.0.0 (unknown permanent failure)
Action: failed
`

func TestParseGateway(t *testing.T) {
	t.Parallel()
	testDsn(t, "gateway", gatewayDsn, 1)
}

const delayedDsn = `Reporting-MTA: dns; sun2.nsfnet-relay.ac.uk

Final-Recipient: rfc822;thomas@de-montfort.ac.uk
Status: 4.0.0 (unknown temporary failure)
Action: delayed
`

func TestParseDelayed(t *testing.T) {
	t.Parallel()
	testDsn(t, "delayed", delayedDsn, 1)
}

const relayDsn = `Reporting-MTA: smtp; relay.example.net
Original-Recipient: rfc822; bounce-test@relay.example.net
Action: failed
Status: 5.1.1
Diagnostic-Code: SMTP; 550 No such recipient
`

func TestParseRelay(t *testing.T) {
	t.Parallel()
	testDsn(t, "relay", relayDsn, 1)
}

const postfixDsn = `Reporting-MTA: dns; mail.example.com
X-Postfix-Queue-ID: DCBE2360090
X-Postfix-Sender: rfc822; sender@example.com
Arrival-Date: Fri, 26 Feb 2021 18:49:43 +0800 (HKT)

Final-Recipient: rfc822; recipient@example.net
Original-Recipient: rfc822;recipient@example.net
Action: failed
Status: 5.0.0
Remote-MTA: dns; mx.example.net
Diagnostic-Code: smtp; 550 User not found: recipient@example.net
`

func TestParsePostfix(t *testing.T) {
	t.Parallel()
	testDsn(t, "postfix", postfixDsn, 1)
}
