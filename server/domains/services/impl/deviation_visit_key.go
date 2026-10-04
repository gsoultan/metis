package impl

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

const (
	// visitKeyVersion names how a visit key is made. A key made another way
	// would start otherwise, and could not be taken for one of these.
	visitKeyVersion = "dv1-"
	// visitKeyLength is how much of the hash a key keeps, in base64url
	// characters: 192 bits, which nobody arrives at twice.
	visitKeyLength = 32
)

// deviationVisitKey identifies the work a command would act on: this kind of
// act, on this instance, at this step of this version of its process, while
// exactly these tasks are open there and exactly these tokens rest there.
//
// It is derived and not drawn, so asking twice gives the same key: a retry of
// an apply finds the row the first attempt wrote, and an apply names the plan
// it previewed by repeating its key. And it changes when the work does — a
// task of the step completed, withdrawn or started, a token arrived or gone,
// the instance moved to another version — so an apply made for work that has
// since changed is told to preview again.
//
// What goes in, and why:
//
//   - the instance and the version it runs: the same step of another version
//     is another step;
//   - the kind: a hold and a waive of one visit are two acts;
//   - the step, or none;
//   - the ids of the open tasks the plan lists, which its caller hands in;
//   - the ids of the tokens on the step. A cancel that names no step is about
//     the whole instance, so it takes every token: one arriving anywhere is a
//     different visit.
//
// What stays out is everything that changes while the work is the same: who
// holds a task and whether they claimed it, the instance's variables, its
// status (an apply asks that of the row it locks), a token on another step.
// With any of those in, a preview could not be applied on an instance
// somebody else is working on.
//
// Each part is written with its length before it and each list with its
// count, so no part can be mistaken for another: a step's id is its author's
// to choose, and written between separators it could be chosen to read as
// another step's tasks. Ids are sorted, so the order a listing happened to
// return is not part of the key.
func deviationVisitKey(instance entities.ProcessInstance, kind entities.DeviationKind, nodeID string, openTaskIDs []uuid.UUID) string {
	h := sha256.New()
	writeKeyPart(h, instance.ID[:])
	var version []byte
	if instance.Definition != nil {
		version = instance.Definition.ID[:]
	}
	writeKeyPart(h, version)
	writeKeyPart(h, []byte(kind))
	writeKeyPart(h, []byte(nodeID))
	writeKeyIDs(h, openTaskIDs)
	writeKeyIDs(h, tokenIDsOn(instance, nodeID))
	return visitKeyVersion + base64.RawURLEncoding.EncodeToString(h.Sum(nil))[:visitKeyLength]
}

// tokenIDsOn is the ids of the tokens an instance holds on a step, and of all
// it holds when no step is named.
func tokenIDsOn(instance entities.ProcessInstance, nodeID string) []uuid.UUID {
	var ids []uuid.UUID
	for _, token := range instance.Tokens {
		if nodeID == "" || (token.Node != nil && token.Node.ID == nodeID) {
			ids = append(ids, token.ID)
		}
	}
	return ids
}

// writeKeyPart writes one part of a key, preceded by how long it is. A hash
// never fails a write.
func writeKeyPart(h hash.Hash, part []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(part)))
	h.Write(length[:])
	h.Write(part)
}

// writeKeyIDs writes a list of ids as one part: how many, then each, in the
// order of the ids themselves.
func writeKeyIDs(h hash.Hash, ids []uuid.UUID) {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	part := make([]byte, 0, len(sorted)*len(uuid.UUID{}))
	for _, id := range sorted {
		part = append(part, id[:]...)
	}
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(sorted)))
	h.Write(count[:])
	writeKeyPart(h, part)
}
