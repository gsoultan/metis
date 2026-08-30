package entities_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

func TestResolveCorrelationKey(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]any
		want     string
		wantErr  bool
	}{
		{
			name:     "static key passes through unchanged",
			template: "fixed-conversation",
			vars:     map[string]any{"orderId": "order-1"},
			want:     "fixed-conversation",
		},
		{
			name:     "empty template stays empty",
			template: "",
			vars:     map[string]any{"orderId": "order-1"},
			want:     "",
		},
		{
			name:     "single placeholder resolves to the variable value",
			template: "${orderId}",
			vars:     map[string]any{"orderId": "order-1"},
			want:     "order-1",
		},
		{
			name:     "placeholder tolerates surrounding whitespace",
			template: "${ orderId }",
			vars:     map[string]any{"orderId": "order-1"},
			want:     "order-1",
		},
		{
			name:     "composite key interleaves literals and placeholders",
			template: "${tenantId}:${orderId}",
			vars:     map[string]any{"tenantId": "acme", "orderId": "order-1"},
			want:     "acme:order-1",
		},
		{
			// JSON numbers arrive as float64; an order id of 4200 must correlate
			// as "4200", not "4.2e+03" or "4200.0", or it will never match the
			// key the sender put on the wire.
			name:     "integral float renders without exponent or trailing zero",
			template: "${orderId}",
			vars:     map[string]any{"orderId": float64(4200)},
			want:     "4200",
		},
		{
			name:     "fractional float keeps its precision",
			template: "${amount}",
			vars:     map[string]any{"amount": 12.5},
			want:     "12.5",
		},
		{
			name:     "integer renders plainly",
			template: "${orderId}",
			vars:     map[string]any{"orderId": 77},
			want:     "77",
		},
		{
			// An unresolved key must not degrade to "". FindMessages treats an
			// empty correlation key as "do not filter", so a silent fallback
			// would deliver a message addressed to one instance to every
			// instance waiting on that message name.
			name:     "missing variable is an error, not an empty key",
			template: "${orderId}",
			vars:     map[string]any{"somethingElse": "x"},
			wantErr:  true,
		},
		{
			name:     "nil variable is an error, not an empty key",
			template: "${orderId}",
			vars:     map[string]any{"orderId": nil},
			wantErr:  true,
		},
		{
			name:     "nil variable map is an error",
			template: "${orderId}",
			vars:     nil,
			wantErr:  true,
		},
		{
			name:     "one missing variable fails the whole composite key",
			template: "${tenantId}:${orderId}",
			vars:     map[string]any{"tenantId": "acme"},
			wantErr:  true,
		},
		{
			name:     "variable resolving to an empty string is an error",
			template: "${orderId}",
			vars:     map[string]any{"orderId": ""},
			wantErr:  true,
		},
		{
			// Without this the malformed text passes through as a literal key
			// and silently matches nothing — the exact failure this resolver
			// exists to prevent.
			name:     "unterminated placeholder is an error, not a literal key",
			template: "${orderId",
			vars:     map[string]any{"orderId": "order-1"},
			wantErr:  true,
		},
		{
			name:     "unterminated placeholder after a valid one is an error",
			template: "${tenantId}:${orderId",
			vars:     map[string]any{"tenantId": "acme", "orderId": "order-1"},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := entities.ResolveCorrelationKey(tc.template, tc.vars)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got key %q", got)
				}
				if got != "" {
					t.Errorf("expected an empty key alongside the error, got %q", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
