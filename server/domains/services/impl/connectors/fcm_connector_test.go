package connectors

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFCMConnectorRefusesBeforeCallingFirebase(t *testing.T) {
	cases := []struct {
		name    string
		config  map[string]any
		payload map[string]any
		want    string
	}{
		{"no key", map[string]any{}, map[string]any{"token": "d", "title": "t"}, "no service account key"},
		{"key is not a service account", map[string]any{FCMServiceAccountKey: "nope"}, map[string]any{"token": "d", "title": "t"}, "not a usable Firebase service account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := NewFCMConnector(nil).Execute(ctx, tc.config, tc.payload)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestFCMMessageValidatesThePayload(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"no token", map[string]any{"title": "t"}, "'token'"},
		{"nothing to show", map[string]any{"token": "d"}, "'title' or a 'body'"},
		{"data not an object", map[string]any{"token": "d", "body": "b", "data": "x"}, "object of text values"},
		{"data value not text", map[string]any{"token": "d", "body": "b", "data": map[string]any{"n": 5}}, `"n" must be text`},
		{"title is a number", map[string]any{"token": "d", "title": 5}, "'title' or a 'body'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fcmMessage(tc.payload)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	msg, err := fcmMessage(map[string]any{"token": "d", "body": "b", "link": "https://x", "data": map[string]any{"task": "1"}})
	if err != nil || msg.Data["task"] != "1" || msg.Link != "https://x" {
		t.Fatalf("a good payload gave %+v, %v", msg, err)
	}
}
