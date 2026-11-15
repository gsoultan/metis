package impl

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

func TestRetryAdviceOf(t *testing.T) {
	testCases := []struct {
		name          string
		err           error
		wantRetryable bool
		wantWait      time.Duration
	}{
		{"a failure that says nothing is retried", errors.New("reset by peer"), true, 0},
		{"a final failure is not", &connectors.ManifestError{Code: "CARD_DECLINED"}, false, 0},
		{"a wrapped retryable failure keeps its wait",
			fmt.Errorf("connector execution failed: %w", &connectors.ManifestError{Retryable: true, RetryAfter: time.Minute}), true, time.Minute},
		{"a partner's wait is bounded", &connectors.ManifestError{Retryable: true, RetryAfter: 48 * time.Hour}, true, maxPartnerRetryWait},
		{"a negative wait is none", &connectors.ManifestError{Retryable: true, RetryAfter: -time.Second}, true, 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			retryable, wait := retryAdviceOf(tc.err)
			if retryable != tc.wantRetryable || wait != tc.wantWait {
				t.Errorf("retryAdviceOf = (%v, %s), want (%v, %s)", retryable, wait, tc.wantRetryable, tc.wantWait)
			}
		})
	}
}
