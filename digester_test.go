package buildah

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func (c *CompositeDigester) isOpen() bool {
	for _, digester := range c.digesters {
		if tarDigester, ok := digester.(*tarDigester); ok {
			if tarDigester.isOpen {
				return true
			}
		}
	}
	return false
}

func TestCompositeDigester(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		itemTypes  []string
		resultType string
	}{
		{
			name:       "download",
			itemTypes:  []string{""},
			resultType: "",
		},
		{
			name:       "file",
			itemTypes:  []string{"file"},
			resultType: "file",
		},
		{
			name:       "dir",
			itemTypes:  []string{"dir"},
			resultType: "dir",
		},
		{
			name:       "multiple-1",
			itemTypes:  []string{"file", "dir"},
			resultType: "multi",
		},
		{
			name:       "multiple-2",
			itemTypes:  []string{"dir", "file"},
			resultType: "multi",
		},
		{
			name:       "multiple-3",
			itemTypes:  []string{"", "dir"},
			resultType: "multi",
		},
		{
			name:       "multiple-4",
			itemTypes:  []string{"", "file"},
			resultType: "multi",
		},
		{
			name:       "multiple-5",
			itemTypes:  []string{"dir", ""},
			resultType: "multi",
		},
		{
			name:       "multiple-6",
			itemTypes:  []string{"file", ""},
			resultType: "multi",
		},
	}
	var digester CompositeDigester
	var i int
	var buf bytes.Buffer
	zero := time.Unix(0, 0)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, filtered := range []bool{false, true} {
				desc := "unfiltered"
				if filtered {
					desc = "filter"
				}
				t.Run(desc, func(t *testing.T) {
					if i > 0 {
						// restart only after it's been used some, to make sure it's not necessary otherwise
						digester.Restart()
					}
					i++
					size := int64(i * 32) // items for this archive will be bigger than the last one
					for _, itemType := range test.itemTypes {
						for int64(buf.Len()) < size {
							err := buf.WriteByte(byte(buf.Len() % 256))
							require.Nil(t, err, "error padding content buffer: %v", err)
						}
						// feed it content that it will treat either as raw data ("") or expect to
						// look like a tarball ("file"/"dir")
						digester.Start(itemType)
						hasher := digester.Hash() // returns an io.WriteCloser
						require.NotNil(t, hasher, "digester returned a null hasher?")
						if itemType == "" {
							// write something that isn't an archive
							n, err := io.Copy(hasher, &buf)
							require.Nil(t, err, "error writing tar content to digester: %v", err)
							require.Equal(t, size, n, "short write writing tar content to digester")
							continue
						}
						// write an archive
						var written bytes.Buffer // a copy of the archive we're generating and digesting
						hasher = &struct {
							io.Writer
							io.Closer
						}{
							Writer: io.MultiWriter(hasher, &written), // splice into the writer
							Closer: hasher,
						}
						if filtered {
							// wrap the WriteCloser in another WriteCloser
							hasher = newTarFilterer(hasher, func(hdr *tar.Header) (tarFilterAction, bool, io.Reader) {
								hdr.ModTime = zero
								return tarFilterKeep, false, nil
							})
							require.NotNil(t, hasher, "newTarFilterer returned a null WriteCloser?")
						}
						// write this item as an archive
						tw := tar.NewWriter(hasher)
						hdr := &tar.Header{
							Name:     "content",
							Size:     size,
							Mode:     0o640,
							ModTime:  time.Now(),
							Typeflag: tar.TypeReg,
						}
						err := tw.WriteHeader(hdr)
						require.Nil(t, err, "error writing tar header to digester: %v", err)
						n, err := io.Copy(tw, &buf)
						require.Nil(t, err, "error writing tar content to digester: %v", err)
						require.Equal(t, size, n, "short write writing tar content to digester")
						err = tw.Flush()
						require.Nil(t, err, "error flushing tar content to digester: %v", err)
						err = tw.Close()
						require.Nil(t, err, "error closing tar archive being written digester: %v", err)
						if filtered {
							// the ContentDigester can close its own if we don't explicitly ask it to,
							// but if we wrapped it in a filter, we have to close the filter to clean
							// up the filter, so we can't skip it to exercise that logic; we have to
							// leave that for the corresponding unfiltered case to try
							hasher.Close()
						}
						// now read the archive back
						tr := tar.NewReader(&written)
						require.NotNil(t, tr, "unable to read byte buffer?")
						hdr, err = tr.Next()
						for err == nil {
							var n int64
							if filtered {
								// the filter should have set the modtime to unix 0
								require.Equal(t, zero, hdr.ModTime, "timestamp for entry should have been zero")
							} else {
								// the filter should have left modtime to "roughly now"
								require.NotEqual(t, zero, hdr.ModTime, "timestamp for entry should not have been zero")
							}
							n, err = io.Copy(io.Discard, tr)
							require.Nil(t, err, "error reading tar content from buffer: %v", err)
							require.Equal(t, hdr.Size, n, "short read reading tar content")
							hdr, err = tr.Next()
						}
						require.Equal(t, io.EOF, err, "finished reading archive with %v, not EOF", err)
					}
					// check the composite digest type matches expectations and the value is not just the
					// digest of zero-length data, which is absolutely not what we wrote
					digestType, digestValue := digester.Digest()
					require.Equal(t, test.resultType, digestType, "expected to get a %q digest back for %v, got %q", test.resultType, test.itemTypes, digestType)
					require.NotEqual(t, digest.Canonical.FromBytes([]byte{}), digestValue, "digester wasn't fed any data")
					require.False(t, digester.isOpen(), "expected digester to have been closed with this usage pattern")
				})
			}
		})
	}
}

func TestTarFilterer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		input, output map[string]string
		breakAfter    int
		filter        func(*tar.Header) (tarFilterAction, bool, io.Reader)
	}{
		{
			name: "none",
			input: map[string]string{
				"file a": "content a",
				"file b": "content b",
			},
			output: map[string]string{
				"file a": "content a",
				"file b": "content b",
			},
			filter: nil,
		},
		{
			name: "plain",
			input: map[string]string{
				"file a": "content a",
				"file b": "content b",
			},
			output: map[string]string{
				"file a": "content a",
				"file b": "content b",
			},
			filter: func(*tar.Header) (tarFilterAction, bool, io.Reader) { return tarFilterKeep, false, nil },
		},
		{
			name: "skip",
			input: map[string]string{
				"file a": "content a",
				"file b": "content b",
			},
			output: map[string]string{
				"file a": "content a",
			},
			filter: func(hdr *tar.Header) (tarFilterAction, bool, io.Reader) {
				var action tarFilterAction
				if hdr.Name == "file b" {
					action = tarFilterSkip
				} else {
					action = tarFilterKeep
				}
				return action, false, nil
			},
		},
		{
			name: "replace",
			input: map[string]string{
				"file a": "content a",
				"file b": "content b",
				"file c": "content c",
			},
			output: map[string]string{
				"file a": "content a",
				"file b": "content b+c",
				"file c": "content c",
			},
			breakAfter: 2,
			filter: func(hdr *tar.Header) (tarFilterAction, bool, io.Reader) {
				if hdr.Name == "file b" {
					content := "content b+c"
					hdr.Size = int64(len(content))
					return tarFilterKeep, true, strings.NewReader(content)
				}
				return tarFilterKeep, false, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			tw := tar.NewWriter(&buffer)
			files := 0
			for filename, contents := range test.input {
				hdr := tar.Header{
					Name:     filename,
					Size:     int64(len(contents)),
					Typeflag: tar.TypeReg,
				}
				err := tw.WriteHeader(&hdr)
				require.Nil(t, err, "unexpected error from TarWriter.WriteHeader")
				n, err := io.CopyN(tw, strings.NewReader(contents), int64(len(contents)))
				require.Nil(t, err, "unexpected error copying to tar writer")
				require.Equal(t, int64(len(contents)), n, "unexpected write length")
				files++
				if test.breakAfter != 0 && files%test.breakAfter == 0 {
					// this test may have us writing multiple archives to the buffer
					// they should still read back as a single archive
					tw.Close()
					tw = tar.NewWriter(&buffer)
				}
			}
			tw.Close()
			output := make(map[string]string)
			pipeReader, pipeWriter := io.Pipe()
			var wg sync.WaitGroup
			wg.Go(func() {
				tr := tar.NewReader(pipeReader)
				hdr, err := tr.Next()
				for err == nil {
					var buffer bytes.Buffer
					var n int64
					n, err = io.Copy(&buffer, tr)
					require.Nil(t, err, "unexpected error copying from tar reader")
					require.Equal(t, hdr.Size, n, "unexpected read length")
					output[hdr.Name] = buffer.String()
					hdr, err = tr.Next()
				}
				require.Equal(t, io.EOF, err, "unexpected error ended our tarstream read")
				pipeReader.Close()
			})
			filterer := newTarFilterer(pipeWriter, test.filter)
			_, err := io.Copy(filterer, &buffer)
			require.Nil(t, err, "unexpected error copying archive through filter to reader")
			filterer.Close()
			wg.Wait()
			require.Equal(t, test.output, output, "got unexpected results")
		})
	}
}
