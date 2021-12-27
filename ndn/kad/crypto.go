/*
 * Copyright (c) 2019-2021,  HII of ETRI.
 *
 * This file is part of geth-ndn (Go Ethereum client for NDN).
 * author: tqtung@gmail.com 
 *
 * geth-ndn is free software: you can redistribute it and/or modify it under the terms
 * of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * geth-ndn is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY;
 * without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR
 * PURPOSE.  See the GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along with
 * geth-ndn, e.g., in COPYING.md file.  If not, see <http://www.gnu.org/licenses/>.
 */


package kad

import (
	"bytes"
	"errors"
//	"fmt"
	"io"
//	"encoding/hex"
	"crypto/ecdsa"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/cipher"
	"crypto/rand"
	"github.com/ethereum/go-ethereum/crypto"
//	"github.com/ethereum/go-ethereum/log"
)
type Crypto struct {
	prv 		*ecdsa.PrivateKey
}

func (c *Crypto) Sign(content []byte) (sig []byte, err error) {
	hash := crypto.Keccak256(content)
	return crypto.Sign(hash, c.prv)
}
func (c *Crypto) Verify(content []byte, sig []byte, pub []byte) bool {
	hash := crypto.Keccak256(content)
	return crypto.VerifySignature(pub, hash, sig[:len(sig)-1])
}

func Pad(src []byte) []byte {
	padding := aes.BlockSize - len(src)%aes.BlockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padtext...)
}
func Unpad(src []byte) ([]byte, error) {
	length := len(src)
	unpadding := int(src[length-1])
	if unpadding > length {
		return nil, errors.New("unpad error. This could happen when incorrect encryption key is used")
	}
	
	return src[:(length - unpadding)], nil					
}

func (c *Crypto) Encrypt(key []byte, plaintext []byte) (ciphertext[]byte, err error) {
	var block cipher.Block
	msg := Pad(plaintext)
	if block, err = aes.NewCipher(key); err == nil {
		ciphertext = make([]byte, aes.BlockSize+len(msg))
		iv := ciphertext[:aes.BlockSize]
		if _, err = io.ReadFull(rand.Reader, iv); err == nil {
			 cfb := cipher.NewCFBEncrypter(block, iv)
			 cfb.XORKeyStream(ciphertext[aes.BlockSize:], msg)
		}
	} 
	return	
}

func (c *Crypto) Decrypt(key []byte, ciphertext []byte) (plaintext []byte, err error) {
	var block	cipher.Block
	if block, err = aes.NewCipher(key); err == nil {
		iv := ciphertext[:aes.BlockSize]
		msg := make([]byte, len(ciphertext)-aes.BlockSize)
		copy(msg, ciphertext[aes.BlockSize:])
		cfb := cipher.NewCFBDecrypter(block, iv)
		cfb.XORKeyStream(msg, msg)
		plaintext,_ = Unpad(msg)
	}
	return
}

func (c *Crypto) SignHmac256(key []byte, message []byte) (sig []byte) {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)
}

func (c *Crypto) ValidateHmac256(key []byte, message []byte, sig []byte) bool {
	return hmac.Equal(sig, c.SignHmac256(key,message))
}
