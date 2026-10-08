//go:build !windows

package desktop

import "fmt"

// protectBytes and unprotectBytes have no implementation off Windows.
//
// The pet is a Windows-first app and its only user-facing secret is an API key;
// silently falling back to a plaintext file on another platform would be a
// security surprise, so the failure is explicit and the settings UI tells the
// user to set PETPC_LLM_API_KEY instead.
func protectBytes([]byte) ([]byte, error) {
	return nil, fmt.Errorf("dpapi: no per-user encryption available on this platform; set %s instead", APIKeyEnvVar)
}

func unprotectBytes([]byte) ([]byte, error) {
	return nil, fmt.Errorf("dpapi: no per-user encryption available on this platform")
}
