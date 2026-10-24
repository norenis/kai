package dashboard

import "crypto/subtle"

// vaultPinHeader carries the vault PIN for reads of vault notes. A header
// (not a query parameter) keeps the PIN out of access logs and history.
const vaultPinHeader = "X-Kai-Vault-Pin"

// vaultPinOK compares against the configured vault PIN in constant time.
// An empty configured PIN fails closed, as in the MCP server.
func (s *Server) vaultPinOK(pin string) bool {
	expected := s.cfg.Brain.VaultPin
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pin), []byte(expected)) == 1
}
