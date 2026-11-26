package rpcpipe

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
)

// pipeListener "accepts" a single connection over a Reader/Writer pair
type pipeListener struct {
	stateLock           sync.Mutex // protects closed, closedChannel, accepted, reader, writer
	closed              bool
	closedChannel       chan struct{}
	accepting, accepted bool
	reader              io.ReadCloser
	writer              io.WriteCloser
	finalError          error     // set at initialization, then not written to again
	afterFirstReadDoer  sync.Once // set at initialization, then not written to again
	afterFirstRead      func()    // set at initialization, then not written to again
}

// Close closes the listener, unblocking any Accept() calls currently waiting,
// so that they will return errors, and closes the reader and writer which were
// used to initialize the listener.
func (pl *pipeListener) Close() error {
	pl.stateLock.Lock()
	defer pl.stateLock.Unlock()
	if pl.closed {
		return nil
	}
	rce := pl.reader.Close()
	wce := pl.writer.Close()
	pl.closed = true
	if pl.accepting {
		pl.closedChannel <- struct{}{}
		close(pl.closedChannel)
	}
	pl.reader = nil
	pl.writer = nil
	return errors.Join(rce, wce)
}

func (pl *pipeListener) Closed() bool {
	pl.stateLock.Lock()
	defer pl.stateLock.Unlock()
	return pl.closed
}

func (pl *pipeListener) Addr() net.Addr {
	return &pipeAddr{}
}

func (pl *pipeListener) Accept() (net.Conn, error) {
	pl.stateLock.Lock()
	if pl.closed {
		defer pl.stateLock.Unlock()
		return nil, pl.finalError
	}
	pl.accepting = true
	if !pl.accepted {
		pl.accepted = true
		pl.stateLock.Unlock()
		return &pipeServerConnection{pl: pl, reader: pl.reader, writer: pl.writer}, nil
	}
	pl.stateLock.Unlock()
	<-pl.closedChannel
	return nil, pl.finalError
}

// NewPipeListener creates a listener which will accept, at most, one "connection" over the provided
// reader and writer.
// After that first connection, attempts to Accept() a new connection will stall until the first
// connection is closed, and eventually return finalError, or io.ErrClosedPipe if finalError is nil.
// The afterFirstRead function will be called after the first call to Read() on the reader, which
// provides an opportunity for a caller to finish setting up the other end of the reader and writer.
func NewPipeListener(reader io.ReadCloser, writer io.WriteCloser, finalError error, afterFirstRead func()) net.Listener {
	if finalError == nil {
		finalError = io.ErrClosedPipe
	}
	p := &pipeListener{
		reader:         reader,
		writer:         writer,
		closedChannel:  make(chan (struct{})),
		finalError:     finalError,
		afterFirstRead: afterFirstRead,
	}
	return p
}

type pipeAddr struct{}

func (p pipeAddr) Network() string {
	return "pipes"
}

func (p pipeAddr) String() string {
	return "local"
}

type pipeServerConnection struct {
	pl        *pipeListener // set at initialization, then not written to again
	stateLock sync.Mutex    // protects rd, wd
	rd        time.Time
	wd        time.Time
	reader    io.ReadCloser  // set at initialization, then not written to again
	writer    io.WriteCloser // set at initialization, then not written to again
}

func (psc *pipeServerConnection) SetDeadline(t time.Time) error {
	psc.stateLock.Lock()
	defer psc.stateLock.Unlock()
	psc.rd, psc.wd = t, t
	return syscall.ENOSYS
}

func (psc *pipeServerConnection) SetReadDeadline(t time.Time) error {
	psc.stateLock.Lock()
	defer psc.stateLock.Unlock()
	psc.rd = t
	return syscall.ENOSYS
}

func (psc *pipeServerConnection) SetWriteDeadline(t time.Time) error {
	psc.stateLock.Lock()
	defer psc.stateLock.Unlock()
	psc.wd = t
	return syscall.ENOSYS
}

func (psc *pipeServerConnection) Read(b []byte) (n int, err error) {
	psc.pl.stateLock.Lock()
	if psc.pl.closed {
		psc.pl.stateLock.Unlock()
		return 0, io.EOF
	}
	psc.pl.stateLock.Unlock()
	n, err = psc.reader.Read(b)
	defer psc.pl.afterFirstReadDoer.Do(func() {
		if psc.pl.afterFirstRead != nil {
			psc.pl.afterFirstRead()
		}
	})
	if errors.Is(err, io.EOF) {
		psc.pl.Close()
	}
	return n, err
}

func (psc *pipeServerConnection) Write(b []byte) (n int, err error) {
	psc.pl.stateLock.Lock()
	if psc.pl.closed {
		psc.pl.stateLock.Unlock()
		return 0, io.ErrClosedPipe
	}
	psc.pl.stateLock.Unlock()
	return psc.writer.Write(b)
}

func (psc *pipeServerConnection) Close() error {
	return psc.pl.Close()
}

func (psc *pipeServerConnection) LocalAddr() net.Addr {
	return &pipeAddr{}
}

func (psc *pipeServerConnection) RemoteAddr() net.Addr {
	return &pipeAddr{}
}

type pipeClientConnection struct {
	stateLock          sync.Mutex // protects closed, rd, wd, reader, writer
	closed             bool
	rd                 time.Time
	wd                 time.Time
	reader             io.ReadCloser
	writer             io.WriteCloser
	afterFirstReadDoer sync.Once // set at initialization, then not written to again
	afterFirstRead     func()    // set at initialization, then not written to again
}

func (pcc *pipeClientConnection) SetDeadline(t time.Time) error {
	pcc.stateLock.Lock()
	defer pcc.stateLock.Unlock()
	pcc.rd, pcc.wd = t, t
	return syscall.ENOSYS
}

func (pcc *pipeClientConnection) SetReadDeadline(t time.Time) error {
	pcc.stateLock.Lock()
	defer pcc.stateLock.Unlock()
	pcc.rd = t
	return syscall.ENOSYS
}

func (pcc *pipeClientConnection) SetWriteDeadline(t time.Time) error {
	pcc.stateLock.Lock()
	defer pcc.stateLock.Unlock()
	pcc.wd = t
	return syscall.ENOSYS
}

func (pcc *pipeClientConnection) Read(b []byte) (n int, err error) {
	pcc.stateLock.Lock()
	if pcc.closed {
		pcc.stateLock.Unlock()
		return 0, io.EOF
	}
	reader := pcc.reader
	pcc.stateLock.Unlock()
	n, err = reader.Read(b)
	pcc.afterFirstReadDoer.Do(func() {
		if pcc.afterFirstRead != nil {
			pcc.afterFirstRead()
		}
	})
	if n == 0 || errors.Is(err, io.EOF) {
		pcc.Close()
	}
	return n, err
}

func (pcc *pipeClientConnection) Write(b []byte) (n int, err error) {
	pcc.stateLock.Lock()
	if pcc.closed {
		pcc.stateLock.Unlock()
		return 0, io.ErrClosedPipe
	}
	writer := pcc.writer
	pcc.stateLock.Unlock()
	return writer.Write(b)
}

// Close closes the reader and writer which were used to initialize the
// listener.
func (pcc *pipeClientConnection) Close() error {
	pcc.stateLock.Lock()
	defer pcc.stateLock.Unlock()
	if pcc.closed {
		return nil
	}
	rce := pcc.reader.Close()
	wce := pcc.writer.Close()
	pcc.closed = true
	pcc.reader = nil
	pcc.writer = nil
	if rce == nil {
		return wce
	}
	if wce == nil {
		return rce
	}
	return errors.Join(rce, wce)
}

func (pcc *pipeClientConnection) LocalAddr() net.Addr {
	return &pipeAddr{}
}

func (pcc *pipeClientConnection) RemoteAddr() net.Addr {
	return &pipeAddr{}
}

// NewPipeContextDialer returns a function which returns a connection that runs over the passed-in reader and writer.
func NewPipeContextDialer(reader io.ReadCloser, writer io.WriteCloser, afterFirstRead func()) func(context.Context, string) (net.Conn, error) {
	return func(context.Context, string) (net.Conn, error) {
		return &pipeClientConnection{
			reader:         reader,
			writer:         writer,
			afterFirstRead: afterFirstRead,
		}, nil
	}
}
