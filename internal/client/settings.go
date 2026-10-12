package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
		enabled instructions currency effort allowPrivateAddresses skipCertificateCheck
		providers { name kind baseUrl hasApiKey enabled allow deny pricingInput pricingOutput pricingCacheRead pricingCacheWrite modelPricing { model input output cacheRead cacheWrite } }
		skillSecrets { skill key hasValue }
		models { default fast quick embedding triage research summarize reply ask schedule compact choices scan synthesize decide categorize embeddingDimensions }
		features { triage summaries draftReplies search research autoReply ask schedules browser connectedServers computer chatApps skills subagents remember knowledge dreaming }
		limits { maxBodyCharacters dailyTokensPerAgent monthlyTokensPerServer dailyCostPerAgent monthlyCostPerServer maxRoundsPerAsk maxRoundsPerResearch maxRoundsPerReply maxRoundsPerDream maxToolCallsPerRun requestTimeout concurrency embeddingTokensPerDay dreamShare ingestChunksPerRun maxRoundsPerSubagent goalTurnsPerDay goalTurnsAlone goalsInProgress backgroundWakesAlone }
		retention { runs corrections }
		search { kind hasApiKey }
		voice { enabled provider transcriptionModel silenceMS speechModel speechVoice askModel }
		tools { disabled confirm }
		browser { enabled cdpEndpoint attachTabs allowPrivateAddresses idleTimeout maxContexts }
		mcpServers { name transport effectiveTransport url command args envNames workingDir auth effectiveAuth hasAuthorization oauthClientId hasOauthClientSecret oauthScopes oauthAuthorizationUrl oauthTokenUrl headless readOnly disabled timeout enabled }
		finance { offeredProviders plaid { environment clientId hasSecret countryCodes products } }
		push { enabled relayUrl relay { enabled apnsKeyId apnsTeamId hasApnsKey apnsTopic } }
		works families kinds
	}
}`

// DocumentGetSettings reads every section of the settings.
const DocumentGetSettings = `query { GetSettings ` + settingsSelection + ` }`

// DocumentGetRecordingSettings reads limits introduced with resumable media uploads.
const DocumentGetRecordingSettings = `query { GetSettings { agent { limits { maxRecordingMediaBytes maxRecordingAccountBytes } } } }`

// GetSettings returns the optional integrations. Secrets are never returned;
// a field that is set reads back as whether it is set.
func GetSettings(ctx context.Context, connection *Client) (Settings, error) {
	var result struct {
		GetSettings Settings `json:"GetSettings"`
	}
	if err := connection.Execute(ctx, DocumentGetSettings, nil, &result); err != nil {
		return nil, err
	}
	if err := readRecordingSettings(ctx, connection, result.GetSettings); err != nil {
		return nil, err
	}
	if err := readTranscriptionSettings(ctx, connection, result.GetSettings); err != nil {
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
	if err := readRecordingSettings(ctx, connection, result.UpdateSettings); err != nil {
		return nil, err
	}
	if err := readTranscriptionSettings(ctx, connection, result.UpdateSettings); err != nil {
		return nil, err
	}
	return result.UpdateSettings, nil
}

// readRecordingSettings keeps existing settings commands compatible with older
// servers while exposing the new limits through the same CLI response.
func readRecordingSettings(ctx context.Context, connection *Client, settings Settings) error {
	var additional struct {
		GetSettings Settings `json:"GetSettings"`
	}
	if err := connection.Execute(ctx, DocumentGetRecordingSettings, nil, &additional); err != nil {
		if strings.Contains(err.Error(), "Cannot query field") && (strings.Contains(err.Error(), "maxRecordingMediaBytes") || strings.Contains(err.Error(), "maxRecordingAccountBytes")) {
			return nil
		}
		return err
	}
	var agentValues map[string]json.RawMessage
	var additionalAgent map[string]json.RawMessage
	if err := json.Unmarshal(settings["agent"], &agentValues); err != nil {
		return err
	}
	if err := json.Unmarshal(additional.GetSettings["agent"], &additionalAgent); err != nil {
		return err
	}
	var limitsValues map[string]any
	var additionalLimits map[string]any
	if err := json.Unmarshal(agentValues["limits"], &limitsValues); err != nil {
		return err
	}
	if err := json.Unmarshal(additionalAgent["limits"], &additionalLimits); err != nil {
		return err
	}
	if agentValues == nil || limitsValues == nil || additionalLimits == nil {
		return fmt.Errorf("client: recording settings response is incomplete")
	}
	for fieldName, fieldValue := range additionalLimits {
		limitsValues[fieldName] = fieldValue
	}
	limitsJSON, err := json.Marshal(limitsValues)
	if err != nil {
		return err
	}
	agentValues["limits"] = limitsJSON
	agentJSON, err := json.Marshal(agentValues)
	if err != nil {
		return err
	}
	settings["agent"] = agentJSON
	return nil
}

// DocumentGetTranscriptionSettings keeps recording model reads compatible with older servers.
const DocumentGetTranscriptionSettings = `query { GetSettings { agent { models { transcription } } } }`

func readTranscriptionSettings(ctx context.Context, connection *Client, settings Settings) error {
	var additional struct {
		GetSettings Settings `json:"GetSettings"`
	}
	if err := connection.Execute(ctx, DocumentGetTranscriptionSettings, nil, &additional); err != nil {
		if strings.Contains(err.Error(), "Cannot query field") && strings.Contains(err.Error(), "transcription") {
			return nil
		}
		return err
	}
	var agentValues, additionalAgent map[string]json.RawMessage
	if err := json.Unmarshal(settings["agent"], &agentValues); err != nil {
		return err
	}
	if err := json.Unmarshal(additional.GetSettings["agent"], &additionalAgent); err != nil {
		return err
	}
	var modelValues, additionalModels map[string]json.RawMessage
	if err := json.Unmarshal(agentValues["models"], &modelValues); err != nil {
		return err
	}
	if err := json.Unmarshal(additionalAgent["models"], &additionalModels); err != nil {
		return err
	}
	if agentValues == nil || modelValues == nil || additionalModels == nil {
		return fmt.Errorf("client: recording model settings response is incomplete")
	}
	for fieldName, fieldValue := range additionalModels {
		modelValues[fieldName] = fieldValue
	}
	modelsJSON, err := json.Marshal(modelValues)
	if err != nil {
		return err
	}
	agentValues["models"] = modelsJSON
	agentJSON, err := json.Marshal(agentValues)
	if err != nil {
		return err
	}
	settings["agent"] = agentJSON
	return nil
}
