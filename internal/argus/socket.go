package argus

import (
	"context"
	"fmt"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"
)

const socketCallTimeout = 1 * time.Second
const rpcDispatchByte = 'R'

// PortsClient talks to argus's daemon via the local unix socket. It opens
// a fresh connection per call and tears it down — there is no long-lived
// rpc.Client or pool. The socket is local; per-call overhead is negligible.
type PortsClient struct {
	socketPath string
}

func NewPortsClient(socketPath string) *PortsClient {
	return &PortsClient{socketPath: socketPath}
}

type portsResp struct {
	MCPPort int
	APIPort int
}

type emptyArgs struct{}

// Ports calls Daemon.Ports over the socket and returns (apiPort, mcpPort).
func (c *PortsClient) Ports(ctx context.Context) (int, int, error) {
	var resp portsResp
	if err := c.call(ctx, "Daemon.Ports", &emptyArgs{}, &resp); err != nil {
		return 0, 0, fmt.Errorf("argus socket Ports: %w", err)
	}
	return resp.APIPort, resp.MCPPort, nil
}

func (c *PortsClient) call(ctx context.Context, method string, args, reply any) error {
	deadline := time.Now().Add(socketCallTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	var dialer net.Dialer
	dialCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	conn, err := dialer.DialContext(dialCtx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.socketPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte{rpcDispatchByte}); err != nil {
		return fmt.Errorf("write dispatch byte: %w", err)
	}

	client := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(conn))
	defer client.Close()

	return client.Call(method, args, reply)
}
