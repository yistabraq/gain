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

type writer struct {
	ring    *giouring.Ring
	sendMsg bool
}

func (w *writer) addWriteRequest(conn *connection, link bool) error {
	entry := w.ring.GetSQE()
	if entry == nil {
		return errors.ErrGettingSQE
	}

	if w.sendMsg {
		entry.PrepareSendMsg(conn.fd, conn.msgHdr, 0)
		entry.UserData = writeDataFlag | uint64(conn.key)
	} else {
		entry.PrepareSend(
			conn.fd,
			uintptr(conn.outboundReadAddress()),
			uint32(conn.outboundBuffer.Buffered()),
			0)
		entry.UserData = writeDataFlag | uint64(conn.fd)
	}

	if link {
		entry.Flags |= giouring.SqeIOLink
	}

	conn.state = connWrite
	conn.setKernelSpace()

	return nil
}

func newWriter(ring *giouring.Ring, sendMsg bool) *writer {
	return &writer{
		ring:    ring,
		sendMsg: sendMsg,
	}
}
