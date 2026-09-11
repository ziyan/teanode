package client

import (
	"context"
	"encoding/json"
)

// Settings are the optional integrations, as the server describes them. Kept
// as raw JSON per section rather than as typed structs: the command line
// prints whatever fields the server reports, so a setting added to the API
// shows up without a change here.
type Settings map[string]json.RawMessage

// settingsSelection asks for every section with every field it has, three
// levels deep, which is as deep as the settings go. Written out rather than
// introspected, because the settings command runs often and a second round
// trip to learn the shape would double its cost.
const settingsSelection = `{
	s3 { enabled bucket region endpoint pathStyle accessKeyId hasSecretAccessKey credentialsFile }
	route53 { enabled zoneId region accessKeyId hasSecretAccessKey credentialsFile }
	antivirus { enabled host port }
	antispam { enabled engine effectiveEngine host port signalsEnabled dnsEnabled bayesEnabled rulesEnabled bayesMinimumMessages bayesLearnedSpam bayesLearnedHam }
	relay { enabled host port security username hasPassword }
	submission { host port effectiveHost effectivePort }
	imap { host port tlsPort effectiveHost effectivePort effectiveTlsPort }
	proxy { socks5 }
	certificates { perDomain hosts acmeEnabled acmeEmail acmeDirectoryUrl acmeChallenge certificateFile privateKeyFile }
	smtp { maxMessageSize maxRecipientsIncoming maxRecipientsOutgoing greylistDelay authRateLimit authRateBurst trustedSenders }
	resolver { nameserver checkInterval externalAddressServices }
	session { lifetime }
	passkey { enabled relyingPartyId displayName origins maximumPerUser }
	listen { smtpIncoming smtpOutgoing imap imaps http https debug }
	sso { providers { id name issuer clientId hasClientSecret groupsClaim createUsers } }
	identity { name mailServers externalAddresses logLevel dataDirectory }
	storage { directory spoolRetention }
	geoip { enabled databaseFile }
	agent {
		enabled instructions currency
		providers { name kind baseUrl hasApiKey enabled allow deny pricingInput pricingOutput pricingCacheRead pricingCacheWrite modelPricing { model input output cacheRead cacheWrite } }
		models { default fast embedding triage research summarize reply ask schedule compact choices }
		features { triage summaries draftReplies search research autoReply ask schedules browser connectedServers }
		limits { maxBodyCharacters dailyTokensPerAgent monthlyTokensPerServer dailyCostPerAgent monthlyCostPerServer maxRoundsPerAsk maxRoundsPerResearch maxRoundsPerReply maxToolCallsPerRun requestTimeout concurrency }
		retention { runs corrections }
		search { kind hasApiKey }
		tools { disabled confirm }
		browser { enabled cdpEndpoint attachTabs allowPrivateAddresses idleTimeout maxContexts }
		mcpServers { name transport effectiveTransport url command args envNames workingDir auth effectiveAuth hasAuthorization oauthClientId hasOauthClientSecret oauthScopes oauthAuthorizationUrl oauthTokenUrl headless readOnly disabled timeout enabled }
		works families kinds
	}
}`

// GetSettings returns the optional integrations. Secrets are never returned;
// a field that is set reads back as whether it is set.
func GetSettings(ctx context.Context, connection *Client) (Settings, error) {
	var result struct {
		GetSettings Settings `json:"GetSettings"`
	}
	if err := connection.Execute(ctx, `query { GetSettings `+settingsSelection+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.GetSettings, nil
}

// UpdateSettings changes one or more sections. Each value is the parameters
// object for that section, as the schema's <Section>ParametersInput declares
// it; a field left out keeps its stored value.
func UpdateSettings(ctx context.Context, connection *Client, sections map[string]any) (Settings, error) {
	var result struct {
		UpdateSettings Settings `json:"UpdateSettings"`
	}
	// The variable declarations name each section's input type, which the
	// schema derives from the Go type: S3Parameters becomes S3ParametersInput.
	//
	// These are spelled out rather than introspected, so they drift silently
	// when a Go type is renamed — the mutation then fails validation as a
	// whole, for every section, not just the one that moved. Two had drifted:
	// SMTPParameters was written here as SmtpParametersInput, which the
	// schema never had, and antispam grew a type of its own when the built-in
	// filter arrived. TestSettingsMutationNamesRealTypes keeps them honest.
	query := `mutation (
		$s3: S3ParametersInput, $route53: Route53ParametersInput,
		$antivirus: ServiceParametersInput, $antispam: AntispamParametersInput,
		$relay: RelayParametersInput, $submission: SubmissionParametersInput, $imap: IMAPParametersInput,
		$proxy: ProxyParametersInput, $certificates: CertificateParametersInput,
		$smtp: SMTPParametersInput, $resolver: ResolverParametersInput,
		$session: SessionParametersInput, $passkey: PasskeyParametersInput,
		$listen: ListenParametersInput, $sso: SSOParametersInput, $identity: IdentityParametersInput,
		$storage: StorageParametersInput, $geoip: GeoIPParametersInput,
		$upgrade: UpgradeParametersInput, $agent: AgentParametersInput
	) {
		UpdateSettings(
			s3: $s3, route53: $route53, antivirus: $antivirus, antispam: $antispam,
			relay: $relay, submission: $submission, imap: $imap, proxy: $proxy, certificates: $certificates,
			smtp: $smtp, resolver: $resolver, session: $session, passkey: $passkey,
			listen: $listen, sso: $sso, identity: $identity, storage: $storage, geoip: $geoip,
			upgrade: $upgrade, agent: $agent
		) ` + settingsSelection + `
	}`
	if err := connection.Execute(ctx, query, sections, &result); err != nil {
		return nil, err
	}
	return result.UpdateSettings, nil
}
