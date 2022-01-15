package ndnsuit

import (
	"errors"
)
var (
	ErrTimeout          = errors.New("timeout")
	ErrNack  	        = errors.New("NACK")
	ErrResponseStatus   = errors.New("bad command response status")
	ErrInteruption	    = errors.New("external interuption")
	ErrOversize	 	    = errors.New("packet is oversized")
	ErrJobAborted	    = errors.New("job was canceled")
	ErrNoObject			= errors.New("object is not found at server")
	ErrUnexpected		= errors.New("unexpected error")
)

