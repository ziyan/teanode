package config

import (
	"os"
	"strings"
)

// Fields that older releases wrote and this one no longer uses.
//
// They are accepted rather than rejected, because refusing to start on a
// configuration that was valid yesterday is a poor way to treat somebody
// upgrading. Where the old value still means something it is migrated; either
// way the field is reported once and disappears the next time the file is
// written.
//
// There is one struct per place rather than one shared struct, because an
// inlined field that collides with a real one is a duplicate YAML key and the
// encoder refuses it — which is exactly what happened when a deprecated
// "enabled" met tls.acme.enabled.

// deprecatedSession covers what used to be the dashboard section.
type deprecatedSession struct {
	// The dashboard is always served now; listen.http and listen.https decide
	// where.
	Enabled *bool `yaml:"enabled,omitempty"`

	// The key lives in session.key.
	SessionKeyFile string `yaml:"sessionKeyFile,omitempty"`

	// Renamed to session.lifetime.
	SessionLifetime Duration `yaml:"sessionLifetime,omitempty"`
}

func (self *deprecatedSession) empty() bool {
	return self.Enabled == nil && self.SessionKeyFile == "" &&
		self.SessionLifetime == 0
}

// deprecatedDkim covers the single signing key that every domain shared.
type deprecatedDkim struct {
	PrivateKeyFile string   `yaml:"privateKeyFile,omitempty"`
	Domain         string   `yaml:"domain,omitempty"`
	Selectors      []string `yaml:"selectors,omitempty"`
}

func (self *deprecatedDkim) empty() bool {
	return self.PrivateKeyFile == "" && self.Domain == "" && len(self.Selectors) == 0
}

// deprecatedAcme covers the account key and certificate that were files.
type deprecatedAcme struct {
	AccountKeyFile  string `yaml:"accountKeyFile,omitempty"`
	CertificateFile string `yaml:"certificateFile,omitempty"`
	PrivateKeyFile  string `yaml:"privateKeyFile,omitempty"`
}

func (self *deprecatedAcme) empty() bool {
	return self.AccountKeyFile == "" && self.CertificateFile == "" && self.PrivateKeyFile == ""
}

// deprecatedLimits covers the caps on how often or how much the agent's work
// runs. The daily and monthly money limits bound spend whatever starts the
// work, so these are gone.
type deprecatedLimits struct {
	RetiredGoalTurnsPerDay int `yaml:"goalTurnsPerDay,omitempty"`
	RetiredGoalsInProgress int `yaml:"goalsInProgress,omitempty"`

	RetiredGoalShare             float64 `yaml:"goalShare,omitempty"`
	RetiredDreamShare            float64 `yaml:"dreamShare,omitempty"`
	RetiredGoalTurnsAlone        int     `yaml:"goalTurnsAlone,omitempty"`
	RetiredBackgroundWakesAlone  int     `yaml:"backgroundWakesAlone,omitempty"`
	RetiredMaxRoundsPerAsk       int     `yaml:"maxRoundsPerAsk,omitempty"`
	RetiredMaxRoundsPerResearch  int     `yaml:"maxRoundsPerResearch,omitempty"`
	RetiredMaxRoundsPerReply     int     `yaml:"maxRoundsPerReply,omitempty"`
	RetiredMaxRoundsPerTriage    int     `yaml:"maxRoundsPerTriage,omitempty"`
	RetiredMaxRoundsPerDream     int     `yaml:"maxRoundsPerDream,omitempty"`
	RetiredMaxRoundsPerSubagent  int     `yaml:"maxRoundsPerSubagent,omitempty"`
	RetiredMaxToolCallsPerRun    int     `yaml:"maxToolCallsPerRun,omitempty"`
	RetiredEmbeddingTokensPerDay int64   `yaml:"embeddingTokensPerDay,omitempty"`

	// Technical bounds that were settings: now constants, or gone.
	RetiredMaxBodyCharacters             int      `yaml:"maxBodyCharacters,omitempty"`
	RetiredScanConcurrency               int      `yaml:"scanConcurrency,omitempty"`
	RetiredRecallTokens                  int      `yaml:"recallTokens,omitempty"`
	RetiredDigestDocumentRunes           int      `yaml:"digestDocumentRunes,omitempty"`
	RetiredRewriteConcurrency            int      `yaml:"rewriteConcurrency,omitempty"`
	RetiredIngestChunksPerRun            int      `yaml:"ingestChunksPerRun,omitempty"`
	RetiredRequestTimeout                Duration `yaml:"requestTimeout,omitempty"`
	RetiredMaxRecordingMediaBytes        ByteSize `yaml:"maxRecordingMediaBytes,omitempty"`
	RetiredMaxRecordingAccountBytes      ByteSize `yaml:"maxRecordingAccountBytes,omitempty"`
	RetiredMaxAttachmentBytes            ByteSize `yaml:"maxAttachmentBytes,omitempty"`
	RetiredMaxSentFileBytes              ByteSize `yaml:"maxSentFileBytes,omitempty"`
	RetiredMaxChatAttachmentAccountBytes ByteSize `yaml:"maxChatAttachmentAccountBytes,omitempty"`
	RetiredMaxScannedAttachmentBytes     ByteSize `yaml:"maxScannedAttachmentBytes,omitempty"`
	RetiredConcurrency                   int      `yaml:"concurrency,omitempty"`
}

// deprecatedAgent covers agent.retention, which is a pair of constants now
// (AgentRunRetention and AgentCorrectionRetention).
type deprecatedAgent struct {
	Retention struct {
		Runs        Duration `yaml:"runs,omitempty"`
		Corrections Duration `yaml:"corrections,omitempty"`
	} `yaml:"retention,omitempty"`
}

// deprecatedBrowser covers the browser bounds that were settings, and are
// BrowserIdleTimeout and BrowserMaxContexts now.
type deprecatedBrowser struct {
	RetiredIdleTimeout Duration `yaml:"idleTimeout,omitempty"`
	RetiredMaxContexts int      `yaml:"maxContexts,omitempty"`
}

// retiredLimitNotices lists each retired key that is set, with the line
// that says where it went.
func (self *deprecatedLimits) retiredLimitNotices() []string {
	const reason = "is no longer used: the daily money limit bounds this"
	var notices []string
	add := func(isSet bool, key, why string) {
		if isSet {
			notices = append(notices, "agent.limits."+key+" "+why)
		}
	}
	add(self.RetiredGoalTurnsPerDay > 0, "goalTurnsPerDay", reason)
	add(self.RetiredGoalsInProgress > 0, "goalsInProgress", reason)
	add(self.RetiredGoalShare > 0, "goalShare", reason)
	add(self.RetiredDreamShare > 0, "dreamShare", reason)
	add(self.RetiredGoalTurnsAlone > 0, "goalTurnsAlone", reason)
	add(self.RetiredBackgroundWakesAlone > 0, "backgroundWakesAlone", reason)
	add(self.RetiredMaxRoundsPerAsk > 0, "maxRoundsPerAsk", reason)
	add(self.RetiredMaxRoundsPerResearch > 0, "maxRoundsPerResearch", reason)
	add(self.RetiredMaxRoundsPerReply > 0, "maxRoundsPerReply", reason)
	add(self.RetiredMaxRoundsPerTriage > 0, "maxRoundsPerTriage", reason)
	add(self.RetiredMaxRoundsPerDream > 0, "maxRoundsPerDream", reason)
	add(self.RetiredMaxRoundsPerSubagent > 0, "maxRoundsPerSubagent", reason)
	add(self.RetiredMaxToolCallsPerRun > 0, "maxToolCallsPerRun", reason)
	add(self.RetiredEmbeddingTokensPerDay > 0, "embeddingTokensPerDay", reason)

	const fixed = "is no longer a setting: it is a fixed technical bound now, and the daily and monthly money limits are the only limits"
	add(self.RetiredMaxBodyCharacters > 0, "maxBodyCharacters", fixed)
	add(self.RetiredScanConcurrency > 0, "scanConcurrency", fixed)
	add(self.RetiredRecallTokens > 0, "recallTokens", fixed)
	add(self.RetiredRewriteConcurrency > 0, "rewriteConcurrency", fixed)
	add(self.RetiredIngestChunksPerRun > 0, "ingestChunksPerRun", fixed)
	add(self.RetiredMaxRecordingMediaBytes > 0, "maxRecordingMediaBytes", fixed)
	add(self.RetiredMaxRecordingAccountBytes > 0, "maxRecordingAccountBytes", fixed)
	add(self.RetiredMaxAttachmentBytes > 0, "maxAttachmentBytes", fixed)
	add(self.RetiredMaxSentFileBytes > 0, "maxSentFileBytes", fixed)
	add(self.RetiredMaxChatAttachmentAccountBytes > 0, "maxChatAttachmentAccountBytes", fixed)
	add(self.RetiredMaxScannedAttachmentBytes > 0, "maxScannedAttachmentBytes", fixed)
	add(self.RetiredConcurrency > 0, "concurrency", fixed)
	add(self.RetiredDigestDocumentRunes > 0, "digestDocumentRunes", "is no longer used: every document is read whole")
	add(self.RetiredRequestTimeout > 0, "requestTimeout", "is no longer used: an answer has no total time limit, a streamed one is cut when nothing arrives for 3 minutes, and one that does not stream gets 15 minutes")
	return notices
}

func (self *deprecatedLimits) empty() bool {
	return *self == deprecatedLimits{}
}

// migrateDeprecated adopts what it can from the older layout and says what it
// did, so an operator is not left wondering where a setting went.
func (self *Configuration) migrateDeprecated() {
	self.migrateSession()
	self.migrateDomainKey()
	self.migrateAcme()
	self.migrateGoalLimits()
	self.migrateRetiredAgentBounds()
}

// migrateRetiredAgentBounds reports the retention and browser keys that are
// constants now.
func (self *Configuration) migrateRetiredAgentBounds() {
	const fixed = "is no longer a setting: it is a fixed technical bound now"
	retention := self.Agent.Retention
	if retention.Runs > 0 {
		log.Warningf("agent.retention.runs %s", fixed)
	}
	if retention.Corrections > 0 {
		log.Warningf("agent.retention.corrections %s", fixed)
	}
	self.Agent.deprecatedAgent = deprecatedAgent{}

	browser := self.Agent.Browser.deprecatedBrowser
	if browser.RetiredIdleTimeout > 0 {
		log.Warningf("agent.browser.idleTimeout %s", fixed)
	}
	if browser.RetiredMaxContexts > 0 {
		log.Warningf("agent.browser.maxContexts %s", fixed)
	}
	self.Agent.Browser.deprecatedBrowser = deprecatedBrowser{}
}

func (self *Configuration) migrateGoalLimits() {
	outdated := &self.Agent.Limits.deprecatedLimits
	if outdated.empty() {
		return
	}
	for _, notice := range outdated.retiredLimitNotices() {
		log.Warningf("%s", notice)
	}
	self.Agent.Limits.deprecatedLimits = deprecatedLimits{}
}

func (self *Configuration) migrateSession() {
	outdated := &self.Session.deprecatedSession
	if outdated.empty() {
		return
	}

	if outdated.SessionLifetime > 0 {
		log.Warningf("dashboard.sessionLifetime is now session.lifetime")
		if self.Session.Lifetime == 0 {
			self.Session.Lifetime = outdated.SessionLifetime
		}
	}
	if outdated.Enabled != nil {
		log.Warningf("dashboard.enabled is no longer used; the dashboard is always served, and listen.http and listen.https decide where")
	}
	if filename := outdated.SessionKeyFile; filename != "" {
		log.Warningf("dashboard.sessionKeyFile is no longer used; the key now lives in session.key")
		if self.Session.Key == "" {
			if content := readFile(self.Path(filename)); content != "" {
				self.Session.Key = strings.TrimSpace(content)
				log.Noticef("adopted the session key from %s; that file can be deleted", self.Path(filename))
			}
		}
	}

	self.Session.deprecatedSession = deprecatedSession{}
}

func (self *Configuration) migrateDomainKey() {
	outdated := &self.DKIM.deprecatedDkim
	if outdated.empty() {
		return
	}

	// The old key was one file shared by every domain. Give it to any domain
	// that has none of its own, so signing keeps working with the DNS records
	// already published.
	if outdated.PrivateKeyFile != "" {
		log.Warningf("dkim.privateKeyFile is no longer used; each domain has its own key, generated when the domain is created")
	}
	if len(outdated.Selectors) > 0 {
		log.Warningf("dkim.selectors is no longer used; a domain publishes the one selector in domains[].dkim.selector")
	}
	if outdated.Domain != "" {
		log.Warningf("dkim.domain is no longer used; the server signs each domain's mail with that domain's own key")
	}

	self.DKIM.deprecatedDkim = deprecatedDkim{}
}

func (self *Configuration) migrateAcme() {
	outdated := &self.TLS.ACME.deprecatedAcme
	if outdated.empty() {
		return
	}

	if filename := outdated.AccountKeyFile; filename != "" {
		log.Warningf("tls.acme.accountKeyFile is no longer used; the account key now lives in tls.acme.accountKey")
		if self.TLS.ACME.AccountKey == "" {
			if content := readFile(self.Path(filename)); content != "" {
				self.TLS.ACME.AccountKey = content
				log.Noticef("adopted the ACME account key from %s; that file can be deleted", self.Path(filename))
			}
		}
	}

	// The issued certificate is adopted too, so an upgrade does not ask the
	// certificate authority for another one it does not need.
	if outdated.CertificateFile != "" && outdated.PrivateKeyFile != "" {
		log.Warningf("tls.acme.certificateFile and privateKeyFile are no longer used; the issued certificate now lives in tls.acme.certificate")
		certificate := readFile(self.Path(outdated.CertificateFile))
		privateKey := readFile(self.Path(outdated.PrivateKeyFile))
		if certificate != "" && privateKey != "" && self.TLS.ACME.Certificate == "" {
			self.TLS.ACME.Certificate = certificate
			self.TLS.ACME.PrivateKey = privateKey
			log.Noticef("adopted the issued certificate; those files can be deleted")
		}
	}

	self.TLS.ACME.deprecatedAcme = deprecatedAcme{}
}

func readFile(filename string) string {
	content, err := os.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(content)
}
