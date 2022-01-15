package ndnsuit

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
		info.KeyLocator = ndn.KeyLocator{Name: s.name}
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
