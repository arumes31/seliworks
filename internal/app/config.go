package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppURL, DatabaseURL, DataDir, StaticDir, ListenAddr, Environment string
	TrustedProxies                                                   []netip.Prefix
	TOTPKey                                                          []byte
	SMTPHost, SMTPPort, SMTPUsername, SMTPPassword, SMTPFrom         string
	SMTPImplicitTLS, SMTPAllowInsecure                               bool
}

func LoadConfig() (Config, error) {
	c := Config{AppURL: os.Getenv("APP_URL"), DatabaseURL: os.Getenv("DATABASE_URL"), DataDir: envDefault("DATA_DIR", "data"), StaticDir: envDefault("STATIC_DIR", "web/dist"), ListenAddr: envDefault("LISTEN_ADDR", ":8080"), Environment: envDefault("APP_ENV", "production")}
	if p := os.Getenv("DATABASE_URL_FILE"); p != "" {
		b, e := os.ReadFile(p)
		if e != nil {
			return c, fmt.Errorf("database secret file: %w", e)
		}
		c.DatabaseURL = strings.TrimSpace(string(b))
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL or DATABASE_URL_FILE is required")
	}
	u, e := url.Parse(c.AppURL)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("APP_URL must be an absolute origin without credentials, query or path")
	}
	if u.Scheme != "https" && (c.Environment != "development" || u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1")) {
		return c, errors.New("APP_URL must use HTTPS; loopback HTTP is allowed only in development")
	}
	c.AppURL = strings.TrimRight(c.AppURL, "/")
	c.SMTPHost = os.Getenv("SMTP_HOST")
	c.SMTPPort = envDefault("SMTP_PORT", "587")
	c.SMTPUsername = os.Getenv("SMTP_USERNAME")
	c.SMTPFrom = os.Getenv("SMTP_FROM")
	c.SMTPImplicitTLS = os.Getenv("SMTP_IMPLICIT_TLS") == "true"
	if p := os.Getenv("SMTP_PASSWORD_FILE"); p != "" {
		b, e := os.ReadFile(p)
		if e != nil {
			return c, errors.New("cannot read SMTP_PASSWORD_FILE")
		}
		c.SMTPPassword = strings.TrimRight(string(b), "\r\n")
	}
	if c.SMTPHost != "" && !validMailAddress(c.SMTPFrom) {
		return c, errors.New("SMTP_FROM must be a valid email address when SMTP is configured")
	}
	if p := os.Getenv("TOTP_KEY_FILE"); p != "" {
		b, e := os.ReadFile(p)
		if e != nil {
			return c, errors.New("cannot read TOTP_KEY_FILE")
		}
		c.TOTPKey, e = hex.DecodeString(strings.TrimSpace(string(b)))
		if e != nil || len(c.TOTPKey) != 32 {
			return c, errors.New("TOTP_KEY_FILE requires 64 hexadecimal characters")
		}
	}
	for _, s := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		p, e := netip.ParsePrefix(strings.TrimSpace(s))
		if e != nil || p.Bits() == 0 {
			return c, errors.New("trusted proxies require explicit, non-global CIDRs")
		}
		c.TrustedProxies = append(c.TrustedProxies, p)
	}
	return c, nil
}
func envDefault(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
func OpenDB(ctx context.Context, url string) (*pgxpool.Pool, error) {
	c, e := pgxpool.ParseConfig(url)
	if e != nil {
		return nil, errors.New("invalid database configuration")
	}
	c.MaxConns = 20
	c.MinConns = 1
	c.MaxConnLifetime = time.Hour
	c.ConnConfig.ConnectTimeout = 5 * time.Second
	c.ConnConfig.RuntimeParams["timezone"] = "UTC"
	c.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	return pgxpool.NewWithConfig(ctx, c)
}
