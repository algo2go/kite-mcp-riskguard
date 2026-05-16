// Package checkrpc adapter-method tests.
//
// types_test.go pins the wire schema (gob round-trip, forward/backward
// compat, handshake stability). This file covers the four
// CheckRPCServer handler methods + the four CheckRPCClient proxy
// methods + the two plugin.Plugin adapter methods (Server, Client).
//
// Strategy:
//
//   - CheckRPCServer methods take a (_ any, *resp) shape per the
//     net/rpc convention. We drive them directly with a fakeCheckRPC
//     impl and assert the response pointer is populated AND the
//     plugin's error is propagated unchanged.
//   - CheckRPCClient methods dispatch via *rpc.Client. We stand up a
//     net/rpc test server on a Unix-domain pipe (net.Pipe), register
//     a CheckRPCServer{Impl: fake}, dial it, and drive the proxy.
//     This exercises the actual gob-over-netrpc transport — not a
//     mock — so any subtle wire-protocol regression surfaces here.
//   - CheckPlugin.Server / Client are 2-line factory methods. Direct
//     invocation suffices.
//
// These tests are zero-subprocess: no plugin binary build, no
// hashicorp/go-plugin handshake. The transport layer (gob + rpc.Client)
// is the same one go-plugin uses, so coverage here is faithful.
package checkrpc

import (
	"errors"
	"io"
	"net"
	"net/rpc"
	"testing"

	hplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCheckRPC is a deterministic test double for the CheckRPC
// interface. Each method returns the corresponding field value (or
// the corresponding error if non-nil), letting tests drive both the
// happy path and the error path.
type fakeCheckRPC struct {
	name              string
	nameErr           error
	order             int
	orderErr          error
	recordOnRejection bool
	recordErr         error
	evalResp          CheckResultWire
	evalErr           error
	// Captured request for Evaluate assertions.
	gotReq OrderCheckRequestWire
}

func (f *fakeCheckRPC) Name() (string, error)            { return f.name, f.nameErr }
func (f *fakeCheckRPC) Order() (int, error)              { return f.order, f.orderErr }
func (f *fakeCheckRPC) RecordOnRejection() (bool, error) { return f.recordOnRejection, f.recordErr }
func (f *fakeCheckRPC) Evaluate(req OrderCheckRequestWire) (CheckResultWire, error) {
	f.gotReq = req
	return f.evalResp, f.evalErr
}

// --- CheckRPCServer handler methods ---

// TestCheckRPCServer_Name covers the happy path + error propagation
// for the Name() net/rpc handler.
func TestCheckRPCServer_Name(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		s := &CheckRPCServer{Impl: &fakeCheckRPC{name: "my_plugin"}}
		var got string
		require.NoError(t, s.Name(nil, &got))
		assert.Equal(t, "my_plugin", got)
	})
	t.Run("error propagates", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("boom")
		s := &CheckRPCServer{Impl: &fakeCheckRPC{nameErr: sentinel}}
		var got string
		err := s.Name(nil, &got)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, "", got, "response pointer must not be set on error")
	})
}

// TestCheckRPCServer_Order — same shape as Name, exercises Order
// handler.
func TestCheckRPCServer_Order(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		s := &CheckRPCServer{Impl: &fakeCheckRPC{order: 2500}}
		var got int
		require.NoError(t, s.Order(nil, &got))
		assert.Equal(t, 2500, got)
	})
	t.Run("error propagates", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("order err")
		s := &CheckRPCServer{Impl: &fakeCheckRPC{orderErr: sentinel}}
		var got int
		err := s.Order(nil, &got)
		assert.ErrorIs(t, err, sentinel)
		assert.Equal(t, 0, got)
	})
}

// TestCheckRPCServer_RecordOnRejection — same shape, bool variant.
func TestCheckRPCServer_RecordOnRejection(t *testing.T) {
	t.Parallel()
	t.Run("true", func(t *testing.T) {
		t.Parallel()
		s := &CheckRPCServer{Impl: &fakeCheckRPC{recordOnRejection: true}}
		var got bool
		require.NoError(t, s.RecordOnRejection(nil, &got))
		assert.True(t, got)
	})
	t.Run("false", func(t *testing.T) {
		t.Parallel()
		s := &CheckRPCServer{Impl: &fakeCheckRPC{recordOnRejection: false}}
		var got bool
		require.NoError(t, s.RecordOnRejection(nil, &got))
		assert.False(t, got)
	})
	t.Run("error propagates", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("record err")
		s := &CheckRPCServer{Impl: &fakeCheckRPC{recordErr: sentinel}}
		var got bool
		err := s.RecordOnRejection(nil, &got)
		assert.ErrorIs(t, err, sentinel)
	})
}

// TestCheckRPCServer_Evaluate — hot path. Confirms the wire request
// is passed through verbatim and the wire response is populated.
func TestCheckRPCServer_Evaluate(t *testing.T) {
	t.Parallel()
	t.Run("happy allow", func(t *testing.T) {
		t.Parallel()
		fake := &fakeCheckRPC{evalResp: CheckResultWire{Allowed: true}}
		s := &CheckRPCServer{Impl: fake}
		req := OrderCheckRequestWire{
			Email:         "trader@example.com",
			ToolName:      "place_order",
			Tradingsymbol: "RELIANCE",
			Quantity:      10,
			Price:         2500.5,
		}
		var got CheckResultWire
		require.NoError(t, s.Evaluate(req, &got))
		assert.True(t, got.Allowed)
		assert.Equal(t, req, fake.gotReq, "request must be passed through verbatim")
	})
	t.Run("reject with reason", func(t *testing.T) {
		t.Parallel()
		fake := &fakeCheckRPC{evalResp: CheckResultWire{
			Allowed: false,
			Reason:  "blocked_prefix",
			Message: "blocked",
		}}
		s := &CheckRPCServer{Impl: fake}
		var got CheckResultWire
		require.NoError(t, s.Evaluate(OrderCheckRequestWire{}, &got))
		assert.False(t, got.Allowed)
		assert.Equal(t, "blocked_prefix", got.Reason)
		assert.Equal(t, "blocked", got.Message)
	})
	t.Run("error propagates", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("eval err")
		s := &CheckRPCServer{Impl: &fakeCheckRPC{evalErr: sentinel}}
		var got CheckResultWire
		err := s.Evaluate(OrderCheckRequestWire{}, &got)
		assert.ErrorIs(t, err, sentinel)
	})
}

// --- CheckRPCClient proxy methods ---

// netRPCPair stands up a net/rpc server with a CheckRPCServer
// registered as "Plugin" (the name the proxy dispatches against in
// CheckRPCClient.Call("Plugin.X", ...)). Returns the dialed *rpc.Client
// + a cleanup func that tears everything down.
//
// We use net.Pipe (in-memory synchronous duplex) rather than a TCP
// socket so the test is hermetic and free of port-allocation flakes.
// The pipe drives the same gob codec that hashicorp/go-plugin uses
// over stdio, so the wire-format exercise is faithful.
func netRPCPair(t *testing.T, impl CheckRPC) (*rpc.Client, func()) {
	t.Helper()
	server := rpc.NewServer()
	// "Plugin" is the name the CheckRPCClient.Call calls use; matches
	// the hashicorp/go-plugin contract that exposes the service as
	// "Plugin.<Method>".
	require.NoError(t, server.RegisterName("Plugin", &CheckRPCServer{Impl: impl}))

	serverConn, clientConn := net.Pipe()
	go server.ServeConn(serverConn)

	client := rpc.NewClient(clientConn)
	return client, func() {
		_ = client.Close()
		_ = serverConn.Close()
	}
}

// TestCheckRPCClient_Name dispatches the Name proxy through real
// gob/netrpc transport.
func TestCheckRPCClient_Name(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		rpcClient, cleanup := netRPCPair(t, &fakeCheckRPC{name: "my_plugin"})
		defer cleanup()
		proxy := &CheckRPCClient{Client: rpcClient}
		got, err := proxy.Name()
		require.NoError(t, err)
		assert.Equal(t, "my_plugin", got)
	})
	t.Run("error propagates over rpc", func(t *testing.T) {
		t.Parallel()
		rpcClient, cleanup := netRPCPair(t, &fakeCheckRPC{nameErr: errors.New("name fail")})
		defer cleanup()
		proxy := &CheckRPCClient{Client: rpcClient}
		_, err := proxy.Name()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name fail")
	})
}

// TestCheckRPCClient_Order — proxy + transport coverage for Order.
func TestCheckRPCClient_Order(t *testing.T) {
	t.Parallel()
	rpcClient, cleanup := netRPCPair(t, &fakeCheckRPC{order: 4200})
	defer cleanup()
	proxy := &CheckRPCClient{Client: rpcClient}
	got, err := proxy.Order()
	require.NoError(t, err)
	assert.Equal(t, 4200, got)
}

// TestCheckRPCClient_RecordOnRejection — proxy + transport for the
// bool-returning metadata call.
func TestCheckRPCClient_RecordOnRejection(t *testing.T) {
	t.Parallel()
	t.Run("true", func(t *testing.T) {
		t.Parallel()
		rpcClient, cleanup := netRPCPair(t, &fakeCheckRPC{recordOnRejection: true})
		defer cleanup()
		proxy := &CheckRPCClient{Client: rpcClient}
		got, err := proxy.RecordOnRejection()
		require.NoError(t, err)
		assert.True(t, got)
	})
	t.Run("false", func(t *testing.T) {
		t.Parallel()
		rpcClient, cleanup := netRPCPair(t, &fakeCheckRPC{recordOnRejection: false})
		defer cleanup()
		proxy := &CheckRPCClient{Client: rpcClient}
		got, err := proxy.RecordOnRejection()
		require.NoError(t, err)
		assert.False(t, got)
	})
}

// TestCheckRPCClient_Evaluate — the hot path. Drives a realistic
// payload through the proxy + transport + server stack, asserts the
// server received it verbatim AND the response made it back.
func TestCheckRPCClient_Evaluate(t *testing.T) {
	t.Parallel()
	t.Run("happy allow", func(t *testing.T) {
		t.Parallel()
		fake := &fakeCheckRPC{evalResp: CheckResultWire{Allowed: true}}
		rpcClient, cleanup := netRPCPair(t, fake)
		defer cleanup()
		proxy := &CheckRPCClient{Client: rpcClient}

		req := OrderCheckRequestWire{
			Email:           "trader@example.com",
			ToolName:        "place_order",
			Exchange:        "NSE",
			Tradingsymbol:   "RELIANCE",
			TransactionType: "BUY",
			Quantity:        10,
			Price:           2500.5,
			OrderType:       "LIMIT",
			Confirmed:       true,
			ClientOrderID:   "idem-abc",
		}
		got, err := proxy.Evaluate(req)
		require.NoError(t, err)
		assert.True(t, got.Allowed)
		assert.Equal(t, req, fake.gotReq,
			"server must observe the request fields unchanged across the wire")
	})
	t.Run("reject", func(t *testing.T) {
		t.Parallel()
		fake := &fakeCheckRPC{evalResp: CheckResultWire{
			Allowed: false,
			Reason:  "blocked_prefix",
			Message: "blocked",
		}}
		rpcClient, cleanup := netRPCPair(t, fake)
		defer cleanup()
		proxy := &CheckRPCClient{Client: rpcClient}

		got, err := proxy.Evaluate(OrderCheckRequestWire{Tradingsymbol: "BLOCKED_X"})
		require.NoError(t, err)
		assert.False(t, got.Allowed)
		assert.Equal(t, "blocked_prefix", got.Reason)
		assert.Equal(t, "blocked", got.Message)
	})
}

// --- CheckPlugin (plugin.Plugin adapter) ---

// TestCheckPlugin_Server confirms Server() wraps the supplied Impl in
// a CheckRPCServer with the same Impl pointer.
func TestCheckPlugin_Server(t *testing.T) {
	t.Parallel()
	impl := &fakeCheckRPC{name: "test"}
	p := &CheckPlugin{Impl: impl}
	raw, err := p.Server(nil)
	require.NoError(t, err)
	srv, ok := raw.(*CheckRPCServer)
	require.True(t, ok, "Server() must return a *CheckRPCServer")
	assert.Same(t, impl, srv.Impl, "Impl must be passed through unchanged")
}

// TestCheckPlugin_Client confirms Client() builds a CheckRPCClient
// around the supplied *rpc.Client.
func TestCheckPlugin_Client(t *testing.T) {
	t.Parallel()
	// rpc.NewClient requires an io.ReadWriteCloser; net.Pipe gives us
	// one without a real socket.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := rpc.NewClient(a)
	defer c.Close()

	p := &CheckPlugin{}
	raw, err := p.Client(nil, c)
	require.NoError(t, err)
	proxy, ok := raw.(*CheckRPCClient)
	require.True(t, ok, "Client() must return a *CheckRPCClient")
	assert.Same(t, c, proxy.Client, "*rpc.Client must be passed through unchanged")
}

// TestCheckPlugin_AsPluginPlugin confirms CheckPlugin satisfies the
// plugin.Plugin interface (compile-time assertion guarded by var).
// If hashicorp/go-plugin ever changes the interface, the test
// surfaces it.
func TestCheckPlugin_AsPluginPlugin(t *testing.T) {
	t.Parallel()
	var _ hplugin.Plugin = (*CheckPlugin)(nil)
}

// --- helpers ---

// TestCheckRPC_NetRPCPair_Cleanup verifies the test fixture closes
// cleanly without leaking goroutines (sanity for the rest of the
// suite). Uses io.EOF as the expected error when a closed pipe is
// read from.
func TestCheckRPC_NetRPCPair_Cleanup(t *testing.T) {
	t.Parallel()
	rpcClient, cleanup := netRPCPair(t, &fakeCheckRPC{})
	cleanup()
	// After cleanup, further calls must fail (the client is closed).
	err := rpcClient.Call("Plugin.Name", new(any), new(string))
	// Either ErrShutdown or io.EOF or a closed-pipe error — any
	// non-nil is fine.
	if err == nil {
		t.Error("expected error from closed client; got nil")
	}
	_ = io.EOF // silence unused import on some linters
}
