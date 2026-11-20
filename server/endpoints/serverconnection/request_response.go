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

// SendRequest sends a release to a server this one gives releases to.
type SendRequest struct {
	ProjectID    string `json:"-"`
	Release      string `json:"-"`
	ConnectionID string `json:"connection_id"`
}

// ProjectRequest names a project by the address.
type ProjectRequest struct {
	ProjectID string `json:"-"`
}

// DeliveryResponse is one release sent.
type DeliveryResponse struct {
	Delivery *entities.ReleaseDelivery `json:"delivery,omitzero"`
	Err      error                     `json:"err,omitzero"`
}

func (r DeliveryResponse) Failed() error { return r.Err }

// DeliveriesResponse is where a project's releases were sent.
type DeliveriesResponse struct {
	Deliveries []entities.ReleaseDelivery `json:"deliveries"`
	Err        error                      `json:"err,omitzero"`
}

func (r DeliveriesResponse) Failed() error { return r.Err }

// CollectedResponse is what checking in now took.
type CollectedResponse struct {
	Collected *contracts.CollectedReleases `json:"collected,omitzero"`
	Err       error                        `json:"err,omitzero"`
}

func (r CollectedResponse) Failed() error { return r.Err }

// CollectRequest is a server checking in, with the token it was given.
type CollectRequest struct {
	Token  string `json:"-"`
	Format string `json:"format"`
}

// ReportRequest is a server saying how far the releases it took got, with
// the token it was given.
type ReportRequest struct {
	Token string `json:"-"`
	contracts.ReleaseReport
}

// CollectionResponse is what a server checking in is handed, bare, as it
// reads it.
type CollectionResponse struct {
	contracts.ReleaseCollection
	Err error `json:"-"`
}

func (r CollectionResponse) Failed() error { return r.Err }

// ReportResponse answers a report.
type ReportResponse struct {
	Err error `json:"err,omitzero"`
}

func (r ReportResponse) Failed() error { return r.Err }
