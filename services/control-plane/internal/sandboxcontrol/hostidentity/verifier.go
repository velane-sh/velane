package hostidentity

import "context"

// EnrollmentProof is provider-neutral evidence collected by the host agent.
// Providers must bind evidence to the persisted challenge nonce and CSR digest.
type EnrollmentProof struct {
	Provider, PoolID, Nonce, CSR string
	IdentityDocument             string
	IdentitySignature            string
	STSSignedRequest             string
}
type VerifiedHost struct {
	PoolID, ProviderInstanceID, AccountID, Region, AMIID, AutoScalingGroup, LaunchTemplateID, LineageID, HostCompatibilityKey string
}
type Verifier interface {
	Verify(context.Context, EnrollmentProof) (VerifiedHost, error)
}
