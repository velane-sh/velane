// Package control contains the host-to-guest protocol contracts.
package control

type QuiesceRequest struct {
	CommandID         string `json:"command_id"`
	DeadlineUnixMilli int64  `json:"deadline_unix_milli"`
}
type QuiesceResponse struct {
	Nonce          string  `json:"nonce"`
	WritableMounts []Mount `json:"writable_mounts"`
	Frozen         bool    `json:"frozen"`
}
type Mount struct{ DriveID, MountPoint string }
type RestoreRequest struct{ SavedNonce, FreshCredentialNonce string }
type RestoreResponse struct {
	Ready bool
	Nonce string
}
