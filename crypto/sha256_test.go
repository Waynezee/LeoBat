package crypto

import (
	"math/rand"
	"testing"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func BenchmarkHash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Hash(RandStringBytesRmndr(1000))
	}
}

func RandStringBytesRmndr(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Int63()%int64(len(letterBytes))]
	}
	return b
}
