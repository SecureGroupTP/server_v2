package randombytes

import "crypto/rand"

type Reader interface {
	Read(p []byte) (int, error)
}

type CryptoReader struct{}

func (CryptoReader) Read(p []byte) (int, error) {
	return rand.Read(p)
}
