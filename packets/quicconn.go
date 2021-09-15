package packets

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"net"

	"github.com/lucas-clemente/quic-go"
	"github.com/netsec-ethz/scion-apps/pkg/appnet"
	"github.com/netsec-ethz/scion-apps/pkg/appnet/appquic"
	"github.com/scionproto/scion/go/lib/snet"

	log "github.com/sirupsen/logrus"
)

var _ UDPConn = (*QUICReliableConn)(nil)

func QUICConnConstructor() UDPConn {
	return &QUICReliableConn{}
}

func (qc *QUICReliableConn) GetType() int {
	return ConnectionTypes.Bidirectional
}

// TODO: Implement SCION/QUIC here
type QUICReliableConn struct { // Former: MonitoredConn
	internalConn quic.Stream
	listener     quic.Listener
	session      quic.Session
	path         *snet.Path
	peer         string
	remote       *snet.UDPAddr
	state        int // See Connection States
	metrics      PathMetrics
	local        *snet.UDPAddr
	Ready        chan bool
}

// This simply wraps conn.Read and will later collect metrics
func (qc *QUICReliableConn) Read(b []byte) (int, error) {
	if qc.internalConn == nil {
		<-qc.Ready
	}
	n, err := qc.internalConn.Read(b)
	if err != nil {
		return n, err
	}
	qc.metrics.ReadBytes += int64(n)
	qc.metrics.ReadPackets++
	return n, err
}

func (qc *QUICReliableConn) Dial(addr snet.UDPAddr, path *snet.Path) error {
	sconn, err := appnet.Listen(nil)
	if err != nil {
		return err
	}

	if addr.Path.IsEmpty() {
		appnet.SetPath(&addr, *path)
	}

	qc.peer = addr.String()
	qc.path = path

	host := appnet.MangleSCIONAddr(qc.peer)
	log.Debugf("Dialing to %s and host %s", addr.String(), qc.peer)

	session, err := quic.Dial(sconn, &addr, host, &tls.Config{
		Certificates:       appquic.GetDummyTLSCerts(),
		NextProtos:         []string{"scion-filetransfer"},
		InsecureSkipVerify: true,
	}, &quic.Config{
		KeepAlive: true,
	})

	if err != nil {
		return err
	}

	log.Debugf("Opening Stream to %s", addr.String())

	qc.session = session
	stream, err := session.OpenStreamSync(context.Background())
	if err != nil {
		return err
	}

	log.Debugf("Opened Stream to %s", addr.String())

	qc.internalConn = stream

	return nil
}

// This simply wraps conn.Write and will later collect metrics
func (qc *QUICReliableConn) Write(b []byte) (int, error) {
	n, err := qc.internalConn.Write(b)
	qc.metrics.WrittenBytes += int64(n)
	qc.metrics.WrittenPackets++
	if err != nil {
		return n, err
	}
	return n, err
}

func (qc *QUICReliableConn) WriteStream(b []byte) (int, error) {
	bts := make([]byte, 8)
	binary.BigEndian.PutUint64(bts, uint64(len(b)))
	n, err := qc.Write(bts)
	if err != nil {
		return n, err
	}

	n, err = qc.Write(b)
	return n, err

}

func (qc *QUICReliableConn) ReadStream(b []byte) (int, error) {
	bts := make([]byte, 8)
	n, err := qc.Read(bts)
	if err != nil {
		return n, err
	}
	len := binary.BigEndian.Uint64(bts)
	buf := make([]byte, 9000)
	b = make([]byte, len)
	var i uint64
	for i < len {
		n, err := qc.Read(buf)
		if err != nil {
			return int(i), err
		}
		copy(b[i:int(i)+n], buf)
		i += uint64(n)
	}

	return int(i), err

}

func (qc *QUICReliableConn) Close() error {
	return qc.internalConn.Close()
}

func (qc *QUICReliableConn) AcceptStream() (quic.Stream, error) {
	log.Debugf("Accepting on quic %s", qc.listener.Addr())
	session, err := qc.listener.Accept(context.Background())
	if err != nil {
		return nil, err
	}

	stream, err := session.AcceptStream(context.Background())
	if err != nil {
		return nil, err
	}

	// qc.internalConn = stream

	return stream, nil
}

func (qc *QUICReliableConn) Listen(addr snet.UDPAddr) error {
	qc.Ready = make(chan bool, 0)
	udpAddr := net.UDPAddr{
		IP:   addr.Host.IP,
		Port: addr.Host.Port,
	}
	qc.local = &addr
	sconn, err := appnet.Listen(&udpAddr)
	if err != nil {
		return err
	}
	listener, err := quic.Listen(sconn, &tls.Config{
		Certificates: appquic.GetDummyTLSCerts(),
		NextProtos:   []string{"scion-filetransfer"},
	}, &quic.Config{
		KeepAlive: true,
	})

	log.Debugf("Listen on quic %s wtih scion %s", listener.Addr(), sconn.LocalAddr())

	if err != nil {
		return err
	}

	qc.listener = listener

	return nil
}

func (qc *QUICReliableConn) GetMetrics() *PathMetrics {
	return &qc.metrics
}

func (qc *QUICReliableConn) GetPath() *snet.Path {
	return qc.path
}
func (qc *QUICReliableConn) GetRemote() *snet.UDPAddr {
	return qc.remote
}

func (qc *QUICReliableConn) SetPath(path *snet.Path) {
	qc.path = path
}
func (qc *QUICReliableConn) SetRemote(remote *snet.UDPAddr) {
	qc.remote = remote
}
func (qc *QUICReliableConn) SetLocal(local snet.UDPAddr) {
	qc.local = &local
}

func (qc *QUICReliableConn) SetStream(stream quic.Stream) {
	qc.internalConn = stream
}
