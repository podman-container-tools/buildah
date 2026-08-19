package chrootuser

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"sync"
)

var lookupUser, lookupGroup sync.Mutex

type lookupPasswdEntry struct {
	name string
	uid  uint64
	gid  uint64
	home string
}
type lookupGroupEntry struct {
	name string
	gid  uint64
	user string
}

func openChrootedFile(rootdir, path string) (*os.File, error) {
	f, err := os.OpenInRoot(rootdir, path)
	if err != nil {
		// Ignore basically all errors, since we didn't check the exit
		// status when we used to use a subprocess.
		// Return an effectively-empty result, so that the caller will
		// behave as though it read an empty file.
		var pw *os.File
		f, pw, err = os.Pipe()
		if err != nil {
			return nil, err
		}
		pw.Close()
	}
	return f, err
}

func scanWithoutComments(rc *bufio.Scanner) (string, bool) {
	for {
		if !rc.Scan() {
			return "", false
		}
		line := rc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		return line, true
	}
}

func parseNextPasswd(rc *bufio.Scanner) *lookupPasswdEntry {
	if !rc.Scan() {
		return nil
	}
	line := rc.Text()
	fields := strings.Split(line, ":")
	if len(fields) != 7 {
		return nil
	}
	uid, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return nil
	}
	gid, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil {
		return nil
	}
	return &lookupPasswdEntry{
		name: fields[0],
		uid:  uid,
		gid:  gid,
		home: fields[5],
	}
}

func parseNextGroup(rc *bufio.Scanner) *lookupGroupEntry {
	// On FreeBSD, /etc/group may contain comments:
	//   https://man.freebsd.org/cgi/man.cgi?query=group&sektion=5&format=html
	// We need to ignore those lines rather than trying to parse them.
	line, ok := scanWithoutComments(rc)
	if !ok {
		return nil
	}
	fields := strings.Split(line, ":")
	if len(fields) != 4 {
		return nil
	}
	gid, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return nil
	}
	return &lookupGroupEntry{
		name: fields[0],
		gid:  gid,
		user: fields[3],
	}
}

func lookupUserInContainer(rootdir, username string) (uid uint64, gid uint64, err error) {
	f, err := openChrootedFile(rootdir, "etc/passwd")
	if err != nil {
		return 0, 0, err
	}
	rc := bufio.NewScanner(f)
	defer f.Close()

	lookupUser.Lock()
	defer lookupUser.Unlock()

	pwd := parseNextPasswd(rc)
	for pwd != nil {
		if pwd.name != username {
			pwd = parseNextPasswd(rc)
			continue
		}
		return pwd.uid, pwd.gid, nil
	}

	return 0, 0, user.UnknownUserError(fmt.Sprintf("error looking up user %q", username))
}

func lookupGroupForUIDInContainer(rootdir string, userid uint64) (username string, gid uint64, err error) {
	f, err := openChrootedFile(rootdir, "etc/passwd")
	if err != nil {
		return "", 0, err
	}
	rc := bufio.NewScanner(f)
	defer f.Close()

	lookupUser.Lock()
	defer lookupUser.Unlock()

	pwd := parseNextPasswd(rc)
	for pwd != nil {
		if pwd.uid != userid {
			pwd = parseNextPasswd(rc)
			continue
		}
		return pwd.name, pwd.gid, nil
	}

	return "", 0, ErrNoSuchUser
}

func lookupAdditionalGroupsForUIDInContainer(rootdir string, userid uint64) (gid []uint32, err error) {
	// Get the username associated with userid
	username, _, err := lookupGroupForUIDInContainer(rootdir, userid)
	if err != nil {
		return nil, err
	}

	f, err := openChrootedFile(rootdir, "etc/group")
	if err != nil {
		return nil, err
	}
	rc := bufio.NewScanner(f)
	defer f.Close()

	lookupGroup.Lock()
	defer lookupGroup.Unlock()

	grp := parseNextGroup(rc)
	for grp != nil {
		if slices.Contains(strings.Split(grp.user, ","), username) {
			gid = append(gid, uint32(grp.gid))
		}
		grp = parseNextGroup(rc)
	}
	return gid, nil
}

func lookupGroupInContainer(rootdir, groupname string) (gid uint64, err error) {
	f, err := openChrootedFile(rootdir, "etc/group")
	if err != nil {
		return 0, err
	}
	rc := bufio.NewScanner(f)
	defer f.Close()

	lookupGroup.Lock()
	defer lookupGroup.Unlock()

	grp := parseNextGroup(rc)
	for grp != nil {
		if grp.name != groupname {
			grp = parseNextGroup(rc)
			continue
		}
		return grp.gid, nil
	}

	return 0, user.UnknownGroupError(fmt.Sprintf("error looking up group %q", groupname))
}

func lookupUIDInContainer(rootdir string, uid uint64) (string, uint64, error) {
	f, err := openChrootedFile(rootdir, "etc/passwd")
	if err != nil {
		return "", 0, err
	}
	rc := bufio.NewScanner(f)
	defer f.Close()

	lookupUser.Lock()
	defer lookupUser.Unlock()

	pwd := parseNextPasswd(rc)
	for pwd != nil {
		if pwd.uid != uid {
			pwd = parseNextPasswd(rc)
			continue
		}
		return pwd.name, pwd.gid, nil
	}

	return "", 0, user.UnknownUserError(fmt.Sprintf("error looking up uid %d", uid))
}

func lookupHomedirInContainer(rootdir string, uid uint64) (string, error) {
	f, err := openChrootedFile(rootdir, "etc/passwd")
	if err != nil {
		return "", err
	}
	rc := bufio.NewScanner(f)
	defer f.Close()

	lookupUser.Lock()
	defer lookupUser.Unlock()

	pwd := parseNextPasswd(rc)
	for pwd != nil {
		if pwd.uid != uid {
			pwd = parseNextPasswd(rc)
			continue
		}
		return pwd.home, nil
	}

	return "", user.UnknownUserError(fmt.Sprintf("error looking up homedir for uid %d", uid))
}
