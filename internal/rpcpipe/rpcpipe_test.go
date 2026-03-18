package rpcpipe

import (
	"errors"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah/internal/rpc/noop"
	"go.podman.io/buildah/internal/rpc/noop/pb"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func init() {
	_ = net.Listener(&pipeListener{})
}

func TestPipeListener(t *testing.T) {
	wg, ctx := errgroup.WithContext(t.Context())
	ignored := "ignore me!"
	requestReader, requestWriter := io.Pipe()
	replyReader, replyWriter := io.Pipe()

	netListener := NewPipeListener(requestReader, replyWriter, nil, nil)
	listener, ok := netListener.(*pipeListener)
	require.True(t, ok, "NewPipeListener() should return a pipeListener")
	server := grpc.NewServer()
	noop.Register(server)
	wg.Go(func() error {
		err := server.Serve(listener)
		t.Logf("finished serving: raw error is %v", err)
		if errors.Is(err, io.ErrClosedPipe) && listener.Closed() {
			err = nil
		}
		t.Logf("finished serving: returning %v", err)
		return err
	})

	t.Log("creating connection")
	conn, err := grpc.NewClient("passthrough:stdio",
		grpc.WithContextDialer(NewPipeContextDialer(replyReader, requestWriter, nil)),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err, "creating first client connection")

	t.Log("initializing client")
	client := pb.NewNoopClient(conn)

	t.Log("sending first client first request")
	resp, err := client.Noop(ctx, &pb.NoopRequest{Ignored: ignored})
	assert.NoError(t, err, "first client first request")
	assert.Equal(t, ignored, resp.GetIgnored(), "first noop did not echo data correctly")

	t.Log("sending first client second request")
	resp, err = client.Noop(ctx, &pb.NoopRequest{Ignored: ignored + ignored})
	assert.NoError(t, err, "first client second request")
	assert.Equal(t, ignored+ignored, resp.GetIgnored(), "second noop did not echo data correctly")

	t.Log("initializing second client")
	client = pb.NewNoopClient(conn)

	t.Log("sending second client first request")
	resp, err = client.Noop(ctx, &pb.NoopRequest{Ignored: ignored})
	assert.NoError(t, err, "second client first request")
	assert.Equal(t, ignored, resp.GetIgnored(), "first noop did not echo data correctly")

	t.Log("sending second client second request")
	resp, err = client.Noop(ctx, &pb.NoopRequest{Ignored: ignored + ignored})
	assert.NoError(t, err, "sending second client second request")
	assert.Equal(t, ignored+ignored, resp.GetIgnored(), "second noop did not echo data correctly")

	t.Log("closing connection")
	err = conn.Close()
	assert.NoError(t, err, "closing connection")

	t.Log("closing listener")
	err = listener.Close()
	assert.NoError(t, err, "closing listener")

	err = wg.Wait()
	assert.NoError(t, err, "server result")
}

func TestPipeListenerClose(t *testing.T) {
	requestReader, requestWriter := io.Pipe()
	replyReader, replyWriter := io.Pipe()
	assert.NoError(t, requestWriter.Close())
	assert.NoError(t, replyReader.Close())

	netListener := NewPipeListener(requestReader, replyWriter, nil, nil)
	assert.NoError(t, netListener.Close())
}
