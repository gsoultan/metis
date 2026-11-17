package totp

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// rfcSecret is RFC 6238's SHA-1 test key, "12345678901234567890".
var rfcSecret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

// The RFC's appendix B gives eight digits; an authenticator shows the last six.
func TestCodesAreRFC6238s(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, c := range cases {
		got, err := CodeAt(rfcSecret, StepAt(time.Unix(c.unix, 0)))
		if err != nil {
			t.Fatalf("CodeAt(%d): %v", c.unix, err)
		}
		if got != c.want {
			t.Errorf("CodeAt(%d) = %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestACodeIsTakenNearNowAndOnce(t *testing.T) {
	now := time.Unix(1111111111, 0)
	step := StepAt(now)
	code, err := CodeAt(rfcSecret, step)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Match(rfcSecret, code, now, 0)
	if !ok || got != step {
		t.Fatalf("Match = %d, %v; want %d, true", got, ok, step)
	}
	if _, ok := Match(rfcSecret, code, now, step); ok {
		t.Fatal("a code was taken a second time")
	}
	previous, _ := CodeAt(rfcSecret, step-1)
	if _, ok := Match(rfcSecret, previous, now, step); ok {
		t.Fatal("an earlier code was taken after a later one")
	}
	if _, ok := Match(rfcSecret, previous, now, 0); !ok {
		t.Fatal("the code of the step before now was refused")
	}
	stale, _ := CodeAt(rfcSecret, step-2)
	if _, ok := Match(rfcSecret, stale, now, 0); ok {
		t.Fatal("a code two steps old was taken")
	}
}

func TestMalformedCodesMatchNothing(t *testing.T) {
	for _, code := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := Match(rfcSecret, code, time.Now(), 0); ok {
			t.Errorf("Match(%q) matched", code)
		}
	}
}

func TestSecretsAreFreshAndReadable(t *testing.T) {
	a, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewSecret()
	if a == b {
		t.Fatal("two secrets were the same")
	}
	if _, err := CodeAt(a, 1); err != nil {
		t.Fatalf("a new secret does not give a code: %v", err)
	}
	if !strings.HasPrefix(URI("Metis", "ana", a), "otpauth://totp/Metis:ana?") {
		t.Fatalf("URI = %s", URI("Metis", "ana", a))
	}
}
