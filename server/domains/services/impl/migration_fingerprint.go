package impl

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// migrationFingerprintVersion begins every fingerprint and names how it was
// made. A fingerprint made another way begins otherwise, so the two never
// compare equal: a request asked for under one and checked under another is
// "no longer the one that was asked for", never silently the same.
const migrationFingerprintVersion = "mf1-"

// migrationFingerprint is one migration's policy as a short name: what a
// second administrator is asked to approve, and what an apply is checked
// against before it runs under their approval.
//
// The policy is both versions, the mapping, each step's decision with its
// reason, the controls whose loss was acknowledged, the controls the plan
// found the migration drops, and the instances it was narrowed to. Neither
// who asked (Actor) nor who approved (Approval) is part of it: the same
// migration is the same migration whoever asks for it, and that is what stops
// a second administrator asking for it again in order to approve it as their
// own.
//
// The same policy is the same fingerprint however its maps and lists were
// ordered, whatever spaces were typed round a reason, and whether or not an
// id was given twice.
//
// Two different policies are never the same bytes. A step's id, a mapping
// and a reason are whatever somebody typed, so no character can be trusted
// to separate them: every part is written after its own length, and every
// list after its name and the number of things in it (fingerprintWriter).
// Joined with separators, mapping "a→b" to "c" and mapping "a" to "b→c" were
// one policy.
func migrationFingerprint(
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
	holds []entities.ComplianceHold,
) string {
	w := fingerprintWriter{sum: sha256.New()}
	w.list("source", [][]string{{sourceDefID.String()}})
	w.list("target", [][]string{{targetDefID.String()}})

	mapping := make([][]string, 0, len(nodeMapping))
	for _, from := range sortedKeys(nodeMapping) {
		mapping = append(mapping, []string{from, nodeMapping[from]})
	}
	w.list("mapping", mapping)

	actions := make([][]string, 0, len(options.Actions))
	for _, nodeID := range sortedKeys(options.Actions) {
		action := options.Actions[nodeID]
		actions = append(actions, []string{nodeID, string(action.Kind), strings.TrimSpace(action.Reason)})
	}
	w.list("actions", actions)

	w.names("acknowledged", options.Acknowledged)
	dropped := make([]string, 0, len(holds))
	for _, hold := range holds {
		dropped = append(dropped, hold.NodeID)
	}
	w.names("controls", dropped)
	instances := make([]string, 0, len(options.Instances))
	for _, id := range options.Instances {
		instances = append(instances, id.String())
	}
	w.names("instances", instances)

	return migrationFingerprintVersion + base64.RawURLEncoding.EncodeToString(w.sum.Sum(nil))[:32]
}

// fingerprintWriter writes the parts of a policy so that no two policies
// write the same bytes: each part after its length, each list after its name
// and its count.
type fingerprintWriter struct{ sum hash.Hash }

// part writes one string after its length.
func (w fingerprintWriter) part(s string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(s)))
	w.sum.Write(length[:])
	w.sum.Write([]byte(s))
}

// list writes a named list of things, each a fixed number of parts.
func (w fingerprintWriter) list(name string, things [][]string) {
	w.part(name)
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(things)))
	w.sum.Write(count[:])
	for _, parts := range things {
		for _, part := range parts {
			w.part(part)
		}
	}
}

// names writes a named list of single names: sorted, and each once.
func (w fingerprintWriter) names(name string, names []string) {
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	things := make([][]string, 0, len(sorted))
	for _, one := range sorted {
		things = append(things, []string{one})
	}
	w.list(name, things)
}
