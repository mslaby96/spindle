package config

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Sessions struct {
	AbsenceTolerance time.Duration
	MinDuration      time.Duration
	LongFactor       float64
	FallbackLong     time.Duration
	AutoResolveAfter time.Duration
	ForgottenAfter   time.Duration
	CatalogIdle      time.Duration
}

type MQTT struct {
	URL      string
	Username string
	Password string
	Prefix   string
}

func (m MQTT) Enabled() bool { return m.URL != "" }

type Backup struct {
	Dir   string
	Every time.Duration // zero turns the automatic backups off
	Keep  int
}

type Config struct {
	ListenAddr string
	DataDir    string
	Location   *time.Location
	// SecureCookies forces the Secure flag even on plain HTTP; without it the flag follows the request.
	SecureCookies bool
	// TrustedProxies may say who the client is, in X-Forwarded-For, and that it used HTTPS.
	TrustedProxies []netip.Prefix
	DiscogsToken   string
	LastFMKey      string
	MQTT           MQTT
	Backup         Backup
	Sessions       Sessions
}

func Load(getenv func(string) string) (*Config, error) {
	l := loader{getenv: getenv}
	dataDir := l.str("SPINDLE_DATA_DIR", "./data")
	c := &Config{
		ListenAddr:     l.str("SPINDLE_LISTEN", ":8080"),
		DataDir:        dataDir,
		Location:       l.location("SPINDLE_TZ"),
		SecureCookies:  l.boolean("SPINDLE_SECURE_COOKIES", false),
		TrustedProxies: l.prefixes("SPINDLE_TRUSTED_PROXIES"),
		DiscogsToken:   l.secret("SPINDLE_DISCOGS_TOKEN"),
		LastFMKey:      l.secret("SPINDLE_LASTFM_KEY"),
		MQTT: MQTT{
			URL:      l.str("SPINDLE_MQTT_URL", ""),
			Username: l.str("SPINDLE_MQTT_USERNAME", ""),
			Password: l.secret("SPINDLE_MQTT_PASSWORD"),
			Prefix:   l.str("SPINDLE_MQTT_DISCOVERY_PREFIX", "homeassistant"),
		},
		Backup: Backup{
			Dir:   l.str("SPINDLE_BACKUP_DIR", filepath.Join(dataDir, "backups")),
			Every: l.optionalDuration("SPINDLE_BACKUP_EVERY", 24*time.Hour),
			Keep:  l.count("SPINDLE_BACKUP_KEEP", 7),
		},
		Sessions: Sessions{
			AbsenceTolerance: l.duration("SPINDLE_ABSENCE_TOLERANCE", 90*time.Second),
			MinDuration:      l.duration("SPINDLE_MIN_DURATION", 5*time.Minute),
			LongFactor:       l.factor("SPINDLE_LONG_FACTOR", 1.3),
			FallbackLong:     l.duration("SPINDLE_FALLBACK_LONG", 90*time.Minute),
			AutoResolveAfter: l.duration("SPINDLE_AUTO_RESOLVE_AFTER", 7*24*time.Hour),
			ForgottenAfter:   l.duration("SPINDLE_FORGOTTEN_AFTER", 180*24*time.Hour),
			CatalogIdle:      l.duration("SPINDLE_CATALOG_IDLE", 10*time.Minute),
		},
	}
	if len(l.errs) > 0 {
		return nil, errors.Join(l.errs...)
	}
	return c, nil
}

func (c *Config) Summary() string {
	set := func(v string) string {
		if v == "" {
			return "unset"
		}
		return "set"
	}
	backups := "off"
	if c.Backup.Every > 0 {
		backups = fmt.Sprintf("%s every %s, keep %d", c.Backup.Dir, c.Backup.Every, c.Backup.Keep)
	}
	secure := "auto"
	if c.SecureCookies {
		secure = "always"
	}
	return fmt.Sprintf("listen=%s data=%s tz=%s discogs_token=%s lastfm_key=%s mqtt=%t backups=%q secure_cookies=%s trusted_proxies=%d",
		c.ListenAddr, c.DataDir, c.Location, set(c.DiscogsToken), set(c.LastFMKey), c.MQTT.Enabled(), backups, secure,
		len(c.TrustedProxies))
}

type loader struct {
	getenv func(string) string
	errs   []error
}

func (l *loader) fail(key, format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf("%s: %s", key, fmt.Sprintf(format, args...)))
}

func (l *loader) str(key, def string) string {
	if v := strings.TrimSpace(l.getenv(key)); v != "" {
		return v
	}
	return def
}

func (l *loader) secret(key string) string {
	v := l.getenv(key)
	if strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		l.fail(key, "contains whitespace")
		return ""
	}
	return v
}

// prefixes reads a comma-separated list of addresses and networks, e.g. "172.16.0.0/12, 127.0.0.1".
func (l *loader) prefixes(key string) []netip.Prefix {
	var out []netip.Prefix
	for _, item := range strings.Split(l.getenv(key), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			addr, aerr := netip.ParseAddr(item)
			if aerr != nil {
				l.fail(key, "expected addresses or networks, got %q", item)
				continue
			}
			p = netip.PrefixFrom(addr, addr.BitLen())
		}
		out = append(out, p.Masked())
	}
	return out
}

func (l *loader) boolean(key string, def bool) bool {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, "expected true or false, got %q", v)
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	d, err := parseDuration(v)
	if err != nil || d <= 0 {
		l.fail(key, "expected a positive duration like 90s, 5m or 7d, got %q", v)
		return def
	}
	return d
}

// optionalDuration is a duration that can also be turned off with "off".
func (l *loader) optionalDuration(key string, def time.Duration) time.Duration {
	if strings.EqualFold(strings.TrimSpace(l.getenv(key)), "off") {
		return 0
	}
	return l.duration(key, def)
}

func (l *loader) count(key string, def int) int {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		l.fail(key, "expected a whole number of at least 1, got %q", v)
		return def
	}
	return n
}

func (l *loader) factor(key string, def float64) float64 {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 1 {
		l.fail(key, "expected a number >= 1, got %q", v)
		return def
	}
	return f
}

func (l *loader) location(key string) *time.Location {
	v := l.getenv(key)
	if v == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(v)
	if err != nil {
		l.fail(key, "unknown time zone %q", v)
		return time.Local
	}
	return loc
}

func parseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
