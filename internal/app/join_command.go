package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/nacl/box"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

const (
	// joinPollEvery is how often a joining machine asks whether it was
	// accepted.
	joinPollEvery = 3 * time.Second
	// joinCallTimeout bounds each call to the installation.
	joinCallTimeout = 15 * time.Second
	// maxJoinReplyBytes bounds what the installation may answer: the sealed
	// settings at most.
	maxJoinReplyBytes = 64 << 10
	// joinDatabaseDialTimeout bounds the check that this machine reaches the
	// database it was given.
	joinDatabaseDialTimeout = 5 * time.Second
)

// joinCommand is `metis join <address> <code>`: add this machine to the
// installation at address, with a code from its Servers page. It shows four
// words to check against the page, waits for an administrator to press
// Accept, and writes the config.yaml it is handed. Nothing is copied by hand.
type joinCommand struct {
	out        io.Writer
	client     *http.Client
	configPath string
	hostname   func() (string, error)
	pollEvery  time.Duration
	dial       func(ctx context.Context, network, address string) (net.Conn, error)
}

func newJoinCommand(out io.Writer) *joinCommand {
	dialer := &net.Dialer{Timeout: joinDatabaseDialTimeout}
	return &joinCommand{
		out: out,
		client: &http.Client{
			Timeout: joinCallTimeout,
			// The code goes only where it was sent.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		configPath: config.DefaultConfigPath,
		hostname:   os.Hostname,
		pollEvery:  joinPollEvery,
		dial:       dialer.DialContext,
	}
}

func (a *App) runJoinCommand(ctx context.Context, args []string, out io.Writer) error {
	return newJoinCommand(out).run(ctx, args)
}

func (j *joinCommand) run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("join", flag.ContinueOnError)
	flags.SetOutput(j.out)
	name := flags.String("name", "", "what the Servers page calls this machine; its host name when empty")
	configPath := flags.String("config", j.configPath, "where to write the settings this machine is given")
	flags.Usage = func() {
		j.say("Usage: join <address> <code> [--name name] [--config path]\n")
		j.say("Adds this machine to the installation at address, with a code from its Servers page.\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(reorderJoinArgs(args)); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		flags.Usage()
		return errors.New("join takes the installation's address and the code the Servers page showed")
	}
	base, err := joinBase(flags.Arg(0))
	if err != nil {
		return err
	}
	code := flags.Arg(1)
	if config.Exists(*configPath) {
		return fmt.Errorf("%s is here already, so this machine is set up; move it aside to join this machine to another installation", *configPath)
	}
	machine := strings.TrimSpace(*name)
	if machine == "" {
		if machine, err = j.hostname(); err != nil {
			return fmt.Errorf("could not read this machine's host name; give one with --name: %w", err)
		}
	}

	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("could not make this machine's key: %w", err)
	}
	var reply contracts.MachineJoinReply
	if err := j.call(ctx, base+serviceimpl.MachineJoinPath, contracts.MachineJoinRequest{
		Format: contracts.MachineJoinFormat, Code: code, Name: machine,
		Key: base64.StdEncoding.EncodeToString(public[:]),
	}, &reply); err != nil {
		return err
	}
	j.say("\n  %s\n\n", reply.Words)
	j.say("Check that the Servers page at %s shows these four words for %s, then press Accept there.\n", base, machine)
	j.say("If it shows other words, press Refuse: something between here and there changed the request.\n")
	j.say("Waiting to be accepted…\n")

	sealed, err := j.waitForAccept(ctx, base, code, reply.ExpiresAt)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return fmt.Errorf("the settings this machine was handed are not base64: %w", err)
	}
	settings, ok := box.OpenAnonymous(nil, raw, public, private)
	if !ok {
		return errors.New("the settings this machine was handed do not open with its key; nothing was written")
	}
	cfg, err := config.Parse(settings)
	if err != nil {
		return fmt.Errorf("the settings this machine was handed could not be read; nothing was written: %w", err)
	}
	if err := writeNewFile(*configPath, settings); err != nil {
		return err
	}
	j.say("Accepted. Wrote %s: this machine runs %s.\n", *configPath, roleWords(cfg.Server.Role))
	j.checkDatabase(ctx, cfg)
	j.say("Start Metis here as usual; it appears on the Servers page within a few seconds.\n")
	return nil
}

// reorderJoinArgs puts the flags before the address and the code, so they
// may be written after them, as people do.
func reorderJoinArgs(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) && arg != "-h" && arg != "--help" && arg != "-help" {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, arg)
	}
	return append(flags, rest...)
}

// joinBase is the installation's address as given, without a trailing slash.
func joinBase(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%q is not an address such as https://metis.example.com", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q has a query or a fragment; give the address the Servers page is reached at", raw)
	}
	return raw, nil
}

func (j *joinCommand) waitForAccept(ctx context.Context, base, code string, until time.Time) (string, error) {
	ticker := time.NewTicker(j.pollEvery)
	defer ticker.Stop()
	for {
		var collection contracts.MachineJoinCollection
		if err := j.call(ctx, base+serviceimpl.MachineJoinCollectPath, struct {
			Format string `json:"format"`
			Code   string `json:"code"`
		}{contracts.MachineJoinFormat, code}, &collection); err != nil {
			return "", err
		}
		if collection.Accepted {
			return collection.Sealed, nil
		}
		if !until.IsZero() && time.Now().After(until) {
			return "", errors.New("nobody accepted this machine before its code ended; make a new code on the Servers page and run join again")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// call posts body and reads the answer into into, or the installation's
// refusal as an error.
func (j *joinCommand) call(ctx context.Context, endpoint string, body, into any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the installation: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Msg("join: could not close the installation's answer")
		}
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJoinReplyBytes))
	if err != nil {
		return fmt.Errorf("could not read the installation's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &refusal) == nil && refusal.Error != "" {
			return fmt.Errorf("the installation refused: %s", refusal.Error)
		}
		return fmt.Errorf("the installation answered %s; is this the address of a Metis server?", resp.Status)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("could not read the installation's answer: %w", err)
	}
	return nil
}

// checkDatabase says whether this machine reaches the database it was given:
// an installation whose database is on localhost, or behind a firewall that
// lets only its first machine in, is found out here, not at the next start.
func (j *joinCommand) checkDatabase(ctx context.Context, cfg *config.Config) {
	dsn, err := cfg.DecryptConnectionString("")
	if err != nil {
		j.say("Could not read the database it names: %v\n", err)
		return
	}
	parsed, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return
	}
	address := net.JoinHostPort(parsed.Host, fmt.Sprint(parsed.Port))
	conn, err := j.dial(ctx, "tcp", address)
	if err != nil {
		j.say("This machine cannot reach the database at %s (%v). Let it through, or give the installation a database address every machine reaches.\n", address, err)
		return
	}
	if err := conn.Close(); err != nil {
		log.Debug().Err(err).Msg("join: could not close the database check")
	}
	j.say("This machine reaches the database at %s.\n", address)
}

// writeNewFile writes data to path, only if nothing is there, readable by
// this user alone: it holds the database's password and the keys.
func writeNewFile(path string, data []byte) error {
	// #nosec G304 -- path comes from --config or the installation default, and
	// O_EXCL means nothing already there is opened.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(fmt.Errorf("could not write %s: %w", path, err), f.Close(), os.Remove(path))
	}
	return f.Close()
}

// say prints to the person running the command. The terminal may be gone,
// as under a script that closed it; the outcome is in the error returned.
func (j *joinCommand) say(format string, args ...any) {
	if _, err := fmt.Fprintf(j.out, format, args...); err != nil {
		log.Debug().Err(err).Msg("join: could not write to its output")
	}
}

func roleWords(role string) string {
	switch role {
	case string(RoleWeb):
		return "pages and API (web)"
	case string(RoleWorker):
		return "background work (worker)"
	}
	return "pages and API and background work (all)"
}
