package vsock

type QuiesceRequest struct{ SandboxID, CommandID string }
type QuiesceResponse struct {
	Nonce            string
	WritableDriveIDs []string
	Frozen           bool
}
type Client interface {
	Quiesce(QuiesceRequest) (QuiesceResponse, error)
	Thaw(string) error
	PostRestoreHandshake(string) error
}
