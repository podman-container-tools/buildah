package copier

import (
	"archive/tar"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	lslog "github.com/sirupsen/logrus/hooks/slog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mode "github.com/tonistiigi/dchapes-mode"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/reexec"
)

func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	flag.Parse()
	logrus.SetOutput(io.Discard)
	logrus.AddHook(lslog.NewHook(slog.Default(), nil))
	if testing.Verbose() {
		logrus.SetLevel(logrus.DebugLevel)
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	os.Exit(m.Run())
}

// makeFileContents creates contents for a file of a specified size
func makeContents(length int64) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()
	buffered := bufio.NewWriter(pipeWriter)
	go func() {
		count := int64(0)
		for count < length {
			if _, err := buffered.Write([]byte{"0123456789abcdef"[count%16]}); err != nil {
				buffered.Flush()
				pipeWriter.CloseWithError(err)
				return
			}
			count++
		}
		buffered.Flush()
		pipeWriter.Close()
	}()
	return pipeReader
}

// makeArchiveSlice creates an archive from the set of headers and returns a byte slice.
func makeArchiveSlice(headers []tar.Header) []byte {
	rc := makeArchive(headers, nil)
	defer rc.Close()
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, rc); err != nil {
		panic("error creating in-memory archive")
	}
	return buf.Bytes()
}

// makeArchive creates an archive from the set of headers.
func makeArchive(headers []tar.Header, contents map[string][]byte) io.ReadCloser {
	if contents == nil {
		contents = make(map[string][]byte)
	}
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		var err error
		buffered := bufio.NewWriter(pipeWriter)
		tw := tar.NewWriter(buffered)
		for _, header := range headers {
			var fileContent []byte
			switch header.Typeflag {
			case tar.TypeLink, tar.TypeSymlink:
				header.Size = 0
			case tar.TypeReg:
				fileContent = contents[header.Name]
				if len(fileContent) != 0 {
					header.Size = int64(len(fileContent))
				}
			}
			if err = tw.WriteHeader(&header); err != nil {
				break
			}
			if header.Typeflag == tar.TypeReg && header.Size > 0 {
				var fileContents io.Reader
				if len(fileContent) > 0 {
					fileContents = bytes.NewReader(fileContent)
				} else {
					rc := makeContents(header.Size)
					defer rc.Close()
					fileContents = rc
				}
				if _, err = io.Copy(tw, fileContents); err != nil {
					break
				}
			}
		}
		tw.Close()
		buffered.Flush()
		if err != nil {
			pipeWriter.CloseWithError(err)
		} else {
			pipeWriter.Close()
		}
	}()
	return pipeReader
}

// makeContextFromArchive creates a temporary directory, and a subdirectory
// inside of it, from an archive and returns its location.  It can be removed
// once it's no longer needed.
func makeContextFromArchive(t *testing.T, archive io.ReadCloser, subdir string) (string, error) {
	tmp := t.TempDir()
	uidMap := []idtools.IDMap{{HostID: os.Getuid(), ContainerID: 0, Size: 1}}
	gidMap := []idtools.IDMap{{HostID: os.Getgid(), ContainerID: 0, Size: 1}}
	err := Put(tmp, path.Join(tmp, subdir), PutOptions{UIDMap: uidMap, GIDMap: gidMap}, archive)
	archive.Close()
	if err != nil {
		return "", err
	}
	return tmp, err
}

// enumerateFiles walks a directory, returning the items it contains as a slice
// of names relative to that directory.
func enumerateFiles(directory string) ([]enumeratedFile, error) {
	var results []enumeratedFile
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if info == nil || err != nil {
			return err
		}
		rel, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if rel != "" && rel != "." {
			results = append(results, enumeratedFile{
				name:      rel,
				mode:      info.Mode() & os.ModePerm,
				isSymlink: info.Mode()&os.ModeSymlink == os.ModeSymlink,
				date:      info.ModTime().UTC().String(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

type expectedError struct {
	inSubdir bool
	name     string
	err      error
}

type enumeratedFile struct {
	name      string
	mode      os.FileMode
	isSymlink bool
	date      string
}

var (
	testDate       = time.Unix(1485449953, 0)
	secondTestDate = time.Unix(1485449953*2, 0)

	uid = os.Getuid()

	testArchiveSlice = makeArchiveSlice([]tar.Header{
		{Name: "item-0", Typeflag: tar.TypeReg, Size: 123, Mode: 0o600, ModTime: testDate},
		{Name: "item-1", Typeflag: tar.TypeReg, Size: 456, Mode: 0o600, ModTime: testDate},
		{Name: "item-2", Typeflag: tar.TypeReg, Size: 789, Mode: 0o600, ModTime: testDate},
	})

	testArchives = []struct {
		name              string
		rootOnly          bool
		headers           []tar.Header
		contents          map[string][]byte
		excludes          []string
		expectedGetErrors []expectedError
		subdirContents    map[string][]string
		renames           []struct {
			name     string
			renames  map[string]string
			expected []string
		}
	}{
		{
			name:     "regular",
			rootOnly: false,
			headers: []tar.Header{
				{Name: "file-0", Typeflag: tar.TypeReg, Size: 123456789, Mode: 0o600, ModTime: testDate},
				{Name: "file-a", Typeflag: tar.TypeReg, Size: 23, Mode: 0o600, ModTime: testDate},
				{Name: "file-b", Typeflag: tar.TypeReg, Size: 23, Mode: 0o600, ModTime: testDate},
				{Name: "file-c", Typeflag: tar.TypeLink, Linkname: "file-a", Mode: 0o600, ModTime: testDate},
				{Name: "file-u", Typeflag: tar.TypeReg, Size: 23, Mode: cISUID | 0o755, ModTime: testDate},
				{Name: "file-g", Typeflag: tar.TypeReg, Size: 23, Mode: cISGID | 0o755, ModTime: testDate},
				{Name: "file-t", Typeflag: tar.TypeReg, Size: 23, Mode: cISVTX | 0o755, ModTime: testDate},
				{Name: "link-0", Typeflag: tar.TypeSymlink, Linkname: "../file-0", Size: 123456789, Mode: 0o777, ModTime: testDate},
				{Name: "link-a", Typeflag: tar.TypeSymlink, Linkname: "file-a", Size: 23, Mode: 0o777, ModTime: testDate},
				{Name: "link-b", Typeflag: tar.TypeSymlink, Linkname: "../file-a", Size: 23, Mode: 0o777, ModTime: testDate},
				{Name: "hlink-0", Typeflag: tar.TypeLink, Linkname: "file-0", Size: 123456789, Mode: 0o600, ModTime: testDate},
				{Name: "hlink-a", Typeflag: tar.TypeLink, Linkname: "/file-a", Size: 23, Mode: 0o600, ModTime: testDate},
				{Name: "hlink-b", Typeflag: tar.TypeLink, Linkname: "../file-b", Size: 23, Mode: 0o600, ModTime: testDate},
				{Name: "subdir-a", Typeflag: tar.TypeDir, Mode: 0o700, ModTime: testDate},
				{Name: "subdir-a/file-n", Typeflag: tar.TypeReg, Size: 108, Mode: 0o660, ModTime: testDate},
				{Name: "subdir-a/file-o", Typeflag: tar.TypeReg, Size: 34, Mode: 0o660, ModTime: testDate},
				{Name: "subdir-a/file-a", Typeflag: tar.TypeSymlink, Linkname: "../file-a", Size: 23, Mode: 0o777, ModTime: testDate},
				{Name: "subdir-a/file-b", Typeflag: tar.TypeSymlink, Linkname: "../../file-b", Size: 23, Mode: 0o777, ModTime: testDate},
				{Name: "subdir-a/file-c", Typeflag: tar.TypeSymlink, Linkname: "/file-c", Size: 23, Mode: 0o777, ModTime: testDate},
				{Name: "subdir-b", Typeflag: tar.TypeDir, Mode: 0o700, ModTime: testDate},
				{Name: "subdir-b/file-n", Typeflag: tar.TypeReg, Size: 216, Mode: 0o660, ModTime: testDate},
				{Name: "subdir-b/file-o", Typeflag: tar.TypeReg, Size: 45, Mode: 0o660, ModTime: testDate},
				{Name: "subdir-c", Typeflag: tar.TypeDir, Mode: 0o700, ModTime: testDate},
				{Name: "subdir-c/file-n", Typeflag: tar.TypeReg, Size: 432, Mode: 0o666, ModTime: testDate},
				{Name: "subdir-c/file-o", Typeflag: tar.TypeReg, Size: 56, Mode: 0o666, ModTime: testDate},
				{Name: "subdir-d", Typeflag: tar.TypeDir, Mode: 0o700, ModTime: testDate},
				{Name: "subdir-d/hlink-0", Typeflag: tar.TypeLink, Linkname: "../file-0", Size: 123456789, Mode: 0o600, ModTime: testDate},
				{Name: "subdir-d/hlink-a", Typeflag: tar.TypeLink, Linkname: "/file-a", Size: 23, Mode: 0o600, ModTime: testDate},
				{Name: "subdir-d/hlink-b", Typeflag: tar.TypeLink, Linkname: "../../file-b", Size: 23, Mode: 0o600, ModTime: testDate},
				{Name: "archive-a", Typeflag: tar.TypeReg, Size: 0, Mode: 0o600, ModTime: testDate},
				{Name: "subdir-e", Typeflag: tar.TypeDir, Mode: 0o500, ModTime: testDate},
				{Name: "subdir-e/file-p", Typeflag: tar.TypeReg, Size: 890, Mode: 0o600, ModTime: testDate},
			},
			contents: map[string][]byte{
				"archive-a": testArchiveSlice,
			},
			expectedGetErrors: []expectedError{
				{inSubdir: false, name: "link-0", err: syscall.ENOENT},
				{inSubdir: false, name: "link-b", err: syscall.ENOENT},
				{inSubdir: false, name: "subdir-a/file-b", err: syscall.ENOENT},
				{inSubdir: true, name: "link-0", err: syscall.ENOENT},
				{inSubdir: true, name: "link-b", err: syscall.ENOENT},
				{inSubdir: true, name: "subdir-a/file-b", err: syscall.ENOENT},
				{inSubdir: true, name: "subdir-a/file-c", err: syscall.ENOENT},
			},
			renames: []struct {
				name     string
				renames  map[string]string
				expected []string
			}{
				{
					name:    "no-match-dir",
					renames: map[string]string{"subdir-z": "subdir-y"},
					expected: []string{
						"file-0",
						"file-a",
						"file-b",
						"file-c",
						"file-u",
						"file-g",
						"file-t",
						"link-0",
						"link-a",
						"link-b",
						"hlink-0",
						"hlink-a",
						"hlink-b",
						"subdir-a",
						"subdir-a/file-n",
						"subdir-a/file-o",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
						"subdir-b",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c",
						"subdir-c/file-n",
						"subdir-c/file-o",
						"subdir-d",
						"subdir-d/hlink-0",
						"subdir-d/hlink-a",
						"subdir-d/hlink-b",
						"subdir-e",
						"subdir-e/file-p",
						"archive-a",
					},
				},
				{
					name:    "no-match-file",
					renames: map[string]string{"file-n": "file-z"},
					expected: []string{
						"file-0",
						"file-a",
						"file-b",
						"file-c",
						"file-u",
						"file-g",
						"file-t",
						"link-0",
						"link-a",
						"link-b",
						"hlink-0",
						"hlink-a",
						"hlink-b",
						"subdir-a",
						"subdir-a/file-n",
						"subdir-a/file-o",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
						"subdir-b",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c",
						"subdir-c/file-n",
						"subdir-c/file-o",
						"subdir-d",
						"subdir-d/hlink-0",
						"subdir-d/hlink-a",
						"subdir-d/hlink-b",
						"subdir-e",
						"subdir-e/file-p",
						"archive-a",
					},
				},
				{
					name:    "directory",
					renames: map[string]string{"subdir-a": "subdir-z"},
					expected: []string{
						"file-0",
						"file-a",
						"file-b",
						"file-c",
						"file-u",
						"file-g",
						"file-t",
						"link-0",
						"link-a",
						"link-b",
						"hlink-0",
						"hlink-a",
						"hlink-b",
						"subdir-z",
						"subdir-z/file-n",
						"subdir-z/file-o",
						"subdir-z/file-a",
						"subdir-z/file-b",
						"subdir-z/file-c",
						"subdir-b",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c",
						"subdir-c/file-n",
						"subdir-c/file-o",
						"subdir-d",
						"subdir-d/hlink-0",
						"subdir-d/hlink-a",
						"subdir-d/hlink-b",
						"subdir-e",
						"subdir-e/file-p",
						"archive-a",
					},
				},
				{
					name:    "file-in-directory",
					renames: map[string]string{"subdir-a/file-n": "subdir-a/file-z"},
					expected: []string{
						"file-0",
						"file-a",
						"file-b",
						"file-c",
						"file-u",
						"file-g",
						"file-t",
						"link-0",
						"link-a",
						"link-b",
						"hlink-0",
						"hlink-a",
						"hlink-b",
						"subdir-a",
						"subdir-a/file-z",
						"subdir-a/file-o",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
						"subdir-b",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c",
						"subdir-c/file-n",
						"subdir-c/file-o",
						"subdir-d",
						"subdir-d/hlink-0",
						"subdir-d/hlink-a",
						"subdir-d/hlink-b",
						"subdir-e",
						"subdir-e/file-p",
						"archive-a",
					},
				},
			},
		},
		{
			name:     "devices",
			rootOnly: true,
			headers: []tar.Header{
				{Name: "char-dev", Typeflag: tar.TypeChar, Devmajor: 0, Devminor: 0, Mode: 0o600, ModTime: testDate},
				{Name: "blk-dev", Typeflag: tar.TypeBlock, Devmajor: 0, Devminor: 0, Mode: 0o600, ModTime: testDate},
			},
		},
	}
)

func TestPutNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testPut(t)
	canChroot = couldChroot
}

func testPut(t *testing.T) {
	uidMap := []idtools.IDMap{{HostID: os.Getuid(), ContainerID: 0, Size: 1}}
	gidMap := []idtools.IDMap{{HostID: os.Getgid(), ContainerID: 0, Size: 1}}

	for i := range testArchives {
		for _, topdir := range []string{"", ".", "top"} {
			t.Run(fmt.Sprintf("archive=%s,topdir=%s", testArchives[i].name, topdir), func(t *testing.T) {
				if uid != 0 && testArchives[i].rootOnly {
					t.Skipf("test archive %q can only be tested with root privileges, skipping", testArchives[i].name)
				}

				dir, err := makeContextFromArchive(t, makeArchive(testArchives[i].headers, testArchives[i].contents), topdir)
				require.NoErrorf(t, err, "error creating context from archive %q, topdir=%q", testArchives[i].name, topdir)

				// enumerate what we expect to have created
				expected := make([]enumeratedFile, 0, len(testArchives[i].headers)+1)
				if topdir != "" && topdir != "." {
					info, err := os.Stat(filepath.Join(dir, topdir))
					require.NoErrorf(t, err, "error statting directory %q", filepath.Join(dir, topdir))
					expected = append(expected, enumeratedFile{
						name:      filepath.FromSlash(topdir),
						mode:      info.Mode() & os.ModePerm,
						isSymlink: info.Mode()&os.ModeSymlink == os.ModeSymlink,
						date:      info.ModTime().UTC().String(),
					})
				}
				for _, hdr := range testArchives[i].headers {
					expected = append(expected, enumeratedFile{
						name:      filepath.Join(filepath.FromSlash(topdir), filepath.FromSlash(hdr.Name)),
						mode:      os.FileMode(hdr.Mode) & os.ModePerm,
						isSymlink: hdr.Typeflag == tar.TypeSymlink,
						date:      hdr.ModTime.UTC().String(),
					})
				}
				sort.Slice(expected, func(i, j int) bool { return strings.Compare(expected[i].name, expected[j].name) < 0 })

				// enumerate what we actually created
				fileList, err := enumerateFiles(dir)
				require.NoErrorf(t, err, "error walking context directory for archive %q, topdir=%q", testArchives[i].name, topdir)
				sort.Slice(fileList, func(i, j int) bool { return strings.Compare(fileList[i].name, fileList[j].name) < 0 })

				// make sure they're the same
				moddedEnumeratedFiles := func(enumerated []enumeratedFile) []enumeratedFile {
					m := make([]enumeratedFile, 0, len(enumerated))
					for i := range enumerated {
						e := enumeratedFile{
							name:      enumerated[i].name,
							mode:      os.FileMode(int64(enumerated[i].mode) & testModeMask),
							isSymlink: enumerated[i].isSymlink,
							date:      enumerated[i].date,
						}
						if testIgnoreSymlinkDates && e.isSymlink {
							e.date = ""
						}
						m = append(m, e)
					}
					return m
				}
				if !reflect.DeepEqual(expected, fileList) && reflect.DeepEqual(moddedEnumeratedFiles(expected), moddedEnumeratedFiles(fileList)) {
					logrus.Warn("chmod() lost some bits and possibly timestamps on symlinks, otherwise we match the source archive")
				} else {
					require.Equal(t, expected, fileList, "list of files in context directory for archive %q under topdir %q should match the archived used to populate it", testArchives[i].name, topdir)
				}
			})
		}

		for _, renames := range testArchives[i].renames {
			t.Run(fmt.Sprintf("archive=%s,rename=%s", testArchives[i].name, renames.name), func(t *testing.T) {
				if uid != 0 && testArchives[i].rootOnly {
					t.Skipf("test archive %q can only be tested with root privileges, skipping", testArchives[i].name)
				}

				tmp := t.TempDir()

				archive := makeArchive(testArchives[i].headers, testArchives[i].contents)
				err := Put(tmp, tmp, PutOptions{UIDMap: uidMap, GIDMap: gidMap, Rename: renames.renames}, archive)
				require.NoErrorf(t, err, "error extracting archive %q to directory %q", testArchives[i].name, tmp)

				var found []string
				err = filepath.WalkDir(tmp, func(path string, _ fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					rel, err := filepath.Rel(tmp, path)
					if err != nil {
						return err
					}
					if rel == "." {
						return nil
					}
					found = append(found, rel)
					return nil
				})
				require.NoErrorf(t, err, "error walking context directory for archive %q under %q", testArchives[i].name, tmp)
				sort.Strings(found)

				expected := renames.expected
				sort.Strings(expected)
				assert.Equal(t, expected, found, "renaming did not work as expected")
			})
		}
	}

	// Overwrite directory
	for _, overwrite := range []bool{false, true} {
		for _, typeFlag := range []byte{tar.TypeReg, tar.TypeLink, tar.TypeSymlink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
			t.Run(fmt.Sprintf("overwrite (dir)=%v,type=%c", overwrite, typeFlag), func(t *testing.T) {
				archive := makeArchiveSlice([]tar.Header{
					{Name: "target", Typeflag: tar.TypeSymlink, Mode: 0o755, Linkname: "target", ModTime: testDate},
					{Name: "target", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
					{Name: "target", Typeflag: tar.TypeSymlink, Mode: 0o755, Linkname: "target", ModTime: testDate},
					{Name: "target", Typeflag: tar.TypeReg, Size: 123, Mode: 0o755, ModTime: testDate},
					{Name: "test", Typeflag: tar.TypeDir, Size: 0, Mode: 0o755, ModTime: testDate},
					{Name: "test/content", Typeflag: tar.TypeReg, Size: 0, Mode: 0o755, ModTime: testDate},
					{Name: "test", Typeflag: typeFlag, Size: 0, Mode: 0o755, Linkname: "target", ModTime: testDate},
				})
				tmp := t.TempDir()
				err := Put(tmp, tmp, PutOptions{UIDMap: uidMap, GIDMap: gidMap, NoOverwriteDirNonDir: !overwrite}, bytes.NewReader(archive))
				if overwrite {
					if !errors.Is(err, syscall.EPERM) {
						assert.Nilf(t, err, "expected to overwrite directory with type %c: %v", typeFlag, err)
					}
				} else {
					assert.Errorf(t, err, "expected an error trying to overwrite directory with type %c", typeFlag)
				}
			})
		}
	}

	// Overwrite non-directory
	for _, overwrite := range []bool{false, true} {
		for _, typeFlag := range []byte{tar.TypeReg, tar.TypeLink, tar.TypeSymlink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
			t.Run(fmt.Sprintf("overwrite (non-dir)=%v,type=%c", overwrite, typeFlag), func(t *testing.T) {
				archive := makeArchiveSlice([]tar.Header{
					{Name: "target", Typeflag: tar.TypeSymlink, Mode: 0o755, Linkname: "target", ModTime: testDate},
					{Name: "target", Typeflag: tar.TypeReg, Mode: 0o755, ModTime: testDate},
					{Name: "target", Typeflag: tar.TypeSymlink, Mode: 0o755, Linkname: "target", ModTime: testDate},
					{Name: "target", Typeflag: tar.TypeReg, Size: 123, Mode: 0o755, ModTime: testDate},
					{Name: "test", Typeflag: typeFlag, Size: 0, Mode: 0o755, Linkname: "target", ModTime: testDate},
					{Name: "test", Typeflag: tar.TypeDir, Size: 0, Mode: 0o755, ModTime: testDate},
					{Name: "test/content", Typeflag: tar.TypeReg, Size: 0, Mode: 0o755, ModTime: testDate},
				})
				tmp := t.TempDir()
				err := Put(tmp, tmp, PutOptions{UIDMap: uidMap, GIDMap: gidMap, NoOverwriteNonDirDir: !overwrite}, bytes.NewReader(archive))
				if overwrite {
					if !errors.Is(err, syscall.EPERM) {
						assert.Nilf(t, err, "expected to overwrite file with type %c: %v", typeFlag, err)
					}
				} else {
					assert.Errorf(t, err, "expected an error trying to overwrite file of type %c", typeFlag)
				}
			})
		}
	}
	for _, ignoreDevices := range []bool{false, true} {
		for _, typeFlag := range []byte{tar.TypeChar, tar.TypeBlock} {
			t.Run(fmt.Sprintf("ignoreDevices=%v,type=%c", ignoreDevices, typeFlag), func(t *testing.T) {
				if uid != 0 && !ignoreDevices {
					t.Skip("can only test !IgnoreDevices with root privileges, skipping")
				}
				archive := makeArchiveSlice([]tar.Header{
					{Name: "test", Typeflag: typeFlag, Size: 0, Mode: 0o600, ModTime: testDate, Devmajor: 0, Devminor: 0},
					{Name: "link", Typeflag: tar.TypeLink, Size: 0, Mode: 0o600, ModTime: testDate, Linkname: "test"},
					{Name: "unrelated", Typeflag: tar.TypeReg, Size: 0, Mode: 0o600, ModTime: testDate},
				})
				tmp := t.TempDir()
				err := Put(tmp, tmp, PutOptions{UIDMap: uidMap, GIDMap: gidMap, IgnoreDevices: ignoreDevices}, bytes.NewReader(archive))
				require.Nilf(t, err, "expected to extract content with typeflag %c without an error: %v", typeFlag, err)
				fileList, err := enumerateFiles(tmp)
				require.Nilf(t, err, "unexpected error scanning the contents of extraction directory for typeflag %c: %v", typeFlag, err)
				expectedItems := 3
				if ignoreDevices {
					expectedItems = 1
				}
				require.Equalf(t, expectedItems, len(fileList), "didn't extract as many things as expected for typeflag %c", typeFlag)
			})
		}
	}

	for _, stripSetuidBit := range []bool{false, true} {
		for _, stripSetgidBit := range []bool{false, true} {
			for _, stripStickyBit := range []bool{false, true} {
				t.Run(fmt.Sprintf("stripSetuidBit=%v,stripSetgidBit=%v,stripStickyBit=%v", stripSetuidBit, stripSetgidBit, stripStickyBit), func(t *testing.T) {
					mode := int64(0o700) | cISUID | cISGID | cISVTX
					archive := makeArchiveSlice([]tar.Header{
						{Name: "test", Typeflag: tar.TypeReg, Size: 0, Mode: mode, ModTime: testDate},
					})
					tmp := t.TempDir()
					putOptions := PutOptions{
						UIDMap:         uidMap,
						GIDMap:         gidMap,
						StripSetuidBit: stripSetuidBit,
						StripSetgidBit: stripSetgidBit,
						StripStickyBit: stripStickyBit,
					}
					err := Put(tmp, tmp, putOptions, bytes.NewReader(archive))
					require.Nilf(t, err, "unexpected error writing sample file", err)
					st, err := os.Stat(filepath.Join(tmp, "test"))
					require.Nilf(t, err, "unexpected error checking permissions of file", err)
					assert.Equalf(t, stripSetuidBit, st.Mode()&os.ModeSetuid == 0, "setuid bit was not set/stripped correctly")
					assert.Equalf(t, stripSetgidBit, st.Mode()&os.ModeSetgid == 0, "setgid bit was not set/stripped correctly")
					assert.Equalf(t, stripStickyBit, st.Mode()&os.ModeSticky == 0, "sticky bit was not set/stripped correctly")
				})
			}
		}
	}
}

func isExpectedError(err error, inSubdir bool, name string, expectedErrors []expectedError) bool {
	// if we couldn't read that content, check if it's one of the expected failures
	for _, expectedError := range expectedErrors {
		if expectedError.inSubdir != inSubdir {
			continue
		}
		if expectedError.name != name {
			continue
		}
		if !strings.Contains(err.Error(), expectedError.err.Error()) {
			// not expecting this specific error
			continue
		}
		// it's an expected failure
		return true
	}
	return false
}

func TestStatNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testStat(t)
	canChroot = couldChroot
}

func testStat(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		for _, topdir := range []string{"", ".", "top"} {
			for _, testArchive := range testArchives {
				if uid != 0 && testArchive.rootOnly {
					t.Logf("test archive %q can only be tested with root privileges, skipping", testArchive.name)
					continue
				}

				dir, err := makeContextFromArchive(t, makeArchive(testArchive.headers, testArchive.contents), topdir)
				require.NoErrorf(t, err, "error creating context from archive %q", testArchive.name)

				root := dir

				for _, testItem := range testArchive.headers {
					name := filepath.FromSlash(testItem.Name)
					if absolute {
						name = filepath.Join(root, topdir, name)
					}
					t.Run(fmt.Sprintf("absolute=%t,topdir=%s,archive=%s,item=%s", absolute, topdir, testArchive.name, name), func(t *testing.T) {
						// read stats about this item
						var excludes []string
						for _, exclude := range testArchive.excludes {
							excludes = append(excludes, filepath.FromSlash(exclude))
						}
						options := StatOptions{
							CheckForArchives: false,
							Excludes:         excludes,
						}
						stats, err := Stat(root, topdir, options, []string{name})
						require.NoErrorf(t, err, "error statting %q: %v", name, err)
						for _, st := range stats {
							// should not have gotten an error
							require.Emptyf(t, st.Error, "expected no error from stat %q", st.Glob)
							// no matching characters -> should have matched one item
							require.NotEmptyf(t, st.Globbed, "expected at least one match on glob %q", st.Glob)
							matches := 0
							for _, glob := range st.Globbed {
								matches++
								require.Equal(t, st.Glob, glob, "expected entry for %q", st.Glob)
								require.NotNil(t, st.Results[glob], "%q globbed %q, but there are no results for it", st.Glob, glob)
								toStat := glob
								if !absolute {
									toStat = filepath.Join(root, topdir, name)
								}
								_, err = os.Lstat(toStat)
								require.NoErrorf(t, err, "got error on lstat() of returned value %q(%q(%q)): %v", toStat, glob, name, err)
								result := st.Results[glob]

								switch testItem.Typeflag {
								case tar.TypeReg:
									if actualContent, ok := testArchive.contents[testItem.Name]; ok {
										testItem.Size = int64(len(actualContent))
									}
									checkStatInfoOwnership(t, result)
									require.Equal(t, testItem.Size, result.Size, "unexpected size difference for %q", name)
									require.True(t, result.IsRegular, "expected %q.IsRegular to be true", glob)
									require.False(t, result.IsDir, "expected %q.IsDir to be false", glob)
									require.False(t, result.IsSymlink, "expected %q.IsSymlink to be false", glob)
								case tar.TypeDir:
									require.False(t, result.IsRegular, "expected %q.IsRegular to be false", glob)
									require.True(t, result.IsDir, "expected %q.IsDir to be true", glob)
									require.False(t, result.IsSymlink, "expected %q.IsSymlink to be false", glob)
								case tar.TypeSymlink:
									require.True(t, result.IsSymlink, "%q is supposed to be a symbolic link, but is not", name)
									require.Equal(t, filepath.FromSlash(testItem.Linkname), result.ImmediateTarget, "%q is supposed to point to %q, but points to %q", glob, testItem.Linkname, result.ImmediateTarget)
								case tar.TypeBlock, tar.TypeChar:
									require.False(t, result.IsRegular, "%q is a regular file, but is not supposed to be", name)
									require.False(t, result.IsDir, "%q is a directory, but is not supposed to be", name)
									require.False(t, result.IsSymlink, "%q is not supposed to be a symbolic link, but appears to be one", name)
								}
							}
							require.Equal(t, 1, matches, "non-glob %q matched %d items, not exactly one", name, matches)
						}
					})
				}
			}
		}
	}
}

func TestGetSingleNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testGetSingle(t)
	canChroot = couldChroot
}

func testGetSingle(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		for _, topdir := range []string{"", ".", "top"} {
			for _, testArchive := range testArchives {
				var excludes []string
				for _, exclude := range testArchive.excludes {
					excludes = append(excludes, filepath.FromSlash(exclude))
				}

				getOptions := GetOptions{
					Excludes:       excludes,
					ExpandArchives: false,
				}

				if uid != 0 && testArchive.rootOnly {
					t.Logf("test archive %q can only be tested with root privileges, skipping", testArchive.name)
					continue
				}

				dir, err := makeContextFromArchive(t, makeArchive(testArchive.headers, testArchive.contents), topdir)
				require.NoErrorf(t, err, "error creating context from archive %q", testArchive.name)

				root := dir

				for _, testItem := range testArchive.headers {
					name := filepath.FromSlash(testItem.Name)
					if absolute {
						name = filepath.Join(root, topdir, name)
					}
					t.Run(fmt.Sprintf("absolute=%t,topdir=%s,archive=%s,item=%s", absolute, topdir, testArchive.name, name), func(t *testing.T) {
						// check if we can get this one item
						err := Get(root, topdir, getOptions, []string{name}, io.Discard)
						// if we couldn't read that content, check if it's one of the expected failures
						if err != nil && isExpectedError(err, topdir != "" && topdir != ".", testItem.Name, testArchive.expectedGetErrors) {
							return
						}
						require.NoErrorf(t, err, "error getting %q under %q", name, filepath.Join(root, topdir))
						// we'll check subdirectories later
						if testItem.Typeflag == tar.TypeDir {
							return
						}
						// check what we get when we get this one item
						pipeReader, pipeWriter := io.Pipe()
						var getErr error
						var wg sync.WaitGroup
						wg.Go(func() {
							getErr = Get(root, topdir, getOptions, []string{name}, pipeWriter)
							pipeWriter.Close()
						})
						tr := tar.NewReader(pipeReader)
						hdr, err := tr.Next()
						for err == nil {
							assert.Equal(t, filepath.Base(name), filepath.FromSlash(hdr.Name), "expected item named %q, got %q", filepath.Base(name), filepath.FromSlash(hdr.Name))
							hdr, err = tr.Next()
						}
						assert.ErrorIs(t, err, io.EOF, "expected EOF at end of archive")
						if !t.Failed() && testItem.Typeflag == tar.TypeReg && testItem.Mode&(cISUID|cISGID|cISVTX) != 0 {
							for _, stripSetuidBit := range []bool{false, true} {
								for _, stripSetgidBit := range []bool{false, true} {
									for _, stripStickyBit := range []bool{false, true} {
										t.Run(fmt.Sprintf("absolute=%t,topdir=%s,archive=%s,item=%s,strip_setuid=%t,strip_setgid=%t,strip_sticky=%t", absolute, topdir, testArchive.name, name, stripSetuidBit, stripSetgidBit, stripStickyBit), func(t *testing.T) {
											var getErr error
											var wg sync.WaitGroup
											getOptions := getOptions
											getOptions.StripSetuidBit = stripSetuidBit
											getOptions.StripSetgidBit = stripSetgidBit
											getOptions.StripStickyBit = stripStickyBit
											pipeReader, pipeWriter := io.Pipe()
											wg.Go(func() {
												getErr = Get(root, topdir, getOptions, []string{name}, pipeWriter)
												pipeWriter.Close()
											})
											tr := tar.NewReader(pipeReader)
											hdr, err := tr.Next()
											for err == nil {
												expectedMode := testItem.Mode
												modifier := ""
												if stripSetuidBit {
													expectedMode &^= cISUID
													modifier += "(with setuid bit stripped) "
												}
												if stripSetgidBit {
													expectedMode &^= cISGID
													modifier += "(with setgid bit stripped) "
												}
												if stripStickyBit {
													expectedMode &^= cISVTX
													modifier += "(with sticky bit stripped) "
												}
												if expectedMode != hdr.Mode && expectedMode&testModeMask == hdr.Mode&testModeMask {
													logrus.Warnf("chmod() lost some bits: expected 0%o, got 0%o", expectedMode, hdr.Mode)
												} else {
													assert.Equal(t, expectedMode, hdr.Mode, "expected item named %q %sto have mode 0%o, got 0%o", hdr.Name, modifier, expectedMode, hdr.Mode)
												}
												hdr, err = tr.Next()
											}
											assert.ErrorIs(t, err, io.EOF, "expected EOF at end of archive")
											wg.Wait()
											assert.NoErrorf(t, getErr, "unexpected error from Get(%q): %v", name, getErr)
											pipeReader.Close()
										})
									}
								}
							}
						}

						wg.Wait()
						assert.NoErrorf(t, getErr, "unexpected error from Get(%q): %v", name, getErr)
						pipeReader.Close()
					})
				}
			}
		}
	}
}

func TestGetMultipleNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testGetMultiple(t)
	canChroot = couldChroot
}

func testGetMultiple(t *testing.T) {
	type getTestArchiveCase struct {
		name               string
		pattern            string
		exclude            []string
		items              []string
		expandArchives     bool
		stripSetuidBit     bool
		stripSetgidBit     bool
		stripStickyBit     bool
		stripXattrs        bool
		keepDirectoryNames bool
		renames            map[string]string
		noDerefSymlinks    bool
		parents            bool
		timestamp          *time.Time
		expectedSymlinks   map[string]string
	}
	getTestArchives := []struct {
		name              string
		headers           []tar.Header
		contents          map[string][]byte
		cases             []getTestArchiveCase
		expectedGetErrors []expectedError
	}{
		{
			name: "regular",
			headers: []tar.Header{
				{Name: "file-0", Typeflag: tar.TypeReg, Size: 123456789, Mode: 0o600},
				{Name: "file-a", Typeflag: tar.TypeReg, Size: 23, Mode: 0o600},
				{Name: "file-b", Typeflag: tar.TypeReg, Size: 23, Mode: 0o600},
				{Name: "link-a", Typeflag: tar.TypeSymlink, Linkname: "file-a", Size: 23, Mode: 0o600},
				{Name: "link-c", Typeflag: tar.TypeSymlink, Linkname: "subdir-c", Mode: 0o700, ModTime: testDate},
				{Name: "archive-a", Typeflag: tar.TypeReg, Size: 0, Mode: 0o600},
				{Name: "non-archive-a", Typeflag: tar.TypeReg, Size: 1199, Mode: 0o600},
				{Name: "hlink-0", Typeflag: tar.TypeLink, Linkname: "file-0", Size: 123456789, Mode: 0o600},
				{Name: "something-a", Typeflag: tar.TypeReg, Size: 34, Mode: 0o600},
				{Name: "subdir-a/", Typeflag: tar.TypeDir, Mode: 0o700},
				{Name: "subdir-a/file-n", Typeflag: tar.TypeReg, Size: 108, Mode: 0o660},
				{Name: "subdir-a/file-o", Typeflag: tar.TypeReg, Size: 45, Mode: 0o660},
				{Name: "subdir-a/file-a", Typeflag: tar.TypeSymlink, Linkname: "../file-a", Size: 23, Mode: 0o600},
				{Name: "subdir-a/file-b", Typeflag: tar.TypeSymlink, Linkname: "../../file-b", Size: 23, Mode: 0o600},
				{Name: "subdir-a/file-c", Typeflag: tar.TypeReg, Size: 56, Mode: 0o600},
				{Name: "subdir-b/", Typeflag: tar.TypeDir, Mode: 0o700},
				{Name: "subdir-b/file-n", Typeflag: tar.TypeReg, Size: 216, Mode: 0o660},
				{Name: "subdir-b/file-o", Typeflag: tar.TypeReg, Size: 67, Mode: 0o660},
				{Name: "subdir-c/", Typeflag: tar.TypeDir, Mode: 0o700},
				{Name: "subdir-c/file-p", Typeflag: tar.TypeReg, Size: 432, Mode: 0o666},
				{Name: "subdir-c/file-q", Typeflag: tar.TypeReg, Size: 78, Mode: 0o666},
				{Name: "subdir-d/", Typeflag: tar.TypeDir, Mode: 0o700},
				{Name: "subdir-d/hlink-0", Typeflag: tar.TypeLink, Linkname: "../file-0", Size: 123456789, Mode: 0o600},
				{Name: "subdir-e/", Typeflag: tar.TypeDir, Mode: 0o700},
				{Name: "subdir-e/subdir-f/", Typeflag: tar.TypeDir, Mode: 0o700},
				{Name: "subdir-e/subdir-f/hlink-b", Typeflag: tar.TypeLink, Linkname: "../../file-b", Size: 23, Mode: 0o600},
			},
			contents: map[string][]byte{
				"archive-a": testArchiveSlice,
			},
			expectedGetErrors: []expectedError{
				{inSubdir: true, name: ".", err: syscall.ENOENT},
				{inSubdir: true, name: "/subdir-b/*", err: fmt.Errorf("matched nothing")},
				{inSubdir: true, name: "../../subdir-b/*", err: fmt.Errorf("matched nothing")},
			},
			cases: []getTestArchiveCase{
				{
					name:    "everything",
					pattern: ".",
					items: []string{
						"file-0",
						"file-a",
						"file-b",
						"link-a",
						"link-c",
						"hlink-0",
						"something-a",
						"archive-a",
						"non-archive-a",
						"subdir-a/",
						"subdir-a/file-n",
						"subdir-a/file-o",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
						"subdir-b/",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c/",
						"subdir-c/file-p",
						"subdir-c/file-q",
						"subdir-d/",
						"subdir-d/hlink-0",
						"subdir-e/",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
					expectedSymlinks: map[string]string{
						"link-a":          "file-a",
						"link-c":          "subdir-c",
						"subdir-a/file-a": "../file-a",
						"subdir-a/file-b": "../../file-b",
					},
				},
				{
					name:    "wildcard",
					pattern: "*",
					items: []string{
						"file-0",
						"file-a",
						"file-b",
						"link-a",
						"hlink-0",
						"something-a",
						"archive-a",
						"non-archive-a",
						"file-n",           // from subdir-a
						"file-o",           // from subdir-a
						"file-a",           // from subdir-a
						"file-b",           // from subdir-a
						"file-c",           // from subdir-a
						"file-n",           // from subdir-b
						"file-o",           // from subdir-b
						"file-p",           // from subdir-c
						"file-p",           // from link-c -> subdir-c
						"file-q",           // from subdir-c
						"file-q",           // from link-c -> subdir-c
						"hlink-0",          // from subdir-d
						"subdir-f/",        // from subdir-e
						"subdir-f/hlink-b", // from subdir-e
					},
					expectedSymlinks: map[string]string{
						"file-a": "../file-a",
						"file-b": "../../file-b",
					},
				},
				{
					name:    "timestamped",
					pattern: "file*",
					items: []string{
						"file-0",
						"file-a",
						"file-b",
					},
					timestamp: &secondTestDate,
				},
				{
					name:    "dot-with-wildcard-includes-and-excludes",
					pattern: ".",
					exclude: []string{"**/*-a", "!**/*-c"},
					items: []string{
						"file-0",
						"file-b",
						"link-c",
						"hlink-0",
						// "subdir-a/file-c", // strings.HasPrefix("**/*-c", "subdir-a/") is false
						"subdir-b/",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c/",
						"subdir-c/file-p",
						"subdir-c/file-q",
						"subdir-d/",
						"subdir-d/hlink-0",
						"subdir-e/",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
					expectedSymlinks: map[string]string{
						"link-c": "subdir-c",
					},
				},
				{
					name:    "everything-with-wildcard-includes-and-excludes",
					pattern: "*",
					exclude: []string{"**/*-a", "!**/*-c"},
					items: []string{
						"file-0",
						"file-b",
						"file-c",
						"file-n",
						"file-o",
						"file-p", // from subdir-c
						"file-p", // from link-c -> subdir-c
						"file-q", // from subdir-c
						"file-q", // from link-c -> subdir-c
						"hlink-0",
						"hlink-0",
						"subdir-f/",
						"subdir-f/hlink-b",
					},
				},
				{
					name:    "dot-with-dot-exclude",
					pattern: ".",
					exclude: []string{".", "!**/*-c"},
					items: []string{
						"file-0",
						"file-a",
						"file-b",
						"link-a",
						"link-c",
						"hlink-0",
						"something-a",
						"archive-a",
						"non-archive-a",
						"subdir-a/",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
						"subdir-a/file-n",
						"subdir-a/file-o",
						"subdir-b/",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c/",
						"subdir-c/file-p",
						"subdir-c/file-q",
						"subdir-d/",
						"subdir-d/hlink-0",
						"subdir-e/",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
					expectedSymlinks: map[string]string{
						"link-a":          "file-a",
						"link-c":          "subdir-c",
						"subdir-a/file-a": "../file-a",
						"subdir-a/file-b": "../../file-b",
					},
				},
				{
					name:    "everything-with-dot-exclude",
					pattern: "*",
					exclude: []string{".", "!**/*-c"},
					items: []string{
						"file-0",
						"file-a",
						"file-a",
						"file-b",
						"file-b",
						"file-c",
						"file-n",
						"file-n",
						"file-o",
						"file-o",
						"file-p",
						"file-p",
						"file-q",
						"file-q",
						"hlink-0",
						"hlink-0",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
						"subdir-f/",
						"subdir-f/hlink-b",
					},
					expectedSymlinks: map[string]string{
						"file-a": "../file-a",
						"file-b": "../../file-b",
					},
				},
				{
					name:    "all-with-all-exclude",
					pattern: "*",
					exclude: []string{"*", "!**/*-c"},
					items: []string{
						"file-c",
						"file-p",
						"file-p",
						"file-q",
						"file-q",
					},
				},
				{
					name:    "everything-with-all-exclude",
					pattern: ".",
					exclude: []string{"*", "!**/*-c"},
					items: []string{
						// "subdir-a/file-c", // strings.HasPrefix("**/*-c", "subdir-a/") is false
						"link-c",
						"subdir-c/",
						"subdir-c/file-p",
						"subdir-c/file-q",
					},
					expectedSymlinks: map[string]string{
						"link-c": "subdir-c",
					},
				},
				{
					name:    "file-wildcard",
					pattern: "file-*",
					items: []string{
						"file-0",
						"file-a",
						"file-b",
					},
				},
				{
					name:    "file-and-dir-wildcard",
					pattern: "*-a",
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
						"file-n", // from subdir-a
						"file-o", // from subdir-a
						"file-a", // from subdir-a
						"file-b", // from subdir-a
						"file-c", // from subdir-a
					},
					expectedSymlinks: map[string]string{
						"file-a": "../file-a",
						"file-b": "../../file-b",
					},
				},
				{
					name:    "file-and-dir-wildcard-with-exclude",
					pattern: "*-a",
					exclude: []string{"subdir-a", "top/subdir-a"},
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
					},
				},
				{
					name:    "file-and-dir-wildcard-with-wildcard-exclude",
					pattern: "*-a",
					exclude: []string{"subdir*", "top/subdir*"},
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
					},
				},
				{
					name:    "file-and-dir-wildcard-with-deep-exclude",
					pattern: "*-a",
					exclude: []string{"**/subdir-a"},
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
					},
				},
				{
					name:    "file-and-dir-wildcard-with-wildcard-deep-exclude",
					pattern: "*-a",
					exclude: []string{"**/subdir*"},
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
					},
				},
				{
					name:    "file-and-dir-wildcard-with-deep-include",
					pattern: "*-a",
					exclude: []string{"**/subdir-a", "!**/file-c"},
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
						"file-c",
					},
				},
				{
					name:    "file-and-dir-wildcard-with-wildcard-deep-include",
					pattern: "*-a",
					exclude: []string{"**/subdir*", "!**/file-c"},
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
						"file-c",
					},
				},
				{
					name:    "subdirectory",
					pattern: "subdir-e",
					items: []string{
						"subdir-f/",
						"subdir-f/hlink-b",
					},
				},
				{
					name:    "subdirectory-wildcard",
					pattern: "*/subdir-*",
					items: []string{
						"hlink-b", // from subdir-e/subdir-f
					},
				},
				{
					name:    "not-expanded-archive",
					pattern: "*archive-a",
					items: []string{
						"archive-a",
						"non-archive-a",
					},
				},
				{
					name:           "expanded-archive",
					pattern:        "*archive-a",
					expandArchives: true,
					items: []string{
						"non-archive-a",
						"item-0",
						"item-1",
						"item-2",
					},
				},
				{
					name:    "subdir-without-name",
					pattern: "subdir-e",
					items: []string{
						"subdir-f/",
						"subdir-f/hlink-b",
					},
				},
				{
					name:               "subdir-with-name",
					pattern:            "subdir-e",
					keepDirectoryNames: true,
					items: []string{
						"subdir-e/",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
				},
				{
					name:               "root-wildcard",
					pattern:            "/subdir-b/*",
					keepDirectoryNames: false,
					items: []string{
						"file-n",
						"file-o",
					},
				},
				{
					name:               "dotdot-wildcard",
					pattern:            "../../subdir-b/*",
					keepDirectoryNames: false,
					items: []string{
						"file-n",
						"file-o",
					},
				},
				{
					name:               "wildcard-with-rename",
					pattern:            "*-a",
					keepDirectoryNames: false,
					renames:            map[string]string{"file-a": "renamed"},
					items: []string{
						"renamed", // from file-a
						"link-a",
						"archive-a",
						"non-archive-a",
						"something-a",
						"file-n",  // from subdir-a
						"file-o",  // from subdir-a
						"renamed", // from subdir-a/file-a -> file-a -> renamed
						"file-b",  // from subdir-a
						"file-c",  // from subdir-a
					},
					expectedSymlinks: map[string]string{
						"file-b":  "../../file-b",
						"renamed": "../file-a",
					},
				},
				{
					name:               "wildcard-with-rename-keep",
					pattern:            "*-a",
					keepDirectoryNames: true,
					renames:            map[string]string{"subdir-a": "subdir-b"},
					items: []string{
						"file-a",
						"link-a",
						"archive-a",
						"non-archive-a",
						"something-a",
						"subdir-b/",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-b/file-a",
						"subdir-b/file-b",
						"subdir-b/file-c",
					},
					expectedSymlinks: map[string]string{
						"subdir-b/file-a": "../file-a",
						"subdir-b/file-b": "../../file-b",
					},
				},
				{
					name:            "no-deref-symlinks-baseline",
					pattern:         "*-a",
					noDerefSymlinks: true,
					items: []string{
						"file-a",
						"link-a",
						"archive-a",
						"non-archive-a",
						"something-a",
						"file-n", // from subdir-a
						"file-o", // from subdir-a
						"file-a", // from subdir-a
						"file-b", // from subdir-a
						"file-c", // from subdir-a
					},
					expectedSymlinks: map[string]string{
						"file-a": "../file-a",
						"file-b": "../../file-b",
						"link-a": "file-a",
					},
				},
				{
					name:            "no-deref-symlinks-directory",
					pattern:         "link-c",
					noDerefSymlinks: true,
					items: []string{
						"link-c",
					},
					expectedSymlinks: map[string]string{
						"link-c": "subdir-c",
					},
				},
				{
					name:            "deref-symlinks-directory",
					pattern:         "link-c",
					noDerefSymlinks: false,
					items: []string{
						"file-p", // from link-c -> subdir-c
						"file-q", // from link-c -> subdir-c
					},
				},
				{
					name:    "wildcard and parents",
					pattern: "*",
					parents: true,
					items: []string{
						"file-0",
						"file-a",
						"file-b",
						"link-a",
						"hlink-0",
						"something-a",
						"archive-a",
						"non-archive-a",
						"subdir-a/",
						"subdir-b/",
						"subdir-c/",
						"subdir-d/",
						"subdir-e/",
						"subdir-a/file-n",
						"subdir-a/file-o",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c/file-p",
						"subdir-c/file-p",
						"subdir-c/file-q",
						"subdir-c/file-q",
						"subdir-d/hlink-0",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
					expectedSymlinks: map[string]string{
						"subdir-a/file-a": "../file-a",
						"subdir-a/file-b": "../../file-b",
					},
				},
				{
					name:    "everything-with-wildcard-includes-and-excludes-parents",
					pattern: "*",
					parents: true,
					exclude: []string{"**/*-a", "!**/*-c"},
					items: []string{
						"file-0",
						"file-b",
						"subdir-a/",
						"subdir-b/",
						"subdir-c/",
						"subdir-d/",
						"subdir-e/",
						"subdir-a/file-c",
						"subdir-b/file-n",
						"subdir-b/file-o",
						"subdir-c/file-p",
						"subdir-c/file-p",
						"subdir-c/file-q",
						"subdir-c/file-q",
						"hlink-0",
						"subdir-d/hlink-0",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
				},
				{
					name:    "file-and-dir-wildcard-parents",
					pattern: "*-a",
					parents: true,
					items: []string{
						"file-a",
						"link-a",
						"something-a",
						"archive-a",
						"non-archive-a",
						"subdir-a/",
						"subdir-a/file-n",
						"subdir-a/file-o",
						"subdir-a/file-a",
						"subdir-a/file-b",
						"subdir-a/file-c",
					},
					expectedSymlinks: map[string]string{
						"subdir-a/file-a": "../file-a",
						"subdir-a/file-b": "../../file-b",
					},
				},
				{
					name:    "root-wildcard-parents",
					pattern: "/subdir-b/*",
					parents: true,
					items: []string{
						"subdir-b/",
						"subdir-b/file-n",
						"subdir-b/file-o",
					},
				},
				{
					name:    "dotdot-wildcard-parents",
					pattern: "../../subdir-b/*",
					parents: true,
					items: []string{
						"subdir-b/",
						"subdir-b/file-n",
						"subdir-b/file-o",
					},
				},
				{
					name:    "dir-with-parents",
					pattern: "subdir-e/subdir-f/",
					parents: true,
					items: []string{
						"subdir-e/",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
				},
				{
					name:    "hlink-with-parents",
					pattern: "subdir-e/subdir-f/hlink-b",
					parents: true,
					items: []string{
						"subdir-e/",
						"subdir-e/subdir-f/",
						"subdir-e/subdir-f/hlink-b",
					},
				},
			},
		},
	}

	for _, topdir := range []string{"", ".", "top"} {
		for _, testArchive := range getTestArchives {
			dir, err := makeContextFromArchive(t, makeArchive(testArchive.headers, testArchive.contents), topdir)
			require.NoErrorf(t, err, "error creating context from archive %q", testArchive.name)

			root := dir

			cases := make(map[string]struct{})
			for _, testCase := range testArchive.cases {
				if _, ok := cases[testCase.name]; ok {
					t.Fatalf("duplicate case %q", testCase.name)
				}
				cases[testCase.name] = struct{}{}
			}

			for _, testCase := range testArchive.cases {
				var excludes []string
				for _, exclude := range testCase.exclude {
					excludes = append(excludes, filepath.FromSlash(exclude))
				}

				getOptions := GetOptions{
					Excludes:           excludes,
					ExpandArchives:     testCase.expandArchives,
					StripSetuidBit:     testCase.stripSetuidBit,
					StripSetgidBit:     testCase.stripSetgidBit,
					StripStickyBit:     testCase.stripStickyBit,
					StripXattrs:        testCase.stripXattrs,
					KeepDirectoryNames: testCase.keepDirectoryNames,
					Rename:             testCase.renames,
					NoDerefSymlinks:    testCase.noDerefSymlinks,
					Parents:            testCase.parents,
					Timestamp:          testCase.timestamp,
				}

				t.Run(fmt.Sprintf("topdir=%s,archive=%s,case=%s,pattern=%s", topdir, testArchive.name, testCase.name, testCase.pattern), func(t *testing.T) {
					// ensure that we can get stuff using this spec
					err := Get(root, topdir, getOptions, []string{testCase.pattern}, io.Discard)
					if err != nil && isExpectedError(err, topdir != "" && topdir != ".", testCase.pattern, testArchive.expectedGetErrors) {
						return
					}
					require.NoErrorf(t, err, "error getting %q under %q", testCase.pattern, filepath.Join(root, topdir))
					// see what we get when we get this pattern
					pipeReader, pipeWriter := io.Pipe()
					var getErr error
					var wg sync.WaitGroup
					wg.Go(func() {
						getErr = Get(root, topdir, getOptions, []string{testCase.pattern}, pipeWriter)
						pipeWriter.Close()
					})
					tr := tar.NewReader(pipeReader)
					hdr, err := tr.Next()
					actualContents := []string{}
					foundSymlinks := make(map[string]string)

					for hdr != nil {
						actualContents = append(actualContents, filepath.FromSlash(hdr.Name))
						assert.Equal(t, "", hdr.Uname, "expected user name field to be cleared")
						assert.Equal(t, "", hdr.Gname, "expected group name field to be cleared")
						if testCase.timestamp != nil {
							assert.Truef(t, testCase.timestamp.Equal(hdr.ModTime), "timestamp was supposed to be forced for %q", hdr.Name)
						}

						if hdr.Typeflag == byte(tar.TypeSymlink) {
							foundSymlinks[hdr.Name] = hdr.Linkname
						}

						if err != nil {
							break
						}
						hdr, err = tr.Next()
					}
					pipeReader.Close()
					sort.Strings(actualContents)
					// compare it to what we were supposed to get
					expectedContents := make([]string, 0, len(testCase.items))
					for _, item := range testCase.items {
						expectedContents = append(expectedContents, filepath.FromSlash(item))
					}
					sort.Strings(expectedContents)
					assert.ErrorIs(t, err, io.EOF, "expected EOF at end of archive")
					wg.Wait()
					assert.NoErrorf(t, getErr, "unexpected error from Get(%q)", testCase.pattern)
					assert.Equal(t, expectedContents, actualContents, "Get(%q,excludes=%v) didn't produce the right set of items", testCase.pattern, excludes)

					expectedSymlinks := testCase.expectedSymlinks
					if expectedSymlinks == nil {
						expectedSymlinks = make(map[string]string)
					}
					assert.Equal(t, expectedSymlinks, foundSymlinks)
				})
			}
		}
	}
}

func TestEvalNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testEval(t)
	canChroot = couldChroot
}

func testEval(t *testing.T) {
	tmp := t.TempDir()
	options := EvalOptions{}
	linkname := filepath.Join(tmp, "link")
	vectors := []struct {
		id, linkTarget, inputPath, evaluatedPath string
	}{
		{"0a", "target", "link/foo", "target/foo"},
		{"1a", "/target", "link/foo", "target/foo"},
		{"2a", "../target", "link/foo", "target/foo"},
		{"3a", "/../target", "link/foo", "target/foo"},
		{"4a", "../../target", "link/foo", "target/foo"},
		{"5a", "target/subdirectory", "link/foo", "target/subdirectory/foo"},
		{"6a", "/target/subdirectory", "link/foo", "target/subdirectory/foo"},
		{"7a", "../target/subdirectory", "link/foo", "target/subdirectory/foo"},
		{"8a", "/../target/subdirectory", "link/foo", "target/subdirectory/foo"},
		{"9a", "../../target/subdirectory", "link/foo", "target/subdirectory/foo"},
		// inputPath is lexically cleaned to "foo" early, so callers
		// won't get values consistent with the kernel, but we use the
		// result for ADD and COPY, where docker build seems to have
		// the same limitation
		{"0b", "target", "link/../foo", "foo"},
		{"1b", "/target", "link/../foo", "foo"},
		{"2b", "../target", "link/../foo", "foo"},
		{"3b", "/../target", "link/../foo", "foo"},
		{"4b", "../../target", "link/../foo", "foo"},
		{"5b", "target/subdirectory", "link/../foo", "foo"},
		{"6b", "/target/subdirectory", "link/../foo", "foo"},
		{"7b", "../target/subdirectory", "link/../foo", "foo"},
		{"8b", "/../target/subdirectory", "link/../foo", "foo"},
		{"9b", "../../target/subdirectory", "link/../foo", "foo"},
	}
	for _, vector := range vectors {
		t.Run(fmt.Sprintf("id=%s", vector.id), func(t *testing.T) {
			err := os.Symlink(vector.linkTarget, linkname)
			if err != nil && errors.Is(err, os.ErrExist) {
				os.Remove(linkname)
				err = os.Symlink(vector.linkTarget, linkname)
			}
			require.NoErrorf(t, err, "error creating link from %q to %q", linkname, vector.linkTarget)
			evaluated, err := Eval(tmp, filepath.Join(tmp, vector.inputPath), options)
			require.NoErrorf(t, err, "error evaluating %q: %v", vector.inputPath, err)
			require.Equalf(t, filepath.Join(tmp, vector.evaluatedPath), evaluated, "evaluation of %q with %q pointing to %q failed", vector.inputPath, linkname, vector.linkTarget)
		})
	}
}

func TestMkdirNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testMkdir(t)
	canChroot = couldChroot
}

func testMkdir(t *testing.T) {
	type testCase struct {
		name        string
		create      string
		makeParents types.OptionalBool
		fail        bool
		expect      []string
	}
	testArchives := []struct {
		name      string
		headers   []tar.Header
		testCases []testCase
	}{
		{
			name: "regular",
			headers: []tar.Header{
				{Name: "subdir-a", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-b", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-b/subdir-c", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle1", Typeflag: tar.TypeSymlink, Linkname: "dangle1-target", ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle2", Typeflag: tar.TypeSymlink, Linkname: "../dangle2-target", ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle3", Typeflag: tar.TypeSymlink, Linkname: "../../dangle3-target", ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle4", Typeflag: tar.TypeSymlink, Linkname: "../../../dangle4-target", ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle5", Typeflag: tar.TypeSymlink, Linkname: "../../../../dangle5-target", ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle6", Typeflag: tar.TypeSymlink, Linkname: "/dangle6-target", ModTime: testDate},
				{Name: "subdir-a/subdir-b/dangle7", Typeflag: tar.TypeSymlink, Linkname: "/../dangle7-target", ModTime: testDate},
			},
			testCases: []testCase{
				{
					name:   "basic",
					create: "subdir-d",
					expect: []string{"subdir-d"},
				},
				{
					name:   "subdir",
					create: "subdir-d/subdir-e/subdir-f",
					expect: []string{"subdir-d", "subdir-d/subdir-e", "subdir-d/subdir-e/subdir-f"},
				},
				{
					name:   "dangling-link-itself",
					create: "subdir-a/subdir-b/dangle1",
					expect: []string{"subdir-a/subdir-b/dangle1-target"},
				},
				{
					name:   "dangling-link-as-intermediate-parent",
					create: "subdir-a/subdir-b/dangle2/final",
					expect: []string{"subdir-a/dangle2-target", "subdir-a/dangle2-target/final"},
				},
				{
					name:   "dangling-link-as-intermediate-grandparent",
					create: "subdir-a/subdir-b/dangle3/final",
					expect: []string{"dangle3-target", "dangle3-target/final"},
				},
				{
					name:   "dangling-link-as-intermediate-attempted-relative-breakout",
					create: "subdir-a/subdir-b/dangle4/final",
					expect: []string{"dangle4-target", "dangle4-target/final"},
				},
				{
					name:   "dangling-link-as-intermediate-attempted-relative-breakout-again",
					create: "subdir-a/subdir-b/dangle5/final",
					expect: []string{"dangle5-target", "dangle5-target/final"},
				},
				{
					name:   "dangling-link-itself-absolute",
					create: "subdir-a/subdir-b/dangle6",
					expect: []string{"dangle6-target"},
				},
				{
					name:   "dangling-link-as-intermediate-absolute",
					create: "subdir-a/subdir-b/dangle6/final",
					expect: []string{"dangle6-target", "dangle6-target/final"},
				},
				{
					name:   "dangling-link-as-intermediate-absolute-relative-breakout",
					create: "subdir-a/subdir-b/dangle7/final",
					expect: []string{"dangle7-target", "dangle7-target/final"},
				},
				{
					name:   "parent-parent-final",
					create: "../../final",
					expect: []string{"final"},
				},
				{
					name:   "root-parent-final",
					create: "/../final",
					expect: []string{"final"},
				},
				{
					name:   "root-parent-intermediate-parent-final",
					create: "/../intermediate/../final",
					expect: []string{"final"},
				},
				{
					name:        "basic-no-make-parents",
					create:      "subdir-d",
					makeParents: types.OptionalBoolFalse,
					expect:      []string{"subdir-d"},
				},
				{
					name:        "subdir-no-make-parents",
					create:      "subdir-d/subdir-e/subdir-f",
					makeParents: types.OptionalBoolFalse,
					fail:        true,
				},
				{
					name:        "subdir-make-parents-true",
					create:      "subdir-d/subdir-e/subdir-f",
					makeParents: types.OptionalBoolTrue,
					expect:      []string{"subdir-d", "subdir-d/subdir-e", "subdir-d/subdir-e/subdir-f"},
				},
				{
					name:        "nested-no-make-parents-parents-exist",
					create:      "subdir-a/subdir-b/subdir-d",
					makeParents: types.OptionalBoolFalse,
					expect:      []string{"subdir-a/subdir-b/subdir-d"},
				},
				{
					name:        "existing-no-make-parents",
					create:      "subdir-a",
					makeParents: types.OptionalBoolFalse,
				},
			},
		},
	}
	for i := range testArchives {
		t.Run(testArchives[i].name, func(t *testing.T) {
			for _, testCase := range testArchives[i].testCases {
				t.Run(testCase.name, func(t *testing.T) {
					dir, err := makeContextFromArchive(t, makeArchive(testArchives[i].headers, nil), "")
					require.NoErrorf(t, err, "error creating context from archive %q, topdir=%q", testArchives[i].name, "")
					root := dir
					options := MkdirOptions{
						ChownNew:    &idtools.IDPair{UID: os.Getuid(), GID: os.Getgid()},
						MakeParents: testCase.makeParents,
					}
					var beforeNames, afterNames []string
					err = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						rel, err := filepath.Rel(dir, path)
						if err != nil {
							return err
						}
						beforeNames = append(beforeNames, rel)
						return nil
					})
					require.NoErrorf(t, err, "error walking directory to catalog pre-Mkdir contents: %v", err)
					err = Mkdir(root, testCase.create, options)
					if testCase.fail {
						require.Errorf(t, err, "expected error creating directory %q under %q with Mkdir", testCase.create, root)
						return
					}
					require.NoErrorf(t, err, "error creating directory %q under %q with Mkdir: %v", testCase.create, root, err)
					err = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						rel, err := filepath.Rel(dir, path)
						if err != nil {
							return err
						}
						afterNames = append(afterNames, rel)
						return nil
					})
					require.NoErrorf(t, err, "error walking directory to catalog post-Mkdir contents: %v", err)
					expected := slices.Clone(beforeNames)
					for _, expect := range testCase.expect {
						expected = append(expected, filepath.FromSlash(expect))
					}
					sort.Strings(expected)
					sort.Strings(afterNames)
					assert.Equal(t, expected, afterNames, "expected different paths")
				})
			}
		})
	}
}

func TestMkfileNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testMkfile(t)
	canChroot = couldChroot
}

func testMkfile(t *testing.T) {
	currentOwner := &idtools.IDPair{UID: os.Getuid(), GID: os.Getgid()}
	mkfileOpts := func(chmod *os.FileMode, modTime *time.Time) MkfileOptions {
		return MkfileOptions{
			ModTimeNew: modTime,
			ChownNew:   currentOwner,
			ChmodNew:   chmod,
		}
	}

	t.Run("basic", func(t *testing.T) {
		root := t.TempDir()
		content := []byte("hello world")
		err := Mkfile(root, "newfile", mkfileOpts(nil, nil), content)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(root, "newfile"))
		require.NoError(t, err)
		assert.Equal(t, content, got)

		info, err := os.Stat(filepath.Join(root, "newfile"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "default permissions should be 0644")
	})

	t.Run("empty-content", func(t *testing.T) {
		root := t.TempDir()
		err := Mkfile(root, "emptyfile", mkfileOpts(nil, nil), []byte{})
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(root, "emptyfile"))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("with-permissions", func(t *testing.T) {
		root := t.TempDir()
		mode := os.FileMode(0o600)
		err := Mkfile(root, "secret", mkfileOpts(&mode, nil), []byte("secret"))
		require.NoError(t, err)

		info, err := os.Stat(filepath.Join(root, "secret"))
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode().Perm())
	})

	t.Run("with-timestamp", func(t *testing.T) {
		root := t.TempDir()
		err := Mkfile(root, "dated", mkfileOpts(nil, &testDate), []byte("dated"))
		require.NoError(t, err)

		info, err := os.Stat(filepath.Join(root, "dated"))
		require.NoError(t, err)
		assert.Equal(t, testDate.Unix(), info.ModTime().Unix())
	})

	t.Run("nested-path", func(t *testing.T) {
		archive := makeArchive([]tar.Header{
			{Name: "subdir-a", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
			{Name: "subdir-a/subdir-b", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
		}, nil)
		root, err := makeContextFromArchive(t, archive, "")
		require.NoError(t, err)

		content := []byte("very deep content")
		err = Mkfile(root, "subdir-a/subdir-b/deepfile", mkfileOpts(nil, nil), content)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(root, "subdir-a", "subdir-b", "deepfile"))
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("path-traversal", func(t *testing.T) {
		root := t.TempDir()
		content := []byte("should be contained")
		err := Mkfile(root, "../../escaped", mkfileOpts(nil, nil), content)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(root, "escaped"))
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("absolute-path", func(t *testing.T) {
		root := t.TempDir()
		content := []byte("absolute")
		err := Mkfile(root, "/absolutefile", mkfileOpts(nil, nil), content)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(root, "absolutefile"))
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("overwrite", func(t *testing.T) {
		root := t.TempDir()
		err := Mkfile(root, "target", mkfileOpts(nil, nil), []byte("original"))
		require.NoError(t, err)

		replacement := []byte("replacement")
		err = Mkfile(root, "target", mkfileOpts(nil, nil), replacement)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(root, "target"))
		require.NoError(t, err)
		assert.Equal(t, replacement, got)
	})
}

func TestCleanerSubdirectory(t *testing.T) {
	testCases := [][2]string{
		{".", "."},
		{"..", "."},
		{"/", "."},
		{"directory/subdirectory/..", "directory"},
		{"directory/../..", "."},
		{"../../directory", "directory"},
		{"../directory/subdirectory", "directory/subdirectory"},
		{"/directory/../..", "."},
		{"/directory/../../directory", "directory"},
	}
	for _, testCase := range testCases {
		t.Run(testCase[0], func(t *testing.T) {
			cleaner := cleanerReldirectory(filepath.FromSlash(testCase[0]))
			assert.Equal(t, testCase[1], filepath.ToSlash(cleaner), "expected to get %q, got %q", testCase[1], cleaner)
		})
	}
}

func TestHandleRename(t *testing.T) {
	renames := map[string]string{
		"a":   "b",
		"c":   "d",
		"a/1": "a/2",
	}
	testCases := [][2]string{
		{"a", "b"},
		{"a/1", "a/2"},
		{"a/1/2", "a/2/2"},
		{"a/1/2/3", "a/2/2/3"},
		{"a/2/3/4", "b/2/3/4"},
		{"a/2/3", "b/2/3"},
		{"a/2", "b/2"},
		{"c/2", "d/2"},
	}
	for i, testCase := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			renamed := handleRename(renames, testCase[0])
			assert.Equal(t, testCase[1], renamed, "expected to get %q, got %q", testCase[1], renamed)
		})
	}
}

func TestRemoveNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testRemove(t)
	canChroot = couldChroot
}

func testRemove(t *testing.T) {
	type testCase struct {
		name          string
		remove        string
		all           bool
		allowNotFound bool
		allowWildcard bool
		fail          bool
		removed       []string
	}
	testArchives := []struct {
		name      string
		headers   []tar.Header
		testCases []testCase
	}{
		{
			name: "regular",
			headers: []tar.Header{
				{Name: "subdir-a", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/file-a", Typeflag: tar.TypeReg, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/file-b", Typeflag: tar.TypeReg, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-b", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-b/subdir-c", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-b/subdir-c/parent", Typeflag: tar.TypeSymlink, Linkname: "..", ModTime: testDate},
				{Name: "subdir-a/subdir-b/subdir-c/link-b", Typeflag: tar.TypeSymlink, Linkname: "../../file-b", ModTime: testDate},
				{Name: "subdir-a/subdir-b/subdir-c/root", Typeflag: tar.TypeSymlink, Linkname: "/", ModTime: testDate},
				{Name: "subdir-a/subdir-d", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-e", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
				{Name: "subdir-a/subdir-e/subdir-f", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
			},
			testCases: []testCase{
				{
					name:    "file",
					remove:  "subdir-a/file-a",
					removed: []string{"subdir-a/file-a"},
				},
				{
					name:          "file-allow-not-found",
					remove:        "subdir-a/file-a",
					allowNotFound: true,
					removed:       []string{"subdir-a/file-a"},
				},
				{
					name:    "file-all",
					remove:  "subdir-a/file-a",
					all:     true,
					removed: []string{"subdir-a/file-a"},
				},
				{
					name:   "missing-file",
					remove: "subdir-a/file-missing",
					fail:   true,
				},
				{
					name:          "missing-file-allow-not-found",
					remove:        "subdir-a/file-missing",
					allowNotFound: true,
				},
				{
					name:   "missing-file-all",
					remove: "subdir-a/file-missing",
					all:    true,
				},
				{
					name:          "missing-directory-allow-not-found",
					remove:        "subdir-a/subdir-missing",
					allowNotFound: true,
				},
				{
					name:   "subdir",
					remove: "subdir-a/subdir-b",
					all:    false,
					fail:   true,
				},
				{
					name:          "subdir-allow-not-found",
					remove:        "subdir-a/subdir-b",
					allowNotFound: true,
					all:           false,
					fail:          true,
				},
				{
					name:   "subdir-all",
					remove: "subdir-a/subdir-b/subdir-c",
					all:    true,
					removed: []string{
						"subdir-a/subdir-b/subdir-c",
						"subdir-a/subdir-b/subdir-c/parent",
						"subdir-a/subdir-b/subdir-c/link-b",
						"subdir-a/subdir-b/subdir-c/root",
					},
				},
				{
					name:    "file-link",
					remove:  "subdir-a/subdir-b/subdir-c/link-b",
					removed: []string{"subdir-a/subdir-b/subdir-c/link-b"},
				},
				{
					name:    "file-link-all",
					remove:  "subdir-a/subdir-b/subdir-c/link-b",
					all:     true,
					removed: []string{"subdir-a/subdir-b/subdir-c/link-b"},
				},
				{
					name:    "file-link-indirect",
					remove:  "subdir-a/subdir-b/subdir-c/parent/subdir-c/link-b",
					removed: []string{"subdir-a/subdir-b/subdir-c/link-b"},
				},
				{
					name:    "file-link-indirect-all",
					remove:  "subdir-a/subdir-b/subdir-c/parent/subdir-c/link-b",
					all:     true,
					removed: []string{"subdir-a/subdir-b/subdir-c/link-b"},
				},
				{
					name:    "dir-link",
					remove:  "subdir-a/subdir-b/subdir-c/root",
					removed: []string{"subdir-a/subdir-b/subdir-c/root"},
				},
				{
					name:    "dir-link-all",
					remove:  "subdir-a/subdir-b/subdir-c/root",
					all:     true,
					removed: []string{"subdir-a/subdir-b/subdir-c/root"},
				},
				{
					name:    "dir-through-link",
					remove:  "subdir-a/subdir-b/subdir-c/root/subdir-a/subdir-d",
					removed: []string{"subdir-a/subdir-d"},
				},
				{
					name:    "dir-through-link-all",
					remove:  "subdir-a/subdir-b/subdir-c/root/subdir-a/subdir-d",
					all:     true,
					removed: []string{"subdir-a/subdir-d"},
				},
				{
					name:   "tree-through-link",
					remove: "subdir-a/subdir-b/subdir-c/root/subdir-a/subdir-e",
					all:    false,
					fail:   true,
				},
				{
					name:    "tree-through-link-all",
					remove:  "subdir-a/subdir-b/subdir-c/root/subdir-a/subdir-e",
					all:     true,
					removed: []string{"subdir-a/subdir-e", "subdir-a/subdir-e/subdir-f"},
				},
				{
					name:          "wildcard-files",
					remove:        "subdir-a/file-*",
					allowWildcard: true,
					removed:       []string{"subdir-a/file-a", "subdir-a/file-b"},
				},
				{
					name:          "wildcard-all-in-dir",
					remove:        "subdir-a/subdir-b/*",
					allowWildcard: true,
					all:           true,
					removed: []string{
						"subdir-a/subdir-b/subdir-c",
						"subdir-a/subdir-b/subdir-c/parent",
						"subdir-a/subdir-b/subdir-c/link-b",
						"subdir-a/subdir-b/subdir-c/root",
					},
				},
				{
					name:          "wildcard-no-match",
					remove:        "subdir-a/nonexistent-*",
					allowWildcard: true,
				},
				{
					name:          "wildcard-allow-not-found",
					remove:        "subdir-a/file-*",
					allowWildcard: true,
					allowNotFound: true,
					removed:       []string{"subdir-a/file-a", "subdir-a/file-b"},
				},
				{
					name:          "wildcard-dir-without-all",
					remove:        "subdir-a/subdir-*",
					allowWildcard: true,
					all:           false,
					fail:          true,
				},
				{
					name:          "wildcard-empty-dir",
					remove:        "subdir-a/subdir-d",
					allowWildcard: true,
					all:           false,
					removed:       []string{"subdir-a/subdir-d"},
				},
				{
					name:          "wildcard-literal-path",
					remove:        "subdir-a/file-a",
					allowWildcard: true,
					removed:       []string{"subdir-a/file-a"},
				},
				{
					name:          "wildcard-literal-missing",
					remove:        "subdir-a/file-nonexistent",
					allowWildcard: true,
				},
				{
					name:          "wildcard-char-class",
					remove:        "subdir-a/file-[ab]",
					allowWildcard: true,
					removed:       []string{"subdir-a/file-a", "subdir-a/file-b"},
				},
				{
					name:          "wildcard-question-mark",
					remove:        "subdir-a/file-?",
					allowWildcard: true,
					removed:       []string{"subdir-a/file-a", "subdir-a/file-b"},
				},
			},
		},
	}
	for i := range testArchives {
		t.Run(testArchives[i].name, func(t *testing.T) {
			for _, testCase := range testArchives[i].testCases {
				t.Run(testCase.name, func(t *testing.T) {
					dir, err := makeContextFromArchive(t, makeArchive(testArchives[i].headers, nil), "")
					require.NoErrorf(t, err, "error creating context from archive %q, topdir=%q", testArchives[i].name, "")
					root := dir
					options := RemoveOptions{All: testCase.all, AllowNotFound: testCase.allowNotFound, AllowWildcard: testCase.allowWildcard}
					beforeNames := make(map[string]struct{})
					err = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						rel, err := filepath.Rel(dir, path)
						if err != nil {
							return err
						}
						beforeNames[rel] = struct{}{}
						return nil
					})
					require.NoErrorf(t, err, "error walking directory to catalog pre-Remove contents: %v", err)
					err = Remove(root, testCase.remove, options)
					if testCase.fail {
						require.Errorf(t, err, "did not expect to succeed removing item %q under %q with Remove", testCase.remove, root)
						return
					}
					require.NoErrorf(t, err, "error removing item %q under %q with Remove: %v", testCase.remove, root, err)
					afterNames := make(map[string]struct{})
					err = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						rel, err := filepath.Rel(dir, path)
						if err != nil {
							return err
						}
						afterNames[rel] = struct{}{}
						return nil
					})
					require.NoErrorf(t, err, "error walking directory to catalog post-Remove contents: %v", err)
					var removed []string
					for beforeName := range beforeNames {
						if _, stillPresent := afterNames[beforeName]; !stillPresent {
							removed = append(removed, beforeName)
						}
					}
					var expected []string
					for _, expect := range testCase.removed {
						expected = append(expected, filepath.FromSlash(expect))
					}
					sort.Strings(expected)
					sort.Strings(removed)
					assert.Equal(t, expected, removed, "expected different paths to be missing")
				})
			}
		})
	}
}

func TestExtendedGlob(t *testing.T) {
	tmpdir := t.TempDir()
	buf := []byte("buffer")
	var expected1, expected2 []string
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "a"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "a", "b"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tmpdir, "a", "b", "a.dat"), buf, 0o600))
	expected1 = append(expected1, filepath.Join(tmpdir, "a", "b", "a.dat"))
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "b"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "b", "c"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "c"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "c", "d"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tmpdir, "c", "d", "c.dat"), buf, 0o600))
	expected1 = append(expected1, filepath.Join(tmpdir, "c", "d", "c.dat"))
	expected2 = append(expected2, filepath.Join(tmpdir, "c", "d", "c.dat"))
	require.NoError(t, os.Mkdir(filepath.Join(tmpdir, "d"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tmpdir, "d", "d.dat"), buf, 0o600))
	expected1 = append(expected1, filepath.Join(tmpdir, "d", "d.dat"))
	expected2 = append(expected2, filepath.Join(tmpdir, "d", "d.dat"))
	matched, err := extendedGlob(filepath.Join(tmpdir, "**", "*.dat"))
	require.NoError(t, err, "globbing")
	require.ElementsMatchf(t, expected1, matched, "**/*.dat")
	matched, err = extendedGlob(filepath.Join(tmpdir, "**", "d", "*.dat"))
	require.NoError(t, err, "globbing")
	require.ElementsMatch(t, expected2, matched, "**/d/*.dat")
	matched, err = extendedGlob(filepath.Join(tmpdir, "**", "**", "d", "*.dat"))
	require.NoError(t, err, "globbing")
	require.ElementsMatch(t, expected2, matched, "**/**/d/*.dat")
	matched, err = extendedGlob(filepath.Join(tmpdir, "**", "d", "**", "*.dat"))
	require.NoError(t, err, "globbing")
	require.ElementsMatch(t, expected2, matched, "**/d/**/*.dat")
}

func testEnsure(t *testing.T) {
	zero := time.Unix(0, 0)
	worldReadable := os.FileMode(0o644)
	ugReadable := os.FileMode(0o750)

	testCases := []struct {
		description   string
		subdir        string
		mkdirs        []string
		options       EnsureOptions
		expectCreated []string
		expectNoted   []EnsureParentPath
	}{
		{
			description: "base",
			subdir:      "subdir",
			options: EnsureOptions{
				Paths: []EnsurePath{
					{
						Path:     filepath.Join(string(os.PathSeparator), "a", "b", "a"),
						Typeflag: tar.TypeReg,
						Chmod:    &worldReadable,
					},
					{
						Path:     filepath.Join("a", "b", "b"),
						Typeflag: tar.TypeReg,
						ModTime:  &zero,
					},
					{
						Path:     filepath.Join(string(os.PathSeparator), "a", "b", "c"),
						Typeflag: tar.TypeDir,
						ModTime:  &zero,
					},
					{
						Path:     filepath.Join("a", "b", "d"),
						Typeflag: tar.TypeDir,
					},
				},
			},
			expectCreated: []string{
				"subdir",
				"subdir/a",
				"subdir/a/b",
				"subdir/a/b/a",
				"subdir/a/b/b",
				"subdir/a/b/c",
				"subdir/a/b/d",
			},
			expectNoted: []EnsureParentPath{},
		},
		{
			description: "nosubdir",
			options: EnsureOptions{
				Paths: []EnsurePath{
					{
						Path:     filepath.Join(string(os.PathSeparator), "a", "b", "c"),
						Typeflag: tar.TypeDir,
						ModTime:  &zero,
					},
					{
						Path:     filepath.Join("a", "b", "d"),
						Typeflag: tar.TypeDir,
					},
				},
			},
			expectCreated: []string{
				"a",
				"a/b",
				"a/b/c",
				"a/b/d",
			},
			expectNoted: []EnsureParentPath{},
		},
		{
			description: "mkdir-first",
			subdir:      "dir/subdir",
			mkdirs:      []string{"dir", "dir/subdir"},
			options: EnsureOptions{
				Paths: []EnsurePath{
					{
						Path:     filepath.Join(string(os.PathSeparator), "a", "b", "a"),
						Typeflag: tar.TypeReg,
						Chmod:    &worldReadable,
					},
					{
						Path:     filepath.Join("a", "b", "b"),
						Typeflag: tar.TypeReg,
						ModTime:  &zero,
					},
					{
						Path:     filepath.Join(string(os.PathSeparator), "a", "b", "c"),
						Typeflag: tar.TypeDir,
						ModTime:  &zero,
					},
					{
						Path:     filepath.Join("a", "b", "d"),
						Typeflag: tar.TypeDir,
					},
				},
			},
			expectCreated: []string{
				"dir/subdir/a",
				"dir/subdir/a/b",
				"dir/subdir/a/b/a",
				"dir/subdir/a/b/b",
				"dir/subdir/a/b/c",
				"dir/subdir/a/b/d",
			},
			expectNoted: []EnsureParentPath{
				{
					Path:  "dir",
					Mode:  &ugReadable,
					Owner: &idtools.IDPair{UID: 1, GID: 1},
					// ModTime gets updated when we create dir/subdir, can't check it
				},
				{
					Path:    "dir/subdir",
					Mode:    &ugReadable,
					Owner:   &idtools.IDPair{UID: 1, GID: 1},
					ModTime: &zero,
				},
			},
		},
	}
	for i := range testCases {
		t.Run(testCases[i].description, func(t *testing.T) {
			testStarted := time.Now()
			tmpdir := t.TempDir()
			for _, mkdir := range testCases[i].mkdirs {
				err := Mkdir(tmpdir, mkdir, MkdirOptions{
					ModTimeNew: &zero,
					ChmodNew:   &ugReadable,
					ChownNew:   &idtools.IDPair{UID: 1, GID: 1},
				})
				require.NoError(t, err, "unexpected error ensuring")
			}
			created, noted, err := Ensure(tmpdir, testCases[i].subdir, testCases[i].options)
			require.NoError(t, err, "unexpected error ensuring")
			require.EqualValues(t, testCases[i].expectCreated, created, "did not expect to create these")
			require.Equal(t, len(testCases[i].expectNoted), len(noted), "noticed the wrong number of things")
			for n := range noted {
				require.Equalf(t, testCases[i].expectNoted[n].Path, noted[n].Path, "noticed item %d path", n)
				if testCases[i].expectNoted[n].Mode != nil {
					require.Equalf(t, testCases[i].expectNoted[n].Mode.Perm(), noted[n].Mode.Perm(), "noticed item %q mode", noted[n].Path)
				}
				if testCases[i].expectNoted[n].Owner != nil {
					require.Equalf(t, *testCases[i].expectNoted[n].Owner, *noted[n].Owner, "noticed item %q owner", noted[n].Path)
				}
				if testCases[i].expectNoted[n].ModTime != nil {
					require.Equalf(t, testCases[i].expectNoted[n].ModTime.UnixNano(), noted[n].ModTime.UnixNano(), "noticed item %q mtime", noted[n].Path)
				}
			}
			for _, item := range testCases[i].options.Paths {
				target := filepath.Join(tmpdir, testCases[i].subdir, item.Path)
				st, err := os.Stat(target)
				require.NoError(t, err, "we supposedly created %q", item.Path)
				if item.Chmod != nil {
					assert.Equalf(t, *item.Chmod, st.Mode().Perm(), "permissions look wrong on %q", item.Path)
				}
				if item.Chown != nil {
					uid, gid, err := owner(st)
					require.NoErrorf(t, err, "expected to be able to read uid:gid for %q", item.Path)
					assert.Equalf(t, item.Chown.UID, uid, "user looks wrong on %q", item.Path)
					assert.Equalf(t, item.Chown.GID, gid, "group looks wrong on %q", item.Path)
				}
				if item.ModTime != nil {
					assert.Equalf(t, item.ModTime.Unix(), st.ModTime().Unix(), "datestamp looks wrong on %q", item.Path)
				} else {
					assert.True(t, !testStarted.After(st.ModTime()), "datestamp is too old on %q: %v < %v", st.ModTime(), testStarted)
				}
			}
		})
	}
}

func TestStatDisallowWildcardNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testStatDisallowWildcard(t)
	canChroot = couldChroot
}

func testStatDisallowWildcard(t *testing.T) {
	headers := []tar.Header{
		{Name: "file-a", Typeflag: tar.TypeReg, Size: 23, Mode: 0o644, ModTime: testDate},
		{Name: "file-b", Typeflag: tar.TypeReg, Size: 45, Mode: 0o644, ModTime: testDate},
		{Name: "subdir", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
		{Name: "subdir/file-c", Typeflag: tar.TypeReg, Size: 67, Mode: 0o644, ModTime: testDate},
	}
	dir, err := makeContextFromArchive(t, makeArchive(headers, nil), "")
	require.NoError(t, err)

	t.Run("wildcard-allowed-glob-matches", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{}, []string{"file-*"})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		require.Empty(t, stats[0].Error)
		require.Len(t, stats[0].Globbed, 2)
	})

	t.Run("disallow-wildcard-literal", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{DisallowWildcard: true}, []string{"file-a"})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		require.Empty(t, stats[0].Error)
		require.Len(t, stats[0].Globbed, 1)
	})

	t.Run("disallow-wildcard-glob-chars-rejected", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{DisallowWildcard: true}, []string{"file-*"})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		require.NotEmpty(t, stats[0].Error, "file-* should be rejected when DisallowWildcard is true")
		require.Contains(t, stats[0].Error, "wildcards are not allowed")
	})
}

func TestStatAllowEmptyWildcardNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testStatAllowEmptyWildcard(t)
	canChroot = couldChroot
}

func testStatAllowEmptyWildcard(t *testing.T) {
	headers := []tar.Header{
		{Name: "file-a", Typeflag: tar.TypeReg, Size: 23, Mode: 0o644, ModTime: testDate},
	}
	dir, err := makeContextFromArchive(t, makeArchive(headers, nil), "")
	require.NoError(t, err)

	t.Run("empty-wildcard-true-no-error", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{AllowEmptyWildcard: true}, []string{"nonexistent-*"})
		require.NoError(t, err)
		require.Empty(t, stats)
	})

	t.Run("empty-wildcard-false-error", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{}, []string{"nonexistent-*"})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		require.Contains(t, stats[0].Error, "matched nothing")
	})

	t.Run("empty-wildcard-true-with-existing", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{AllowEmptyWildcard: true}, []string{"file-*"})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		require.Empty(t, stats[0].Error)
		require.Len(t, stats[0].Globbed, 1)
	})

	t.Run("empty-wildcard-true-literal-missing", func(t *testing.T) {
		stats, err := Stat(dir, dir, StatOptions{AllowEmptyWildcard: true}, []string{"nonexistent-file"})
		require.NoError(t, err)
		require.Len(t, stats, 1)
		require.Contains(t, stats[0].Error, "no such file or directory")
	})
}

func TestGetDisallowWildcardNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testGetDisallowWildcard(t)
	canChroot = couldChroot
}

func testGetDisallowWildcard(t *testing.T) {
	headers := []tar.Header{
		{Name: "file-a", Typeflag: tar.TypeReg, Size: 23, Mode: 0o644, ModTime: testDate},
		{Name: "file-b", Typeflag: tar.TypeReg, Size: 45, Mode: 0o644, ModTime: testDate},
	}
	dir, err := makeContextFromArchive(t, makeArchive(headers, nil), "")
	require.NoError(t, err)

	t.Run("wildcard-allowed-glob-matches", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{}, []string{"file-*"}, io.Discard)
		require.NoError(t, err)
	})

	t.Run("disallow-wildcard-literal", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{DisallowWildcard: true}, []string{"file-a"}, io.Discard)
		require.NoError(t, err)
	})

	t.Run("disallow-wildcard-glob-chars-rejected", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{DisallowWildcard: true}, []string{"file-*"}, io.Discard)
		require.Error(t, err)
		require.Contains(t, err.Error(), "wildcards are not allowed")
	})
}

func TestGetAllowEmptyWildcardNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testGetAllowEmptyWildcard(t)
	canChroot = couldChroot
}

func testGetAllowEmptyWildcard(t *testing.T) {
	headers := []tar.Header{
		{Name: "file-a", Typeflag: tar.TypeReg, Size: 23, Mode: 0o644, ModTime: testDate},
	}
	dir, err := makeContextFromArchive(t, makeArchive(headers, nil), "")
	require.NoError(t, err)

	t.Run("empty-wildcard-true-no-error", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{AllowEmptyWildcard: true}, []string{"nonexistent-*"}, io.Discard)
		require.NoError(t, err)
	})

	t.Run("empty-wildcard-false-error", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{}, []string{"nonexistent-*"}, io.Discard)
		require.Error(t, err)
		require.Contains(t, err.Error(), "matched nothing")
	})

	t.Run("empty-wildcard-true-literal-missing", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{AllowEmptyWildcard: true}, []string{"nonexistent-file"}, io.Discard)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no such file or directory")
	})

	t.Run("empty-wildcard-true-with-existing", func(t *testing.T) {
		err := Get(dir, dir, GetOptions{AllowEmptyWildcard: true}, []string{"file-*"}, io.Discard)
		require.NoError(t, err)
	})
}

func TestEnsureNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testEnsure(t)
	canChroot = couldChroot
}

func testConditionalRemove(t *testing.T) {
	mode, mismatchedMode := os.FileMode(0o751), os.FileMode(0o755)
	now := time.Now()
	then := time.Unix(now.Unix()/2, 0)
	type create struct {
		path     string
		typeFlag byte
		mtime    *time.Time
		mode     *os.FileMode
	}
	testCases := []struct {
		description     string
		subdir          string
		create          []create
		remove          ConditionalRemoveOptions
		expectedRemoved []string
		expectedRemain  []string
	}{
		{
			description: "withoutsubdir",
			create: []create{
				{path: "/a", typeFlag: tar.TypeDir},
				{path: "b", typeFlag: tar.TypeReg},
				{path: "c/d", typeFlag: tar.TypeReg},
				{path: "c/e", typeFlag: tar.TypeReg},
			},
			remove: ConditionalRemoveOptions{
				Paths: []ConditionalRemovePath{
					{Path: "a"},
					{Path: "b"},
					{Path: "c"},
					{Path: "c/e"},
				},
			},
			expectedRemoved: []string{"a", "b", "c/e"},
			expectedRemain:  []string{"c/d", "c"},
		},
		{
			description: "withsubdir",
			subdir:      "subdir",
			create: []create{
				{path: "/a", typeFlag: tar.TypeDir},
				{path: "b", typeFlag: tar.TypeReg},
				{path: "c/d", typeFlag: tar.TypeReg},
				{path: "c/e", typeFlag: tar.TypeReg},
			},
			remove: ConditionalRemoveOptions{
				Paths: []ConditionalRemovePath{
					{Path: "a"},
					{Path: "b"},
					{Path: "c"},
					{Path: "c/e"},
				},
			},
			expectedRemoved: []string{"a", "b", "c/e"},
			expectedRemain:  []string{"c/d", "c"},
		},
		{
			description: "withsubdir",
			subdir:      "subdir",
			create: []create{
				{path: "/a", typeFlag: tar.TypeDir},
				{path: "b", typeFlag: tar.TypeReg},
				{path: "c/d", typeFlag: tar.TypeReg},
				{path: "c/e", typeFlag: tar.TypeReg},
			},
			remove: ConditionalRemoveOptions{
				Paths: []ConditionalRemovePath{
					{Path: "a"},
					{Path: "b"},
					{Path: "c"},
					{Path: "c/e"},
				},
			},
			expectedRemoved: []string{"a", "b", "c/e"},
			expectedRemain:  []string{"c/d", "c"},
		},
		{
			description: "unconditional",
			create: []create{
				{path: "/a", typeFlag: tar.TypeDir, mtime: &then, mode: &mode},
				{path: "b", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
				{path: "c/d", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
				{path: "c/e", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
			},
			remove: ConditionalRemoveOptions{
				Paths: []ConditionalRemovePath{
					{Path: "a"},
					{Path: "b"},
					{Path: "c"},
					{Path: "c/e"},
				},
			},
			expectedRemoved: []string{"a", "b", "c/e"},
			expectedRemain:  []string{"c/d", "c"},
		},
		{
			description: "conditions-not-met",
			create: []create{
				{path: "/a", typeFlag: tar.TypeDir, mtime: &then, mode: &mode},
				{path: "b", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
				{path: "c/d", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
				{path: "c/e", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
			},
			remove: ConditionalRemoveOptions{
				Paths: []ConditionalRemovePath{
					{Path: "a", Mode: &mismatchedMode},
					{Path: "b", Mode: &mismatchedMode},
					{Path: "c", Mode: &mismatchedMode},
					{Path: "c/e", Mode: &mismatchedMode},
					{Path: "a", ModTime: &now},
					{Path: "b", ModTime: &now},
					{Path: "c", ModTime: &now},
					{Path: "c/e", ModTime: &now},
				},
			},
			expectedRemain: []string{"a", "b", "c/e", "c/d", "c"},
		},
		{
			description: "conditions-met",
			create: []create{
				{path: "/a", typeFlag: tar.TypeDir, mtime: &then, mode: &mode},
				{path: "b", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
				{path: "c/d", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
				{path: "c/e", typeFlag: tar.TypeReg, mtime: &then, mode: &mode},
			},
			remove: ConditionalRemoveOptions{
				Paths: []ConditionalRemovePath{
					{Path: "a", ModTime: &then, Mode: &mode},
					{Path: "b", ModTime: &then, Mode: &mode},
					{Path: "c"},
					{Path: "c/d", ModTime: &then, Mode: &mode},
				},
			},
			expectedRemoved: []string{"a", "b", "c/d"},
			expectedRemain:  []string{"c", "c/e"},
		},
	}
	for i := range testCases {
		t.Run(testCases[i].description, func(t *testing.T) {
			tmpdir := t.TempDir()
			var create EnsureOptions
			for _, what := range testCases[i].create {
				create.Paths = append(create.Paths, EnsurePath{
					Path:     what.path,
					Typeflag: what.typeFlag,
					ModTime:  what.mtime,
					Chmod:    what.mode,
				})
			}
			created, _, err := Ensure(tmpdir, testCases[i].subdir, create)
			require.NoErrorf(t, err, "unexpected error creating %#v", create)
			remove := testCases[i].remove
			for _, what := range created {
				remove.Paths = append(remove.Paths, ConditionalRemovePath{
					Path: what,
				})
			}
			removed, err := ConditionalRemove(tmpdir, testCases[i].subdir, testCases[i].remove)
			require.NoError(t, err, "unexpected error removing")
			expectedRemoved := slices.Clone(testCases[i].expectedRemoved)
			slices.Sort(expectedRemoved)
			require.EqualValues(t, expectedRemoved, removed, "did not expect these to be removed")
			var remain []string
			err = filepath.Walk(filepath.Join(tmpdir, testCases[i].subdir), func(path string, _ fs.FileInfo, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(filepath.Join(tmpdir, testCases[i].subdir), path)
				if err != nil {
					return fmt.Errorf("computing path of %q relative to %q: %w", path, filepath.Join(tmpdir, testCases[i].subdir), err)
				}
				if rel != "" && rel == "." {
					return nil
				}
				remain = append(remain, rel)
				return nil
			})
			slices.Sort(remain)
			expectedRemain := slices.Clone(testCases[i].expectedRemain)
			slices.Sort(expectedRemain)
			require.NoError(t, err, "unexpected error checking what's left")
			require.EqualValues(t, expectedRemain, remain, "did not expect these to be left behind")
		})
	}
}

func TestConditionalRemoveNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testConditionalRemove(t)
	canChroot = couldChroot
}

func TestSortedExtendedGlob(t *testing.T) {
	tmpdir := t.TempDir()
	buf := []byte("buffer")
	expect := []string{}
	for _, name := range []string{"z", "y", "x", "a", "b", "c", "d", "e", "f"} {
		require.NoError(t, os.WriteFile(filepath.Join(tmpdir, name), buf, 0o600))
		expect = append(expect, filepath.Join(tmpdir, name))
	}
	sort.Strings(expect)

	matched, err := extendedGlob(filepath.Join(tmpdir, "*"))
	require.NoError(t, err, "globbing")
	require.ElementsMatch(t, expect, matched, "sorted globbing")
}

func TestTarPutNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	defer func() { canChroot = couldChroot }()
	testTarPut(t)
}

func testTarPut(t *testing.T) {
	testCases := []struct {
		name               string
		trailingNullsBytes int
	}{
		{
			name:               "without-trailing-nulls",
			trailingNullsBytes: 0,
		},
		{
			name:               "with-trailing-nulls",
			trailingNullsBytes: 8192,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testDir := t.TempDir()

			var tarBuf bytes.Buffer
			tw := tar.NewWriter(&tarBuf)

			hdr := &tar.Header{
				Name:    "testfile.txt",
				Mode:    0o644,
				Size:    11,
				ModTime: time.Now(),
			}
			err := tw.WriteHeader(hdr)
			require.NoError(t, err, "failed to write tar header")

			_, err = tw.Write([]byte("hello world"))
			require.NoError(t, err, "failed to write tar content")

			err = tw.Close()
			require.NoError(t, err, "failed to close tar writer")

			// Add extra trailing null bytes to simulate what many tar implementations do.
			// This reproduces issue https://github.com/containers/buildah/issues/6573 where
			// tar.Reader returns EOF after the standard EOF marker but before all trailing nulls
			// are consumed, causing broken pipe.
			if tc.trailingNullsBytes > 0 {
				extraNulls := make([]byte, tc.trailingNullsBytes)
				tarBuf.Write(extraNulls)
			}

			err = Put(testDir, testDir, PutOptions{}, &tarBuf)
			require.NoError(t, err, "Put returned error")

			extractedFile := filepath.Join(testDir, "testfile.txt")
			content, err := os.ReadFile(extractedFile)
			require.NoError(t, err, "failed to read extracted file")
			require.Equal(t, "hello world", string(content), "extracted file content mismatch")
		})
	}
}

func TestCannotChangeMultipleRequestsWithDifferentChroot(t *testing.T) {
	testDir := t.TempDir()

	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, stdinR.Close())
		assert.NoError(t, stdinW.Close())
	})

	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, stdoutR.Close())
		assert.NoError(t, stdoutW.Close())
	})

	stderr := bytes.Buffer{}

	cmd := reexec.Command(copierCommand)
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = &stderr
	cmd.Dir = testDir

	require.NoError(t, cmd.Start())

	encoder := json.NewEncoder(stdinW)
	decoder := json.NewDecoder(stdoutR)

	req := request{Request: requestEval, Root: testDir, Directory: "/"}

	receiveResponse := func() response {
		var resp response
		require.NoError(t, stdoutR.SetReadDeadline(time.Now().Add(time.Second*5)))
		if !assert.NoError(t, decoder.Decode(&resp), "failed to decode response from copier") {
			_ = cmd.Wait()
			t.Logf("stderr: %s", stderr.String())
			t.FailNow()
		}
		return resp
	}

	require.NoError(t, encoder.Encode(&req), "failed to send first request to copier")
	resp := receiveResponse()
	require.Empty(t, resp.Error, "first request returned an error: %s", resp.Error)

	require.NoError(t, encoder.Encode(&req), "failed to send second request to copier")
	resp = receiveResponse()
	require.Empty(t, resp.Error, "second request returned an error: %s", resp.Error)

	require.NoError(t, encoder.Encode(&request{Request: requestQuit}))
	require.NoError(t, cmd.Wait())
}

func TestChmodNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testChmod(t)
	canChroot = couldChroot
}

func testChmod(t *testing.T) {
	dirMode := os.FileMode(0o755)
	fileMode := os.FileMode(0o644)
	cases := []struct {
		chmod                 string
		chmodDirs, chmodFiles *os.FileMode
		startMode, endMode    os.FileMode
		isDir                 bool
		err                   error
	}{
		{chmod: "u=rw", startMode: 0o400, endMode: 0o600},
		{chmod: "u=rwX", startMode: 0o400, endMode: 0o600},
		{chmod: "u=rw", startMode: 0o400 | fs.ModeDir, endMode: 0o600 | fs.ModeDir, isDir: true},
		{chmod: "u=rwX", startMode: 0o400 | fs.ModeDir, endMode: 0o700 | fs.ModeDir, isDir: true},
		{chmod: "u=rwXabc", err: mode.ErrSyntax},
		{chmod: "go=", startMode: 0o777, endMode: 0o700},
		{chmod: "644", startMode: 0o753, endMode: 0o644},
		{chmod: "", startMode: 0o753, endMode: 0o753},
		{chmodDirs: &dirMode, startMode: 0o500 | fs.ModeDir, endMode: 0o755 | fs.ModeDir, isDir: true},
		{chmodDirs: &dirMode, startMode: 0o400, endMode: 0o400},
		{chmodFiles: &fileMode, startMode: 0o500 | fs.ModeDir, endMode: 0o500 | fs.ModeDir, isDir: true},
		{chmodFiles: &fileMode, startMode: 0o400, endMode: 0o644},
		{chmod: "u=rwX,go=", chmodFiles: &fileMode, startMode: 0o444, endMode: 0o600},
		{chmod: "u=rwX,go=", chmodDirs: &dirMode, startMode: 0o555 | fs.ModeDir, endMode: 0o700 | fs.ModeDir, isDir: true},
	}

	name := "test"
	uidMap := []idtools.IDMap{{HostID: os.Getuid(), ContainerID: 0, Size: 1}}
	gidMap := []idtools.IDMap{{HostID: os.Getgid(), ContainerID: 0, Size: 1}}

	for _, v := range cases {
		t.Run(fmt.Sprintf("get chmod=%v,chmodDirs=%v,chmodFiles=%v,startMode=%O,endMode=%O,isDir=%v", v.chmod, v.chmodDirs, v.chmodFiles, v.startMode, v.endMode, v.isDir), func(t *testing.T) {
			testDir := t.TempDir()
			fn := filepath.Join(testDir, name)
			var err error
			if v.isDir {
				err = os.Mkdir(fn, 0)
			} else {
				_, err = os.Create(fn)
			}
			if err != nil {
				t.Fatalf("failed to create %s: %v", name, err)
			}
			if err = os.Chmod(fn, v.startMode); err != nil {
				t.Fatalf("failed to chmod %s: %v", name, err)
			}

			pipeReader, pipeWriter := io.Pipe()
			var wg sync.WaitGroup
			wg.Go(func() {
				opts := GetOptions{Chmod: v.chmod, ChmodDirs: v.chmodDirs, ChmodFiles: v.chmodFiles}
				err = Get(testDir, "", opts, []string{name}, pipeWriter)
				pipeWriter.Close()
			})
			tr := tar.NewReader(pipeReader)
			hdr, tarErr := tr.Next()
			for tarErr == nil {
				assert.Equal(t, int64(v.endMode), hdr.Mode)
				hdr, tarErr = tr.Next()
			}
			assert.ErrorIs(t, tarErr, io.EOF, "expected EOF at end of archive")
			wg.Wait()
			if v.err != nil {
				require.ErrorContains(t, err, v.err.Error())
			}
			pipeReader.Close()
		})

		t.Run(fmt.Sprintf("put chmod=%v,chmodDirs=%v,chmodFiles=%v,startMode=%O,endMode=%O,isDir=%v", v.chmod, v.chmodDirs, v.chmodFiles, v.startMode, v.endMode, v.isDir), func(t *testing.T) {
			testDir := t.TempDir()
			typeFlag := byte(tar.TypeReg)
			if v.isDir {
				typeFlag = tar.TypeDir
			}
			archive := makeArchiveSlice([]tar.Header{
				{Name: name, Typeflag: typeFlag, Size: 0, Mode: int64(v.startMode), ModTime: testDate},
			})
			opts := PutOptions{
				UIDMap:     uidMap,
				GIDMap:     gidMap,
				Chmod:      v.chmod,
				ChmodDirs:  v.chmodDirs,
				ChmodFiles: v.chmodFiles,
			}
			err := Put(testDir, "", opts, bytes.NewReader(archive))
			if err != nil {
				require.ErrorContains(t, err, v.err.Error())
				return
			}
			require.ErrorIs(t, err, v.err)
			fn := filepath.Join(testDir, name)
			fi, err := os.Stat(fn)
			if err != nil {
				t.Fatalf("failed to stat %s: %v", fn, err)
			}
			assert.Equal(t, v.endMode, fi.Mode())
		})
	}
}

func TestPutTimestampNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	defer func() { canChroot = couldChroot }()
	testPutTimestamp(t)
}

func testPutTimestamp(t *testing.T) {
	override := time.Unix(1600000000, 0)
	originalTime := time.Unix(1500000000, 0)
	uidMap := []idtools.IDMap{{HostID: os.Getuid(), ContainerID: 0, Size: 1}}
	gidMap := []idtools.IDMap{{HostID: os.Getgid(), ContainerID: 0, Size: 1}}

	t.Run("overrides-archive-entry-timestamps", func(t *testing.T) {
		root := t.TempDir()
		archive := makeArchive([]tar.Header{
			{Name: "subdir/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: originalTime},
			{Name: "subdir/file.txt", Typeflag: tar.TypeReg, Size: 3, Mode: 0o644, ModTime: originalTime},
		}, map[string][]byte{"subdir/file.txt": []byte("abc")})
		err := Put(root, root, PutOptions{UIDMap: uidMap, GIDMap: gidMap, Timestamp: &override}, archive)
		require.NoError(t, err)

		for _, name := range []string{"subdir", "subdir/file.txt"} {
			info, err := os.Lstat(filepath.Join(root, name))
			require.NoError(t, err)
			assert.Equal(t, override.Unix(), info.ModTime().Unix(), "%q should have overridden timestamp", name)
		}
	})

	t.Run("overrides-created-directory-timestamps", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "dest", "nested")
		archive := makeArchive([]tar.Header{
			{Name: "a/b/file.txt", Typeflag: tar.TypeReg, Size: 2, Mode: 0o644, ModTime: originalTime},
		}, map[string][]byte{"a/b/file.txt": []byte("hi")})
		err := Put(root, target, PutOptions{UIDMap: uidMap, GIDMap: gidMap, Timestamp: &override}, archive)
		require.NoError(t, err)

		for _, name := range []string{"dest", "dest/nested", "dest/nested/a", "dest/nested/a/b", "dest/nested/a/b/file.txt"} {
			info, err := os.Lstat(filepath.Join(root, name))
			require.NoError(t, err)
			assert.Equal(t, override.Unix(), info.ModTime().Unix(), "%q should have overridden timestamp", name)
		}
	})

	t.Run("preserves-original-without-override", func(t *testing.T) {
		root := t.TempDir()
		archive := makeArchive([]tar.Header{
			{Name: "file.txt", Typeflag: tar.TypeReg, Size: 5, Mode: 0o644, ModTime: originalTime},
		}, map[string][]byte{"file.txt": []byte("hello")})
		err := Put(root, root, PutOptions{UIDMap: uidMap, GIDMap: gidMap}, archive)
		require.NoError(t, err)

		info, err := os.Lstat(filepath.Join(root, "file.txt"))
		require.NoError(t, err)
		assert.Equal(t, originalTime.Unix(), info.ModTime().Unix())
	})
}

func TestPutCreateDestPathNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	defer func() { canChroot = couldChroot }()
	testPutCreateDestPath(t)
}

func testPutCreateDestPath(t *testing.T) {
	uidMap := []idtools.IDMap{{HostID: os.Getuid(), ContainerID: 0, Size: 1}}
	gidMap := []idtools.IDMap{{HostID: os.Getgid(), ContainerID: 0, Size: 1}}

	archive := func() io.Reader {
		return bytes.NewReader(makeArchiveSlice([]tar.Header{
			{Name: "file.txt", Typeflag: tar.TypeReg, Size: 5, Mode: 0o644, ModTime: testDate},
		}))
	}

	testCases := []struct {
		name           string
		createDestPath types.OptionalBool
		destExists     bool
		fail           bool
	}{
		{name: "unset-creates-dest", destExists: false},
		{name: "true-creates-dest", createDestPath: types.OptionalBoolTrue, destExists: false},
		{name: "false-missing-dest-fails", createDestPath: types.OptionalBoolFalse, destExists: false, fail: true},
		{name: "false-existing-dest-succeeds", createDestPath: types.OptionalBoolFalse, destExists: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			if tc.destExists {
				require.NoError(t, os.Mkdir(dest, 0o755))
			}

			opts := PutOptions{UIDMap: uidMap, GIDMap: gidMap, CreateDestPath: tc.createDestPath}
			err := Put(root, dest, opts, archive())

			if tc.fail {
				require.ErrorContains(t, err, "does not exist and CreateDestPath is false",
					"expected the CreateDestPath guard to reject a missing destination")
				return
			}
			require.NoError(t, err)

			info, err := os.Stat(dest)
			require.NoError(t, err, "destination should exist")
			require.True(t, info.IsDir(), "destination should be a directory")

			_, err = os.Stat(filepath.Join(dest, "file.txt"))
			require.NoError(t, err, "file should have been written into dest")
		})
	}
}

func TestSymlinkNoChroot(t *testing.T) {
	couldChroot := canChroot
	canChroot = false
	testSymlink(t)
	canChroot = couldChroot
}

func testSymlink(t *testing.T) {
	currentOwner := &idtools.IDPair{UID: os.Getuid(), GID: os.Getgid()}
	symlinkOpts := func(modTime *time.Time) SymlinkOptions {
		return SymlinkOptions{
			ModTimeNew: modTime,
			ChownNew:   currentOwner,
		}
	}

	t.Run("basic", func(t *testing.T) {
		root := t.TempDir()
		err := Symlink(root, "target", "newlink", symlinkOpts(nil))
		require.NoError(t, err)

		info, err := os.Lstat(filepath.Join(root, "newlink"))
		require.NoError(t, err)
		assert.NotEqual(t, info.Mode()&os.ModeSymlink, 0, "should be a symlink")

		got, err := os.Readlink(filepath.Join(root, "newlink"))
		require.NoError(t, err)
		assert.Equal(t, "target", got)
	})

	t.Run("with-timestamp", func(t *testing.T) {
		root := t.TempDir()
		err := Symlink(root, "target", "dated", symlinkOpts(&testDate))
		require.NoError(t, err)

		info, err := os.Lstat(filepath.Join(root, "dated"))
		require.NoError(t, err)
		assert.NotEqual(t, info.Mode()&os.ModeSymlink, 0)

		if !testIgnoreSymlinkDates {
			assert.Equal(t, testDate.Unix(), info.ModTime().Unix())
		}
	})

	t.Run("nested-path", func(t *testing.T) {
		archive := makeArchive([]tar.Header{
			{Name: "subdir-a", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
			{Name: "subdir-a/subdir-b", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: testDate},
		}, nil)
		root, err := makeContextFromArchive(t, archive, "")
		require.NoError(t, err)

		err = Symlink(root, "target", "subdir-a/subdir-b/deeplink", symlinkOpts(nil))
		require.NoError(t, err)

		got, err := os.Readlink(filepath.Join(root, "subdir-a", "subdir-b", "deeplink"))
		require.NoError(t, err)
		assert.Equal(t, "target", got)
	})

	t.Run("path-traversal", func(t *testing.T) {
		root := t.TempDir()
		err := Symlink(root, "target", "../../escaped", symlinkOpts(nil))
		require.NoError(t, err)

		got, err := os.Readlink(filepath.Join(root, "escaped"))
		require.NoError(t, err)
		assert.Equal(t, "target", got)
	})

	t.Run("absolute-path", func(t *testing.T) {
		root := t.TempDir()
		err := Symlink(root, "target", "/absolutelink", symlinkOpts(nil))
		require.NoError(t, err)

		got, err := os.Readlink(filepath.Join(root, "absolutelink"))
		require.NoError(t, err)
		assert.Equal(t, "target", got)
	})

	t.Run("overwrite", func(t *testing.T) {
		root := t.TempDir()
		err := Symlink(root, "original-target", "link", symlinkOpts(nil))
		require.NoError(t, err)

		err = Symlink(root, "new-target", "link", symlinkOpts(nil))
		require.NoError(t, err)

		got, err := os.Readlink(filepath.Join(root, "link"))
		require.NoError(t, err)
		assert.Equal(t, "new-target", got)
	})
}
