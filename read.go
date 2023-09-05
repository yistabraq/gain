// Copyright (c) 2023 Paweł Gaczyński
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gain

import (
	"github.com/pawelgaczynski/giouring"
	"github.com/yistabraq/gain/pkg/errors"
)

type reader struct {
	ring    *giouring.Ring
	recvMsg bool
}

func (r *reader) addReadRequest(conn *connection) error {
	entry := r.ring.GetSQE()
	if entry == nil {
		return errors.ErrGettingSQE
	}

	conn.inboundBuffer.GrowIfUnsufficientFreeSpace()

	if r.recvMsg {
		entry.PrepareRecvMsg(conn.fd, conn.msgHdr, 0)
		entry.UserData = readDataFlag | uint64(conn.key)
	} else {
		entry.PrepareRecv(
			conn.fd,
			uintptr(conn.inboundWriteAddress()),
			uint32(conn.inboundBuffer.Available()),
			0)
		entry.UserData = readDataFlag | uint64(conn.fd)
	}

	conn.state = connRead
	conn.setKernelSpace()

	return nil
}

func newReader(ring *giouring.Ring, recvMsg bool) *reader {
	return &reader{
		ring:    ring,
		recvMsg: recvMsg,
	}
}
