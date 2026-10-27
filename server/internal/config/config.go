package config

import (
	"errors"
	"fmt"
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

type Config struct {
	ListenAddr    string
	DataDir       string
	Location      *time.Location
	SecureCookies bool
	DiscogsToken  string
	LastFMKey     string
	MQTT          MQTT
	Sessions      Sessions
}

func Load(getenv func(string) string) (*Config, error) {
	l := loader{getenv: getenv}
	c := &Config{
		ListenAddr:    l.str("SPINDLE_LISTEN", ":8080"),
		DataDir:       l.str("SPINDLE_DATA_DIR", "./data"),
		Location:      l.location("SPINDLE_TZ"),
		SecureCookies: l.boolean("SPINDLE_SECURE_COOKIES", false),
		DiscogsToken:  l.secret("SPINDLE_DISCOGS_TOKEN"),
		LastFMKey:     l.secret("SPINDLE_LASTFM_KEY"),
		MQTT: MQTT{
			URL:      l.str("SPINDLE_MQTT_URL", ""),
			Username: l.str("SPINDLE_MQTT_USERNAME", ""),
			Password: l.secret("SPINDLE_MQTT_PASSWORD"),
			Prefix:   l.str("SPINDLE_MQTT_DISCOVERY_PREFIX", "homeassistant"),
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
	return fmt.Sprintf("listen=%s data=%s tz=%s discogs_token=%s lastfm_key=%s mqtt=%t",
		c.ListenAddr, c.DataDir, c.Location, set(c.DiscogsToken), set(c.LastFMKey), c.MQTT.Enabled())
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
