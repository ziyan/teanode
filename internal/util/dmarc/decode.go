package dmarc

import (
	"encoding/xml"
	"io"
)

type FeedbackRecordDKIM struct {
	Domain      string `xml:"domain" json:"domain,omitempty"`
	Selector    string `xml:"selector" json:"selector,omitempty"`
	Result      string `xml:"result" json:"result,omitempty"` // none, pass, fail, policy, neutral, temperror, permerror
	HumanResult string `xml:"human_result" json:"humanResult,omitempty"`
}

type FeedbackRecordSPF struct {
	Domain string `xml:"domain" json:"domain,omitempty"`
	Scope  string `xml:"scope" json:"scope,omitempty"`   // helo, mfrom
	Result string `xml:"result" json:"result,omitempty"` // none, neutral, pass, fail, softfail, temperror, permerror
}

type FeedbackRecord struct {
	SourceIP string `xml:"row>source_ip" json:"sourceIp,omitempty"` // ip address
	Count    uint64 `xml:"row>count" json:"count,omitempty"`        // number of matching messages

	Disposition string `xml:"row>policy_evaluated>disposition" json:"disposition,omitempty"` // none, quarantine, reject
	DKIM        string `xml:"row>policy_evaluated>dkim" json:"dkim,omitempty"`               // pass, fail
	SPF         string `xml:"row>policy_evaluated>spf" json:"spf,omitempty"`                 // pass, fail

	// override reason, one of forwarded, sampled_out, trusted_forwarder, mailing_list, local_policy, other
	ReasonType    string `xml:"row>policy_evaluated>reason>type" json:"reasonType,omitempty"`
	ReasonComment string `xml:"row>policy_evaluated>reason>comment" json:"reasonComment,omitempty"`

	HeaderFrom   string `xml:"identifiers>header_from" json:"headerFrom,omitempty"`     // from domain
	EnvelopeFrom string `xml:"identifiers>envelope_from" json:"envelopeFrom,omitempty"` // mail from domain
	EnvelopeTo   string `xml:"identifiers>envelope_to" json:"envelopeTo,omitempty"`     // envelope recipient domain

	// results
	DKIMs []FeedbackRecordDKIM `xml:"auth_results>dkim" json:"dkims,omitempty"`
	SPFs  []FeedbackRecordSPF  `xml:"auth_results>spf" json:"spfs,omitempty"`
}

type Feedback struct {
	// reporter info
	OrganizationName string   `xml:"report_metadata>org_name" json:"organizationName,omitempty"`
	Email            string   `xml:"report_metadata>email" json:"email,omitempty"`
	ExtraContactInfo string   `xml:"report_metadata>extra_contact_info" json:"extraContactInfo,omitempty"`
	ReportID         string   `xml:"report_metadata>report_id" json:"reportId,omitempty"`
	Begin            uint64   `xml:"report_metadata>date_range>begin" json:"begin,omitempty"`
	End              uint64   `xml:"report_metadata>date_range>end" json:"end,omitempty"`
	Errors           []string `xml:"report_metadata>error" json:"errors,omitempty"`

	// observed published policy
	Domain          string  `xml:"policy_published>domain" json:"domain,omitempty"`
	DKIMAlignment   string  `xml:"policy_published>adkim" json:"dkimAlignment,omitempty"` // r, s
	SPFAlignment    string  `xml:"policy_published>aspf" json:"spfAlignment,omitempty"`   // r, s
	Policy          string  `xml:"policy_published>p" json:"policy,omitempty"`            // none, quarantine, reject
	SubdomainPolicy string  `xml:"policy_published>sp" json:"subdomainPolicy,omitempty"`  // none, quarantine, reject
	Percent         *uint64 `xml:"policy_published>pct" json:"percent,omitempty"`
	FailureOptions  string  `xml:"policy_published>fo" json:"failureOptions,omitempty"`

	Records []FeedbackRecord `xml:"record" json:"records,omitempty"`
}

func Decode(reader io.Reader) (*Feedback, error) {
	var feedback Feedback
	if err := xml.NewDecoder(reader).Decode(&feedback); err != nil {
		return nil, err
	}
	return &feedback, nil
}
