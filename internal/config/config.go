package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the fully typed application configuration assembled from
// environment variables (.env + process environment).
type Config struct {
	App        App
	HTTP       HTTP
	RateLimit  RateLimit
	Database   Database
	Cache      Cache
	Redis      Redis
	Queue      Queue
	Schedule   Schedule
	Auth       Auth
	Tenancy    Tenancy
	Billing    Billing
	Storage    Storage
	Files      Files
	Processing Processing
	Mail       Mail
	Ops        Ops
	Security   Security
}

// App holds application-level settings.
type App struct {
	Name             string
	Env              string
	Debug            bool
	URL              string
	Timezone         string
	Locale           string
	FallbackLocale   string
	SupportedLocales []string
	DataDir          string
	StorageDir       string
	CacheDir         string
	LogDir           string
	LogLevel         string
	EnvFile          string
}

// HTTP holds HTTP server and transport settings.
type HTTP struct {
	ListenAddr          string
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	ShutdownTimeout     time.Duration
	RequestTimeout      time.Duration
	MaxRequestBodyBytes int64
	StrictJSONFields    bool
	TrustedProxies      []string
}

// RateLimit holds the global request-rate limiting settings.
type RateLimit struct {
	PerMinute int
}

// Database holds the named connection configurations.
type Database struct {
	Primary     Connection
	Connections []Connection
}

// Connection describes one named database connection.
type Connection struct {
	Name        string
	Driver      string
	DSN         string
	Path        string
	MaxConns    int32
	MinConns    int32
	ConnTimeout time.Duration
}

// Cache holds the cache driver settings.
type Cache struct {
	Driver  string
	Prefix  string
	FileDir string
}

// Redis holds the Redis client settings (cache driver, queue-adjacent use).
type Redis struct {
	Addr     string
	Password string
	DB       int
	CacheDB  int
}

// Channel holds per-queue retry/timeout settings.
type Channel struct {
	Tries   int
	Timeout time.Duration
}

// Queue holds the queue engine settings.
type Queue struct {
	Driver            string
	WorkerConcurrency int
	Critical          Channel
	Default           Channel
	Heavy             Channel
}

// Schedule holds scheduler settings.
type Schedule struct {
	Timezone string
}

// Auth holds authentication and password settings.
type Auth struct {
	TokenExpiration    time.Duration
	LoginMaxAttempts   int
	LoginDecay         time.Duration
	CheckIsActive      bool
	CheckEmailVerified bool
	CheckPhoneVerified bool
	OTPEnabled         bool
	OTPLength          int
	OTPExpiry          time.Duration
	DefaultPassword    string
	BcryptRounds       int
}

// Tenancy holds default tenant/branch seeding settings.
type Tenancy struct {
	DefaultTenantID     int64
	DefaultTenantName   string
	DefaultTenantCode   string
	DefaultTenantSlug   string
	DefaultTenantEmail  string
	DefaultTenantPhone  string
	DefaultTenantActive bool
	DefaultBranchID     int64
	DefaultBranchName   string
}

// Billing holds currency and payment gateway settings.
type Billing struct {
	Currency string
	Bkash    Bkash
	Nagad    Nagad
	Bank     BankTransfer
}

// Bkash holds bKash gateway credentials.
type Bkash struct {
	BaseURL     string
	AppKey      string
	AppSecret   string
	Username    string
	Password    string
	CallbackURL string
}

// Nagad holds Nagad gateway credentials (integration not yet implemented).
type Nagad struct {
	BaseURL        string
	MerchantID     string
	MerchantNumber string
	PublicKey      string
	PrivateKey     string
}

// BankTransfer holds manual bank-transfer instructions.
type BankTransfer struct {
	AccountName   string
	AccountNumber string
	BankName      string
	Branch        string
}

// Storage holds the storage disk roots and S3 settings.
type Storage struct {
	PublicDir  string
	PrivateDir string
	S3         S3
}

// S3 holds the S3-compatible disk settings (aws-sdk-go-v2).
type S3 struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool
	PublicURL string
}

// Files holds the disk used for the file resource and attachments.
type Files struct {
	Disk string
}

// Processing holds the disk and path prefix for data-processing artifacts.
type Processing struct {
	Disk     string
	BasePath string
}

// Mail holds the mail driver settings.
type Mail struct {
	Driver      string
	Host        string
	Port        int
	Username    string
	Password    string
	Encryption  string
	FromAddress string
	FromName    string
}

// Ops holds optional operational surfaces (docs UI, metrics) and the CORS
// policy. All are disabled by default in production and config-gated.
type Ops struct {
	DocsEnabled    bool
	MetricsEnabled bool
	CORS           CORS
}

// CORS is the cross-origin policy. An empty AllowedOrigins list disables CORS
// handling entirely (no CORS headers are emitted).
type CORS struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         time.Duration
}

// Security holds hardening switches applied by the security-headers middleware.
type Security struct {
	HeadersEnabled bool
}

// Configured reports whether the S3 disk has the settings it needs.
func (s S3) Configured() bool {
	return s.Region != "" && s.Bucket != "" && s.AccessKey != "" && s.SecretKey != ""
}

// IsProduction reports whether the application runs in production.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.App.Env, "production")
}

// IsDebug reports whether verbose/debug behavior is enabled and allowed.
func (c *Config) IsDebug() bool {
	return c.App.Debug && !c.IsProduction()
}

// Load resolves the environment file (explicit flag → APP_ENV_FILE → .env),
// loads it without overriding existing process values, then builds and returns
// the typed configuration. A missing default file is not an error.
func Load(envFile string) (*Config, error) {
	resolved, err := resolveEnvFile(envFile)
	if err != nil {
		return nil, err
	}
	if resolved != "" {
		if err := godotenv.Load(resolved); err != nil {
			return nil, fmt.Errorf("loading env file %q: %w", resolved, err)
		}
	}

	cfg := &Config{}
	if err := cfg.loadApp(resolved); err != nil {
		return nil, err
	}
	if err := cfg.loadHTTP(); err != nil {
		return nil, err
	}
	if err := cfg.loadRateLimit(); err != nil {
		return nil, err
	}
	if err := cfg.loadDatabase(); err != nil {
		return nil, err
	}
	if err := cfg.loadCache(); err != nil {
		return nil, err
	}
	if err := cfg.loadRedis(); err != nil {
		return nil, err
	}
	if err := cfg.loadQueue(); err != nil {
		return nil, err
	}
	if err := cfg.loadSchedule(); err != nil {
		return nil, err
	}
	if err := cfg.loadAuth(); err != nil {
		return nil, err
	}
	if err := cfg.loadTenancy(); err != nil {
		return nil, err
	}
	if err := cfg.loadBilling(); err != nil {
		return nil, err
	}
	if err := cfg.loadStorage(); err != nil {
		return nil, err
	}
	if err := cfg.loadFiles(); err != nil {
		return nil, err
	}
	if err := cfg.loadProcessing(); err != nil {
		return nil, err
	}
	if err := cfg.loadMail(); err != nil {
		return nil, err
	}
	if err := cfg.loadOps(); err != nil {
		return nil, err
	}
	if err := cfg.loadSecurity(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the configuration required by long-running commands
// (e.g. serve). Short-lived commands (version, help) call it only when they
// actually need the validated values.
func (c *Config) Validate() error {
	if c.HTTP.ListenAddr == "" {
		return errors.New("APP_LISTEN_ADDR must not be empty")
	}
	if c.HTTP.MaxRequestBodyBytes <= 0 {
		return errors.New("HTTP_MAX_REQUEST_BODY_BYTES must be greater than zero")
	}
	if c.HTTP.RequestTimeout <= 0 {
		return errors.New("HTTP_REQUEST_TIMEOUT must be greater than zero")
	}
	if len(c.App.SupportedLocales) == 0 {
		return errors.New("APP_SUPPORTED_LOCALES must not be empty")
	}
	if !containsFold(c.App.SupportedLocales, c.App.FallbackLocale) {
		return fmt.Errorf("APP_FALLBACK_LOCALE %q must be part of APP_SUPPORTED_LOCALES", c.App.FallbackLocale)
	}
	if c.RateLimit.PerMinute <= 0 {
		return errors.New("RATE_LIMIT_PER_MINUTE must be greater than zero")
	}
	for _, conn := range c.Database.Connections {
		if err := validateConnection(conn); err != nil {
			return err
		}
	}
	switch c.Cache.Driver {
	case "memory", "file", "redis", "database":
	default:
		return fmt.Errorf("CACHE_DRIVER %q is not supported", c.Cache.Driver)
	}
	switch c.Queue.Driver {
	case "river", "sync":
	default:
		return fmt.Errorf("QUEUE_DRIVER %q is not supported", c.Queue.Driver)
	}
	if c.Queue.WorkerConcurrency < 1 {
		return errors.New("QUEUE_WORKER_CONCURRENCY must be at least 1")
	}
	for name, channel := range map[string]Channel{
		"QUEUE_CRITICAL": c.Queue.Critical,
		"QUEUE_DEFAULT":  c.Queue.Default,
		"QUEUE_HEAVY":    c.Queue.Heavy,
	} {
		if channel.Tries < 1 {
			return fmt.Errorf("%s_TRIES must be at least 1", name)
		}
		if channel.Timeout <= 0 {
			return fmt.Errorf("%s_TIMEOUT must be greater than zero", name)
		}
	}
	if _, err := time.LoadLocation(c.Schedule.Timezone); err != nil {
		return fmt.Errorf("SCHEDULE_TIMEZONE %q is invalid: %w", c.Schedule.Timezone, err)
	}
	if c.Auth.LoginMaxAttempts < 1 {
		return errors.New("AUTH_LOGIN_MAX_ATTEMPTS must be at least 1")
	}
	if c.Auth.OTPLength < 4 || c.Auth.OTPLength > 8 {
		return errors.New("AUTH_OTP_LENGTH must be between 4 and 8")
	}
	if c.Auth.BcryptRounds < 4 || c.Auth.BcryptRounds > 31 {
		return errors.New("BCRYPT_ROUNDS must be between 4 and 31")
	}
	if c.Auth.TokenExpiration < 0 {
		return errors.New("AUTH_TOKEN_EXPIRATION must not be negative")
	}
	switch c.Files.Disk {
	case "public", "s3":
	default:
		return fmt.Errorf("FILES_DISK %q is not supported (public|s3; local disks are not publicly addressable)", c.Files.Disk)
	}
	if c.Files.Disk == "s3" && !c.Storage.S3.Configured() {
		return errors.New("FILES_DISK=s3 requires S3_REGION, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY")
	}
	switch c.Processing.Disk {
	case "private", "public", "s3":
	default:
		return fmt.Errorf("DATA_PROCESSING_DISK %q is not supported (private|public|s3)", c.Processing.Disk)
	}
	if c.Processing.Disk == "s3" && !c.Storage.S3.Configured() {
		return errors.New("DATA_PROCESSING_DISK=s3 requires S3_REGION, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY")
	}
	switch c.Mail.Driver {
	case "log", "smtp":
	default:
		return fmt.Errorf("MAIL_DRIVER %q is not supported (log|smtp)", c.Mail.Driver)
	}
	if c.Mail.Driver == "smtp" {
		if c.Mail.Host == "" {
			return errors.New("MAIL_HOST must not be empty when MAIL_DRIVER=smtp")
		}
		if c.Mail.Port < 1 || c.Mail.Port > 65535 {
			return errors.New("MAIL_PORT must be between 1 and 65535")
		}
		if c.Mail.FromAddress == "" {
			return errors.New("MAIL_FROM_ADDRESS must not be empty when MAIL_DRIVER=smtp")
		}
	}
	return nil
}

func validateConnection(conn Connection) error {
	switch conn.Driver {
	case "pgsql":
		if conn.DSN == "" {
			return fmt.Errorf("DATABASE_%s_DSN must not be empty for connection %q", envKey(conn.Name), conn.Name)
		}
	case "sqlite":
		return fmt.Errorf("database connection %q: sqlite is not supported (PostgreSQL only in v1; deferred to a future version)", conn.Name)
	default:
		return fmt.Errorf("database connection %q: unsupported driver %q", conn.Name, conn.Driver)
	}
	if conn.MaxConns < 1 {
		return fmt.Errorf("database connection %q: max connections must be at least 1", conn.Name)
	}
	if conn.MinConns < 0 || conn.MinConns > conn.MaxConns {
		return fmt.Errorf("database connection %q: min connections must be between 0 and max connections", conn.Name)
	}
	return nil
}

func (c *Config) loadApp(envFile string) error {
	c.App = App{
		Name:             getString("APP_NAME", "ims-go-api"),
		Env:              getString("APP_ENV", "local"),
		URL:              getString("APP_URL", "http://localhost:8080"),
		Timezone:         getString("APP_TIMEZONE", "UTC"),
		Locale:           getString("APP_LOCALE", "en"),
		FallbackLocale:   getString("APP_FALLBACK_LOCALE", "en"),
		SupportedLocales: splitCSV(getString("APP_SUPPORTED_LOCALES", "en")),
		DataDir:          getString("APP_DATA_DIR", "./var"),
		StorageDir:       getString("APP_STORAGE_DIR", "./var/storage"),
		CacheDir:         getString("APP_CACHE_DIR", "./var/cache"),
		LogDir:           getString("APP_LOG_DIR", ""),
		LogLevel:         getString("APP_LOG_LEVEL", "info"),
		EnvFile:          envFile,
	}
	debug, err := getBool("APP_DEBUG", false)
	if err != nil {
		return err
	}
	c.App.Debug = debug
	return nil
}

func (c *Config) loadHTTP() error {
	c.HTTP.ListenAddr = getString("APP_LISTEN_ADDR", ":8080")

	var err error
	if c.HTTP.ReadTimeout, err = getDuration("HTTP_READ_TIMEOUT", 15*time.Second); err != nil {
		return err
	}
	if c.HTTP.WriteTimeout, err = getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second); err != nil {
		return err
	}
	if c.HTTP.IdleTimeout, err = getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second); err != nil {
		return err
	}
	if c.HTTP.ShutdownTimeout, err = getDuration("HTTP_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return err
	}
	if c.HTTP.RequestTimeout, err = getDuration("HTTP_REQUEST_TIMEOUT", 60*time.Second); err != nil {
		return err
	}
	if c.HTTP.MaxRequestBodyBytes, err = getInt64("HTTP_MAX_REQUEST_BODY_BYTES", 10*1024*1024); err != nil {
		return err
	}
	if c.HTTP.StrictJSONFields, err = getBool("HTTP_STRICT_JSON_FIELDS", true); err != nil {
		return err
	}
	c.HTTP.TrustedProxies = splitCSV(getString("HTTP_TRUSTED_PROXIES", ""))
	return nil
}

func (c *Config) loadRateLimit() error {
	perMinute, err := getInt("RATE_LIMIT_PER_MINUTE", 1000)
	if err != nil {
		return err
	}
	c.RateLimit.PerMinute = perMinute
	return nil
}

func (c *Config) loadDatabase() error {
	primary, err := loadConnection("primary", "DATABASE_")
	if err != nil {
		return err
	}
	connections := []Connection{primary}

	for _, name := range splitCSV(getString("DATABASE_CONNECTIONS", "")) {
		if strings.EqualFold(name, "primary") {
			continue
		}
		conn, err := loadConnection(name, "DATABASE_"+envKey(name)+"_")
		if err != nil {
			return err
		}
		connections = append(connections, conn)
	}

	c.Database = Database{Primary: primary, Connections: connections}
	return nil
}

func loadConnection(name, prefix string) (Connection, error) {
	conn := Connection{
		Name:   name,
		Driver: getString(prefix+"DRIVER", "pgsql"),
		DSN:    getString(prefix+"DSN", "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api?sslmode=disable"),
		Path:   getString(prefix+"PATH", ""),
	}

	maxConns, err := getInt(prefix+"MAX_CONNS", 20)
	if err != nil {
		return Connection{}, err
	}
	minConns, err := getInt(prefix+"MIN_CONNS", 2)
	if err != nil {
		return Connection{}, err
	}
	connTimeout, err := getDuration(prefix+"CONN_TIMEOUT", 5*time.Second)
	if err != nil {
		return Connection{}, err
	}
	conn.MaxConns = int32(maxConns)
	conn.MinConns = int32(minConns)
	conn.ConnTimeout = connTimeout
	return conn, nil
}

func envKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (c *Config) loadCache() error {
	c.Cache = Cache{
		Driver:  getString("CACHE_DRIVER", "memory"),
		Prefix:  getString("CACHE_PREFIX", "ims-"),
		FileDir: getString("CACHE_FILE_DIR", ""),
	}
	if c.Cache.FileDir == "" {
		c.Cache.FileDir = filepath.Join(c.App.CacheDir, "data")
	}
	return nil
}

func (c *Config) loadRedis() error {
	db, err := getInt("REDIS_DB", 0)
	if err != nil {
		return err
	}
	cacheDB, err := getInt("REDIS_CACHE_DB", 1)
	if err != nil {
		return err
	}
	c.Redis = Redis{
		Addr:     getString("REDIS_ADDR", "127.0.0.1:6379"),
		Password: getString("REDIS_PASSWORD", ""),
		DB:       db,
		CacheDB:  cacheDB,
	}
	return nil
}

func (c *Config) loadQueue() error {
	concurrency, err := getInt("QUEUE_WORKER_CONCURRENCY", 10)
	if err != nil {
		return err
	}
	critical, err := loadChannel("QUEUE_CRITICAL_", 3, 60)
	if err != nil {
		return err
	}
	defaultChannel, err := loadChannel("QUEUE_DEFAULT_", 5, 120)
	if err != nil {
		return err
	}
	heavy, err := loadChannel("QUEUE_HEAVY_", 1, 1800)
	if err != nil {
		return err
	}
	c.Queue = Queue{
		Driver:            getString("QUEUE_DRIVER", "river"),
		WorkerConcurrency: concurrency,
		Critical:          critical,
		Default:           defaultChannel,
		Heavy:             heavy,
	}
	return nil
}

func loadChannel(prefix string, defaultTries, defaultTimeoutSeconds int) (Channel, error) {
	tries, err := getInt(prefix+"TRIES", defaultTries)
	if err != nil {
		return Channel{}, err
	}
	timeoutSeconds, err := getInt(prefix+"TIMEOUT", defaultTimeoutSeconds)
	if err != nil {
		return Channel{}, err
	}
	return Channel{Tries: tries, Timeout: time.Duration(timeoutSeconds) * time.Second}, nil
}

func (c *Config) loadSchedule() error {
	c.Schedule = Schedule{Timezone: getString("SCHEDULE_TIMEZONE", c.App.Timezone)}
	return nil
}

func (c *Config) loadAuth() error {
	cfg := Auth{
		DefaultPassword: getString("AUTH_DEFAULT_PASSWORD", "123456"),
	}

	var err error
	if cfg.LoginMaxAttempts, err = getInt("AUTH_LOGIN_MAX_ATTEMPTS", 5); err != nil {
		return err
	}
	if cfg.CheckIsActive, err = getBool("AUTH_CHECK_IS_ACTIVE", true); err != nil {
		return err
	}
	if cfg.CheckEmailVerified, err = getBool("AUTH_CHECK_EMAIL_VERIFIED", true); err != nil {
		return err
	}
	if cfg.CheckPhoneVerified, err = getBool("AUTH_CHECK_PHONE_VERIFIED", false); err != nil {
		return err
	}
	if cfg.OTPEnabled, err = getBool("AUTH_OTP_ENABLED", false); err != nil {
		return err
	}
	if cfg.OTPLength, err = getInt("AUTH_OTP_LENGTH", 6); err != nil {
		return err
	}
	if cfg.BcryptRounds, err = getInt("BCRYPT_ROUNDS", 12); err != nil {
		return err
	}

	tokenMinutes, err := getInt("AUTH_TOKEN_EXPIRATION", 0)
	if err != nil {
		return err
	}
	decayMinutes, err := getInt("AUTH_LOGIN_DECAY_MINUTES", 1)
	if err != nil {
		return err
	}
	otpMinutes, err := getInt("AUTH_OTP_EXPIRY_MINUTES", 5)
	if err != nil {
		return err
	}
	cfg.TokenExpiration = time.Duration(tokenMinutes) * time.Minute
	cfg.LoginDecay = time.Duration(decayMinutes) * time.Minute
	cfg.OTPExpiry = time.Duration(otpMinutes) * time.Minute
	c.Auth = cfg
	return nil
}

func (c *Config) loadTenancy() error {
	tenantID, err := getInt64("DEFAULT_TENANT_ID", 1)
	if err != nil {
		return err
	}
	branchID, err := getInt64("DEFAULT_BRANCH_ID", 1)
	if err != nil {
		return err
	}
	active, err := getBool("DEFAULT_TENANT_IS_ACTIVE", true)
	if err != nil {
		return err
	}
	c.Tenancy = Tenancy{
		DefaultTenantID:     tenantID,
		DefaultTenantName:   getString("DEFAULT_TENANT_NAME", "Main Tenant"),
		DefaultTenantCode:   getString("DEFAULT_TENANT_CODE", "fad58de7366495db4650cfefac2fcd61"),
		DefaultTenantSlug:   getString("DEFAULT_TENANT_SLUG", "main-tenant"),
		DefaultTenantEmail:  getString("DEFAULT_TENANT_EMAIL", "tenant@example.com"),
		DefaultTenantPhone:  getString("DEFAULT_TENANT_PHONE", ""),
		DefaultTenantActive: active,
		DefaultBranchID:     branchID,
		DefaultBranchName:   getString("DEFAULT_BRANCH_NAME", "Default Branch"),
	}
	return nil
}

func (c *Config) loadBilling() error {
	c.Billing = Billing{
		Currency: getString("BILLING_CURRENCY", "BDT"),
		Bkash: Bkash{
			BaseURL:     getString("BKASH_BASE_URL", ""),
			AppKey:      getString("BKASH_APP_KEY", ""),
			AppSecret:   getString("BKASH_APP_SECRET", ""),
			Username:    getString("BKASH_USERNAME", ""),
			Password:    getString("BKASH_PASSWORD", ""),
			CallbackURL: getString("BKASH_CALLBACK_URL", ""),
		},
		Nagad: Nagad{
			BaseURL:        getString("NAGAD_BASE_URL", ""),
			MerchantID:     getString("NAGAD_MERCHANT_ID", ""),
			MerchantNumber: getString("NAGAD_MERCHANT_NUMBER", ""),
			PublicKey:      getString("NAGAD_PUBLIC_KEY", ""),
			PrivateKey:     getString("NAGAD_PRIVATE_KEY", ""),
		},
		Bank: BankTransfer{
			AccountName:   getString("BANK_TRANSFER_ACCOUNT_NAME", ""),
			AccountNumber: getString("BANK_TRANSFER_ACCOUNT_NUMBER", ""),
			BankName:      getString("BANK_TRANSFER_BANK_NAME", ""),
			Branch:        getString("BANK_TRANSFER_BRANCH", ""),
		},
	}
	return nil
}

// Configured reports whether the bKash credentials are complete.
func (b Bkash) Configured() bool {
	return b.BaseURL != "" && b.AppKey != "" && b.AppSecret != "" && b.Username != "" && b.Password != ""
}

func (c *Config) loadStorage() error {
	pathStyle, err := getBool("S3_PATH_STYLE", false)
	if err != nil {
		return err
	}
	publicDir := getString("STORAGE_PUBLIC_DIR", "")
	if publicDir == "" {
		publicDir = filepath.Join(c.App.StorageDir, "public")
	}
	privateDir := getString("STORAGE_PRIVATE_DIR", "")
	if privateDir == "" {
		privateDir = filepath.Join(c.App.StorageDir, "private")
	}
	c.Storage = Storage{
		PublicDir:  publicDir,
		PrivateDir: privateDir,
		S3: S3{
			Endpoint:  getString("S3_ENDPOINT", ""),
			Region:    getString("S3_REGION", ""),
			Bucket:    getString("S3_BUCKET", ""),
			AccessKey: getString("S3_ACCESS_KEY", ""),
			SecretKey: getString("S3_SECRET_KEY", ""),
			PathStyle: pathStyle,
			PublicURL: strings.TrimRight(getString("S3_PUBLIC_URL", ""), "/"),
		},
	}
	return nil
}

func (c *Config) loadFiles() error {
	c.Files = Files{Disk: getString("FILES_DISK", "public")}
	return nil
}

func (c *Config) loadProcessing() error {
	c.Processing = Processing{
		Disk:     getString("DATA_PROCESSING_DISK", "private"),
		BasePath: strings.Trim(getString("DATA_PROCESSING_BASE_PATH", "jobs"), "/"),
	}
	return nil
}

func (c *Config) loadMail() error {
	port, err := getInt("MAIL_PORT", 2525)
	if err != nil {
		return err
	}
	c.Mail = Mail{
		Driver:      getString("MAIL_DRIVER", "log"),
		Host:        getString("MAIL_HOST", "127.0.0.1"),
		Port:        port,
		Username:    getString("MAIL_USERNAME", ""),
		Password:    getString("MAIL_PASSWORD", ""),
		Encryption:  getString("MAIL_ENCRYPTION", ""),
		FromAddress: getString("MAIL_FROM_ADDRESS", "hello@example.com"),
		FromName:    getString("MAIL_FROM_NAME", c.App.Name),
	}
	return nil
}

// loadOps reads the optional operational surfaces. Docs default to enabled
// outside production (and off in production) unless DOCS_ENABLED is set;
// metrics and CORS are off unless explicitly configured.
func (c *Config) loadOps() error {
	docsDefault := !c.IsProduction()
	docs, err := getBool("DOCS_ENABLED", docsDefault)
	if err != nil {
		return err
	}
	metrics, err := getBool("METRICS_ENABLED", false)
	if err != nil {
		return err
	}
	maxAgeSecs, err := getInt("CORS_MAX_AGE", 600)
	if err != nil {
		return err
	}
	c.Ops = Ops{
		DocsEnabled:    docs,
		MetricsEnabled: metrics,
		CORS: CORS{
			AllowedOrigins: splitCSV(getString("CORS_ALLOWED_ORIGINS", "")),
			AllowedMethods: splitCSV(getString("CORS_ALLOWED_METHODS", "GET,POST,PATCH,PUT,DELETE,OPTIONS")),
			AllowedHeaders: splitCSV(getString("CORS_ALLOWED_HEADERS",
				"Authorization,Content-Type,Accept,Accept-Language,X-Acting-Tenant,X-Acting-Branch,X-Tenant-Code,X-Branch-Id,X-Request-Id")),
			MaxAge: time.Duration(maxAgeSecs) * time.Second,
		},
	}
	return nil
}

// loadSecurity reads the hardening switches.
func (c *Config) loadSecurity() error {
	headers, err := getBool("SECURITY_HEADERS_ENABLED", true)
	if err != nil {
		return err
	}
	c.Security = Security{HeadersEnabled: headers}
	return nil
}

func resolveEnvFile(explicit string) (string, error) {
	path := explicit
	required := path != ""
	if path == "" {
		path = os.Getenv("APP_ENV_FILE")
		required = path != ""
	}
	if path == "" {
		path = ".env"
	}
	if _, err := os.Stat(path); err != nil {
		if required {
			return "", fmt.Errorf("env file %q: %w", path, err)
		}
		return "", nil
	}
	return path, nil
}

func getString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getBool(key string, def bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s: invalid boolean %q", key, raw)
	}
	return v, nil
}

func getInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, raw)
	}
	return v, nil
}

func getInt64(key string, def int64) (int64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, raw)
	}
	return v, nil
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	raw = strings.TrimSpace(raw)
	if d, err := time.ParseDuration(raw); err == nil {
		return d, nil
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("%s: invalid duration %q", key, raw)
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsFold(list []string, needle string) bool {
	for _, item := range list {
		if strings.EqualFold(item, needle) {
			return true
		}
	}
	return false
}
