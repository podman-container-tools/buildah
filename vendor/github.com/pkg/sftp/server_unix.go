//go:build !windows && !plan9
// +build !windows,!plan9

package sftp

func (s *Server) toLocalTarget(p string) string {
	return p
}
