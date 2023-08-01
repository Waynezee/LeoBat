package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestVerify(t *testing.T) {
	pub, pri, _ := GenKeyPair()
	msg := RandStringBytesRmndr(32)
	signature := Sign(pri, msg)
	t.Log("signature:", signature)
	verify := Verify(pub, msg, signature)
	t.Log("verify:", verify)
}

func BenchmarkSign(b *testing.B) {
	b.StopTimer()
	_, pri, _ := GenKeyPair()
	msg := RandStringBytesRmndr(32)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_ = Sign(pri, msg)
	}
}

func BenchmarkVerify(b *testing.B) {
	b.StopTimer()
	pub, pri, _ := GenKeyPair()
	msg := RandStringBytesRmndr(32)
	signature := Sign(pri, msg)
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		Verify(pub, msg, signature)
	}
}

func BenchmarkEd25519Sign(b *testing.B) {
	b.StopTimer()
	_, pri, _ := ed25519.GenerateKey(rand.Reader)
	msg := RandStringBytesRmndr(32)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_ = ed25519.Sign(pri, msg)
	}
}

func BenchmarkEd25519Verify(b *testing.B) {
	b.StopTimer()
	pub, pri, _ := ed25519.GenerateKey(rand.Reader)
	msg := RandStringBytesRmndr(32)
	sig := ed25519.Sign(pri, msg)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		ed25519.Verify(pub, msg, sig)
	}
}
