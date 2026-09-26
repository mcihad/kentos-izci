// Package config loads izci's runtime configuration from the environment.
//
// Values come from real environment variables first; a `.env` file (default:
// `.env` in the working directory) only fills in variables that are not already
// set. Production deployments (Docker/Dokploy) pass real environment variables
// and ship no `.env` at all. Every variable is documented in `.env.example` and
// in docs/yapilandirma.md.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

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
	DataDir   string      `env:"IZCI_DATA_DIR" envDefault:"./data"`

	HTTP      HTTPConfig
	Log       LogConfig
	DB        DBConfig
	Retention RetentionConfig
	Ingest    IngestConfig
	Auth      AuthConfig
	Telegram  TelegramConfig
	GitHub    GitHubConfig
	AI        AIConfig
	MCP       MCPConfig
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
}

// AuthConfig configures sign-in.
type AuthConfig struct {
	// Signup is "invite" (default), "open" or "closed".
	Signup string `env:"AUTH_SIGNUP" envDefault:"invite"`
	// SessionTTL is the sliding lifetime of browser sessions.
	SessionTTL time.Duration `env:"AUTH_SESSION_TTL" envDefault:"720h"`
	// PasswordLogin enables email + password sign-in.
	PasswordLogin bool `env:"AUTH_PASSWORD_LOGIN" envDefault:"true"`
	// InvitationTTL is how long invitation links stay valid.
	InvitationTTL time.Duration `env:"AUTH_INVITATION_TTL" envDefault:"168h"`
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
}

// MCPConfig configures the Model Context Protocol endpoint.
type MCPConfig struct {
	// Enabled serves MCP at /mcp for personal access tokens.
	Enabled bool `env:"MCP_ENABLED" envDefault:"true"`
}

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
	if c.AI.Timeout < time.Minute {
		errs = append(errs, fmt.Errorf("AI_TIMEOUT must be at least 1m (got %s)", c.AI.Timeout))
	}

	if c.Ingest.Ack != "durable" && c.Ingest.Ack != "async" {
		errs = append(errs, fmt.Errorf("INGEST_ACK must be durable or async (got %q)", c.Ingest.Ack))
	}
	if c.Ingest.Shards < 1 || c.Ingest.Shards > 64 {
		errs = append(errs, fmt.Errorf("INGEST_SHARDS must be between 1 and 64 (got %d)", c.Ingest.Shards))
	}
	if c.Ingest.BatchSize < 1 || c.Ingest.BatchSize > 5000 {
		errs = append(errs, fmt.Errorf("INGEST_BATCH_SIZE must be between 1 and 5000 (got %d)", c.Ingest.BatchSize))
	}

	if c.DB.MaxConns < 2 {
		errs = append(errs, fmt.Errorf("DB_MAX_CONNS must be at least 2 (got %d)", c.DB.MaxConns))
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
