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

	"bytes"
	"net/url"
	"encoding/hex"
	"github.com/usnistgov/ndn-dpdk/ndn"
	ndntype "github.com/usnistgov/ndn-dpdk/ndn/an"
)

func BuildName(names ...ndn.Name) ndn.Name {
	l := 0
	for _, n := range names {
		l += len(n)
	}
	newname := make([]ndn.NameComponent, l)
	l = 0
	for _, n := range names {
		copy(newname[l:], n[:])
		l += len(n)
	}
	return newname
}

func CopyName(src ndn.Name) (des ndn.Name) {
	des = make([]ndn.NameComponent,len(src))
	copy(des[:],src)
	return
}

func PrettyName(name ndn.Name) string {
	buf := new(bytes.Buffer)	
	for _, c := range name {
		buf.WriteByte('/')
		buf.WriteString(PrettyNameComponent(&c))
	}
	return buf.String()
}

func PrettyNameComponent(c *ndn.NameComponent) string {
	if c.Type == ndntype.TtImplicitSha256DigestComponent || c.Type == ndntype.TtParametersSha256DigestComponent {
		hexv := hex.EncodeToString(c.Value)
		return hexv[:4]+"..."
	} 
	
	return url.QueryEscape(string(c.Value))
}
