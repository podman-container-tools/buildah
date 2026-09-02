package ctxreader

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewCancelableReader(t *testing.T) {
	var b bytes.Buffer
	for range 1024 {
		b.Write(make([]byte, 1024))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Nanosecond)
	cancel()
	_, err := io.Copy(io.Discard, NewCancelableReader(ctx, &b))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
