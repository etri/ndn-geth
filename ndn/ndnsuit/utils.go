package ndnsuit

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
