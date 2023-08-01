package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
)

func GenKeyPair() (pubKey, priKey []byte, e error) {
	pubKey, priKey, e = ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return nil, nil, e
	}
	return
}

func Sign(priKey, message []byte) []byte {
	return ed25519.Sign(priKey, message)
}

func Verify(pubKey, message, sig []byte) bool {
	return ed25519.Verify(pubKey, message, sig)
}
