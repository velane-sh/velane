// Package restore forbids workload execution until a fresh post-restore nonce
// and credentials have been installed.
package restore

import "errors"

type CredentialRotator interface{ Rotate(freshNonce string) error }
type Handshake struct {
	expectedNonce string
	complete      bool
}

func New(savedNonce string) Handshake { return Handshake{expectedNonce: savedNonce} }
func (h *Handshake) Complete(returnedNonce, freshCredentialNonce string, rotator CredentialRotator) error {
	if h.complete || h.expectedNonce == "" || returnedNonce != h.expectedNonce || freshCredentialNonce == "" || rotator == nil {
		return errors.New("post-restore handshake rejected")
	}
	if err := rotator.Rotate(freshCredentialNonce); err != nil {
		return err
	}
	h.complete = true
	return nil
}
func (h Handshake) Ready() bool { return h.complete }
