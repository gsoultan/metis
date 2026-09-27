package connects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
)

// An error the endpoint chain returns reaches a Connect caller with the code
// its REST twin answers with, and not as `unknown`.

func refused(err error) error {
	next := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, err
	}
	_, got := refusalCodes()(next)(context.Background(), connect.NewRequest(&struct{}{}))
	return got
}

func TestEachRefusalGetsTheCodeRESTAnswersWith(t *testing.T) {
	cases := []struct {
		err  error
		want connect.Code
	}{
		{auth.ErrUnauthorized, connect.CodeUnauthenticated},
		{auth.ErrAuthenticationFailed, connect.CodeUnauthenticated},
		{fmt.Errorf("%w: this needs the ADMIN role", apierr.ErrForbidden), connect.CodePermissionDenied},
		{fmt.Errorf("%w: name is required", apierr.ErrInvalidArgument), connect.CodeInvalidArgument},
		{fmt.Errorf("%w: project", apierr.ErrNotFound), connect.CodeNotFound},
		{errors.New("the database went away"), connect.CodeInternal},
	}
	for _, c := range cases {
		if got := connect.CodeOf(refused(c.err)); got != c.want {
			t.Errorf("%q came back as %v, want %v", c.err, got, c.want)
		}
	}
}

func TestAConnectErrorKeepsItsOwnCode(t *testing.T) {
	made := connect.NewError(connect.CodeFailedPrecondition, errors.New("already completed"))
	if got := refused(made); !errors.Is(got, made) || connect.CodeOf(got) != connect.CodeFailedPrecondition {
		t.Fatalf("a Connect error was replaced with %v", got)
	}
}

func TestTheMessageIsRedactedLikeREST(t *testing.T) {
	got := refused(fmt.Errorf("%w: dial failed password=hunter2", apierr.ErrInvalidArgument))
	var connectErr *connect.Error
	if !errors.As(got, &connectErr) {
		t.Fatalf("got %T, want a Connect error", got)
	}
	if strings.Contains(connectErr.Message(), "hunter2") {
		t.Fatalf("the message carries the password: %q", connectErr.Message())
	}
}

func TestSuccessPassesThrough(t *testing.T) {
	if err := refused(nil); err != nil {
		t.Fatalf("a successful call came back with %v", err)
	}
}
