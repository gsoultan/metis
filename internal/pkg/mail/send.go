// Package mail hands a message to an SMTP server within a deadline.
//
// net/smtp's SendMail takes no context and sets no timeout: a server that
// accepts the connection and then says nothing holds the caller for as long as
// the operating system keeps the socket open. Inside a bounded worker pool that
// is a slot lost for good, and inside a transaction it is a connection and its
// row locks held for as long.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// DefaultTimeout bounds a send whose context carries no deadline of its own.
const DefaultTimeout = 30 * time.Second

// Send delivers msg to the recipients through the server at addr, as
// net/smtp.SendMail does — STARTTLS when offered, then auth — but gives up at
// ctx's deadline, or after DefaultTimeout when it has none.
func Send(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(DefaultTimeout)
	}
	dialCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return err
	}
	// The deadline covers every read and write after the dial, which is where
	// a silent server would otherwise hold the caller.
	if err := conn.SetDeadline(deadline); err != nil {
		return errors.Join(err, conn.Close())
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.Join(err, conn.Close())
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return errors.Join(err, conn.Close())
	}
	if err := deliver(client, host, auth, from, to, msg); err != nil {
		return errors.Join(err, client.Close())
	}
	return nil
}

// deliver is SendMail's conversation. Mail and Rcpt refuse a line break in an
// address, as SendMail does, so a recipient cannot smuggle in a header.
func deliver(client *smtp.Client, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp: the server does not accept authentication")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp: %s was refused: %w", recipient, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
