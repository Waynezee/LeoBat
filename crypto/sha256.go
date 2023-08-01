package crypto

import (
	"crypto/sha256"
	"encoding/base64"
)

func Hash(data []byte) string {
	hash := sha256.New()
	hash.Write(data)
	result := hash.Sum(nil)
	return base64.StdEncoding.EncodeToString(result)
	// return utils.Bytes2Str(result)
}
