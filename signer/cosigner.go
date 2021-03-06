package signer

type CosignerServer struct{}

type CosignerHasEphemeralSecretPartRequest struct {
	ID     int
	Height int64
	Round  int64
	Step   int8
}

type CosignerHasEphemeralSecretPartResponse struct {
	Exists                   bool
	EphemeralSecretPublicKey []byte
}

type CosignerSetEphemeralSecretPartRequest struct {
	SourceID                       int
	SourceEphemeralSecretPublicKey []byte
	Height                         int64
	Round                          int64
	Step                           int8
	EncryptedSharePart             []byte
	SourceSig                      []byte
}

// Cosigner interface is a set of methods for an m-of-n threshold signature.
// This interface abstracts the underlying key storage and management
type Cosigner interface {
	// Get the ID of the cosigner
	// The ID is the shamir index: 1, 2, etc...
	GetID() int

	// Get the ephemeral secret part for an ephemeral share
	// The ephemeral secret part is encrypted for the receiver
	GetEphemeralSecretPart(req *CosignerGetEphemeralSecretPartRequest) (*CosignerGetEphemeralSecretPartResponse, error)

	// Store an ephemeral secret share part provided by another cosigner
	SetEphemeralSecretPart(req CosignerSetEphemeralSecretPartRequest) error

	// Query whether the cosigner has an ehpemeral secret part set
	HasEphemeralSecretPart(req CosignerHasEphemeralSecretPartRequest) (CosignerHasEphemeralSecretPartResponse, error)

	// Sign the requested bytes
	Sign(req *CosignerSignRequest) (*CosignerSignResponse, error)
}
