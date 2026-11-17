package entities

import (
	"testing"
	"time"
)

func TestAnIntegrationKeyIsLiveUntilRevokedOrExpired(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	later, earlier := now.Add(time.Hour), now.Add(-time.Hour)
	for _, tc := range []struct {
		name string
		key  IntegrationAPIKey
		want bool
	}{
		{"no expiry", IntegrationAPIKey{}, true},
		{"expires later", IntegrationAPIKey{ExpiresAt: &later}, true},
		{"expired", IntegrationAPIKey{ExpiresAt: &earlier}, false},
		{"expires now", IntegrationAPIKey{ExpiresAt: &now}, false},
		{"revoked", IntegrationAPIKey{RevokedAt: &earlier}, false},
	} {
		if got := tc.key.Live(now); got != tc.want {
			t.Errorf("%s: Live = %v, want %v", tc.name, got, tc.want)
		}
	}
}
