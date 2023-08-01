package crypto

import (
	"testing"
)

func TestBlsVerify(t *testing.T) {
	Init()
	priKey, pubKey, mpubKey := Generate(4, 2)
	sig := make([][]byte, 4)
	msg := RandStringBytesRmndr(32)
	for i := 0; i < 4; i++ {
		sig[i] = BlsSign(msg, priKey[i])
		t.Log("Verify Pass: ", BlsVerify(msg, pubKey[i], sig[i]))
	}

	m := make(map[uint32][]byte, 2)
	m[2] = sig[1]
	m[4] = sig[3]
	cosig := Recover(m)
	t.Log("Bls Verify : ", BlsVerify(msg, mpubKey, cosig))

	m1 := make(map[uint32][]byte, 2)
	m1[2] = sig[1]
	m1[3] = sig[2]
	cosig1 := Recover(m1)
	t.Log("Bls Verify : ", BlsVerify(msg, mpubKey, cosig1))

	m2 := make(map[uint32][]byte, 2)
	m2[3] = sig[2]
	m2[4] = sig[3]
	cosig2 := Recover(m2)
	t.Log("Bls Verify : ", BlsVerify(msg, mpubKey, cosig2))
}
