package pq

import (
	"crypto/hmac"
	"crypto/sha1"
	"fmt"
	"strings"

	"crypto/sha256"

	"golang.org/x/crypto/pbkdf2"
)

// import (

// 	"fmt"
// 	"hash"

//
// )

// type derivedKeys struct {
// 	ClientKey []byte
// 	StoredKey []byte
// 	ServerKey []byte
// }
// type KeyFactors struct {
// 	Salt  string
// 	Iters int
// }

// type StoredCredentials struct {
// 	KeyFactors
// 	StoredKey []byte
// 	ServerKey []byte
// }

// type HashGeneratorFcn func() hash.Hash

// var SHA1 HashGeneratorFcn = func() hash.Hash { return sha1.New() }
// var SHA256 HashGeneratorFcn = func() hash.Hash { return sha256.New() }

// func computeHash(hg HashGeneratorFcn, b []byte) []byte {
// 	h := hg()
// 	h.Write(b)
// 	return h.Sum(nil)
// }

// func computeHMAC(hg HashGeneratorFcn, key, data []byte) []byte {
// 	mac := hmac.New(hg, key)
// 	mac.Write(data)
// 	return mac.Sum(nil)
// }

func charToByte(c byte) byte {
	return byte(strings.Index("0123456789ABCDEF", string(c)))
}

func hexStringToBytes(hexString string) []byte {

	if hexString == "" {
		return []byte("")
	}

	upperString := strings.ToUpper(hexString)
	bytes_len := len(upperString) / 2
	array := make([]byte, bytes_len)

	for i := 0; i < bytes_len; i++ {
		pos := i * 2
		array[i] = byte(charToByte(upperString[pos])<<4 | charToByte(upperString[pos+1]))
	}
	return array
}

func generateKFromPBKDF2(password string, random64code string, server_iteration int) []byte {
	random32code := hexStringToBytes(random64code)
	pwdEn := pbkdf2.Key([]byte(password), random32code, server_iteration, 32, sha1.New)
	return pwdEn
}

func bytesToHexString(src []byte) string {
	s := ""
	for i := 0; i < len(src); i++ {
		v := src[i] & 0xFF
		hv := fmt.Sprintf("%x", v)
		if len(hv) < 2 {
			s += hv
			s += "0"
		} else {
			s += hv
		}
	}
	return s
}

func getKeyFromHmac(key []byte, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func getSha256(message []byte) []byte {
	hash := sha256.New()
	hash.Write(message)

	return hash.Sum(nil)
}

func XOR_between_password(password1 []byte, password2 []byte, length int) []byte {
	array := make([]byte, length)
	for i := 0; i < length; i++ {
		array[i] = (password1[i] ^ password2[i])
	}
	return array
}

func bytesToHex(bytes []byte) []byte {
	lookup :=
		[16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}
	result := make([]byte, len(bytes)*2)
	pos := 0
	for i := 0; i < len(bytes); i++ {
		c := int(bytes[i] & 0xFF)
		j := c >> 4
		result[pos] = lookup[j]
		pos++
		j = (c & 0xF)
		result[pos] = lookup[j]
		pos++
	}
	return result

}

func RFC5802Algorithm(password string, random64code string, token string, server_signature string, server_iteration int) []byte {
	k := generateKFromPBKDF2(password, random64code, server_iteration)
	server_key := getKeyFromHmac(k, []byte("Sever Key"))
	client_key := getKeyFromHmac(k, []byte("Client Key"))
	stored_key := getSha256(client_key)
	tokenbyte := hexStringToBytes(token)
	client_signature := getKeyFromHmac(server_key, tokenbyte)
	if server_signature != "" && server_signature != bytesToHexString(client_signature) {
		return []byte("")
	}
	hmac_result := getKeyFromHmac(stored_key, tokenbyte)
	h := XOR_between_password(hmac_result, client_key, len(client_key))
	result := bytesToHex(h)
	return result

}
