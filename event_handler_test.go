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

package gain_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	. "github.com/stretchr/testify/require"
	"github.com/yistabraq/gain"
	gainErrors "github.com/yistabraq/gain/pkg/errors"
	gainNet "github.com/yistabraq/gain/pkg/net"
)

const (
	tcp = iota
	udp
	both
)

type clientBehavior func(net.Conn)

func testHandlerMethod(
	t *testing.T, network string, asyncHandler bool, architecture gain.ServerArchitecture,
	callbacks callbacksHolder, clientBehavior clientBehavior, callCounts []int, shutdown bool,
) {
	t.Helper()
	Equal(t, 4, len(callCounts))

	eventHandlerTester := newEventHandlerTester(callbacks, network)
	eventHandlerTester.onAcceptWg.Add(callCounts[0])
	eventHandlerTester.onReadWg.Add(callCounts[1])
	eventHandlerTester.onWriteWg.Add(callCounts[2])
	eventHandlerTester.onCloseWg.Add(callCounts[3])

	server, port := newTestConnServer(t, network, asyncHandler, architecture, eventHandlerTester)

	conn, err := net.DialTimeout(network, fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil && !errors.Is(err, syscall.ECONNRESET) {
		conn, err = net.DialTimeout(network, fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			log.Panic(err)
		}
	}

	clientBehavior(conn)

	if callCounts[0] > 0 {
		eventHandlerTester.onAcceptWg.Wait()
	}

	if callCounts[1] > 0 {
		eventHandlerTester.onReadWg.Wait()
	}

	if callCounts[2] > 0 {
		eventHandlerTester.onWriteWg.Wait()
	}

	if callCounts[3] > 0 {
		eventHandlerTester.onCloseWg.Wait()
	}

	eventHandlerTester.finished.Store(true)

	Equal(t, 1, int(eventHandlerTester.onStartCount.Load()))
	Equal(t, callCounts[0], int(eventHandlerTester.onAcceptCount.Load()))
	Equal(t, callCounts[1], int(eventHandlerTester.onReadCount.Load()))
	Equal(t, callCounts[2], int(eventHandlerTester.onWriteCount.Load()))
	Equal(t, callCounts[3], int(eventHandlerTester.onCloseCount.Load()))

	if shutdown {
		server.Shutdown()
	}
}

const eventHandlerTestDataSize = 512

var eventHandlerTestData = func() []byte {
	data := make([]byte, eventHandlerTestDataSize)
	_, err := rand.Read(data)
	if err != nil {
		log.Panic(err)
	}

	return data
}()

type eventHandlerTestCase struct {
	name           string
	network        string
	async          bool
	architecture   gain.ServerArchitecture
	callbacks      callbacksHolder
	clientBehavior clientBehavior
	callCounts     []int
}

func testEventHandler(t *testing.T, testCases []eventHandlerTestCase, shutdown bool) {
	t.Helper()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testHandlerMethod(
				t, testCase.network, testCase.async, testCase.architecture,
				testCase.callbacks, testCase.clientBehavior, testCase.callCounts, shutdown,
			)
		})
	}
}

func createTestCases(
	suffix string, networks int, callbacks callbacksHolder, clientBehavior clientBehavior, callCounts [][]int,
) []eventHandlerTestCase {
	tcpTestCases := []eventHandlerTestCase{}

	if networks == tcp || networks == both {
		tcpTestCases = append(tcpTestCases, []eventHandlerTestCase{
			{
				fmt.Sprintf("TestShardingTCPSync%s", suffix),
				gainNet.TCP, false, gain.SocketSharding, callbacks, clientBehavior, callCounts[0],
			},
			{
				fmt.Sprintf("TestShardingTCPAsync%s", suffix),
				gainNet.TCP, true, gain.SocketSharding, callbacks, clientBehavior, callCounts[1],
			},
			{
				fmt.Sprintf("TestReactorTCPSync%s", suffix),
				gainNet.TCP, false, gain.Reactor, callbacks, clientBehavior, callCounts[2],
			},
			{
				fmt.Sprintf("TestReactorTCPAsync%s", suffix),
				gainNet.TCP, true, gain.Reactor, callbacks, clientBehavior, callCounts[3],
			},
		}...)
	}

	udpTestCases := []eventHandlerTestCase{}

	if networks == udp || networks == both {
		var index int
		if networks == both {
			index = 4
		}

		udpTestCases = append(udpTestCases, []eventHandlerTestCase{
			{
				fmt.Sprintf("TestShardingUDPSync%s", suffix),
				gainNet.UDP, false, gain.SocketSharding, callbacks, clientBehavior, callCounts[index],
			},
			{
				fmt.Sprintf("TestShardingUDPAsync%s", suffix),
				gainNet.UDP, true, gain.SocketSharding, callbacks, clientBehavior, callCounts[index+1],
			},
		}...)
	}
	testCases := []eventHandlerTestCase{}
	testCases = append(testCases, tcpTestCases...)
	testCases = append(testCases, udpTestCases...)

	return testCases
}

func TestEventHandlerOnRead(t *testing.T) {
	callbacks := callbacksHolder{
		onReadCallback: func(conn gain.Conn, n int, network string) {
			buffer, err := conn.Next(n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
		},
	}
	clientBehavior := func(conn net.Conn) {
		err := conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 500))
		if err != nil {
			log.Panic(err)
		}

		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		buffer := make([]byte, 1024)

		err = conn.SetReadDeadline(time.Now().Add(time.Millisecond * 500))
		if err != nil {
			log.Panic(err)
		}
		n, err = conn.Read(buffer)
		Equal(t, n, 0)
		NotNil(t, err)
		conn.Close()
	}

	testCases := createTestCases("JustRead", both, callbacks, clientBehavior, [][]int{
		{1, 1, 0, 1},
		{1, 1, 0, 1},
		{1, 1, 0, 1},
		{1, 1, 0, 1},
		{0, 1, 0, 0},
		{0, 1, 0, 0},
	})

	testEventHandler(t, testCases, true)

	callbacks = callbacksHolder{
		onReadCallback: func(conn gain.Conn, n int, network string) {
			buffer, err := conn.Next(n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
			bytesWritten, err := conn.Write(buffer)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, bytesWritten)
		},
		onWriteCallback: func(conn gain.Conn, n int, network string) {
			buf, err := conn.Next(-1)
			Equal(t, 0, len(buf))
			Nil(t, err)
		},
	}
	clientBehavior = func(conn net.Conn) {
		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err = conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
		conn.Close()
	}

	testCases = createTestCases("ReadAndWrite", both, callbacks, clientBehavior, [][]int{
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{0, 1, 1, 0},
		{0, 1, 1, 0},
	})

	testEventHandler(t, testCases, true)

	callbacks = callbacksHolder{
		onReadCallback: func(conn gain.Conn, n int, network string) {
			buffer, err := conn.Next(-1)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
			bytesWritten, err := conn.Write(buffer)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, bytesWritten)
			err = conn.Close()
			Nil(t, err)
		},
		onWriteCallback: func(conn gain.Conn, n int, network string) {
			buf, err := conn.Next(-1)
			Equal(t, 0, len(buf))
			Equal(t, gainErrors.ErrConnectionClosed, err)
		},
	}
	clientBehavior = func(conn net.Conn) {
		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err = conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
	}

	testCases = createTestCases("ReadWriteAndClose", tcp, callbacks, clientBehavior, [][]int{
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
	})

	testEventHandler(t, testCases, true)
}

func TestEventHandlerOnAccept(t *testing.T) {
	callbacks := callbacksHolder{
		onAcceptCallback: func(conn gain.Conn, network string) {
			err := conn.SetLinger(0)
			Nil(t, err)
			err = conn.Close()
			Nil(t, err)
		},
	}
	clientBehavior := func(conn net.Conn) {
		if conn != nil {
			time.Sleep(time.Millisecond * 50)
			n, err := conn.Write(eventHandlerTestData)
			Equal(t, 0, n)
			NotNil(t, err)
		}
	}

	testCases := createTestCases("JustClose", tcp, callbacks, clientBehavior, [][]int{
		{1, 0, 0, 1},
		{1, 0, 0, 1},
		{1, 0, 0, 1},
		{1, 0, 0, 1},
	})

	testEventHandler(t, testCases, true)

	callbacks = callbacksHolder{
		onAcceptCallback: func(conn gain.Conn, network string) {
			err := conn.SetLinger(0)
			Nil(t, err)
			err = conn.Close()
			Nil(t, err)
		},
		onReadCallback: func(conn gain.Conn, n int, network string) {
			buffer, err := conn.Next(n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
			bytesWritten, err := conn.Write(buffer)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, bytesWritten)
			err = conn.Close()
			Nil(t, err)
		},
	}
	clientBehavior = func(conn net.Conn) {
		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err = conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
	}

	testCases = createTestCases("JustClose", udp, callbacks, clientBehavior, [][]int{
		{0, 1, 1, 0},
		{0, 1, 1, 0},
	})

	testEventHandler(t, testCases, true)

	callbacks = callbacksHolder{
		onAcceptCallback: func(conn gain.Conn, network string) {
			n, err := conn.Write(eventHandlerTestData)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, n)
		},
	}
	clientBehavior = func(conn net.Conn) {
		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err := conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
		conn.Close()
	}

	testCases = createTestCases("Write", tcp, callbacks, clientBehavior, [][]int{
		{1, 0, 1, 1},
		{1, 0, 1, 1},
		{1, 0, 1, 1},
		{1, 0, 1, 1},
	})

	testEventHandler(t, testCases, true)

	callbacks = callbacksHolder{
		onAcceptCallback: func(conn gain.Conn, network string) {
			n, err := conn.Write(eventHandlerTestData)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, n)
			err = conn.Close()
			Nil(t, err)
		},
	}
	clientBehavior = func(conn net.Conn) {
		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err := conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
	}

	testCases = createTestCases("WriteAndClose", tcp, callbacks, clientBehavior, [][]int{
		{1, 0, 1, 1},
		{1, 0, 1, 1},
		{1, 0, 1, 1},
		{1, 0, 1, 1},
	})

	testEventHandler(t, testCases, true)
}

func TestEventHandlerOnWrite(t *testing.T) {
	callbacks := callbacksHolder{
		onAcceptCallback: func(conn gain.Conn, network string) {
			var once sync.Once
			conn.SetContext(&once)
		},
		onReadCallback: func(conn gain.Conn, n int, network string) {
			buffer, err := conn.Next(n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
			bytesWritten, err := conn.Write(buffer)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, bytesWritten)
		},
		onWriteCallback: func(conn gain.Conn, n int, network string) {
			time.Sleep(time.Millisecond * 100)
			once, ok := conn.Context().(*sync.Once)
			if !ok {
				log.Panic()
			}

			once.Do(func() {
				bytesWritten, err := conn.Write(eventHandlerTestData)
				Nil(t, err)
				Equal(t, eventHandlerTestDataSize, bytesWritten)
			})
		},
	}
	clientBehavior := func(conn net.Conn) {
		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)

		for i := 0; i < 2; i++ {
			buffer := make([]byte, eventHandlerTestDataSize*2)
			n, err = conn.Read(buffer)
			Equal(t, eventHandlerTestDataSize, n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
		}

		conn.Close()
	}

	testCases := createTestCases("AdditionalWrite", tcp, callbacks, clientBehavior, [][]int{
		{1, 1, 2, 1},
		{1, 1, 2, 1},
		{1, 1, 2, 1},
		{1, 1, 2, 1},
	})

	testEventHandler(t, testCases, true)
}

func TestEventHandlerSetConnectionProperties(t *testing.T) {
	setConnectionProperties := func(conn gain.Conn, network string) {
		err := conn.SetLinger(30)
		NoError(t, err)
		err = conn.SetReadBuffer(2048)
		NoError(t, err)
		err = conn.SetWriteBuffer(2048)
		NoError(t, err)

		if network == gainNet.TCP {
			err = conn.SetKeepAlivePeriod(time.Minute)
			NoError(t, err)
			err = conn.SetNoDelay(true)
			NoError(t, err)
		}
	}
	callbacks := callbacksHolder{
		onAcceptCallback: func(conn gain.Conn, network string) {
			setConnectionProperties(conn, network)
		},
		onReadCallback: func(conn gain.Conn, n int, network string) {
			setConnectionProperties(conn, network)
			buffer, err := conn.Next(n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
			bytesWritten, err := conn.Write(buffer)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, bytesWritten)
		},
		onWriteCallback: func(conn gain.Conn, n int, network string) {
			setConnectionProperties(conn, network)
			conn.Close()
		},
	}
	clientBehavior := func(conn net.Conn) {
		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)

		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err = conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])
	}

	testCases := createTestCases("All", both, callbacks, clientBehavior, [][]int{
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{0, 1, 1, 0},
		{0, 1, 1, 0},
	})

	testEventHandler(t, testCases, true)
}

func TestEventHandlerAsyncShutdown(t *testing.T) {
	var server gain.Server
	callbacks := callbacksHolder{
		onStartCallback: func(s gain.Server, network string) {
			server = s
		},
		onReadCallback: func(conn gain.Conn, n int, network string) {
			buffer, err := conn.Next(n)
			Nil(t, err)
			Equal(t, eventHandlerTestData, buffer)
			bytesWritten, err := conn.Write(buffer)
			Nil(t, err)
			Equal(t, eventHandlerTestDataSize, bytesWritten)
		},
	}
	clientBehavior := func(conn net.Conn) {
		n, err := conn.Write(eventHandlerTestData)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)

		buffer := make([]byte, eventHandlerTestDataSize*2)
		n, err = conn.Read(buffer)
		Equal(t, eventHandlerTestDataSize, n)
		Nil(t, err)
		Equal(t, eventHandlerTestData, buffer[:eventHandlerTestDataSize])

		conn.Close()

		server.AsyncShutdown()
	}

	testCases := createTestCases("All", both, callbacks, clientBehavior, [][]int{
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{1, 1, 1, 1},
		{0, 1, 1, 0},
		{0, 1, 1, 0},
	})

	testEventHandler(t, testCases, false)
}
