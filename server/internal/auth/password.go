package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	MinPassword  = 8
)

var (
	ErrWeakPassword = fmt.Errorf("password must be at least %d characters", MinPassword)
	ErrBusy         = errors.New("too many logins at once, try again in a moment")
)

// hashing limits how many Argon2 computations (19 MiB each) run at once. A request waits at most hashWait for a turn,
// so a flood of logins gets turned away instead of piling up behind each other.
var (
	hashing  = make(chan struct{}, 4)
	hashWait = 5 * time.Second
)

func argonKey(pw, salt []byte, t, m uint32, p uint8, n uint32) ([]byte, error) {
	wait := time.NewTimer(hashWait)
	defer wait.Stop()
	select {
	case hashing <- struct{}{}:
	case <-wait.C:
		return nil, ErrBusy
	}
	defer func() { <-hashing }()
	return argon2.IDKey(pw, salt, t, m, p, n), nil
}

func HashPassword(pw string) (string, error) {
	if len(pw) < MinPassword {
		return "", ErrWeakPassword
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := argonKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func VerifyPassword(encoded, pw string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash")
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got, err := argonKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
