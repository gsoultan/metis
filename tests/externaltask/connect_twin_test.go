package externaltask_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/api/proto/services/servicesconnect"
)

// bearer signs every request of a Connect client in as one account.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// The Connect twin extends the same lock, and refuses the same worker. A
// service's refusal travels in the reply's error field over Connect, with no
// lock_expiration beside it.
func TestTheConnectTwinExtendsAndRefusesAsHTTPDoes(t *testing.T) {
	w := newWorld(t)
	taskID := w.fetch(t, holder)
	client := servicesconnect.NewExternalTaskServiceClient(
		&http.Client{Transport: bearer{token: w.member, next: w.server.Client().Transport}},
		w.server.URL+"/api/v1")

	asked := time.Now()
	reply, err := client.ExtendExternalTaskLock(t.Context(), connect.NewRequest(&pbendpoints.ExtendExternalTaskLockRequest{
		TaskId: taskID, WorkerId: holder, LockDurationMs: tenMinutesMS,
	}))
	if err != nil || reply.Msg.GetError() != "" || reply.Msg.GetLockExpiration() == nil {
		t.Fatalf("the holder extending over Connect: %v, %s", err, describe(reply))
	}
	until := reply.Msg.GetLockExpiration().AsTime()
	if early, late := asked.Add(10*time.Minute-time.Second), time.Now().Add(10*time.Minute+time.Second); until.Before(early) || until.After(late) {
		t.Fatalf("the lock now runs out at %v; want ten minutes from now", until)
	}

	refused, err := client.ExtendExternalTaskLock(t.Context(), connect.NewRequest(&pbendpoints.ExtendExternalTaskLockRequest{
		TaskId: taskID, WorkerId: anotherWorker, LockDurationMs: tenMinutesMS,
	}))
	if err != nil || !strings.Contains(refused.Msg.GetError(), "fetch it again") || refused.Msg.GetLockExpiration() != nil {
		t.Fatalf("another worker extending over Connect: %v, %s; want the refusal in the reply", err, describe(refused))
	}
}

func describe(reply *connect.Response[pbendpoints.ExtendExternalTaskLockResponse]) string {
	if reply == nil {
		return "no reply"
	}
	return fmt.Sprintf("error %q, lock_expiration %v", reply.Msg.GetError(), reply.Msg.GetLockExpiration())
}
