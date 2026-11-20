package entities

import (
	"crypto/sha256"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ServerPurpose is what an installation is for, in the order releases travel:
// designed and simulated in development, optionally tried in staging, run in
// production.
type ServerPurpose string

const (
	ServerDevelopment ServerPurpose = "development"
	ServerStaging     ServerPurpose = "staging"
	ServerProduction  ServerPurpose = "production"
)

// Valid says whether p is one of the three purposes.
func (p ServerPurpose) Valid() bool {
	switch p {
	case ServerDevelopment, ServerStaging, ServerProduction:
		return true
	}
	return false
}

// ConnectionSide says which way releases go over a connection, from this
// installation's side of it.
type ConnectionSide string

const (
	// ConnectionUpstream is a server this one takes releases from: the one it
	// entered a code of. It is trusted only once a second administrator here
	// accepts it.
	ConnectionUpstream ConnectionSide = "upstream"
	// ConnectionDownstream is a server this one gives releases to: one that
	// entered a code made here.
	ConnectionDownstream ConnectionSide = "downstream"
)

// ServerConnection is another installation this one exchanges releases with.
//
// Only releases travel over it, never data: instances, tasks, people and
// passwords stay where they are.
type ServerConnection struct {
	ID uuid.UUID `json:"id"`
	// Name and Purpose are what the other server calls itself and is for.
	Name    string         `json:"name"`
	Purpose ServerPurpose  `json:"purpose"`
	Side    ConnectionSide `json:"side"`
	// URL is where the other server is reached. Empty for a downstream server
	// that gave no address it can be reached at: it collects its releases.
	URL string `json:"url,omitzero"`
	// Key is the other server's base64 Ed25519 public key, which its releases
	// and live notes are signed with.
	Key string `json:"key"`
	// Words are the four words both servers showed when they were connected,
	// so two people could check they connected the servers they meant to.
	Words       string     `json:"words"`
	RequestedBy string     `json:"requested_by"`
	AcceptedBy  string     `json:"accepted_by,omitzero"`
	AcceptedAt  *time.Time `json:"accepted_at,omitzero"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitzero"`
	CreatedAt   time.Time  `json:"created_at"`

	// Token is what this server presents to an upstream one, and TokenHash
	// the SHA-256 of what a downstream one presents here. Neither is ever
	// shown.
	Token     string `json:"-"`
	TokenHash string `json:"-"`
	// Trusted says an accepted upstream connection's seal opens with this
	// server's encryption key. A row written into the database by anything
	// else is accepted in name only, and its key is not trusted.
	Trusted bool `json:"-"`
}

// Accepted says whether a second administrator accepted the connection. A
// downstream one needs no acceptance: it takes nothing from the other server.
func (c ServerConnection) Accepted() bool {
	return c.Side == ConnectionDownstream || c.AcceptedAt != nil
}

// ConnectionCode is a one-time code an administrator made for another server
// to connect with. Only its SHA-256 is kept.
type ConnectionCode struct {
	ID        uuid.UUID
	Hash      string
	CreatedBy string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// connectionWords are 256 short, distinct English words, so four of them
// carry 32 bits of the connection and read aloud without confusion.
var connectionWords = strings.Fields(`
able acid arch area army atom aunt axis baby back bake ball band bank barn base
bath bead beam bean bear beef bell belt bend bike bird blue boat body bold bone
book boot bowl brew bush cake calm camp cape card cart cave cell chef chin city
clay clip club coal coat code coin cold cone cook cool cord corn crab crew crow
cube cup curl dark dawn deck deer desk dial dice dish dock doll door dove drum
duck dune dust east echo edge face fair farm fawn fern film fish flag flat fly
foam fog folk fork fort frog fuel gate gear gift girl glow goat gold golf gown
grid gulf hair hall hand harp hawk heat herb hill hive hook horn hose iron jade
jazz jeep king kite knee knot lace lake lamp lane leaf lens lily lime lion lock
loft loop lynx mail malt map mask mast meal mill mint mist moon moss moth nail
navy neck nest net news nose nut oak oar oven owl pace pack page pail palm park
path peak pear pen pie pier pine pipe plum pond pony pool port pot pump rack
raft rail rain ramp reef rice ring road robe rock roof rope rose ruby rug sail
salt sand seal seed ship shoe silk sink sled snow soap sock sofa song soup star
stem sun swan tail tank tape taxi team tent tide tile toad tree tube twig vase
vest vine wall wand wave web well wind wing wolf wood wool yard yarn zone`)

// ConnectionWords are the four words two servers show while they connect,
// read from the code and both servers' keys. A server in the middle that
// swapped either key would change them.
func ConnectionWords(code, upstreamKey, downstreamKey string) string {
	sum := sha256.Sum256([]byte("metis.connection/v1\n" + code + "\n" + upstreamKey + "\n" + downstreamKey))
	words := make([]string, 4)
	for i := range words {
		words[i] = connectionWords[sum[i]]
	}
	return strings.Join(words, " ")
}
