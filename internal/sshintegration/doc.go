// Package sshintegration starts isolated, local OpenSSH servers for tests.
//
// Tests should call Start and pass Server.Profile and Server.SecretStore to an
// sshworkspace.Connector. The server uses only temporary files and t.Cleanup;
// it does not read or change the user's SSH configuration.
package sshintegration
