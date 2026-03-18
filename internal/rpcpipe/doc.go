// The rpcpipe package provides two functions which provide enough
// functionality to run some GRPC over a subprocess's stdio.
//
// NewPipeContextDialer() can be called by the client to create a DialOption
// which can be passed to grpc.NewClient().  It is expected to be given the
// caller's stdin/stdout pair, and will close them when the connection is
// closed.  The target URI passed to grpc.NewClient() will most likely need to
// include the "passthrough:" prefix.
//
// NewPipeListener() can be called by the server.  It is expected to be given
// the parent process's ends of the pipes which were used for the child
// process's stdio, and will accept only one client connection.
//
// Note that these are barely functional - in particular, network addresses
// returned are not going to be particularly useful, and attempts to set
// read/write deadlines will return ENOSYS errors, so using io.NopCloser() or
// similar items to avoid closing the underlying pipes will cause attempts to
// close the connections to stall while attempting blocking I/O.  The grpc
// library doesn't seem to mind these.
package rpcpipe
