package serverconnection

import (
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// ConnectRequest connects to the server before this one, with a code made
// there.
type ConnectRequest struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

// IDRequest names one connection by the address.
type IDRequest struct {
	ID string `json:"-"`
}

// ListResponse is this server and every connection it has.
type ListResponse struct {
	Server      *contracts.ServerIdentity   `json:"server,omitzero"`
	Connections []entities.ServerConnection `json:"connections"`
	Err         error                       `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// CodeResponse is a one-time code, shown once.
type CodeResponse struct {
	Code *contracts.IssuedConnectionCode `json:"code,omitzero"`
	Err  error                           `json:"err,omitzero"`
}

func (r CodeResponse) Failed() error { return r.Err }

// ConnectionResponse is one connection.
type ConnectionResponse struct {
	Connection *entities.ServerConnection `json:"connection,omitzero"`
	Err        error                      `json:"err,omitzero"`
}

func (r ConnectionResponse) Failed() error { return r.Err }

// RemoveResponse answers a removal.
type RemoveResponse struct {
	Err error `json:"err,omitzero"`
}

func (r RemoveResponse) Failed() error { return r.Err }

// HandshakeResponse is what this server answers another that was given one
// of its codes. It is the reply itself, with no envelope, as the other server
// reads it.
type HandshakeResponse struct {
	contracts.ServerHandshakeReply
	Err error `json:"-"`
}

func (r HandshakeResponse) Failed() error { return r.Err }
