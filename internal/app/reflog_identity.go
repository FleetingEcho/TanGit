package app

import "lazygitapp/internal/repository"

type reflogItemIdentity struct {
	Hash       string
	Subject    string
	Timestamp  int64
	Occurrence int
}

// reflogRowIdentity is stable under insertion of a different record above an
// entry. With no Git-provided row ID, the occurrence among otherwise identical
// records is the only stable way to distinguish repeated reset operations
// without making scroll position itself part of the identity.
func reflogRowIdentity(entries []repository.Commit, source int) reflogItemIdentity {
	entry := entries[source]
	occurrence := 0
	for i := 0; i < source; i++ {
		candidate := entries[i]
		if candidate.Hash == entry.Hash && candidate.Subject == entry.Subject && candidate.UnixTimestamp == entry.UnixTimestamp {
			occurrence++
		}
	}
	return reflogItemIdentity{Hash: entry.Hash, Subject: entry.Subject, Timestamp: entry.UnixTimestamp, Occurrence: occurrence}
}
