package secrets

import "testing"

func TestHashIsStableAndHidesTheSecret(t *testing.T) {
	a, b := Hash("spd_secret"), Hash("spd_secret")
	if a != b || len(a) != 64 || a == "spd_secret" || Hash("spd_other") == a {
		t.Fatalf("%q %q", a, b)
	}
}
