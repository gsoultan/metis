package pg

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/connectioncode"
	"github.com/gsoultan/metis/server/repositories/store/releasedelivery"
	"github.com/gsoultan/metis/server/repositories/store/serverconnection"
	"github.com/gsoultan/metis/server/repositories/store/serversigningkey"
)

type serverConnectionRepository struct{ conn }

// NewServerConnectionRepository returns the store of the servers releases are
// exchanged with. Installation-wide, so no tenant scope applies.
func NewServerConnectionRepository(c *db.Conn) contracts.ServerConnectionRepository {
	return &serverConnectionRepository{conn{conn: c}}
}

// maxServerConnections bounds the connections read: an installation has a
// handful, one per server before and after it.
const maxServerConnections = 100

func (r *serverConnectionRepository) CreateCode(ctx context.Context, code entities.ConnectionCode) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := connectioncode.Create()
	ins.SetID(uuid.Must(uuid.NewV7()))
	ins.SetHash(code.Hash)
	ins.SetCreatedBy(code.CreatedBy)
	ins.SetExpiresAt(code.ExpiresAt.UTC())
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not keep the code: %w", err)
	}
	return nil
}

// UseCode is one UPDATE, so two servers that race with the same code cannot
// both have it.
func (r *serverConnectionRepository) UseCode(ctx context.Context, hash string, now time.Time) (entities.ConnectionCode, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.ConnectionCode{}, err
	}
	var used connectioncode.Mut
	used.SetUsedAt(now.UTC())
	n, err := connectioncode.New().
		Where(connectioncode.Hash.Eq(hash), connectioncode.UsedAt.IsNull(), connectioncode.ExpiresAt.Gt(now.UTC())).
		UpdateAll(ctx, ex, used)
	if err != nil {
		return entities.ConnectionCode{}, fmt.Errorf("could not use the code: %w", err)
	}
	if n == 0 {
		return entities.ConnectionCode{}, fmt.Errorf("%w: no such code", apierr.ErrNotFound)
	}
	row, found, err := connectioncode.New().Where(connectioncode.Hash.Eq(hash)).One(ctx, ex)
	if err != nil || !found {
		return entities.ConnectionCode{}, fmt.Errorf("could not read the code back: %w", err)
	}
	code := entities.ConnectionCode{ID: row.ID, Hash: row.Hash, CreatedBy: row.CreatedBy, ExpiresAt: row.ExpiresAt}
	if at, ok := row.UsedAt.Get(); ok {
		code.UsedAt = &at
	}
	return code, nil
}

func (r *serverConnectionRepository) Create(ctx context.Context, c entities.ServerConnection) (entities.ServerConnection, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	if _, found, err := serverconnection.New().
		Where(serverconnection.Key.Eq(c.Key), serverconnection.Side.Eq(string(c.Side))).
		One(ctx, ex); err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not read the connections: %w", err)
	} else if found {
		return entities.ServerConnection{}, apierr.Invalidf("this server is already connected to %s; remove that connection first to connect it again", c.Name)
	}
	token := ""
	if c.Token != "" {
		if token, err = crypto.Encrypt(c.Token); err != nil {
			return entities.ServerConnection{}, fmt.Errorf("could not seal the connection's token: %w", err)
		}
	}
	ins := serverconnection.Create()
	ins.SetID(uuid.Must(uuid.NewV7()))
	ins.SetName(c.Name)
	ins.SetPurpose(string(c.Purpose))
	ins.SetSide(string(c.Side))
	ins.SetURL(c.URL)
	ins.SetKey(c.Key)
	ins.SetWords(c.Words)
	ins.SetToken(token)
	ins.SetTokenHash(c.TokenHash)
	ins.SetTrust("")
	ins.SetRequestedBy(c.RequestedBy)
	ins.SetAcceptedBy("")
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not keep the connection: %w", err)
	}
	return serverConnectionFrom(row, false)
}

func (r *serverConnectionRepository) Get(ctx context.Context, id uuid.UUID) (entities.ServerConnection, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	row, found, err := serverconnection.New().Where(serverconnection.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not read the connection: %w", err)
	}
	if !found {
		return entities.ServerConnection{}, fmt.Errorf("%w: no such connection", apierr.ErrNotFound)
	}
	return serverConnectionFrom(row, true)
}

func (r *serverConnectionRepository) List(ctx context.Context) ([]entities.ServerConnection, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := serverconnection.New().
		Order(serverconnection.CreatedAt.Asc(), serverconnection.ID.Asc()).
		Limit(maxServerConnections).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the connections: %w", err)
	}
	out := make([]entities.ServerConnection, 0, len(rows))
	for _, row := range rows {
		c, err := serverConnectionFrom(row, false)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Accept is one UPDATE that only an unaccepted upstream connection matches,
// so two administrators accepting at once record one of them.
func (r *serverConnectionRepository) Accept(ctx context.Context, id uuid.UUID, by string, at time.Time) (entities.ServerConnection, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.ServerConnection{}, err
	}
	row, found, err := serverconnection.New().Where(serverconnection.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not read the connection: %w", err)
	}
	if !found {
		return entities.ServerConnection{}, fmt.Errorf("%w: no connection waiting to be accepted", apierr.ErrNotFound)
	}
	trust, err := crypto.Encrypt(trustStatement(row.ID, row.Key))
	if err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not seal the connection's trust: %w", err)
	}
	var accepted serverconnection.Mut
	accepted.SetAcceptedBy(by)
	accepted.SetAcceptedAt(at.UTC())
	accepted.SetTrust(trust)
	n, err := serverconnection.New().
		Where(serverconnection.ID.Eq(id),
			serverconnection.Key.Eq(row.Key),
			serverconnection.Side.Eq(string(entities.ConnectionUpstream)),
			serverconnection.AcceptedAt.IsNull()).
		UpdateAll(ctx, ex, accepted)
	if err != nil {
		return entities.ServerConnection{}, fmt.Errorf("could not accept the connection: %w", err)
	}
	if n == 0 {
		return entities.ServerConnection{}, fmt.Errorf("%w: no connection waiting to be accepted", apierr.ErrNotFound)
	}
	return r.Get(ctx, id)
}

func (r *serverConnectionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	// The releases sent over it first: they mean nothing without it.
	if _, err := releasedelivery.New().Where(releasedelivery.ConnectionID.Eq(id.String())).DeleteAll(ctx, ex); err != nil {
		return fmt.Errorf("could not remove the releases sent over the connection: %w", err)
	}
	n, err := serverconnection.New().Where(serverconnection.ID.Eq(id)).DeleteAll(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not remove the connection: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: no such connection", apierr.ErrNotFound)
	}
	return nil
}

func (r *serverConnectionRepository) SigningSeed(ctx context.Context) ([]byte, bool, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, false, err
	}
	// The oldest: if two replicas made one at once, both use the first.
	row, found, err := serversigningkey.New().
		Order(serversigningkey.CreatedAt.Asc(), serversigningkey.ID.Asc()).
		One(ctx, ex)
	if err != nil {
		return nil, false, fmt.Errorf("could not read the signing key: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	plain, err := crypto.Decrypt(row.Seed)
	if err != nil {
		return nil, false, fmt.Errorf("could not open the signing key: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(plain)
	if err != nil {
		return nil, false, errors.New("the signing key kept is not base64")
	}
	return seed, true, nil
}

func (r *serverConnectionRepository) SaveSigningSeed(ctx context.Context, seed []byte) ([]byte, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	sealed, err := crypto.Encrypt(base64.StdEncoding.EncodeToString(seed))
	if err != nil {
		return nil, fmt.Errorf("could not seal the signing key: %w", err)
	}
	ins := serversigningkey.Create()
	ins.SetID(uuid.Must(uuid.NewV7()))
	ins.SetSeed(sealed)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return nil, fmt.Errorf("could not keep the signing key: %w", err)
	}
	kept, _, err := r.SigningSeed(ctx)
	return kept, err
}

func serverConnectionFrom(row serverconnection.Row, withToken bool) (entities.ServerConnection, error) {
	c := entities.ServerConnection{
		ID:          row.ID,
		Name:        row.Name,
		Purpose:     entities.ServerPurpose(row.Purpose),
		Side:        entities.ConnectionSide(row.Side),
		URL:         row.URL,
		Key:         row.Key,
		Words:       row.Words,
		TokenHash:   row.TokenHash,
		RequestedBy: row.RequestedBy,
		AcceptedBy:  row.AcceptedBy,
		CreatedAt:   row.CreatedAt,
	}
	if at, ok := row.AcceptedAt.Get(); ok {
		c.AcceptedAt = &at
		c.Trusted = trustOpens(row)
	}
	if at, ok := row.LastSeenAt.Get(); ok {
		c.LastSeenAt = &at
	}
	if withToken && row.Token != "" {
		token, err := crypto.Decrypt(row.Token)
		if err != nil {
			return entities.ServerConnection{}, fmt.Errorf("could not open the connection's token: %w", err)
		}
		c.Token = token
	}
	return c, nil
}

// trustStatement is what an accepted connection's Trust seals: the row and
// the key it trusts, so a seal copied onto another row or key opens to the
// wrong statement.
func trustStatement(id uuid.UUID, key string) string {
	return "metis.connection-trust/v1\n" + id.String() + "\n" + key
}

// trustOpens says whether an accepted connection's seal is this server's own
// and names this row and key.
func trustOpens(row serverconnection.Row) bool {
	if row.Trust == "" || !crypto.IsCiphertext(row.Trust) {
		return false
	}
	plain, err := crypto.Decrypt(row.Trust)
	return err == nil && plain == trustStatement(row.ID, row.Key)
}
