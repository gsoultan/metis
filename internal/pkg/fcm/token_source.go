package fcm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
)

const (
	// GoogleTokenURL is where a signed assertion is traded for an access token.
	GoogleTokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // a public endpoint address, not a credential
	messagingScope = "https://www.googleapis.com/auth/firebase.messaging"
	jwtBearerGrant = "urn:ietf:params:oauth:grant-type:jwt-bearer" //nolint:gosec // the RFC 7523 grant type name, not a credential
	assertionLife  = time.Hour
	// refreshMargin renews a token this long before Google says it expires, so
	// a send never starts with a token that lapses on the way.
	refreshMargin = 5 * time.Minute
)

// tokenSource hands out an access token for one service account, signing in
// again only when the one it holds is about to lapse.
type tokenSource struct {
	account  ServiceAccount
	tokenURL string
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// Token returns a token valid for at least refreshMargin.
func (s *tokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.now().Add(refreshMargin).Before(s.expires) {
		return s.token, nil
	}
	token, lifetime, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token, s.expires = token, s.now().Add(lifetime)
	return s.token, nil
}

// forget drops the held token, so the next send signs in again. Used when
// Google refuses a token it issued, which happens when a key is revoked.
func (s *tokenSource) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = ""
}

func (s *tokenSource) fetch(ctx context.Context) (string, time.Duration, error) {
	assertion, err := s.assertion()
	if err != nil {
		return "", 0, err
	}
	form := url.Values{"grant_type": {jwtBearerGrant}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("fcm: build sign-in request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fcm: sign in to Google: %w", err)
	}
	defer closeBody(resp)
	body, err := httpclient.ReadResponseBody(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("fcm: read Google's sign-in answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("%w: Google refused the service account (%s)", ErrCredentials, resp.Status)
	}
	return parseToken(body)
}

func parseToken(body []byte) (string, time.Duration, error) {
	var answer tokenResponse
	if err := json.Unmarshal(body, &answer); err != nil || answer.AccessToken == "" {
		return "", 0, fmt.Errorf("fcm: Google's sign-in answer held no access token")
	}
	return answer.AccessToken, time.Duration(answer.ExpiresIn) * time.Second, nil
}

func (s *tokenSource) assertion() (string, error) {
	issued := s.now()
	claims := jwt.MapClaims{
		"iss":   s.account.ClientEmail,
		"scope": messagingScope,
		"aud":   s.tokenURL,
		"iat":   issued.Unix(),
		"exp":   issued.Add(assertionLife).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(s.account.privateKey)
	if err != nil {
		return "", fmt.Errorf("fcm: sign the sign-in assertion: %w", err)
	}
	return signed, nil
}
