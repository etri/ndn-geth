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


package ndnapp


import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
//	"crypto/x509"
	"encoding/asn1"
	"errors"
	"math/big"
	"github.com/usnistgov/ndn-dpdk/ndn"
	ndntype "github.com/usnistgov/ndn-dpdk/ndn/an"
)

var (
	ErrInvalidSig = errors.New("invalid signature")
)

type EcdsaSigning struct {
	name ndn.Name
	prvKey *ecdsa.PrivateKey
	pubKey *ecdsa.PublicKey

}

type ecdsaSignature struct {
	R, S *big.Int
}

func EcdsaSigningFromPrivateKey(key *ecdsa.PrivateKey) *EcdsaSigning {
	return &EcdsaSigning{
		name:	ndn.ParseName("/dummy"),
		prvKey:	key,
		pubKey:	&key.PublicKey,
	}
}

func EcdsaSigningFromPubKey(key *ecdsa.PublicKey) *EcdsaSigning {
	return &EcdsaSigning{
		name:	ndn.ParseName("/dummy"),
		prvKey:	nil,
		pubKey: key,	
	}
}

func (s *EcdsaSigning) Sign(packet ndn.Signable) error {
	return packet.SignWith(func(name ndn.Name, info *ndn.SigInfo) (ndn.LLSign, error){
		info.Type = ndntype.SigSha256WithEcdsa
		info.KeyLocator = ndn.KeyLocator{Name: name}
		return func(input []byte) (sig []byte, err error) {
			digest := sha256.Sum256(input)
			var ecsig ecdsaSignature
			ecsig.R, ecsig.S, err = ecdsa.Sign(rand.Reader, s.prvKey, digest[:])
			if err != nil {
				sig, err = asn1.Marshal(ecsig)
			}
			return
		},nil
	})
}

func (s *EcdsaSigning) Verify(packet ndn.Verifiable) error {
	return packet.VerifyWith(func(name ndn.Name, info ndn.SigInfo) (ndn.LLVerify, error) {
		if info.Type != ndntype.SigSha256WithEcdsa {
			return nil, ndn.ErrSigType
		}
		return func(input, sig []byte) (err error) {
			digest := sha256.Sum256(input)
			var ecsig ecdsaSignature
			if _, err = asn1.Unmarshal(sig, &ecsig); err != nil {
				return err
			}
		
			if !ecdsa.Verify(s.pubKey, digest[:], ecsig.R, ecsig.S) {
				err = ErrInvalidSig
			}
			return err
		}, nil
	})
}
