package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/api/v1api"
	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/configdb"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/dns"
	"github.com/ziyan/teanode/internal/frontend"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/util/autoacme"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/clamav"
	"github.com/ziyan/teanode/internal/util/debugutil"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/dropper"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/periodic"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/util/resolver"
	"github.com/ziyan/teanode/internal/util/smtpc"
	"github.com/ziyan/teanode/internal/util/smtpd"
	"github.com/ziyan/teanode/internal/util/spamc"
	"github.com/ziyan/teanode/internal/version"
	"github.com/ziyan/teanode/internal/web"
)

// shutdownTimeout bounds a graceful shutdown. Beyond it the process is killed,
// because a mail server that will not exit blocks a restart, and the queue is
// on disk anyway.
const shutdownTimeout = 30 * time.Second

// NewRunCommand builds "teanode run", the server itself.
func NewRunCommand() *cli.Command {
	return &cli.Command{
		Name:   "run",
		Usage:  "run the mail server",
		Action: runServer,
	}
}

// runServer runs the server and, when an upgrade has put a new binary in
// place, becomes it.
//
// The exec is out here rather than at the end of serve because a process image
// replaced by exec never returns: every deferred close inside — the mailer,
// the queue, the database pool, the storage client — would be skipped, and
// in-flight deliveries abandoned mid-flight. Out here, all of that has already
// run.
func runServer(ctx context.Context, command *cli.Command) error {
	target, err := serveUntilStopped(ctx, command)
	if err != nil {
		return err
	}
	if target == "" {
		return nil
	}

	log.Noticef("restarting into %s", target)
	if err := upgrade.Restart(target); err != nil {
		// Exec only replaces this image when it succeeds, so there is still
		// somebody here to say so. Exiting cleanly is then the fallback: a
		// supervisor starts a new one, and the staged binary is found at the
		// next start anyway.
		log.Errorf("could not run the upgraded binary, so this process is exiting for whatever supervises it: %s", err)
	}
	return nil
}

// serveUntilStopped is the run, and returns the binary this process should
// become afterwards, if an upgrade staged one.
func serveUntilStopped(ctx context.Context, command *cli.Command) (string, error) {
	// The environment says how to reach the database. Everything else is in
	// the database, so this is the only thing that has to be told to each
	// instance separately.
	bootstrapped, err := bootstrap.Load()
	if err != nil {
		return "", err
	}

	// A binary an upgrade staged, if there is one, before anything at all is
	// opened. This is what makes an upgrade survive a container recreate: the
	// image still carries the old binary, and this reaches past it.
	//
	// It has to come before the database. Migrate reverts migrations it does
	// not recognise, so the image's older binary opening the database first
	// would drop the columns the newer one added — and the data in them —
	// seconds before handing over to it. It does not return when it finds a
	// binary to run.
	upgrade.ExecStagedIfNewer(bootstrapped.UpgradeDirectory, version.Version())

	database, closeDatabase, err := openDatabase(bootstrapped)
	if err != nil {
		return "", err
	}
	defer closeDatabase()

	seeded, err := configdb.Initialize(database, bootstrapped.SeedConfiguration)
	if err != nil {
		return "", err
	}

	store, err := configdb.Open(database, bootstrapped.Database)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = store.Close()
	}()

	if !seeded {
		// Variables that would have described a new server did nothing here.
		// Saying so is the difference between an operator editing their
		// compose file and wondering why nothing changed, and knowing where
		// to change it instead.
		bootstrapped.ReportIgnoredSeed(store.Current())
	}

	configuration := store.Current()
	if err := configuration.ValidateFiles(); err != nil {
		return "", err
	}

	// A level given on the command line has already been applied and wins;
	// otherwise the configured level takes effect now.
	if command.Root().String("log-level") == "" {
		SetLogLevel(configuration.Server.LogLevel)
	}
	log.Noticef("starting teanode %s as instance %q", version.String(), bootstrapped.InstanceID)

	if err := configuration.EnsureDataDirectory(); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", configuration.DataDirectory(), err)
	}

	// Generated secrets are stored with the rest of the configuration, so
	// that an instance joining later derives the same SMTP passwords and
	// accepts the same sessions as the ones already running.
	if err := config.EnsureSecrets(store); err != nil {
		return "", err
	}

	// An installation upgraded from a release that stored the signing keys in
	// the clear rewrites them once, here, rather than waiting for whatever
	// unrelated edit would otherwise have done it.
	if err := configdb.EnsureSealed(database, store); err != nil {
		return "", err
	}
	configuration = store.Current()
	secret := configuration.Secret()

	server, err := openServer(store, database, secret, bootstrapped.InstanceID, bootstrapped.UpgradeDirectory)
	if err != nil {
		return "", err
	}
	defer server.close()

	if err := server.serve(ctx); err != nil {
		return "", err
	}
	// Read before the deferred closes run, and acted on by the caller after
	// they have.
	return server.execTarget(), nil
}

// openDatabase connects and migrates, before there is any configuration to
// read, because the configuration is in there.
func openDatabase(bootstrapped *bootstrap.Bootstrap) (db.Database, func(), error) {
	database, closeDatabase, err := openBootstrapDatabase(bootstrapped)
	if err != nil {
		return nil, nil, err
	}
	if err := migrate(database, bootstrapped.UpgradeDirectory); err != nil {
		closeDatabase()
		return nil, nil, err
	}
	return database, closeDatabase, nil
}

// server holds everything constructed for one run, so that shutdown can undo
// it in the reverse order.
type server struct {
	store    config.Store
	secret   []byte
	instance string

	// upgradeDirectory is where a staged binary goes and where the next start
	// looks for one. From the environment, because that next start has to
	// find it before it can read anything from the database.
	upgradeDirectory string

	// restarter ends this process when the dashboard asks. Its trigger closes
	// restartRequested, which serve is waiting on.
	restarter        *api.Restarter
	restartRequested chan struct{}

	// upgrader knows what has been released and, after an upgrade, what this
	// process should become. Read at the end of serve, once everything is
	// drained: that is the only safe moment to replace the process image.
	upgrader upgrade.Manager

	closers  []func()
	database db.Database
	acme     autoacme.Manager
	exchange mx.Exchange
	storage  storage.Storage
	locator  geoip.Locator
	resolver resolver.Resolver
	dropper  dropper.Dropper
	handler  http.Handler

	listeners struct {
		smtpIncoming net.Listener
		smtpOutgoing net.Listener
		http         net.Listener
		https        net.Listener
	}
}

func (self *server) defer_(close func()) {
	self.closers = append(self.closers, close)
}

func (self *server) close() {
	for index := len(self.closers) - 1; index >= 0; index-- {
		self.closers[index]()
	}
}

func openServer(store config.Store, database db.Database, secret []byte, instance, upgradeDirectory string) (*server, error) {
	configuration := store.Current()
	self := &server{
		store:            store,
		secret:           secret,
		database:         database,
		instance:         instance,
		upgradeDirectory: upgradeDirectory,
		restartRequested: make(chan struct{}),
	}
	// The trigger only asks; serve does the shutting down, in the same place
	// and the same order as a signal would, so a restart and a SIGTERM leave
	// the queue in the same state.
	self.restarter = api.NewRestarter(func() {
		close(self.restartRequested)
	})

	success := false
	defer func() {
		if !success {
			self.close()
		}
	}()

	// Debugging endpoint first, so that a server which hangs during startup
	// can still be inspected.
	if configuration.Listen.Debug != "" {
		stopDebugServer, err := debugutil.RunDebugServer(configuration.Listen.Debug)
		if err != nil {
			return nil, err
		}
		self.defer_(stopDebugServer)
	}

	if err := self.listen(configuration); err != nil {
		return nil, err
	}

	if err := self.openCertificates(configuration); err != nil {
		return nil, err
	}

	for _, domain := range configuration.Domains {
		var enabled int
		for _, alias := range domain.Aliases {
			if !alias.Disabled {
				enabled++
			}
		}
		if enabled == 0 {
			log.Warningf("domain %q has no enabled alias, so mail for it will be refused; add one with the dashboard or in the configuration file", domain.Domain)
		}
	}

	if self.acme == nil && configuration.TLS.CertificateFile == "" {
		log.Warningf("no certificate configured: SMTP will not offer STARTTLS and mail to and from this server will cross the network in the clear; enable tls.acme or set tls.certificateFile")
	}

	self.locator = openLocator(configuration)
	self.resolver = resolver.New()

	antispamClient, err := openAntispam(configuration)
	if err != nil {
		return nil, err
	}
	if antispamClient != nil {
		self.defer_(func() {
			if err := antispamClient.Close(); err != nil {
				log.Errorf("failed to close spamassassin: %s", err)
			}
		})
	}

	antivirusClient, err := openAntivirus(configuration)
	if err != nil {
		return nil, err
	}
	if antivirusClient != nil {
		self.defer_(func() {
			if err := antivirusClient.Close(); err != nil {
				log.Errorf("failed to close clamav: %s", err)
			}
		})
	}

	if err := self.openStorage(configuration); err != nil {
		return nil, err
	}

	if err := self.openExchange(configuration, antispamClient, antivirusClient); err != nil {
		return nil, err
	}

	if err := self.openWeb(configuration); err != nil {
		return nil, err
	}

	self.dropper, err = dropper.Open()
	if err != nil {
		return nil, fmt.Errorf("cannot open the drop list: %w", err)
	}
	self.defer_(func() {
		if err := self.dropper.Close(); err != nil {
			log.Errorf("failed to close drop list: %s", err)
		}
	})

	success = true
	return self, nil
}

func (self *server) listen(configuration *config.Configuration) error {
	listeners := []struct {
		name    string
		address string
		target  *net.Listener
	}{
		{"incoming smtp", configuration.Listen.SMTPIncoming, &self.listeners.smtpIncoming},
		{"outgoing smtp", configuration.Listen.SMTPOutgoing, &self.listeners.smtpOutgoing},
		{"http", configuration.Listen.HTTP, &self.listeners.http},
		{"https", configuration.Listen.HTTPS, &self.listeners.https},
	}
	for _, entry := range listeners {
		if entry.address == "" {
			continue
		}
		listener, err := net.Listen("tcp", entry.address)
		if err != nil {
			return fmt.Errorf("cannot listen for %s on %s: %w", entry.name, entry.address, err)
		}
		log.Noticef("listening for %s on %s", entry.name, entry.address)
		*entry.target = listener
		self.defer_(func() {
			_ = listener.Close()
		})
	}
	return nil
}

// serverCertificateKey identifies the server's own certificate, as opposed to
// one belonging to a domain. Empty, because a domain's key is its identifier
// and no domain has an empty one.
const serverCertificateKey = ""

func (self *server) openCertificates(configuration *config.Configuration) error {
	if !configuration.TLS.ACME.Enabled {
		return nil
	}

	settings := &autoacme.Settings{
		ACMEEmail:    configuration.TLS.ACME.Email,
		Challenge:    configuration.TLS.ACME.Challenge,
		DirectoryURL: configuration.TLS.ACME.DirectoryURL,

		// The account key and the issued certificates live in the
		// configuration, so that a copy of that one file is a working server.
		AccountKey: configuration.TLS.ACME.AccountKey,

		// The server's own certificate first, because the first is what a
		// client that sends no name at all is served.
		Certificates: []autoacme.CertificateRequest{{
			Key:         serverCertificateKey,
			Hosts:       configuration.TLS.Hosts,
			Certificate: configuration.TLS.ACME.Certificate,
			PrivateKey:  configuration.TLS.ACME.PrivateKey,
		}},
		SaveAccountKey: func(key string) error {
			return self.store.Update(func(configuration *config.Configuration) error {
				configuration.TLS.ACME.AccountKey = key
				return nil
			})
		},
		SaveCertificate: func(key, certificate, privateKey string) error {
			return self.store.Update(func(configuration *config.Configuration) error {
				if key == serverCertificateKey {
					configuration.TLS.ACME.Certificate = certificate
					configuration.TLS.ACME.PrivateKey = privateKey
					return nil
				}
				domain := configuration.FindDomainByID(key)
				if domain == nil {
					// The domain was removed while its certificate was being
					// obtained. Nothing to keep it on, and nothing to fix.
					return nil
				}
				domain.TLS.Certificate = certificate
				domain.TLS.PrivateKey = privateKey
				return nil
			})
		},
	}
	// One certificate per domain, so a sender connecting to a domain's own
	// mail server name is handed a certificate for the name it asked for
	// rather than one naming a domain it has never heard of.
	//
	// Only for a domain whose name differs from the server's own: the domain
	// the server is named under is already covered by the certificate above,
	// and asking for a second one for the same name would spend rate limit to
	// obtain a duplicate.
	if configuration.TLS.ACME.PerDomain {
		for _, domain := range configuration.Domains {
			if domain == nil || domain.Domain == "" {
				continue
			}
			var hosts []string
			for _, host := range configuration.MailHostsFor(domain) {
				// Only names in this domain's own zone. A domain pointing at
				// a name somebody else owns is served that owner's
				// certificate, which is correct: it is their name.
				if !domain.InThisDomain(host) {
					continue
				}
				// Not one the server's own certificate already covers, which
				// for a wildcard is most of the names under its domain.
				if !autoacme.Covers(configuration.TLS.Hosts, strings.TrimSuffix(host, ".")) {
					hosts = append(hosts, host)
				}
			}
			if len(hosts) == 0 {
				continue
			}
			settings.Certificates = append(settings.Certificates, autoacme.CertificateRequest{
				Key:   domain.ID,
				Hosts: hosts,
				// Always http-01, whatever the server's own certificate uses.
				// dns-01 needs credentials for the zone the name lives in and
				// the solver is configured with one zone, so it can prove the
				// server's own names and nobody else's; tls-alpn-01 would need
				// port 443 to be the mail server's. http-01 needs nothing but
				// the name resolving here, which it does — it is the same
				// record the MX points at.
				Challenge:   "http-01",
				Certificate: domain.TLS.Certificate,
				PrivateKey:  domain.TLS.PrivateKey,
			})
		}
	}

	if configuration.TLS.ACME.Route53.Enabled {
		route53 := configuration.TLS.ACME.Route53
		awsConfig, err := loadAWSConfig(route53.Region, route53.AccessKeyID, route53.SecretAccessKey, configuration.Path(route53.CredentialsFile))
		if err != nil {
			return fmt.Errorf("cannot load AWS configuration for the Route53 challenge solver: %w", err)
		}
		settings.Route53ZoneID = configuration.TLS.ACME.Route53.ZoneID
		settings.Route53Nameservers = configuration.TLS.ACME.Route53.Nameservers
		settings.AWSConfig = awsConfig
	}

	manager, err := autoacme.Open(settings)
	if err != nil {
		return fmt.Errorf("cannot set up automatic certificates: %w", err)
	}
	self.acme = manager
	self.defer_(func() {
		if err := manager.Close(); err != nil {
			log.Errorf("failed to close acme: %s", err)
		}
	})
	return nil
}

// tlsConfig returns the TLS configuration used by both the SMTP listeners and
// the HTTPS server: an ACME managed certificate, or the operator's own files.
func (self *server) tlsConfig(configuration *config.Configuration) (*tls.Config, error) {
	if self.acme != nil {
		return &tls.Config{GetCertificate: self.acme.GetCertificate}, nil
	}
	if configuration.TLS.CertificateFile == "" {
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(
		configuration.Path(configuration.TLS.CertificateFile),
		configuration.Path(configuration.TLS.PrivateKeyFile),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot load the certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{certificate}}, nil
}

// relaySettings turns the configured relay into what the mail path needs, or
// nil when mail is delivered by MX lookup.
func relaySettings(configuration *config.Configuration) *mx.RelaySettings {
	relay := configuration.SMTP.Relay
	if !relay.Enabled {
		return nil
	}

	settings := &mx.RelaySettings{
		Host:     relay.Host,
		Port:     relay.Port,
		Username: relay.Username,
		Password: relay.Password,
	}
	switch relay.Security {
	case config.RelaySecurityTLS:
		settings.TLS = smtpc.TLSImplicit
	case config.RelaySecurityNone:
		settings.TLS = smtpc.TLSOpportunistic
	default:
		settings.TLS = smtpc.TLSRequired
	}

	log.Noticef("outgoing mail is relayed through %s (%s), not delivered by MX lookup",
		settings.Address(), relay.Security)
	return settings
}

func (self *server) openStorage(configuration *config.Configuration) error {
	settings := &storage.Settings{
		Directory: configuration.Path(configuration.Storage.Directory),
		Retention: configuration.Storage.SpoolRetention.Duration(),
	}
	if configuration.Storage.S3.Enabled {
		settings.S3 = &storage.S3Settings{
			Bucket:          configuration.Storage.S3.Bucket,
			Region:          configuration.Storage.S3.Region,
			AccessKeyID:     configuration.Storage.S3.AccessKeyID,
			SecretAccessKey: configuration.Storage.S3.SecretAccessKey,
			CredentialsFile: configuration.Path(configuration.Storage.S3.CredentialsFile),
			Endpoint:        configuration.Storage.S3.Endpoint,
			PathStyle:       configuration.Storage.S3.PathStyle,
		}
	}

	opened, err := storage.Open(settings)
	if err != nil {
		return err
	}
	self.storage = opened
	self.defer_(func() {
		if err := opened.Close(); err != nil {
			log.Errorf("failed to close storage: %s", err)
		}
	})
	return nil
}

func (self *server) openExchange(configuration *config.Configuration, antispamClient spamc.Client, antivirusClient clamav.Client) error {
	settings := &mx.Settings{
		Server:          configuration.Server.Name,
		Service:         fmt.Sprintf("teanode/%s", version.Version()),
		MailServers:     configuration.MailServers(),
		Secret:          self.secret,
		LogDirectory:    configuration.Server.LogDirectory,
		SOCKS5Proxy:     configuration.SMTP.SOCKS5Proxy,
		DisableSendMail: configuration.SMTP.DisableSend,
		Relay:           relaySettings(configuration),
	}
	exchange, err := mx.Open(self.database, self.store, self.storage, self.resolver, antispamClient, antivirusClient, self.locator, settings)
	if err != nil {
		return err
	}
	self.exchange = exchange
	self.defer_(func() {
		if err := exchange.Close(); err != nil {
			log.Warningf("failed to close exchange: %s", err)
		}
	})
	return nil
}

func (self *server) openWeb(configuration *config.Configuration) error {
	if len(configuration.Users) == 0 {
		log.Warningf("this server has no account yet, so anyone who can reach the dashboard can claim it; open it and create one, or bind listen.http and listen.https to 127.0.0.1")
	}

	mailerComponent, err := mailer.New(self.database, self.store, self.exchange, nil)
	if err != nil {
		return fmt.Errorf("cannot create the mailer: %w", err)
	}
	self.defer_(func() {
		if err := mailerComponent.Close(); err != nil {
			log.Errorf("failed to close mailer: %s", err)
		}
	})

	verifier, err := dns.Open(self.store, &dns.Settings{
		Nameserver:    configuration.DNS.Nameserver,
		CheckInterval: configuration.DNS.CheckInterval.Duration(),
	})
	if err != nil {
		return fmt.Errorf("cannot create the DNS verifier: %w", err)
	}
	self.defer_(func() {
		if err := verifier.Close(); err != nil {
			log.Errorf("failed to close dns verifier: %s", err)
		}
	})

	authenticator, err := web.NewAuthenticator(self.store, self.database)
	if err != nil {
		return fmt.Errorf("cannot set up dashboard authentication: %w", err)
	}

	// Sessions and tokens expire and are revoked; without a sweep the rows
	// stay forever. Hourly, because what it removes is measured in days.
	scavengeContext, stopScavenging := context.WithCancel(context.Background())
	var scavengeGroup sync.WaitGroup
	scavenger := periodic.New(scavengeContext, &scavengeGroup, func(context.Context) error {
		return authenticator.Scavenge()
	}, &periodic.Settings{
		Interval: time.Hour,
		Name:     "web:scavenge",
	})
	scavenger.Start()
	self.defer_(func() {
		scavenger.Stop()
		stopScavenging()
		scavengeGroup.Wait()
	})

	// Half-finished WebAuthn challenges. In this process unless a Redis is
	// configured: one instance is the ordinary case, and a challenge that does
	// not survive a restart costs one retry. Behind a load balancer it has to
	// be shared, because WebAuthn is two requests and the browser has no
	// reason to come back to the instance it started with.
	ceremonies := ceremony.NewMemoryStore()
	if address := strings.TrimSpace(configuration.Passkey.Redis.Address); address != "" {
		client := redis.NewClient(&redis.Options{
			Addr:     address,
			Username: configuration.Passkey.Redis.Username,
			Password: configuration.Passkey.Redis.Password,
			DB:       configuration.Passkey.Redis.Database,
		})
		self.defer_(func() {
			if err := client.Close(); err != nil {
				log.Errorf("failed to close the redis connection: %s", err)
			}
		})
		ceremonies = ceremony.NewRedisStore(client)
		log.Noticef("parking passkey ceremonies in redis at %s", address)
	}

	// What has been released since this was built, and — if it is turned on
	// and this deployment can — installing it. Built after the restarter,
	// because an upgrade ends in a restart and a manager with nothing to ask
	// for one would swap a binary and leave the old one running.
	upgrader, err := upgrade.New(self.store, self.restarter, self.upgradeDirectory)
	if err != nil {
		return err
	}
	self.upgrader = upgrader
	self.defer_(func() {
		if err := upgrader.Close(); err != nil {
			log.Errorf("failed to stop the upgrade checker: %s", err)
		}
	})

	apiComponent, err := v1api.New(self.database, self.store, self.storage, self.locator, verifier, mailerComponent, upgrader, authenticator, ceremonies, &api.Settings{
		Secret: self.secret,
		// The instance, not the server name: the name is the same on every
		// instance sharing this database, and this is the field that says
		// which process you are talking to.
		BackendID: self.instance,
		Restarter: self.restarter,
	})
	if err != nil {
		return fmt.Errorf("cannot create the API: %w", err)
	}

	webServer, err := web.NewServer(self.database, &web.Settings{}, []web.Component{
		apiComponent,
		web.NewStaticComponent(frontend.Handler()),
	})
	if err != nil {
		return fmt.Errorf("cannot create the web server: %w", err)
	}

	// Order matters. Authentication runs before the routes it protects, and
	// the ACME challenge path is exempt inside it because a certificate
	// authority arrives with no session.
	self.handler = web.ApplyMiddlewares(webServer,
		web.MakeServerNameMiddleware(fmt.Sprintf("teanode/%s", version.Version())),
		web.MakeSecurityHeadersMiddleware(frontend.InlineScriptHashes()),
		web.MakeAuthenticationMiddleware(authenticator, autoacme.ChallengePath),
		web.NoStoreMiddleware,
		web.LoggingMiddleware,
		web.CompressionMiddleware,
	)
	return nil
}

// withChallengeHandler puts the ACME http-01 handler in front of everything
// else on the plain HTTP listener. It has to come first: the certificate
// authority fetches the challenge over plain HTTP with no credentials, so it
// must not meet a redirect to HTTPS, authentication, or the dashboard's
// catch-all route.
//
// When a different challenge type is configured this returns the handler
// unchanged, and when the dashboard is disabled it returns a handler that
// serves only challenges.
func withChallengeHandler(handler http.Handler, manager autoacme.Manager) http.Handler {
	if manager == nil {
		return handler
	}
	challengeHandler := manager.HTTPHandler()
	if challengeHandler == nil {
		return handler
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, autoacme.ChallengePath) {
			challengeHandler.ServeHTTP(response, request)
			return
		}
		if handler == nil {
			http.NotFound(response, request)
			return
		}
		handler.ServeHTTP(response, request)
	})
}

// authLimiterAddresses caps how many addresses the submission limiter keeps
// buckets for at once. Bounded because the key is a remote address and there
// are more of those than there is memory.
const authLimiterAddresses = 8192

// startupOnly names the parts of the configuration that are read once, when
// the process builds what it needs, and are not re-read afterwards. Changing
// one takes a restart.
//
// Watched rather than assumed, because the configuration is shared now: it
// can change while this process is running, from the dashboard or from
// another instance, and a setting that appears to save and does nothing is
// worth an hour of somebody's afternoon.
func startupOnly(configuration *config.Configuration) map[string]any {
	return map[string]any{
		"listen":               configuration.Listen,
		"tls":                  configuration.TLS,
		"smtp.relay":           configuration.SMTP.Relay,
		"storage":              configuration.Storage,
		"server.dataDirectory": configuration.Server.DataDirectory,
		"antivirus":            configuration.Antivirus,
		"antispam":             configuration.Antispam,
		"geoip":                configuration.GeoIP,
		// Read once when the checker is built. Automatic and window are
		// re-read every cycle and are deliberately not here: changing those
		// takes effect without a restart, and listing them would ask for one
		// that is not needed.
		"upgrade.enabled":       configuration.Upgrade.Enabled,
		"upgrade.checkInterval": configuration.Upgrade.CheckInterval,
	}
}

// warnOnStartupOnlyChanges says which of those changed, once per change.
func (self *server) warnOnStartupOnlyChanges() func() {
	running := encodeSections(startupOnly(self.store.Current()))

	return self.store.Subscribe(func(configuration *config.Configuration) {
		current := encodeSections(startupOnly(configuration))

		var changed []string
		for name, encoded := range current {
			if running[name] != encoded {
				changed = append(changed, name)
			}
		}
		if len(changed) == 0 {
			return
		}
		sort.Strings(changed)

		// The new values are adopted as the ones to compare against, so that
		// this is said once for a change rather than on every reload
		// afterwards.
		running = current
		self.restarter.AddPending(changed...)
		log.Warningf("%s changed, and %s only read at startup; restart this instance to pick %s up",
			strings.Join(changed, ", "),
			plural(len(changed), "is", "are"),
			plural(len(changed), "it", "them"))
	})
}

func encodeSections(sections map[string]any) map[string]string {
	encoded := make(map[string]string, len(sections))
	for name, section := range sections {
		content, err := yaml.Marshal(section)
		if err != nil {
			// Cannot happen for these types, and a failure here must not stop
			// a server that is otherwise fine from running.
			log.Errorf("cannot compare the %s settings: %s", name, err)
			continue
		}
		encoded[name] = string(content)
	}
	return encoded
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

func (self *server) serve(ctx context.Context) error {
	configuration := self.store.Current()

	unsubscribe := self.warnOnStartupOnlyChanges()
	defer unsubscribe()

	tlsConfig, err := self.tlsConfig(configuration)
	if err != nil {
		return err
	}

	var waitGroup sync.WaitGroup
	stopped := make(chan string, 4)

	// The plain HTTP listener also answers ACME http-01 challenges, so it is
	// worth running even when the dashboard is switched off.
	httpHandler := withChallengeHandler(self.handler, self.acme)
	httpServer := &http.Server{Handler: httpHandler}
	httpsServer := &http.Server{Handler: self.handler}

	if self.listeners.http != nil && httpHandler != nil {
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			if err := httpServer.Serve(self.listeners.http); err != nil && err != http.ErrServerClosed {
				log.Errorf("http server exited with error: %s", err)
			}
			stopped <- "http"
		}()
	}

	if self.listeners.https != nil && self.handler != nil && tlsConfig != nil {
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			if err := httpsServer.Serve(tls.NewListener(self.listeners.https, tlsConfig)); err != nil && err != http.ErrServerClosed {
				log.Errorf("https server exited with error: %s", err)
			}
			stopped <- "https"
		}()
	}

	greeting := fmt.Sprintf("%s teanode/%s", configuration.Server.Name, version.Version())

	if self.listeners.smtpIncoming != nil {
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			if err := smtpd.Serve(self.listeners.smtpIncoming, self.exchange.HandleEnvelope, self.locator, self.resolver, self.dropper, &smtpd.Settings{
				Outgoing:       false,
				Greeting:       greeting,
				Timeout:        time.Hour,
				MaxSize:        int(configuration.SMTP.MaxMessageSize.Bytes()),
				MaxRecipients:  configuration.SMTP.MaxRecipientsIncoming,
				TLSConfig:      tlsConfig,
				Secret:         self.secret,
				TrustedSenders: configuration.SMTP.TrustedSenders,
				Delay:          configuration.SMTP.GreylistDelay.Duration(),

				RequireReverseDNS: configuration.SMTP.RequireReverseDNS,
			}); err != nil {
				log.Debugf("incoming smtp server exited: %s", err)
			}
			stopped <- "incoming smtp"
		}()
	}

	// One limiter for the submission listener, built once so that every
	// connection counts against the same buckets. Nil when either setting is
	// zero, which is how an operator turns the limit off.
	var authLimiter *ratelimit.Registry
	if configuration.SMTP.AuthRateLimit > 0 && configuration.SMTP.AuthRateBurst > 0 {
		authLimiter = ratelimit.NewRegistry(
			float64(configuration.SMTP.AuthRateLimit)/60.0,
			int64(configuration.SMTP.AuthRateBurst),
			authLimiterAddresses,
			time.Hour,
		)
	}

	if self.listeners.smtpOutgoing != nil {
		waitGroup.Add(1)
		go func() {
			defer deferutil.Recover()
			defer waitGroup.Done()
			if err := smtpd.Serve(self.listeners.smtpOutgoing, self.exchange.HandleEnvelope, self.locator, self.resolver, self.dropper, &smtpd.Settings{
				Outgoing:      true,
				Greeting:      greeting,
				Timeout:       time.Hour,
				MaxSize:       int(configuration.SMTP.MaxMessageSize.Bytes()),
				MaxRecipients: configuration.SMTP.MaxRecipientsOutgoing,
				TLSConfig:     tlsConfig,
				Secret:        self.secret,

				AuthLimiter: authLimiter,
			}); err != nil {
				log.Debugf("outgoing smtp server exited: %s", err)
			}
			stopped <- "outgoing smtp"
		}()
	}

	// SIGHUP re-reads the configuration file, so that an operator who edits it
	// by hand does not have to restart and drop connections.
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)

	// SIGQUIT dumps goroutine stacks, which is how a wedged server is
	// diagnosed in production.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGQUIT)
	defer signal.Stop(quit)

	log.Noticef("teanode is running")

	var reason string
	// Far enough to serve. A staged binary is only trusted after this: the
	// marker beside it is what stops a release that crashes on startup from
	// being exec'd again on every restart, for ever.
	upgrade.Started(self.upgradeDirectory)

	for reason == "" {
		select {
		case <-ctx.Done():
			reason = "interrupted"
		case <-self.restartRequested:
			reason = "restart requested through the API"
		case <-reload:
			log.Noticef("reloading configuration")
			if err := self.store.Reload(); err != nil {
				log.Errorf("failed to reload configuration, keeping the previous one: %s", err)
			}
		case <-quit:
			log.Warningf("%s", debugutil.GetAllStacks())
		case name := <-stopped:
			reason = name + " listener stopped"
		}
	}

	log.Noticef("shutting down: %s", reason)

	timer := time.AfterFunc(shutdownTimeout, func() {
		log.Errorf("graceful shutdown timed out after %s: %s", shutdownTimeout, debugutil.GetAllStacks())
		os.Exit(1)
	})
	defer timer.Stop()

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownContext); err != nil {
		log.Errorf("failed to shut down http: %s", err)
	}
	if err := httpsServer.Shutdown(shutdownContext); err != nil {
		log.Errorf("failed to shut down https: %s", err)
	}
	if self.listeners.smtpIncoming != nil {
		_ = self.listeners.smtpIncoming.Close()
	}
	if self.listeners.smtpOutgoing != nil {
		_ = self.listeners.smtpOutgoing.Close()
	}

	waitGroup.Wait()

	// An upgrade is not applied here. runServer does it, after every deferred
	// close has run: exec never returns, so doing it from inside this function
	// would skip all of them and abandon whatever the mailer and the queue
	// were part way through.
	return nil
}

// execTarget is the binary this process should become, if an upgrade staged
// one. Nothing when the web component was never built, which is every command
// other than run.
func (self *server) execTarget() string {
	if self.upgrader == nil {
		return ""
	}
	return self.upgrader.ExecTarget()
}

// openLocator returns a GeoIP locator, or one that locates nothing when the
// operator has not supplied a MaxMind database. None is bundled: the licence
// requires each user to accept it themselves.
func openLocator(configuration *config.Configuration) geoip.Locator {
	if !configuration.GeoIP.Enabled {
		return geoip.NewNullLocator()
	}
	return geoip.NewLocator(configuration.Path(configuration.GeoIP.DatabaseFile))
}

func openAntispam(configuration *config.Configuration) (spamc.Client, error) {
	if !configuration.Antispam.Enabled {
		return nil, nil
	}
	client, err := spamc.Open(&spamc.Settings{
		Host: configuration.Antispam.Host,
		Port: configuration.Antispam.Port,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot connect to spamassassin at %s:%d: %w", configuration.Antispam.Host, configuration.Antispam.Port, err)
	}
	return client, nil
}

func openAntivirus(configuration *config.Configuration) (clamav.Client, error) {
	if !configuration.Antivirus.Enabled {
		return nil, nil
	}
	client, err := clamav.Open(&clamav.Settings{
		Host: configuration.Antivirus.Host,
		Port: configuration.Antivirus.Port,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot connect to clamav at %s:%d: %w", configuration.Antivirus.Host, configuration.Antivirus.Port, err)
	}
	return client, nil
}

// loadAWSConfig builds an AWS configuration for the optional Route53 and S3
// integrations.
//
// Credentials come from teanode.yaml when they are set there, from a shared
// credentials file when one is named, and otherwise from the default AWS
// chain — which is how an instance role works, and the option with no
// long-lived secret to leak.
func loadAWSConfig(region, accessKeyId, secretAccessKey, credentialsFile string) (aws.Config, error) {
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	switch {
	case accessKeyId != "" && secretAccessKey != "":
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyId, secretAccessKey, "")))
	case credentialsFile != "":
		options = append(options, awsconfig.WithSharedCredentialsFiles([]string{credentialsFile}))
	}
	return awsconfig.LoadDefaultConfig(context.Background(), options...)
}
