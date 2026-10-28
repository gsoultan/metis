package impl

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"fmt"
	"sync"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/secrets"
)

// EnvSetupToken names the setup token an operator chooses instead of reading
// the generated one out of the log: an automated install, or several replicas
// behind one address, which would otherwise each print a token of their own.
const EnvSetupToken = "METIS_SETUP_TOKEN"

const (
	// generatedTokenBytes is 160 bits, which is not guessed through a wizard
	// that allows maxTokenFailures attempts a minute.
	generatedTokenBytes = 20

	// maxTokenFailures is how many wrong tokens one process takes in a
	// tokenFailureWindow before it stops comparing. It bounds a guessing run
	// against a chosen token, which may be weaker than a generated one.
	maxTokenFailures   = 10
	tokenFailureWindow = time.Minute
)

// ErrSetupTokenRequired refuses setup without the token this server printed.
var ErrSetupTokenRequired = fmt.Errorf(
	"%w: the setup token is missing or wrong. It is printed in the server's log when Metis starts unconfigured, "+
		"or it is the value of %s", apierr.ErrForbidden, EnvSetupToken)

// ErrSetupTokenThrottled refuses setup while too many wrong tokens have been
// tried.
var ErrSetupTokenThrottled = fmt.Errorf(
	"%w: too many wrong setup tokens; wait a minute and try again", apierr.ErrForbidden)

// setupToken is the secret that makes the first-run wizard the operator's.
//
// Setup is anonymous by necessity — nobody can sign in before it — so without
// this, whoever reaches a fresh installation first becomes its administrator:
// the window between a container starting and its operator opening the page
// belongs to anyone who can reach the port. The token is printed only where
// the operator looks, the server's log, and every setup call must carry it.
//
// It is compared by digest in constant time, so the time a refusal takes says
// nothing about it. The plain text is kept only for a generated token, which
// has to be printed, and is dropped once setup is done.
type setupToken struct {
	digest [sha256.Size]byte
	// generated means nobody chose it: the log is the only place it exists.
	generated bool
	// unusable is why the configured token was refused, if it was. Setup is
	// then closed rather than open without one: absent constraint means deny.
	unusable error

	mu          sync.Mutex
	windowStart time.Time
	failures    int
	now         func() time.Time
	plain       string
}

// forget drops the plain text once the installation is set up and the token
// can no longer open anything.
func (t *setupToken) forget() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.plain = ""
}

// printable returns the token to print, and whether there is one: a chosen
// token is the operator's already and is not echoed into the log.
func (t *setupToken) printable() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.plain, t.plain != ""
}

// newSetupToken takes the operator's token, or generates one when none was
// given. A configured token that would not pass as a secret is not replaced
// with a generated one — that would leave the operator's automation failing
// with a token they believe is right — but refused, and setup with it.
func newSetupToken(configured string) *setupToken {
	t := &setupToken{now: time.Now}
	if configured == "" {
		configured = generateSetupToken()
		t.generated = true
	} else if !secrets.Allowed() {
		if err := secrets.Validate(EnvSetupToken, configured); err != nil {
			t.unusable = err
		}
	}
	t.digest = sha256.Sum256([]byte(configured))
	if t.generated {
		t.plain = configured
	}
	return t
}

// generateSetupToken returns a token that reads cleanly off a log line.
func generateSetupToken() string {
	raw := make([]byte, generatedTokenBytes)
	// crypto/rand.Read does not fail on any platform Go supports, and panics
	// rather than return a short read where it could.
	_, _ = rand.Read(raw)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
}

// admit reports whether supplied is the setup token.
func (t *setupToken) admit(supplied string) error {
	if t.unusable != nil {
		return fmt.Errorf("%w: %s is set but cannot be used: %w", apierr.ErrForbidden, EnvSetupToken, t.unusable)
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if now := t.now(); now.Sub(t.windowStart) >= tokenFailureWindow {
		t.windowStart, t.failures = now, 0
	}
	if t.failures >= maxTokenFailures {
		return ErrSetupTokenThrottled
	}
	digest := sha256.Sum256([]byte(supplied))
	if subtle.ConstantTimeCompare(digest[:], t.digest[:]) != 1 {
		t.failures++
		return ErrSetupTokenRequired
	}
	return nil
}
