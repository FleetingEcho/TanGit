// Package sshworkspace provides SSH command execution, interactive terminals,
// POSIX remote filesystem operations, and streaming local/remote transfers.
//
// Profile values and SecretRef keys contain no credentials. Passwords and
// private-key passphrases are obtained from an injected SecretStore. A memory
// store can therefore provide process-lifetime secrets without persistence.
package sshworkspace
