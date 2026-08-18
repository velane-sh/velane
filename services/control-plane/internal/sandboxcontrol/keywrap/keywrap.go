package keywrap

import "context"

type WrappedKey struct{ Ciphertext, ContextDigest string }
type Wrapper interface {
	GenerateAndWrap(context.Context, map[string]string) (plaintext []byte, wrapped WrappedKey, err error)
	Unwrap(context.Context, WrappedKey, map[string]string) ([]byte, error)
}
