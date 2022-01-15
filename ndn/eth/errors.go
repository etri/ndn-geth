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



package eth

import (
	"errors"
)

var (
	ErrJobAborted		error = errors.New("job is aborted")
	ErrUnexpected		error = errors.New("unexpected thing happends")
	ErrNoPeer			error = errors.New("no more peer is available")
	ErrTooManyTries		error = errors.New("too many attemps to download an object")
	ErrFetchForTooLong	error = errors.New("fetching takes too much time")
	ErrInvalidBlock		error = errors.New("invalid block")
	ErrInvalidHeader	error = errors.New("invalid block header")
	ErrNoBlockFound		error = errors.New("no block is found")
	ErrNoTxFound		error = errors.New("no transaction is found")
	ErrNoHeaderFound	error = errors.New("no header is found")
	ErrInvalidRequest	error = errors.New("request is invalid")
	ErrObjNotFound		error = errors.New("object is not found")
)


type ObjSegmentMarshaling struct {
	Wire	[]byte
	Id		uint16
	Total	uint16
}
