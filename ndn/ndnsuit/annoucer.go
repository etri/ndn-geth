package ndnsuit

import (
//	"fmt"
	//"crypto/ecdsa"
	//"crypto/rand"
	"github.com/usnistgov/ndn-dpdk/ndn"
	"github.com/ethereum/go-ethereum/crypto"
//	"github.com/ethereum/go-ethereum/log"
)


type PrefixAnnouncer interface {
	Announce(Sender,chan bool) error
	Prefix() string
}

type pannouncer struct {
	prefix		ndn.Name
	signer		ndn.Signer
}

func newPrefixAnnouncer(prefix ndn.Name, signer ndn.Signer) PrefixAnnouncer {
	ret := &pannouncer{prefix: prefix, signer: signer}
	if signer == nil {
		//create a dummy key for signing prefix announcement messages
		//key, _ := ecdsa.GenerateKey(S256(), rand.Reader)
		key, _ := crypto.GenerateKey()
	    ret.signer = EcdsaSigningFromPrivateKey(key)
	}
	return ret
}

func (pa *pannouncer) Prefix() string {
	return PrettyName(pa.prefix)
}
//prefix annoucement
func (pa *pannouncer) Announce(sender Sender, quit chan bool) (e error) {
	params := Parameters {
		Name: pa.prefix,
		ExpirationPeriod: PREFIX_EXPIRATION_PERIOD,
	}

	e = SendControl(sender,"rib","register",&params, pa.signer, quit)
	return
}


