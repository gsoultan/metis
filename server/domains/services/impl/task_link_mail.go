package impl

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// EnvPublicURL is the address people reach this server at, which a task
// link is written against. Unset, no links are sent: a link to wherever the
// server thinks it is would be a link to nowhere.
const EnvPublicURL = "METIS_PUBLIC_URL"

// EnvTaskLinkLifetime is how long a task link is good for, as a Go duration.
const EnvTaskLinkLifetime = "METIS_TASK_LINK_LIFETIME"

// TaskLinkPath is where a link opens. The token goes after a "#", in the
// part of an address a browser never sends: it reaches no proxy's access
// log and no server's, only the page, which posts it.
const TaskLinkPath = "/p/t"

// ParsePublicURL reads EnvPublicURL: an absolute http or https address with
// a host, and nothing after its path.
func ParsePublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s is not an address: %w", EnvPublicURL, err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%s must be an http or https address with a host, such as https://metis.example.com", EnvPublicURL)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%s must be an address with no query, fragment or credentials", EnvPublicURL)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// TaskLinkURL is the address a link opens at.
func TaskLinkURL(publicURL, token string) string {
	return publicURL + TaskLinkPath + "#" + token
}

// NewTaskLinkMailer mails each link to the person it is for, once the
// transaction that issued it commits: a link to a task a rollback undid is a
// link to nothing.
//
// The address is the person's in the directory, read when the link was
// issued and carried on it, rather than a User's: a link is for People.
func NewTaskLinkMailer(settings EmailSettings, publicURL string, schedule DeliveryScheduler, opts ...EmailOption) func(context.Context, servicecontracts.IssuedTaskLink, entities.ProcessEvent) {
	if schedule == nil {
		schedule = DeliverNow
	}
	return func(ctx context.Context, link servicecontracts.IssuedTaskLink, event entities.ProcessEvent) {
		to := link.Person.Email
		channel := NewEmailNotificationChannel(settings,
			func(context.Context, string) (string, error) { return to, nil }, opts...)
		if channel == nil {
			return
		}
		notification := entities.Notification{
			User:    &entities.User{Username: link.Person.Username},
			Type:    entities.NotificationTaskAssignment,
			Title:   taskLinkSubject(event),
			Message: taskLinkMessage(link, event),
			Link:    TaskLinkURL(publicURL, link.Token),
		}
		schedule(ctx, func(deliveryCtx context.Context) {
			if err := channel.Deliver(deliveryCtx, notification); err != nil {
				log.Warn().Err(err).Str("participant_id", link.Person.ID.String()).
					Msg("A task link was issued but not mailed; the task is in the person's inbox")
			}
		})
	}
}

func taskLinkSubject(event entities.ProcessEvent) string {
	if event.Node != nil && event.Node.Name != "" {
		return event.Node.Name + ": a task is waiting for you"
	}
	return "A task is waiting for you"
}

func taskLinkMessage(link servicecontracts.IssuedTaskLink, event entities.ProcessEvent) string {
	name := link.Person.DisplayName
	if name == "" {
		name = link.Person.Username
	}
	task := "A task"
	if event.Node != nil && event.Node.Name != "" {
		task = fmt.Sprintf("%q", event.Node.Name)
	}
	where := ""
	if event.Instance != nil && event.Instance.Definition != nil {
		if process := event.Instance.Definition.Name; process != "" {
			where = " in " + process
		} else if key := event.Instance.Definition.Key; key != "" {
			where = " in " + key
		}
	}
	return fmt.Sprintf("Hello %s,\r\n\r\n%s%s is waiting for you. Open the link below to see it and complete it. "+
		"The link works once, only for you, until %s.",
		name, task, where, link.ExpiresAt.UTC().Format(time.RFC1123))
}
