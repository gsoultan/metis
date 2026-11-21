package impl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// The settings that say what this server is, as the servers it connects to
// see it.
const (
	// EnvServerName is what this server calls itself to the others.
	EnvServerName = "METIS_SERVER_NAME"
	// EnvServerPurpose is development, staging or production.
	EnvServerPurpose = "METIS_SERVER_PURPOSE"
)

// ServerHandshakePath is where a server answers another that was given one of
// its codes. It is public: the code admits the call.
const ServerHandshakePath = "/api/v1/server-connections/handshake"

const (
	// connectionCodeLifetime is how long a code is good for: long enough to
	// walk to the other server's page, short enough that one written down and
	// forgotten is no use.
	connectionCodeLifetime = 15 * time.Minute
	// connectionCodeBytes is the code's randomness: 80 bits, which nobody
	// guesses in fifteen minutes behind the request limit.
	connectionCodeBytes = 10
	// handshakeTimeout bounds the call to the other server.
	handshakeTimeout = 15 * time.Second
	// maxHandshakeReplyBytes bounds what the other server may answer.
	maxHandshakeReplyBytes = 16 << 10
	// maxServerNameLength is the column's size.
	maxServerNameLength = 255
	// maxServerURLLength is the column's size.
	maxServerURLLength = 2048
)

// codeEncoding writes a code in letters and digits that read aloud and type
// without confusion, with no padding.
var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type serverConnectionService struct {
	repo       repocontracts.ServerConnectionRepository
	keys       ReleaseKeys
	deliveries ReleaseDeliveryParts
	client     *http.Client
	collector  *http.Client
	now        func() time.Time
	name       func() string
	purpose    func() (entities.ServerPurpose, error)
}

// ServerIdentitySource says what this server is called and is for. Nil
// fields read METIS_SERVER_NAME and METIS_SERVER_PURPOSE.
type ServerIdentitySource struct {
	Name    func() string
	Purpose func() (entities.ServerPurpose, error)
}

// NewServerConnectionService returns the service that connects servers and
// sends releases over the connections. keys signs this server's side; nil is
// the connection keys over repo.
func NewServerConnectionService(repo repocontracts.ServerConnectionRepository, keys ReleaseKeys, identity ServerIdentitySource, deliveries ReleaseDeliveryParts) contracts.ServerConnectionService {
	if keys == nil {
		keys = NewConnectionReleaseKeys(repo)
	}
	if identity.Purpose == nil {
		identity.Purpose = ThisServerPurpose
	}
	if identity.Name == nil {
		identity.Name = func() string { return thisServerName(identity.Purpose) }
	}
	return &serverConnectionService{repo: repo, keys: keys, deliveries: deliveries,
		client: handshakeClient(), collector: collectClient(), now: time.Now,
		name: identity.Name, purpose: identity.Purpose}
}

// handshakeClient calls the server whose code an administrator typed in. It
// may reach a private address, as a server in a company network is, because
// an administrator chose it, not a process definition. It follows no
// redirect: the code goes only where it was sent.
func handshakeClient() *http.Client {
	return &http.Client{
		Timeout: handshakeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *serverConnectionService) ServerConnectionIdentity(ctx context.Context) (contracts.ServerIdentity, error) {
	if _, err := connectionAdmin(ctx); err != nil {
		return contracts.ServerIdentity{}, err
	}
	return s.identity(ctx)
}

func (s *serverConnectionService) identity(ctx context.Context) (contracts.ServerIdentity, error) {
	private, err := s.keys.SigningKey(ctx)
	if err != nil {
		return contracts.ServerIdentity{}, err
	}
	purpose, err := s.purpose()
	if err != nil {
		return contracts.ServerIdentity{}, err
	}
	return contracts.ServerIdentity{
		Name:        s.name(),
		Purpose:     purpose,
		Key:         publicKeyText(private),
		URL:         strings.TrimSpace(envvar.Get(EnvPublicURL)),
		TrustLocked: trustLocked(),
	}, nil
}

// trustLocked says the operator set the trusted keys in the environment,
// which then decides alone whose releases this server takes.
func trustLocked() bool {
	raw, set := envvar.FromEnvironment(EnvReleaseTrustedKeys)
	return set && strings.TrimSpace(raw) != ""
}

// errTrustLocked refuses a connection the environment would overrule.
var errTrustLocked = apierr.Forbiddenf(
	"the servers this one takes releases from are set in its environment (%s), so they are not changed here", EnvReleaseTrustedKeys)

func (s *serverConnectionService) MakeServerConnectionCode(ctx context.Context) (contracts.IssuedConnectionCode, error) {
	caller, err := connectionAdmin(ctx)
	if err != nil {
		return contracts.IssuedConnectionCode{}, err
	}
	raw := make([]byte, connectionCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return contracts.IssuedConnectionCode{}, fmt.Errorf("could not make a code: %w", err)
	}
	code := groupCode(codeEncoding.EncodeToString(raw))
	expires := s.now().UTC().Add(connectionCodeLifetime)
	if err := s.repo.CreateCode(ctx, entities.ConnectionCode{
		Hash: codeHash(code), CreatedBy: caller, ExpiresAt: expires,
	}); err != nil {
		return contracts.IssuedConnectionCode{}, err
	}
	return contracts.IssuedConnectionCode{Code: code, ExpiresAt: expires}, nil
}

func (s *serverConnectionService) CreateServerConnection(ctx context.Context, rawURL, code string) (entities.ServerConnection, error) {
	caller, err := connectionAdmin(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	if trustLocked() {
		return entities.ServerConnection{}, errTrustLocked
	}
	base, err := connectionURL(rawURL)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	code = normalCode(code)
	if code == "" {
		return entities.ServerConnection{}, apierr.Invalidf("enter the code the other server showed")
	}
	me, err := s.identity(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	reply, err := s.handshake(ctx, base, contracts.ServerHandshake{
		Format: contracts.ServerHandshakeFormat, Code: code,
		Name: me.Name, Purpose: me.Purpose, Key: me.Key, URL: me.URL,
	})
	if err != nil {
		return entities.ServerConnection{}, err
	}
	return s.repo.Create(ctx, entities.ServerConnection{
		Name: reply.Name, Purpose: reply.Purpose, Side: entities.ConnectionUpstream,
		URL: base, Key: reply.Key, Token: reply.Token,
		Words:       entities.ConnectionWords(code, reply.Key, me.Key),
		RequestedBy: caller,
	})
}

// handshake sends the code to the other server and reads its answer, which
// is believed only as far as it is well formed: the key it names is trusted
// once a second administrator here accepts it.
func (s *serverConnectionService) handshake(ctx context.Context, base string, hello contracts.ServerHandshake) (contracts.ServerHandshakeReply, error) {
	body, err := json.Marshal(hello)
	if err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ServerHandshakePath, bytes.NewReader(body))
	if err != nil {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("that address cannot be called: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("could not reach %s: %v", base, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Str("url", base).Msg("could not close the other server's reply")
		}
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHandshakeReplyBytes+1))
	if err != nil {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s did not answer in full: %v", base, err)
	}
	if len(data) > maxHandshakeReplyBytes {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s answered with more than a connection needs; is it a Metis server?", base)
	}
	if resp.StatusCode != http.StatusOK {
		var refused struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &refused) == nil && refused.Error != "" {
			return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s refused the connection: %s", base, refused.Error)
		}
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s refused the connection (%s); is it a Metis server, and is the code still good?", base, resp.Status)
	}
	var reply contracts.ServerHandshakeReply
	if err := json.Unmarshal(data, &reply); err != nil || reply.Format != contracts.ServerHandshakeFormat {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s did not answer as a Metis server does", base)
	}
	if err := checkPeer(reply.Name, reply.Purpose, reply.Key); err != nil {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s answered with %v", base, err)
	}
	if err := checkOrder(reply.Purpose, hello.Purpose); err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	if len(reply.Token) < 32 || len(reply.Token) > 128 {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("%s did not answer as a Metis server does", base)
	}
	return reply, nil
}

func (s *serverConnectionService) AnswerServerHandshake(ctx context.Context, hello contracts.ServerHandshake) (contracts.ServerHandshakeReply, error) {
	if hello.Format != contracts.ServerHandshakeFormat {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("this server connects with %s", contracts.ServerHandshakeFormat)
	}
	if err := checkPeer(hello.Name, hello.Purpose, hello.Key); err != nil {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("the connecting server sent %v", err)
	}
	peerURL := ""
	if strings.TrimSpace(hello.URL) != "" {
		u, err := connectionURL(hello.URL)
		if err != nil {
			return contracts.ServerHandshakeReply{}, err
		}
		peerURL = u
	}
	// The code first: until it is good, the caller is nobody, and learns
	// nothing about this server, not even what it is for. A good code is
	// spent even if what follows refuses the connection.
	code := normalCode(hello.Code)
	used, err := s.repo.UseCode(ctx, codeHash(code), s.now())
	if errors.Is(err, apierr.ErrNotFound) {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("that code is not one this server made, was used already, or has expired; make a new one")
	} else if err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	me, err := s.identity(ctx)
	if err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	if err := checkOrder(me.Purpose, hello.Purpose); err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	if hello.Key == me.Key {
		return contracts.ServerHandshakeReply{}, apierr.Invalidf("a server cannot connect to itself")
	}
	token, err := connectionToken()
	if err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	if _, err := s.repo.Create(ctx, entities.ServerConnection{
		Name: hello.Name, Purpose: hello.Purpose, Side: entities.ConnectionDownstream,
		URL: peerURL, Key: hello.Key, TokenHash: tokenHash(token),
		Words:       entities.ConnectionWords(code, me.Key, hello.Key),
		RequestedBy: used.CreatedBy,
	}); err != nil {
		return contracts.ServerHandshakeReply{}, err
	}
	return contracts.ServerHandshakeReply{
		Format: contracts.ServerHandshakeFormat, Name: me.Name, Purpose: me.Purpose, Key: me.Key, Token: token,
	}, nil
}

func (s *serverConnectionService) ListServerConnections(ctx context.Context) ([]entities.ServerConnection, error) {
	if _, err := connectionAdmin(ctx); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *serverConnectionService) AcceptServerConnection(ctx context.Context, id uuid.UUID) (entities.ServerConnection, error) {
	caller, err := connectionAdmin(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	if trustLocked() {
		return entities.ServerConnection{}, errTrustLocked
	}
	c, err := s.repo.Get(ctx, id)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	if c.Side != entities.ConnectionUpstream || c.AcceptedAt != nil {
		return entities.ServerConnection{}, apierr.Invalidf("%s is not waiting to be accepted", c.Name)
	}
	if c.RequestedBy == caller {
		return entities.ServerConnection{}, apierr.Forbiddenf(
			"%s connected %s, so another administrator accepts it: a server whose releases this one takes is trusted by two people", caller, c.Name)
	}
	accepted, err := s.repo.Accept(ctx, id, caller, s.now())
	if err != nil {
		return entities.ServerConnection{}, err
	}
	accepted.Token = ""
	return accepted, nil
}

func (s *serverConnectionService) RemoveServerConnection(ctx context.Context, id uuid.UUID) error {
	if _, err := connectionAdmin(ctx); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

// connectionAdmin is the administrator of every organization asking, by name.
// The route admits only one; this is the second lock.
func connectionAdmin(ctx context.Context) (string, error) {
	caller := signedIn(ctx)
	if caller == nil || caller.Username == "" || !slices.Contains(caller.Roles, entities.RoleAdmin) {
		return "", apierr.Forbiddenf("only an administrator of every organization can connect this server to others")
	}
	return caller.Username, nil
}

// ThisServerPurpose is what this server is for: METIS_SERVER_PURPOSE, or
// development when it is not set.
func ThisServerPurpose() (entities.ServerPurpose, error) {
	raw := strings.ToLower(strings.TrimSpace(envvar.Get(EnvServerPurpose)))
	if raw == "" {
		return entities.ServerDevelopment, nil
	}
	purpose := entities.ServerPurpose(raw)
	if !purpose.Valid() {
		return "", fmt.Errorf("%s is %q; it is development, staging or production", EnvServerPurpose, raw)
	}
	return purpose, nil
}

func thisServerName(purposeOf func() (entities.ServerPurpose, error)) string {
	if name := strings.TrimSpace(envvar.Get(EnvServerName)); name != "" {
		return name
	}
	purpose, err := purposeOf()
	if err != nil {
		return "Metis"
	}
	return strings.ToUpper(string(purpose[:1])) + string(purpose[1:])
}

// checkPeer refuses a server's description that is not one.
func checkPeer(name string, purpose entities.ServerPurpose, key string) error {
	switch {
	case strings.TrimSpace(name) == "" || len(name) > maxServerNameLength || strings.ContainsFunc(name, isControlRune):
		return errors.New("a name that is empty, too long or not one line")
	case !purpose.Valid():
		return fmt.Errorf("the purpose %q, which is not development, staging or production", purpose)
	}
	if decoded, err := base64.StdEncoding.DecodeString(key); err != nil || len(decoded) != ed25519.PublicKeySize {
		return errors.New("a key that is not a base64 Ed25519 public key")
	}
	return nil
}

func isControlRune(r rune) bool { return r < 0x20 || r == 0x7f }

// purposeRank orders the purposes the way releases travel.
func purposeRank(p entities.ServerPurpose) int {
	switch p {
	case entities.ServerDevelopment:
		return 0
	case entities.ServerStaging:
		return 1
	}
	return 2
}

// checkOrder refuses a connection that would send releases backwards:
// production takes them from staging or development, staging from
// development, and development from nobody.
func checkOrder(upstream, downstream entities.ServerPurpose) error {
	if purposeRank(upstream) >= purposeRank(downstream) {
		return apierr.Invalidf("a %s server takes releases from a server before it, and this one is %s; make the code on the server releases come from",
			downstream, upstream)
	}
	return nil
}

// connectionURL is a server's address as it is kept: an absolute https URL
// with no credentials, query or fragment, without a trailing slash. Plain
// http only where private networks are allowed, which is a developer's
// machine as a rule.
func connectionURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || len(raw) > maxServerURLLength {
		return "", apierr.Invalidf("enter the other server's address, such as https://dev.metis.example.com")
	}
	switch {
	case u.User != nil:
		return "", apierr.Invalidf("leave the user name and password out of the address")
	case u.RawQuery != "" || u.Fragment != "":
		return "", apierr.Invalidf("enter the server's address alone, without anything after a ? or #")
	case u.Scheme == "https":
	case u.Scheme == "http" && httpclient.PrivateNetworksAllowed():
	case u.Scheme == "http":
		return "", apierr.Invalidf("servers connect over https; plain http is allowed only where private networks are (METIS_HTTP_ALLOW_PRIVATE_NETWORKS)")
	default:
		return "", apierr.Invalidf("enter an https address")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// groupCode writes a code in groups of four, as people read it.
func groupCode(code string) string {
	var groups []string
	for len(code) > 4 {
		groups = append(groups, code[:4])
		code = code[4:]
	}
	return strings.Join(append(groups, code), "-")
}

// normalCode is a code as it is compared: capitals, groups as the code was
// made, whatever spaces or dashes were typed.
func normalCode(code string) string {
	code = strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, code))
	if code == "" || len(code) > 64 {
		return ""
	}
	return groupCode(code)
}

func codeHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// connectionToken is what a downstream server presents to collect its
// releases: 256 bits.
func connectionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("could not make a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// connectionReleaseKeys keeps the release keys in the database: a signing key
// made the first time one is needed, and the keys of the servers this one
// takes releases from once a second administrator accepted them.
// METIS_RELEASE_SIGNING_KEY and METIS_RELEASE_TRUSTED_KEYS, when set, still
// count: an operator who set the signing key in the environment keeps it.
type connectionReleaseKeys struct {
	repo repocontracts.ServerConnectionRepository
}

// NewConnectionReleaseKeys returns the release keys kept with the server's
// connections. They are also the release policy: a production with a staging
// before it takes a release live only with staging's note.
func NewConnectionReleaseKeys(repo repocontracts.ServerConnectionRepository) interface {
	ReleaseKeys
	ReleasePolicy
} {
	return connectionReleaseKeys{repo: repo}
}

func (k connectionReleaseKeys) SigningKey(ctx context.Context) (ed25519.PrivateKey, error) {
	if raw, set := envvar.FromEnvironment(EnvReleaseSigningKey); set && strings.TrimSpace(raw) != "" {
		return releaseSigningKey()
	}
	ctx = entities.WithSystemContext(ctx)
	seed, found, err := k.repo.SigningSeed(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		fresh := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(fresh); err != nil {
			return nil, fmt.Errorf("could not make a signing key: %w", err)
		}
		if seed, err = k.repo.SaveSigningSeed(ctx, fresh); err != nil {
			return nil, err
		}
	}
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("the signing key kept is not an Ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func (k connectionReleaseKeys) TrustedKeys(ctx context.Context) ([]string, error) {
	keys, err := releaseTrustedKeys()
	if err != nil || len(keys) > 0 {
		// Set in the environment, the operator's list is the whole of it.
		return keys, err
	}
	upstream, err := k.acceptedUpstream(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range upstream {
		if !slices.Contains(keys, c.Key) {
			keys = append(keys, c.Key)
		}
	}
	return keys, nil
}

// RequiresLiveNote is true when a staging this server takes releases from is
// accepted: a release then reaches production only once it went live there.
func (k connectionReleaseKeys) RequiresLiveNote(ctx context.Context, _ uuid.UUID) (bool, error) {
	upstream, err := k.acceptedUpstream(ctx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(upstream, func(c entities.ServerConnection) bool {
		return c.Purpose == entities.ServerStaging
	}), nil
}

func (k connectionReleaseKeys) acceptedUpstream(ctx context.Context) ([]entities.ServerConnection, error) {
	all, err := k.repo.List(entities.WithSystemContext(ctx))
	if err != nil {
		return nil, err
	}
	var accepted []entities.ServerConnection
	for _, c := range all {
		if c.Side == entities.ConnectionUpstream && c.AcceptedAt != nil && c.Trusted {
			accepted = append(accepted, c)
		}
	}
	return accepted, nil
}
