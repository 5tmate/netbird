package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc/metadata"

	sigProto "github.com/netbirdio/netbird/shared/signal/proto"
	"github.com/netbirdio/netbird/signal/server"
)

// headerBufferingStream defers the response headers until the first message is
// sent, which is how a proxy that releases a gRPC response only once body bytes
// exist (AWS CloudFront, AWS ALB) presents the Signal server to a client.
type headerBufferingStream struct {
	sigProto.SignalExchange_ConnectStreamServer
}

func (s headerBufferingStream) SendHeader(md metadata.MD) error {
	return s.SetHeader(md)
}

type headerBufferingSignalServer struct {
	*server.Server
}

func (s *headerBufferingSignalServer) ConnectStream(stream sigProto.SignalExchange_ConnectStreamServer) error {
	return s.Server.ConnectStream(headerBufferingStream{stream})
}

func newProbeTestClient(t *testing.T, probeInterval time.Duration) (*GrpcClient, context.Context) {
	t.Helper()
	realServer, err := server.NewServer(context.Background(), otel.Meter(""))
	require.NoError(t, err)
	addr := serveTestSignalExchange(t, &headerBufferingSignalServer{Server: realServer})

	key, err := wgtypes.GenerateKey()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	client, err := NewClient(ctx, addr, key, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	client.registrationTimeout = 500 * time.Millisecond
	client.registrationProbeInterval = probeInterval

	receiveDone := make(chan struct{})
	go func() {
		defer close(receiveDone)
		_ = client.Receive(ctx, func(*sigProto.Message) error { return nil })
	}()
	t.Cleanup(func() {
		cancel()
		<-receiveDone
	})
	return client, ctx
}

// TestRegistersBehindHeaderBufferingProxy: with the registration probe, the
// server's echo of the probe is the first body frame on the stream, the proxy
// releases the header with it, and the client registers well inside the bound.
func TestRegistersBehindHeaderBufferingProxy(t *testing.T) {
	client, ctx := newProbeTestClient(t, 50*time.Millisecond)

	streamReady := make(chan struct{})
	go func() {
		client.WaitStreamConnected(ctx)
		close(streamReady)
	}()
	select {
	case <-streamReady:
	case <-time.After(5 * time.Second):
		t.Fatal("client never registered behind a header-buffering proxy")
	}
	require.True(t, client.StreamConnected())
}

// TestNeverRegistersBehindHeaderBufferingProxyWithoutProbe pins the failure the
// probe exists for: with probing off, nothing ever puts a body frame on the
// stream, the header never arrives, and every registration attempt times out.
func TestNeverRegistersBehindHeaderBufferingProxyWithoutProbe(t *testing.T) {
	client, _ := newProbeTestClient(t, 0)

	time.Sleep(2 * time.Second) // four registration bounds
	require.False(t, client.StreamConnected(), "client registered without any body frame on the stream")
}
