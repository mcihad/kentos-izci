// Package config loads izci's runtime configuration from the environment.
//
// Values come from real environment variables first; a `.env` file (default:
// `.env` in the working directory) only fills in variables that are not already
// set. Production deployments (Docker/Dokploy) pass real environment variables
// and ship no `.env` at all. Every variable is documented in `.env.example` and
// in docs/yapilandirma.md.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// DefaultEnvFile is loaded when no explicit env file is given. A missing
// default file is not an error.
const DefaultEnvFile = ".env"

// Environment names the deployment mode.
type Environment string

// Supported environments.
const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
	EnvTest        Environment = "test"
)

// Config is the complete, validated runtime configuration.
type Config struct {
	Env       Environment `env:"IZCI_ENV" envDefault:"development"`
	PublicURL string      `env:"IZCI_PUBLIC_URL" envDefault:"http://localhost:5300"`
	SecretKey SecretKey   `env:"IZCI_SECRET_KEY"`
	// SecretKeyPrevious are earlier master keys, newest first: they still
	// open stored secrets and verify signatures while "izci secrets rotate"
	// moves everything to SecretKey.
	SecretKeyPrevious SecretKeys `env:"IZCI_SECRET_KEY_PREVIOUS"`
	DataDir           string     `env:"IZCI_DATA_DIR" envDefault:"./data"`

	HTTP      HTTPConfig
	Log       LogConfig
	DB        DBConfig
	Retention RetentionConfig
	Ingest    IngestConfig
	Auth      AuthConfig
	Telegram  TelegramConfig
	SMTP      SMTPConfig
	Notify    NotifyConfig
	GitHub    GitHubConfig
	AI        AIConfig
	MCP       MCPConfig
	Metrics   MetricsConfig
	Self      SelfConfig
	// OIDC holds the configured OpenID Connect providers (from
	// OIDC_PROVIDERS and OIDC_<ID>_* variables).
	OIDC []OIDCProvider `env:"-"`

	// secretGenerated is true when no IZCI_SECRET_KEY was configured and an
	// ephemeral key was generated (development only).
	secretGenerated bool
}

// HTTPConfig configures the single public listener.
type HTTPConfig struct {
	Addr            string        `env:"IZCI_HTTP_ADDR" envDefault:":5300"`
	TLSCertFile     string        `env:"IZCI_TLS_CERT_FILE"`
	TLSKeyFile      string        `env:"IZCI_TLS_KEY_FILE"`
	TrustedProxies  PrefixList    `env:"IZCI_TRUSTED_PROXIES"`
	ShutdownTimeout time.Duration `env:"IZCI_SHUTDOWN_TIMEOUT" envDefault:"20s"`
}

// LogConfig configures structured logging.
type LogConfig struct {
	Level  slog.Level `env:"IZCI_LOG_LEVEL" envDefault:"info"`
	Format string     `env:"IZCI_LOG_FORMAT"` // "json" or "text"; defaults by environment
}

// DBConfig configures PostgreSQL access.
type DBConfig struct {
	URL         string `env:"DATABASE_URL"`
	MaxConns    int32  `env:"DB_MAX_CONNS" envDefault:"20"`
	AutoMigrate bool   `env:"DB_AUTO_MIGRATE" envDefault:"true"`
	// AdminURL is a superuser connection used only by `izci db bootstrap`
	// to create the izci role and databases in development.
	AdminURL string `env:"DB_ADMIN_URL"`
	// TestURL points integration tests at a disposable database.
	TestURL string `env:"IZCI_TEST_DATABASE_URL"`
	// AllowPrivilegedRole lets izci run as a superuser/BYPASSRLS role in
	// production. Row-level security is silently skipped for such roles, so
	// this must stay false outside of emergencies.
	AllowPrivilegedRole bool `env:"DB_ALLOW_PRIVILEGED_ROLE" envDefault:"false"`
}

// RetentionConfig bounds how long events are kept.
type RetentionConfig struct {
	// Days is the instance-wide maximum age of events; whole daily partitions
	// older than this are dropped. Organization limits may be shorter.
	Days int `env:"RETENTION_DAYS" envDefault:"90"`
	// PremakeDays is how many future daily partitions are created ahead.
	PremakeDays int `env:"PARTITION_PREMAKE_DAYS" envDefault:"7"`
}

// IngestConfig tunes the event pipeline.
type IngestConfig struct {
	// Shards is the number of parallel batch writers.
	Shards int `env:"INGEST_SHARDS" envDefault:"4"`
	// QueueSize is the capacity of each writer queue.
	QueueSize int `env:"INGEST_QUEUE_SIZE" envDefault:"5000"`
	// BatchSize caps events per database transaction.
	BatchSize int `env:"INGEST_BATCH_SIZE" envDefault:"500"`
	// Linger optionally waits for more events before writing a batch.
	Linger time.Duration `env:"INGEST_LINGER" envDefault:"0s"`
	// Ack is "durable" (answer after commit) or "async" (answer once queued).
	Ack string `env:"INGEST_ACK" envDefault:"durable"`
	// AckTimeout bounds how long a durable call waits for its batch.
	AckTimeout time.Duration `env:"INGEST_ACK_TIMEOUT" envDefault:"15s"`
	// RateLimitPerMinute is the default per-project limit.
	RateLimitPerMinute int `env:"INGEST_RATE_LIMIT_PER_MINUTE" envDefault:"6000"`
	// DedupeSize is how many recent event ids are remembered for retries.
	DedupeSize int `env:"INGEST_DEDUPE_SIZE" envDefault:"200000"`
	// MaxAttachmentBytes bounds one file sent with an event (screenshots,
	// logs); larger files are dropped.
	MaxAttachmentBytes int `env:"INGEST_ATTACHMENT_MAX_BYTES" envDefault:"10485760"`
	// SymbolCacheMB bounds the memory of parsed source maps and debug files
	// kept to resolve stack traces.
	SymbolCacheMB int `env:"INGEST_SYMBOL_CACHE_MB" envDefault:"256"`
	// UploadMaxMB bounds one uploaded file (a source map bundle or a debug
	// file) and each file inside a bundle; an assembly holds about this
	// much memory.
	UploadMaxMB int `env:"INGEST_UPLOAD_MAX_MB" envDefault:"128"`
}

// AuthConfig configures sign-in.
type AuthConfig struct {
	// Signup is "invite" (default), "open" or "closed". Open sign-ups
	// confirm the e-mail address with a link (SMTP; in development the
	// link is logged).
	Signup string `env:"AUTH_SIGNUP" envDefault:"invite"`
	// SignupOrgLimit is how many organizations a user may own while
	// sign-up is open (instance administrators have no limit); 0 removes
	// the limit.
	SignupOrgLimit int `env:"AUTH_SIGNUP_ORG_LIMIT" envDefault:"3"`
	// SessionTTL is how long an unused browser session stays valid; every
	// use extends it (sliding idle timeout).
	SessionTTL time.Duration `env:"AUTH_SESSION_TTL" envDefault:"720h"`
	// SessionMaxAge ends a browser session this long after sign-in, however
	// often it is used.
	SessionMaxAge time.Duration `env:"AUTH_SESSION_MAX_AGE" envDefault:"720h"`
	// PasswordLogin enables email + password sign-in.
	PasswordLogin bool `env:"AUTH_PASSWORD_LOGIN" envDefault:"true"`
	// InvitationTTL is how long invitation links stay valid.
	InvitationTTL time.Duration `env:"AUTH_INVITATION_TTL" envDefault:"168h"`
	// RequireTwoFactorSuperadmin is "auto" (only in production), "true" or
	// "false": instance administrators without two-factor authentication
	// keep their account but cannot use their administrator rights.
	RequireTwoFactorSuperadmin string `env:"AUTH_REQUIRE_2FA_SUPERADMIN" envDefault:"auto"`
	// SetupToken is the code the first-run setup screen asks for. Empty:
	// one is derived from IZCI_SECRET_KEY and printed in the log while no
	// user exists.
	SetupToken Secret `env:"IZCI_SETUP_TOKEN"`
	// Providers lists OpenID Connect provider ids, e.g. "kimlik,google".
	Providers []string `env:"OIDC_PROVIDERS" envSeparator:","`
}

// TelegramConfig configures the notification bot.
type TelegramConfig struct {
	// BotToken is the token from @BotFather; empty disables Telegram.
	BotToken Secret `env:"TELEGRAM_BOT_TOKEN"`
	// Mode is "polling" (default; no public URL needed) or "webhook".
	Mode string `env:"TELEGRAM_MODE" envDefault:"polling"`
	// WebhookSecret authenticates webhook calls through the
	// X-Telegram-Bot-Api-Secret-Token header (webhook mode only).
	WebhookSecret Secret `env:"TELEGRAM_WEBHOOK_SECRET"`
	// APIURL is the Bot API base URL; change it for a local Bot API server.
	APIURL string `env:"TELEGRAM_API_URL" envDefault:"https://api.telegram.org"`
}

// Enabled reports whether a bot token is configured.
func (t TelegramConfig) Enabled() bool { return t.BotToken != "" }

// SMTPConfig configures outgoing e-mail: notification channels and the
// links that confirm self sign-ups.
type SMTPConfig struct {
	// Host is the SMTP server; empty disables e-mail notifications.
	Host string `env:"SMTP_HOST"`
	// Port is the SMTP port: 587 for STARTTLS, 465 for implicit TLS.
	Port int `env:"SMTP_PORT" envDefault:"587"`
	// Username and Password authenticate (PLAIN) when set.
	Username string `env:"SMTP_USERNAME"`
	Password Secret `env:"SMTP_PASSWORD"`
	// From is the sender, e.g. "İzci <izci@example.com>".
	From string `env:"SMTP_FROM"`
	// TLS is "starttls" (default), "tls" (implicit, port 465) or "none"
	// (local relays only).
	TLS string `env:"SMTP_TLS" envDefault:"starttls"`
}

// Enabled reports whether an SMTP server is configured.
func (s SMTPConfig) Enabled() bool { return s.Host != "" }

// NotifyConfig configures outgoing notification requests (Slack, Teams,
// webhooks, Jira).
type NotifyConfig struct {
	// AllowPrivateNetworks lets channels call addresses in private,
	// loopback and link-local networks, e.g. an internal Jira. Keep it off
	// when organization admins are not trusted with the server's network.
	AllowPrivateNetworks bool `env:"NOTIFY_ALLOW_PRIVATE_NETWORKS" envDefault:"false"`
}

// GitHubConfig configures GitHub (or GitHub Enterprise Server) access.
type GitHubConfig struct {
	// APIURL is the REST API base, e.g. https://github.example.com/api/v3.
	APIURL string `env:"GITHUB_API_URL" envDefault:"https://api.github.com"`
	// WebURL is the web base used for links and git remotes.
	WebURL string `env:"GITHUB_WEB_URL" envDefault:"https://github.com"`
	// AppID, AppSlug and AppPrivateKey configure an optional GitHub App;
	// organizations can also connect with a personal access token.
	AppID         int64  `env:"GITHUB_APP_ID"`
	AppSlug       string `env:"GITHUB_APP_SLUG"`
	AppPrivateKey Secret `env:"GITHUB_APP_PRIVATE_KEY"`
	// AppPrivateKeyFile reads the key from a file instead.
	AppPrivateKeyFile string `env:"GITHUB_APP_PRIVATE_KEY_FILE"`
	// AppClientID and AppClientSecret are the app's OAuth credentials; izci
	// uses them to verify who installed the app.
	AppClientID     string `env:"GITHUB_APP_CLIENT_ID"`
	AppClientSecret Secret `env:"GITHUB_APP_CLIENT_SECRET"`
}

// AppConfigured reports whether a GitHub App is configured.
func (g GitHubConfig) AppConfigured() bool {
	return g.AppID > 0 && g.AppSlug != "" && (g.AppPrivateKey != "" || g.AppPrivateKeyFile != "") &&
		g.AppClientID != "" && g.AppClientSecret != ""
}

// AIConfig configures AI analysis with opencode.
type AIConfig struct {
	Enabled bool `env:"AI_ENABLED" envDefault:"false"`
	// OpencodeBin is the opencode executable.
	OpencodeBin string `env:"AI_OPENCODE_BIN" envDefault:"opencode"`
	// Model is the default model ("provider/model"); empty uses opencode's.
	Model string `env:"AI_OPENCODE_MODEL"`
	// OpencodeConfig is an optional opencode config file applied to runs,
	// e.g. for custom providers. Permissions set by izci always win.
	OpencodeConfig string `env:"AI_OPENCODE_CONFIG"`
	// MaxConcurrent bounds parallel runs on this instance.
	MaxConcurrent int `env:"AI_MAX_CONCURRENT" envDefault:"2"`
	// Timeout stops a run that takes longer.
	Timeout time.Duration `env:"AI_TIMEOUT" envDefault:"20m"`
	// OutputLanguage is the language of analyses written for people.
	OutputLanguage string `env:"AI_OUTPUT_LANGUAGE" envDefault:"Turkish"`
	// Git identity of commits made for AI fixes.
	GitAuthorName  string `env:"AI_GIT_AUTHOR_NAME" envDefault:"izci-bot"`
	GitAuthorEmail string `env:"AI_GIT_AUTHOR_EMAIL" envDefault:"izci-bot@users.noreply.github.com"`
	// AllowedCommands are the shell commands projects may let the agent run
	// (e.g. "go test", "dotnet test"); a project entry must equal one of
	// them or add arguments. Empty: the agent runs no commands. A test or
	// build command runs code the agent can change, so allow them only
	// with AI_SANDBOX=bwrap or on a server dedicated to analyses.
	AllowedCommands []string `env:"AI_ALLOWED_COMMANDS" envSeparator:","`
	// Sandbox isolates the agent: "bwrap" (Linux, bubblewrap) shows it only
	// the run's checkout and repository, system directories read-only, in
	// its own process namespace; "none" runs it as the server user.
	Sandbox string `env:"AI_SANDBOX" envDefault:"none"`
}

// MCPConfig configures the Model Context Protocol endpoint.
type MCPConfig struct {
	// Enabled serves MCP at /mcp for personal access tokens.
	Enabled bool `env:"MCP_ENABLED" envDefault:"true"`
}

// MetricsConfig configures the Prometheus endpoint.
type MetricsConfig struct {
	// Enabled serves Prometheus metrics at /metrics.
	Enabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	// Token, when set, must be sent as "Authorization: Bearer <token>".
	// Without a token only direct requests from loopback and private
	// addresses are answered.
	Token Secret `env:"METRICS_TOKEN"`
}

// SelfConfig makes izci report its own errors to an izci server with the Go
// SDK: to a separate monitoring instance or to a project on itself.
type SelfConfig struct {
	// URL is the base URL of the izci server receiving the events.
	URL string `env:"IZCI_SELF_URL"`
	// Key is the ingest key ("izk_…") of the project collecting the errors.
	Key Secret `env:"IZCI_SELF_KEY"`
	// Environment of the reported events; defaults to IZCI_ENV.
	Environment string `env:"IZCI_SELF_ENVIRONMENT"`
}

// Enabled reports whether self-monitoring is configured.
func (s SelfConfig) Enabled() bool { return s.URL != "" && s.Key != "" }

// Secret is a string that is never printed: String returns a placeholder so
// secrets do not leak through logs or error messages.
type Secret string

// Value returns the secret itself.
func (s Secret) Value() string { return string(s) }

func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return "[redacted]"
}

// OIDCProvider is one OpenID Connect identity provider, configured with
// OIDC_<ID>_* variables (ID upper-cased, e.g. OIDC_KIMLIK_ISSUER).
type OIDCProvider struct {
	ID             string   `env:"-"`
	Name           string   `env:"NAME"`
	Issuer         string   `env:"ISSUER"`
	ClientID       string   `env:"CLIENT_ID"`
	ClientSecret   string   `env:"CLIENT_SECRET"`
	Scopes         []string `env:"SCOPES" envSeparator:"," envDefault:"openid,profile,email"`
	Icon           string   `env:"ICON" envDefault:"generic"`
	AutoCreate     bool     `env:"AUTO_CREATE" envDefault:"true"`
	AllowedDomains []string `env:"ALLOWED_DOMAINS" envSeparator:","`
	DefaultOrg     string   `env:"DEFAULT_ORG"`
	DefaultRole    string   `env:"DEFAULT_ROLE" envDefault:"member"`
	// TrustEmail vouches for the provider's e-mail addresses: one without
	// an email_verified claim counts as verified, and a first sign-in links
	// to an existing account with the same address. Only for providers that
	// never let users set an unverified address (a company directory).
	TrustEmail bool `env:"TRUST_EMAIL"`
}

// Load reads the env file (if any) and parses the environment.
// An empty envFile means DefaultEnvFile, which may be absent.
func Load(envFile string) (*Config, error) {
	explicit := envFile != ""
	if !explicit {
		envFile = DefaultEnvFile
	}
	if err := godotenv.Load(envFile); err != nil {
		if !explicit && errors.Is(err, fs.ErrNotExist) {
			// No .env file: rely on real environment variables only.
		} else {
			return nil, fmt.Errorf("load env file %q: %w", envFile, err)
		}
	}

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	for _, id := range cfg.Auth.Providers {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		p := OIDCProvider{ID: id}
		prefix := "OIDC_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_"
		if err := env.ParseWithOptions(&p, env.Options{Prefix: prefix}); err != nil {
			return nil, fmt.Errorf("parse OIDC provider %q: %w", id, err)
		}
		if p.Name == "" {
			p.Name = id
		}
		cfg.OIDC = append(cfg.OIDC, p)
	}
	if err := cfg.finalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) finalize() error {
	var errs []error

	switch c.Env {
	case EnvDevelopment, EnvProduction, EnvTest:
	default:
		errs = append(errs, fmt.Errorf("IZCI_ENV must be development, production or test (got %q)", c.Env))
	}

	if c.Log.Format == "" {
		if c.Env == EnvProduction {
			c.Log.Format = "json"
		} else {
			c.Log.Format = "text"
		}
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		errs = append(errs, fmt.Errorf("IZCI_LOG_FORMAT must be json or text (got %q)", c.Log.Format))
	}

	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("IZCI_PUBLIC_URL must be an absolute http(s) URL (got %q)", c.PublicURL))
	}

	if (c.HTTP.TLSCertFile == "") != (c.HTTP.TLSKeyFile == "") {
		errs = append(errs, errors.New("IZCI_TLS_CERT_FILE and IZCI_TLS_KEY_FILE must be set together"))
	}

	if c.Retention.Days < 1 || c.Retention.Days > 3650 {
		errs = append(errs, fmt.Errorf("RETENTION_DAYS must be between 1 and 3650 (got %d)", c.Retention.Days))
	}
	if c.Retention.PremakeDays < 1 || c.Retention.PremakeDays > 60 {
		errs = append(errs, fmt.Errorf("PARTITION_PREMAKE_DAYS must be between 1 and 60 (got %d)", c.Retention.PremakeDays))
	}

	switch c.Auth.Signup {
	case "invite", "open", "closed":
	default:
		errs = append(errs, fmt.Errorf("AUTH_SIGNUP must be invite, open or closed (got %q)", c.Auth.Signup))
	}
	if c.Auth.Signup == "open" && c.IsProduction() && !c.SMTP.Enabled() {
		errs = append(errs, errors.New("AUTH_SIGNUP=open needs SMTP_HOST in production: new accounts confirm their e-mail address with a link"))
	}
	if c.Auth.SignupOrgLimit < 0 {
		errs = append(errs, fmt.Errorf("AUTH_SIGNUP_ORG_LIMIT must not be negative (got %d)", c.Auth.SignupOrgLimit))
	}
	switch c.Auth.RequireTwoFactorSuperadmin {
	case "auto", "true", "false":
	default:
		errs = append(errs, fmt.Errorf("AUTH_REQUIRE_2FA_SUPERADMIN must be auto, true or false (got %q)", c.Auth.RequireTwoFactorSuperadmin))
	}
	if c.Auth.SessionTTL < time.Minute || c.Auth.SessionMaxAge < time.Minute {
		errs = append(errs, fmt.Errorf("AUTH_SESSION_TTL and AUTH_SESSION_MAX_AGE must be at least 1m (got %s and %s)",
			c.Auth.SessionTTL, c.Auth.SessionMaxAge))
	}
	for _, p := range c.OIDC {
		if p.Issuer == "" || p.ClientID == "" {
			errs = append(errs, fmt.Errorf("OIDC provider %q needs OIDC_%s_ISSUER and OIDC_%s_CLIENT_ID",
				p.ID, strings.ToUpper(p.ID), strings.ToUpper(p.ID)))
		}
		switch p.DefaultRole {
		case "viewer", "member", "admin", "owner":
		default:
			errs = append(errs, fmt.Errorf("OIDC_%s_DEFAULT_ROLE must be viewer, member, admin or owner", strings.ToUpper(p.ID)))
		}
	}

	switch c.Telegram.Mode {
	case "polling":
	case "webhook":
		if c.Telegram.Enabled() && !validWebhookSecret(c.Telegram.WebhookSecret.Value()) {
			errs = append(errs, errors.New("TELEGRAM_WEBHOOK_SECRET must be 16-256 characters of A-Z, a-z, 0-9, _ or - in webhook mode"))
		}
		if c.Telegram.Enabled() && !strings.HasPrefix(c.PublicURL, "https://") {
			errs = append(errs, errors.New("TELEGRAM_MODE=webhook needs an https IZCI_PUBLIC_URL that Telegram can reach"))
		}
	default:
		errs = append(errs, fmt.Errorf("TELEGRAM_MODE must be polling or webhook (got %q)", c.Telegram.Mode))
	}
	if c.SMTP.Enabled() {
		if addr, err := mail.ParseAddress(c.SMTP.From); err != nil || !strings.Contains(addr.Address, "@") {
			errs = append(errs, fmt.Errorf("SMTP_FROM must be an e-mail address such as \"İzci <izci@example.com>\" (got %q)", c.SMTP.From))
		}
		if c.SMTP.Port < 1 || c.SMTP.Port > 65535 {
			errs = append(errs, fmt.Errorf("SMTP_PORT must be a port number (got %d)", c.SMTP.Port))
		}
		switch c.SMTP.TLS {
		case "starttls", "tls", "none":
		default:
			errs = append(errs, fmt.Errorf("SMTP_TLS must be starttls, tls or none (got %q)", c.SMTP.TLS))
		}
	}
	c.Telegram.APIURL = strings.TrimRight(c.Telegram.APIURL, "/")
	c.GitHub.APIURL = strings.TrimRight(c.GitHub.APIURL, "/")
	c.GitHub.WebURL = strings.TrimRight(c.GitHub.WebURL, "/")
	if (c.GitHub.AppID > 0 || c.GitHub.AppSlug != "") && !c.GitHub.AppConfigured() {
		errs = append(errs, errors.New("GITHUB_APP_ID, GITHUB_APP_SLUG, GITHUB_APP_PRIVATE_KEY(_FILE), GITHUB_APP_CLIENT_ID and "+
			"GITHUB_APP_CLIENT_SECRET must be set together"))
	}
	if c.AI.MaxConcurrent < 1 || c.AI.MaxConcurrent > 32 {
		errs = append(errs, fmt.Errorf("AI_MAX_CONCURRENT must be between 1 and 32 (got %d)", c.AI.MaxConcurrent))
	}
	switch c.AI.Sandbox {
	case "none":
	case "bwrap":
		if runtime.GOOS != "linux" {
			errs = append(errs, fmt.Errorf("AI_SANDBOX=bwrap needs Linux (this is %s)", runtime.GOOS))
		}
	default:
		errs = append(errs, fmt.Errorf("AI_SANDBOX must be none or bwrap (got %q)", c.AI.Sandbox))
	}
	var commands []string
	for _, cmd := range c.AI.AllowedCommands {
		if cmd = strings.Join(strings.Fields(cmd), " "); cmd != "" {
			commands = append(commands, cmd)
		}
	}
	c.AI.AllowedCommands = commands
	if c.AI.Timeout < time.Minute {
		errs = append(errs, fmt.Errorf("AI_TIMEOUT must be at least 1m (got %s)", c.AI.Timeout))
	}

	c.Self.URL = strings.TrimRight(strings.TrimSpace(c.Self.URL), "/")
	if (c.Self.URL == "") != (c.Self.Key == "") {
		errs = append(errs, errors.New("IZCI_SELF_URL and IZCI_SELF_KEY must be set together"))
	} else if c.Self.Enabled() {
		if u, err := url.Parse(c.Self.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("IZCI_SELF_URL must be an absolute http(s) URL (got %q)", c.Self.URL))
		}
		if !strings.HasPrefix(c.Self.Key.Value(), "izk_") {
			errs = append(errs, errors.New("IZCI_SELF_KEY must be a project ingest key (izk_…)"))
		}
	}
	if c.Self.Environment == "" {
		c.Self.Environment = string(c.Env)
	}

	if c.Ingest.Ack != "durable" && c.Ingest.Ack != "async" {
		errs = append(errs, fmt.Errorf("INGEST_ACK must be durable or async (got %q)", c.Ingest.Ack))
	}
	if c.Ingest.Shards < 1 || c.Ingest.Shards > 64 {
		errs = append(errs, fmt.Errorf("INGEST_SHARDS must be between 1 and 64 (got %d)", c.Ingest.Shards))
	}
	if c.Ingest.MaxAttachmentBytes < 1 || c.Ingest.MaxAttachmentBytes > 40<<20 {
		errs = append(errs, fmt.Errorf("INGEST_ATTACHMENT_MAX_BYTES must be between 1 and 41943040 (got %d)", c.Ingest.MaxAttachmentBytes))
	}
	if c.Ingest.UploadMaxMB < 1 || c.Ingest.UploadMaxMB > 2048 {
		errs = append(errs, fmt.Errorf("INGEST_UPLOAD_MAX_MB must be between 1 and 2048 (got %d)", c.Ingest.UploadMaxMB))
	}
	if c.Ingest.SymbolCacheMB < 16 || c.Ingest.SymbolCacheMB > 16384 {
		errs = append(errs, fmt.Errorf("INGEST_SYMBOL_CACHE_MB must be between 16 and 16384 (got %d)", c.Ingest.SymbolCacheMB))
	}
	if c.Ingest.BatchSize < 1 || c.Ingest.BatchSize > 5000 {
		errs = append(errs, fmt.Errorf("INGEST_BATCH_SIZE must be between 1 and 5000 (got %d)", c.Ingest.BatchSize))
	}

	if c.DB.MaxConns < 2 {
		errs = append(errs, fmt.Errorf("DB_MAX_CONNS must be at least 2 (got %d)", c.DB.MaxConns))
	}

	for _, k := range c.SecretKeyPrevious {
		if bytes.Equal(k, c.SecretKey) {
			errs = append(errs, errors.New("IZCI_SECRET_KEY_PREVIOUS must not contain IZCI_SECRET_KEY"))
		}
	}
	if len(c.SecretKey) == 0 && len(c.SecretKeyPrevious) > 0 {
		errs = append(errs, errors.New("IZCI_SECRET_KEY_PREVIOUS needs IZCI_SECRET_KEY, the key that replaces them"))
	}
	if len(c.SecretKey) == 0 {
		if c.Env == EnvProduction {
			errs = append(errs, errors.New("IZCI_SECRET_KEY is required in production (generate with: openssl rand -base64 32)"))
		} else {
			key := make([]byte, secretKeySize)
			if _, err := rand.Read(key); err != nil {
				return fmt.Errorf("generate ephemeral secret key: %w", err)
			}
			c.SecretKey = key
			c.secretGenerated = true
		}
	}

	return errors.Join(errs...)
}

// IsProduction reports whether izci runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == EnvProduction }

// SuperadminTwoFactor reports whether instance administrators need
// two-factor authentication to use their rights (AUTH_REQUIRE_2FA_SUPERADMIN).
func (c *Config) SuperadminTwoFactor() bool {
	switch c.Auth.RequireTwoFactorSuperadmin {
	case "true":
		return true
	case "auto":
		return c.IsProduction()
	}
	return false
}

// SecretGenerated reports whether an ephemeral secret key is in use.
func (c *Config) SecretGenerated() bool { return c.secretGenerated }

// RequireDatabase returns an error when DATABASE_URL is missing.
func (c *Config) RequireDatabase() error {
	if strings.TrimSpace(c.DB.URL) == "" {
		return errors.New("DATABASE_URL is not set (see .env.example)")
	}
	return nil
}

// TLSEnabled reports whether the listener terminates TLS itself.
func (c *Config) TLSEnabled() bool { return c.HTTP.TLSCertFile != "" }

const secretKeySize = 32

// SecretKey is the 32-byte master key used for AES-GCM encryption of stored
// secrets and HMAC signing. Accepts standard/URL base64 or hex encoding.
type SecretKey []byte

// UnmarshalText implements encoding.TextUnmarshaler.
func (k *SecretKey) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" {
		*k = nil
		return nil
	}
	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		hex.DecodeString,
	}
	for _, decode := range decoders {
		if b, err := decode(s); err == nil && len(b) == secretKeySize {
			*k = b
			return nil
		}
	}
	return fmt.Errorf("IZCI_SECRET_KEY must encode exactly %d bytes as base64 or hex", secretKeySize)
}

// String never reveals the key.
func (k SecretKey) String() string {
	if len(k) == 0 {
		return ""
	}
	return "[redacted]"
}

// SecretKeys is a comma-separated list of master keys
// (IZCI_SECRET_KEY_PREVIOUS).
type SecretKeys []SecretKey

// UnmarshalText implements encoding.TextUnmarshaler.
func (ks *SecretKeys) UnmarshalText(text []byte) error {
	*ks = nil
	for i, part := range strings.FieldsFunc(string(text), func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		var k SecretKey
		if err := k.UnmarshalText([]byte(part)); err != nil {
			return fmt.Errorf("IZCI_SECRET_KEY_PREVIOUS key %d must encode exactly %d bytes as base64 or hex", i+1, secretKeySize)
		}
		*ks = append(*ks, k)
	}
	return nil
}

// String never reveals the keys.
func (ks SecretKeys) String() string {
	if len(ks) == 0 {
		return ""
	}
	return fmt.Sprintf("[%d redacted]", len(ks))
}

// Bytes returns the keys for secret.NewBox.
func (ks SecretKeys) Bytes() [][]byte {
	out := make([][]byte, len(ks))
	for i, k := range ks {
		out[i] = k
	}
	return out
}

// PrefixList is a comma-separated list of CIDR prefixes or bare IP addresses.
type PrefixList []netip.Prefix

// UnmarshalText implements encoding.TextUnmarshaler.
func (p *PrefixList) UnmarshalText(text []byte) error {
	var out PrefixList
	for _, part := range strings.Split(string(text), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			prefix, err := netip.ParsePrefix(part)
			if err != nil {
				return fmt.Errorf("invalid CIDR %q: %w", part, err)
			}
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return fmt.Errorf("invalid IP %q: %w", part, err)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	*p = out
	return nil
}

// Contains reports whether addr falls into any prefix of the list.
func (p PrefixList) Contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range p {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// LookupEnvFile returns the env file path chosen by the IZCI_ENV_FILE
// variable, or "" for the default.
func LookupEnvFile() string {
	return os.Getenv("IZCI_ENV_FILE")
}

// validWebhookSecret checks Telegram's rules for secret_token.
func validWebhookSecret(s string) bool {
	if len(s) < 16 || len(s) > 256 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
