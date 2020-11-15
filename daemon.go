package p2pd

import (
	"context"
	"fmt"
	"net"
	"time"

	"os"
	"sync"

	"github.com/libp2p/go-libp2p-daemon/config"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-core/routing"
	"github.com/libp2p/go-libp2p-core/sec"
	"github.com/libp2p/go-libp2p-core/transport"
	tptu "github.com/libp2p/go-libp2p-transport-upgrader"

	multierror "github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log"
	ci "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/mux"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	dhtopts "github.com/libp2p/go-libp2p-kad-dht/opts"
	ps "github.com/libp2p/go-libp2p-pubsub"
	ws "github.com/libp2p/go-ws-transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var log = logging.Logger("p2pd")

type Daemon struct {
	ctx          context.Context
	host         host.Host
	listener     manet.Listener
	tListener    transport.Listener
	incomingConn net.Conn

	dht    *dht.IpfsDHT
	pubsub *ps.PubSub

	mx sync.Mutex
	// stream handlers: map of protocol.ID to multi-address
	handlers map[protocol.ID]ma.Multiaddr
	// closed is set when the daemon is shutting down
	closed bool
}

type secureTransport struct {
	secureInbound  func(ctx context.Context, insecure net.Conn) (sec.SecureConn, error)
	secureOutbound func(ctx context.Context, insecure net.Conn, p peer.ID) (sec.SecureConn, error)
}

func (st secureTransport) SecureInbound(ctx context.Context, insecure net.Conn) (sec.SecureConn, error) {
	return st.secureInbound(ctx, insecure)
}

func (st secureTransport) SecureOutbound(ctx context.Context, insecure net.Conn, p peer.ID) (sec.SecureConn, error) {
	return st.secureOutbound(ctx, insecure, p)
}

type Conn struct {
	net.Conn
}

func (ic Conn) LocalPeer() peer.ID {
	return peer.ID(1)
}

func (ic Conn) RemotePeer() peer.ID {
	return peer.ID(1)
}

func (ic Conn) RemotePublicKey() ci.PubKey {
	return nil
}

// LocalPrivateKey returns the private key for the local peer.
func (ic Conn) LocalPrivateKey() ci.PrivKey {
	return nil
}

type nullMuxer struct{}

func (m *nullMuxer) NewConn(c net.Conn, isServer bool) (mux.MuxedConn, error) {
	fmt.Printf("Jim nullMuxer NewConn\n")
	time.Sleep(60 * time.Second)
	return nil, fmt.Errorf("disabled")
	// return mplex.DefaultTransport.NewConn(c, isServer)
}

func NewDaemon(ctx context.Context, maddr ma.Multiaddr, dhtMode string, opts ...libp2p.Option) (*Daemon, error) {
	d := &Daemon{
		ctx:      ctx,
		handlers: make(map[protocol.ID]ma.Multiaddr),
	}

	if dhtMode != "" {
		var dhtOpts []dhtopts.Option
		if dhtMode == config.DHTClientMode {
			dhtOpts = append(dhtOpts, dhtopts.Client(true))
		} else if dhtMode == config.DHTServerMode {
			dhtOpts = append(dhtOpts, dhtopts.Mode(dht.ModeServer))
		}

		opts = append(opts, libp2p.Routing(d.DHTRoutingFactory(dhtOpts)))
	}

	h, err := libp2p.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	d.host = h

	fmt.Printf("Jim control listen maddr: %v\n", maddr.String())

	st := secureTransport{}
	st.secureInbound = func(ctx context.Context, insecure net.Conn) (sec.SecureConn, error) {
		fmt.Printf("Jim secureInbound: %v\n", insecure)
		d.incomingConn = insecure
		return Conn{insecure}, nil
	}
	u := tptu.Upgrader{
		Secure: st,
		Muxer:  &nullMuxer{},
	}
	cfg := ws.DefaultWebsocketConfig()
	wsTransport := ws.WebsocketTransport{
		Upgrader: &u,
		Config:   cfg,
	}
	wstListener, err := wsTransport.Listen(maddr)
	if err != nil {
		h.Close()
		return nil, err
	}
	fmt.Printf("Jim2\n")
	d.tListener = wstListener

	/*
		l, err := manet.Listen(maddr)
		if err != nil {
			h.Close()
			return nil, err
		}
		d.listener = l
	*/

	go d.listen()
	go d.trapSignals()

	return d, nil
}

func (d *Daemon) Listener() manet.Listener {
	return d.listener
}

func (d *Daemon) DHTRoutingFactory(opts []dhtopts.Option) func(host.Host) (routing.PeerRouting, error) {
	makeRouting := func(h host.Host) (routing.PeerRouting, error) {
		dhtInst, err := dht.New(d.ctx, h, opts...)
		if err != nil {
			return nil, err
		}
		d.dht = dhtInst
		return dhtInst, nil
	}

	return makeRouting
}

func (d *Daemon) EnablePubsub(router string, sign, strict bool) error {
	var opts []ps.Option

	if !sign {
		opts = append(opts, ps.WithMessageSigning(false))
	} else if !strict {
		opts = append(opts, ps.WithStrictSignatureVerification(false))
	}

	switch router {
	case "floodsub":
		pubsub, err := ps.NewFloodSub(d.ctx, d.host, opts...)
		if err != nil {
			return err
		}
		d.pubsub = pubsub
		return nil

	case "gossipsub":
		pubsub, err := ps.NewGossipSub(d.ctx, d.host, opts...)
		if err != nil {
			return err
		}
		d.pubsub = pubsub
		return nil

	default:
		return fmt.Errorf("unknown pubsub router: %s", router)
	}

}

func (d *Daemon) ID() peer.ID {
	return d.host.ID()
}

func (d *Daemon) Addrs() []ma.Multiaddr {
	return d.host.Addrs()
}

func (d *Daemon) listen() {
	for {
		if d.isClosed() {
			return
		}

		c, err := d.tListener.Accept()
		if err != nil {
			log.Errorw("error accepting connection", "error", err)
			continue
		}
		fmt.Printf("Jim incoming connection %v\n", c)

		/*
			c, err := d.listener.Accept()
			if err != nil {
				log.Errorw("error accepting connection", "error", err)
				continue
			}
		*/

		log.Debug("incoming connection")
		// go d.handleConn(c)
		go d.handleConn(d.incomingConn)
	}
}

func (d *Daemon) isClosed() bool {
	d.mx.Lock()
	defer d.mx.Unlock()
	return d.closed
}

func clearUnixSockets(path ma.Multiaddr) error {
	c, _ := ma.SplitFirst(path)
	if c.Protocol().Code != ma.P_UNIX {
		return nil
	}

	if err := os.Remove(c.Value()); err != nil {
		return err
	}

	return nil
}

func (d *Daemon) Close() error {
	d.mx.Lock()
	d.closed = true
	d.mx.Unlock()

	var merr *multierror.Error
	if err := d.host.Close(); err != nil {
		merr = multierror.Append(err)
	}

	listenAddr := d.listener.Multiaddr()
	if err := d.listener.Close(); err != nil {
		merr = multierror.Append(merr, err)
	}

	if err := clearUnixSockets(listenAddr); err != nil {
		merr = multierror.Append(merr, err)
	}

	return merr.ErrorOrNil()
}
