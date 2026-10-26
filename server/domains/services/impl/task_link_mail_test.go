package impl

import (
	"context"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

func TestTheLinkIsMailedToThePersonsOwnAddressAfterCommit(t *testing.T) {
	var sentTo []string
	var message string
	send := func(_ context.Context, _ string, _ smtp.Auth, _ string, to []string, msg []byte) error {
		sentTo, message = to, string(msg)
		return nil
	}
	var scheduled func(context.Context)
	schedule := func(_ context.Context, deliver func(context.Context)) { scheduled = deliver }

	mail := NewTaskLinkMailer(EmailSettings{Host: "smtp.example.com", From: "metis@example.com"},
		"https://metis.example.com", schedule, WithMailSender(send))
	link := servicecontracts.IssuedTaskLink{
		Person:    entities.Person{ID: uuid.New(), Username: "ada", DisplayName: "Ada", Email: "ada@example.com"},
		TaskID:    uuid.New(),
		Token:     "mtl_secret",
		ExpiresAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
	}
	mail(t.Context(), link, entities.ProcessEvent{Node: &entities.Node{Name: "Approve\r\nBcc: mallory@example.com"}})

	if sentTo != nil {
		t.Fatal("the link was mailed before the transaction that issued it committed")
	}
	if scheduled == nil {
		t.Fatal("nothing was scheduled")
	}
	scheduled(t.Context())
	if len(sentTo) != 1 || sentTo[0] != "ada@example.com" {
		t.Fatalf("mailed to %v, want ada's address alone", sentTo)
	}
	if !strings.Contains(message, "https://metis.example.com/p/t#mtl_secret") {
		t.Fatalf("the mail does not carry the link:\n%s", message)
	}
	if strings.Contains(message, "\r\nBcc:") {
		t.Fatalf("a step name broke the headers:\n%s", message)
	}
}

func TestThePublicAddressIsAnAbsoluteWebAddress(t *testing.T) {
	for _, good := range []string{"https://metis.example.com", "https://metis.example.com/", "http://localhost:8080/metis"} {
		if _, err := ParsePublicURL(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
	for _, bad := range []string{"metis.example.com", "ftp://metis.example.com", "https://", "https://u:p@metis.example.com", "https://metis.example.com/?a=b", "javascript:alert(1)"} {
		if _, err := ParsePublicURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	got, _ := ParsePublicURL("https://metis.example.com/")
	if url := TaskLinkURL(got, "mtl_x"); url != "https://metis.example.com/p/t#mtl_x" {
		t.Fatalf("link written as %q", url)
	}
}

func TestAnIssuedTokenIsKeptOnlyAsItsHash(t *testing.T) {
	token, hash, err := newTaskLinkToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, TaskLinkPrefix) || len(token) < 40 {
		t.Fatalf("token %q is not 32 random bytes", token)
	}
	if hash == token || strings.Contains(hash, strings.TrimPrefix(token, TaskLinkPrefix)) || hash != hashTaskLinkToken(token) {
		t.Fatalf("hash %q of %q", hash, token)
	}
	other, _, _ := newTaskLinkToken()
	if other == token {
		t.Fatal("two tokens alike")
	}
}

func TestALinkIsGoodOnlyUnusedUnvoidedAndInTime(t *testing.T) {
	now := time.Now()
	used, voided := now.Add(-time.Minute), now.Add(-time.Minute)
	if !(entities.TaskLink{ExpiresAt: now.Add(time.Hour)}).GoodAt(now) {
		t.Fatal("a fresh link is not good")
	}
	for name, link := range map[string]entities.TaskLink{
		"used":    {ExpiresAt: now.Add(time.Hour), UsedAt: &used},
		"voided":  {ExpiresAt: now.Add(time.Hour), VoidedAt: &voided},
		"expired": {ExpiresAt: now},
	} {
		if link.GoodAt(now) {
			t.Errorf("a %s link is good", name)
		}
	}
}
