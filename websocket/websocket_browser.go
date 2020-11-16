// +build js,wasm

package websocket

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/libp2p/go-libp2p-core/transport"
	tptu "github.com/libp2p/go-libp2p-transport-upgrader"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// WebsocketTransport is the actual go-libp2p transport
type WebsocketTransport struct {
	Config *WebsocketConfig
}

func New() *WebsocketTransport {
	return &WebsocketTransport{
		Config: DefaultWebsocketConfig(),
	}
}

// NewWithOptions returns a WebsocketTransport constructor function compatible
// with the libp2p.New host constructor. Currently in the browser no options
// are supported.
func NewWithOptions(opts ...Option) func(u *tptu.Upgrader) *WebsocketTransport {
	c := DefaultWebsocketConfig()

	// Apply functional options.
	for _, o := range opts {
		o(c)
	}

	return func(u *tptu.Upgrader) *WebsocketTransport {
		t := &WebsocketTransport{
			Config: c,
		}
		return t
	}
}

func (t *WebsocketTransport) maDial(ctx context.Context, raddr ma.Multiaddr) (mnc manet.Conn, err error) {
	defer func() {
		if r := recover(); r != nil {
			mnc = nil
			switch e := r.(type) {
			case error:
				err = e
			default:
				err = fmt.Errorf("recovered from non-error value: (%T) %+v", e, e)
			}
		}
	}()

	fmt.Printf("Jim go-libp2p-daemon/websocket browser dial %v\n", raddr.String())
	wsurl, err := parseMultiaddr(raddr)
	if err != nil {
		fmt.Printf("Jim go-libp2p-daemon/websocket dial err %v\n", err)
		return nil, err
	}
	wsurlFixed := strings.Replace(wsurl.String(), "wssdaemon:", "wss:", 1)
	wsurlFixed = strings.Replace(wsurlFixed, "wsdaemon:", "ws:", 1)
	fmt.Printf("Jim go-libp2p-daemon/websocket %v\n", wsurlFixed)

	rawConn := js.Global().Get("WebSocket").New(wsurlFixed)
	conn := NewConn(rawConn)
	if err := conn.waitForOpen(); err != nil {
		conn.Close()
		return nil, err
	}
	mnc, err = manet.WrapNetConn(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return mnc, nil
}

func (t *WebsocketTransport) Listen(a ma.Multiaddr) (transport.Listener, error) {
	return nil, errors.New("Listen not implemented on js/wasm")
}
