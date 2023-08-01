package crypto

import (
	"bytes"
	"encoding/binary"

	"github.com/herumi/bls-eth-go-binary/bls"
)

func Init() {
	bls.Init(bls.BLS12_381)
}

func Generate(n int, f int) (priKey, pubKey [][]byte, masterPubKey []byte) {
	blsId := make([]bls.ID, n)
	for i := uint8(0); i < uint8(n); i++ {
		buf := new(bytes.Buffer)
		err := binary.Write(buf, binary.LittleEndian, i+1)
		if err != nil {
			panic(err)
		}
		blsId[i].SetLittleEndian(buf.Bytes())
	}

	var sec bls.SecretKey
	sec.SetByCSPRNG()
	// GetMasterSecretKey
	msk := sec.GetMasterSecretKey(f)
	secShares := make([]bls.SecretKey, n)
	pubShares := make([]bls.PublicKey, n)
	pubVec := make([][]byte, n)
	priVec := make([][]byte, n)

	for j := 0; j < n; j++ {
		// GetPrivateKey
		secShares[j].Set(msk, &blsId[j])
		priVec[j] = secShares[j].Serialize()
		// priVec[j] = base64.StdEncoding.EncodeToString(secShares[j].Serialize())
		// GetPublicKey
		pubShares[j] = *secShares[j].GetPublicKey()
		pubVec[j] = pubShares[j].Serialize()
		// pubVec[j] = base64.StdEncoding.EncodeToString(pubShares[j].Serialize())
	}
	mpKey := sec.GetPublicKey().Serialize()
	// mpKey := base64.StdEncoding.EncodeToString(sec.GetPublicKey().Serialize())
	return priVec, pubVec, mpKey
}

// Sign signs the corresponding part of threshold signature
func BlsSign(msg []byte, priKey []byte) []byte {
	var secretKey bls.SecretKey
	secretKey.Deserialize(priKey)
	sig := secretKey.SignByte(msg)
	return sig.Serialize()
}

// Recover recovers the threshold signature
func Recover(sigShares map[uint32][]byte) []byte {
	t := len(sigShares)
	sigVec := make([]bls.Sign, t)
	idVec := make([]bls.ID, t)

	i := uint32(0)
	for key, sig := range sigShares {
		sigVec[i].Deserialize(sig)
		buf := new(bytes.Buffer)
		err := binary.Write(buf, binary.LittleEndian, uint8(key))
		if err != nil {
			panic(err)
		}
		idVec[i].SetLittleEndian(buf.Bytes())
		i++
	}

	var cosign bls.Sign
	cosign.Recover(sigVec, idVec)
	return cosign.Serialize()
}

func BlsVerify(msg, pubKey, sig []byte) bool {
	var publicKey bls.PublicKey
	publicKey.Deserialize(pubKey)
	var sign bls.Sign
	sign.Deserialize(sig)
	return sign.VerifyByte(&publicKey, msg)
}
