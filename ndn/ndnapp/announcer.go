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
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/usnistgov/ndn-dpdk/ndn"
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

	if e = SendControl(sender,"rib","register",&params, pa.signer, quit); e!=nil {
		log.Info(fmt.Sprintf("Failed to register on %s: %s", pa.prefix.String(), e.Error()))
		return
	}
	log.Info(fmt.Sprintf("%s was registered", PrettyName(pa.prefix)))
	return
}


