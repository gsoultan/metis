package interceptors_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// A signed-in caller who lacked a role was answered 401, the answer for a
// caller nobody knows. It told them to sign in again, which changes nothing,
// when what they needed was somebody to grant them the role. 401 is for a
// request with no identity; a known caller without the right gets 403, and
// the reply names the role.
//
// Through the real chain and the real error encoder, because the status is
// decided by the pair: the interceptor picks the error, the encoder the code.

func servedBehind(roles ...string) http.Handler {
	chain := interceptors.NewInterceptorFactory(nil, nil).ProtectedChainWithRoles("DeleteSomething", roles...)
	reached := func(context.Context, any) (any, error) {
		return map[string]string{"status": "done"}, nil
	}
	return httptransport.NewServer(
		chain(reached),
		func(context.Context, *http.Request) (any, error) { return nil, nil },
		common.EncodeResponse,
		httptransport.ServerErrorEncoder(common.EncodeError),
	)
}

func member(roles ...string) entities.User {
	return entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "dana",
		Roles:         roles,
		Organizations: []*entities.Organization{{ID: uuid.Must(uuid.NewV7())}},
	}
}

func TestARoleRefusalTellsASignedInCallerWhatIsMissing(t *testing.T) {
	cases := []struct {
		name      string
		required  []string
		principal any
		want      int
		mentions  []string
	}{
		{"nobody signed in", []string{entities.RoleAdmin}, nil, http.StatusUnauthorized, nil},
		{"a principal of no known kind", []string{entities.RoleAdmin}, "not-a-user", http.StatusUnauthorized, nil},
		{"signed in without the role", []string{entities.RoleAdmin}, member(entities.RoleUser),
			http.StatusForbidden, []string{entities.RoleAdmin}},
		{"signed in with neither of two roles", []string{entities.RoleAdmin, entities.RoleDesigner}, member(entities.RoleOperator),
			http.StatusForbidden, []string{entities.RoleAdmin, entities.RoleDesigner}},
		{"signed in with the role", []string{entities.RoleAdmin}, member(entities.RoleUser, entities.RoleAdmin),
			http.StatusOK, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/something", nil)
			if tc.principal != nil {
				req = req.WithContext(context.WithValue(req.Context(), pkgauth.UserContextKey, tc.principal))
			}
			rec := httptest.NewRecorder()
			servedBehind(tc.required...).ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("got %d (%s), want %d", rec.Code, strings.TrimSpace(rec.Body.String()), tc.want)
			}
			var reply struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
				t.Fatalf("the reply is not JSON: %q", rec.Body.String())
			}
			for _, role := range tc.mentions {
				if !strings.Contains(reply.Error, role) {
					t.Errorf("the refusal %q does not name the %s role it wants", reply.Error, role)
				}
			}
		})
	}
}
