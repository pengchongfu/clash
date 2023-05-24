package obfs

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	mathRand "math/rand"
	"net"
	"net/http"

	"github.com/Dreamacro/clash/common/pool"
)

// HTTPObfs is shadowsocks http simple-obfs implementation
type HTTPObfs struct {
	net.Conn
	host                string
	port                string
	buf                 []byte
	offset              int
	firstRequest        bool
	firstRequestBufList [][]byte
	firstResponse       bool
}

func (ho *HTTPObfs) Read(b []byte) (int, error) {
	if ho.buf != nil {
		n := copy(b, ho.buf[ho.offset:])
		ho.offset += n
		if ho.offset == len(ho.buf) {
			pool.Put(ho.buf)
			ho.buf = nil
		}
		return n, nil
	}

	if ho.firstResponse {
		buf := pool.Get(pool.RelayBufferSize)
		n, err := ho.Conn.Read(buf)
		if err != nil {
			pool.Put(buf)
			return 0, err
		}
		idx := bytes.Index(buf[:n], []byte("\r\n\r\n"))
		if idx == -1 {
			pool.Put(buf)
			return 0, io.EOF
		}
		ho.firstResponse = false
		length := n - (idx + 4)
		n = copy(b, buf[idx+4:n])
		if length > n {
			ho.buf = buf[:idx+4+length]
			ho.offset = idx + 4 + n
		} else {
			pool.Put(buf)
		}
		return n, nil
	}
	return ho.Conn.Read(b)
}

func (ho *HTTPObfs) Write(b []byte) (int, error) {
	if ho.firstRequest {
		// firstRequestBufList holds iv packet and socks addr packet and they are sent with the first data packet
		// see https://github.com/Dreamacro/clash/pull/2765
		if len(ho.firstRequestBufList) < 2 {
			ho.firstRequestBufList = append(ho.firstRequestBufList, append([]byte{}, b...))
			return len(b), nil
		}

		firstRequestBuf := append(ho.firstRequestBufList[0], ho.firstRequestBufList[1]...)
		firstRequestBuf = append(firstRequestBuf, b...)

		randBytes := make([]byte, 16)
		rand.Read(randBytes)
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/", ho.host), bytes.NewBuffer(firstRequestBuf))
		req.Header.Set("User-Agent", fmt.Sprintf("curl/7.%d.%d", mathRand.Int()%54, mathRand.Int()%2))
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		req.Host = ho.host
		if ho.port != "80" {
			req.Host = fmt.Sprintf("%s:%s", ho.host, ho.port)
		}
		req.Header.Set("Sec-WebSocket-Key", base64.URLEncoding.EncodeToString(randBytes))
		req.ContentLength = int64(len(firstRequestBuf))
		err := req.Write(ho.Conn)
		ho.firstRequest = false
		ho.firstRequestBufList = nil
		return len(b), err
	}

	return ho.Conn.Write(b)
}

// NewHTTPObfs return a HTTPObfs
func NewHTTPObfs(conn net.Conn, host string, port string) net.Conn {
	return &HTTPObfs{
		Conn:          conn,
		firstRequest:  true,
		firstResponse: true,
		host:          host,
		port:          port,
	}
}
