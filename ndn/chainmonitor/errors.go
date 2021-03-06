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


package chainmonitor

import (
	"errors"
)

var (
	ErrRpcNoMethod 		error = errors.New("rpc method unknown")
	ErrRpcNoID			error = errors.New("rpc request has no ID")
	ErrRpcNoArray		error = errors.New("params must be a json array")
	ErrUnexpected		error = errors.New("unexpected thing happends")
	ErrObjNotFound		error = errors.New("object is not found")
)



