package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"absence tolerance", c.Sessions.AbsenceTolerance, 90 * time.Second},
		{"min duration", c.Sessions.MinDuration, 5 * time.Minute},
		{"fallback long", c.Sessions.FallbackLong, 90 * time.Minute},
		{"auto resolve", c.Sessions.AutoResolveAfter, 7 * 24 * time.Hour},
		{"forgotten", c.Sessions.ForgottenAfter, 180 * 24 * time.Hour},
		{"catalog idle", c.Sessions.CatalogIdle, 10 * time.Minute},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if c.Sessions.LongFactor != 1.3 {
		t.Errorf("long factor: got %v", c.Sessions.LongFactor)
	}
	if c.ListenAddr != ":8080" {
		t.Errorf("listen addr: got %q", c.ListenAddr)
	}
	if c.MQTT.Enabled() {
		t.Error("mqtt should be disabled without url")
	}
	if c.SecureCookies {
		t.Error("secure cookies should be off by default")
	}
}

func TestOverrides(t *testing.T) {
	c, err := Load(env(map[string]string{
		"SPINDLE_ABSENCE_TOLERANCE":  "2m",
		"SPINDLE_AUTO_RESOLVE_AFTER": "3d",
		"SPINDLE_LONG_FACTOR":        "1.5",
		"SPINDLE_TZ":                 "Europe/Warsaw",
		"SPINDLE_MQTT_URL":           "mqtt://broker:1883",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sessions.AbsenceTolerance != 2*time.Minute {
		t.Errorf("tolerance: got %v", c.Sessions.AbsenceTolerance)
	}
	if c.Sessions.AutoResolveAfter != 72*time.Hour {
		t.Errorf("auto resolve: got %v", c.Sessions.AutoResolveAfter)
	}
	if c.Sessions.LongFactor != 1.5 {
		t.Errorf("long factor: got %v", c.Sessions.LongFactor)
	}
	if c.Location.String() != "Europe/Warsaw" {
		t.Errorf("tz: got %v", c.Location)
	}
	if !c.MQTT.Enabled() {
		t.Error("mqtt should be enabled")
	}
}

func TestInvalidValueNamesVariable(t *testing.T) {
	cases := map[string]string{
		"SPINDLE_MIN_DURATION":       "five",
		"SPINDLE_LONG_FACTOR":        "0.5",
		"SPINDLE_TZ":                 "Mars/Olympus",
		"SPINDLE_SECURE_COOKIES":     "maybe",
		"SPINDLE_AUTO_RESOLVE_AFTER": "twod",
		"SPINDLE_CATALOG_IDLE":       "0s",
	}
	for k, v := range cases {
		_, err := Load(env(map[string]string{k: v}))
		if err == nil {
			t.Errorf("%s=%q: expected error", k, v)
			continue
		}
		if !strings.Contains(err.Error(), k) {
			t.Errorf("%s: error %q does not name the variable", k, err)
		}
	}
}

func TestSecretNotInError(t *testing.T) {
	secret := "abc def\nsecret-token-value"
	_, err := Load(env(map[string]string{"SPINDLE_DISCOGS_TOKEN": secret}))
	if err == nil {
		t.Fatal("expected error for token with whitespace")
	}
	if strings.Contains(err.Error(), "secret-token-value") || strings.Contains(err.Error(), "abc") {
		t.Errorf("error leaks token: %q", err)
	}
	if !strings.Contains(err.Error(), "SPINDLE_DISCOGS_TOKEN") {
		t.Errorf("error does not name variable: %q", err)
	}
}

func TestSummaryRedactsSecrets(t *testing.T) {
	c, err := Load(env(map[string]string{
		"SPINDLE_DISCOGS_TOKEN": "tok123",
		"SPINDLE_LASTFM_KEY":    "key456",
		"SPINDLE_MQTT_URL":      "mqtt://broker:1883",
		"SPINDLE_MQTT_PASSWORD": "pw789",
	}))
	if err != nil {
		t.Fatal(err)
	}
	s := c.Summary()
	for _, v := range []string{"tok123", "key456", "pw789"} {
		if strings.Contains(s, v) {
			t.Errorf("summary leaks %q: %s", v, s)
		}
	}
	if !strings.Contains(s, "discogs_token=set") || !strings.Contains(s, "lastfm_key=set") {
		t.Errorf("summary hides what is configured: %s", s)
	}
	if bare, _ := Load(env(nil)); !strings.Contains(bare.Summary(), "discogs_token=unset") {
		t.Errorf("summary without a token: %s", bare.Summary())
	}
}

func TestSecretWithLeadingSpaceIsRejected(t *testing.T) {
	if _, err := Load(env(map[string]string{"SPINDLE_DISCOGS_TOKEN": " tok123"})); err == nil {
		t.Fatal("token starting with a space accepted")
	}
}

func TestLongFactorOfOneIsAccepted(t *testing.T) {
	c, err := Load(env(map[string]string{"SPINDLE_LONG_FACTOR": "1"}))
	if err != nil || c.Sessions.LongFactor != 1 {
		t.Fatalf("%v %v", c, err)
	}
}

func TestBackupDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"SPINDLE_DATA_DIR": "/srv/spindle"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Backup.Dir != "/srv/spindle/backups" || c.Backup.Every != 24*time.Hour || c.Backup.Keep != 7 {
		t.Fatalf("backup %+v", c.Backup)
	}
}

func TestBackupSettings(t *testing.T) {
	c, err := Load(env(map[string]string{"SPINDLE_BACKUP_DIR": "/backups", "SPINDLE_BACKUP_EVERY": "6h", "SPINDLE_BACKUP_KEEP": "30"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Backup.Dir != "/backups" || c.Backup.Every != 6*time.Hour || c.Backup.Keep != 30 {
		t.Fatalf("backup %+v", c.Backup)
	}
	off, err := Load(env(map[string]string{"SPINDLE_BACKUP_EVERY": "off"}))
	if err != nil || off.Backup.Every != 0 {
		t.Fatalf("off: %+v %v", off.Backup, err)
	}
	for k, v := range map[string]string{"SPINDLE_BACKUP_KEEP": "0", "SPINDLE_BACKUP_EVERY": "soon"} {
		if _, err := Load(env(map[string]string{k: v})); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: %v", k, v, err)
		}
	}
}

func TestTrustedProxies(t *testing.T) {
	c, err := Load(env(map[string]string{"SPINDLE_TRUSTED_PROXIES": "172.16.0.0/12, 127.0.0.1 ,::1"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 3 || c.TrustedProxies[1].String() != "127.0.0.1/32" || c.TrustedProxies[2].String() != "::1/128" {
		t.Fatalf("%v", c.TrustedProxies)
	}
	if !strings.Contains(c.Summary(), "trusted_proxies=3") {
		t.Errorf("summary: %s", c.Summary())
	}
	if _, err := Load(env(map[string]string{"SPINDLE_TRUSTED_PROXIES": "10.0.0.0/8,nope"})); err == nil ||
		!strings.Contains(err.Error(), "SPINDLE_TRUSTED_PROXIES") {
		t.Fatalf("bad entry: %v", err)
	}
	if bare, _ := Load(env(nil)); len(bare.TrustedProxies) != 0 {
		t.Fatalf("default %v", bare.TrustedProxies)
	}
}

func TestSummarySaysWhetherCookiesAreSecure(t *testing.T) {
	bare, _ := Load(env(nil))
	if !strings.Contains(bare.Summary(), "secure_cookies=auto") {
		t.Errorf("%s", bare.Summary())
	}
	on, _ := Load(env(map[string]string{"SPINDLE_SECURE_COOKIES": "true"}))
	if !strings.Contains(on.Summary(), "secure_cookies=always") {
		t.Errorf("%s", on.Summary())
	}
}
