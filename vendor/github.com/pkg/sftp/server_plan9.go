package sftp

import (
	"path"
	"path/filepath"
)

func (s *Server) toLocalTarget(p string) string {
	lp := filepath.FromSlash(p)

	if path.IsAbs(p) {
		tmp := lp[1:]

		if filepath.IsAbs(tmp) {
			// If the FromSlash without any starting slashes is absolute,
			// then we have a filepath encoded with a prefix '/'.
			// e.g. "/#s/boot" to "#s/boot"
			return tmp
		}
	}

	return lp
}
