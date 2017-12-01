// +build linux freebsd solaris

package ttrpc

import (
	"context"
	"net"
	"os/user"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

var (
	UnixSocketRequireSameUser = UnixCredentialsFunc(requireSameUser)
	UnixSocketRequireRoot     = UnixCredentialsFunc(requireRoot)
)

type UnixCredentialsFunc func(*unix.Ucred) error

func (fn UnixCredentialsFunc) Handshake(ctx context.Context, conn net.Conn) (net.Conn, interface{}, error) {
	uc, err := requireUnixSocket(conn)
	if err != nil {
		return nil, nil, errors.Wrap(err, "ttrpc.UnixCredentialsFunc: require unix socket")
	}

	fp, err := uc.File()
	if err != nil {
		return nil, nil, errors.Wrap(err, "ttrpc.UnixCredentialsFunc: failed to get unix file")
	}
	defer fp.Close() // this gets duped and must be closed when this method is complete.

	ucred, err := unix.GetsockoptUcred(int(fp.Fd()), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "ttrpc.UnixCredentialsFunc: failed to retrieve socket peer credentials")
	}

	if err := fn(ucred); err != nil {
		return nil, nil, errors.Wrapf(err, "ttrpc.UnixCredentialsFunc: credential check failed")
	}

	return uc, ucred, nil
}

func UnixSocketRequireUidGid(uid, gid uint32) UnixCredentialsFunc {
	return func(ucred *unix.Ucred) error {
		return requireUidGid(ucred, uid, gid)
	}
}

func requireRoot(ucred *unix.Ucred) error {
	return requireUidGid(ucred, 0, 0)
}

func requireSameUser(ucred *unix.Ucred) error {
	u, err := user.Current()
	if err != nil {
		return errors.Wrapf(err, "could not resolve current user")
	}

	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return errors.Wrapf(err, "failed to parse current user uid: %v", u.Uid)
	}

	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return errors.Wrapf(err, "failed to parse current user gid: %v", u.Gid)
	}

	return requireUidGid(ucred, uint32(uid), uint32(gid))
}

func requireUidGid(ucred *unix.Ucred, uid, gid uint32) error {
	if (uid != ucred.Uid) || (gid != ucred.Gid) {
		return errors.Wrap(syscall.EPERM, "ttrpc: invalid credentials")
	}
	return nil
}

func requireUnixSocket(conn net.Conn) (*net.UnixConn, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errors.New("a unix socket connection is required")
	}

	return uc, nil
}
