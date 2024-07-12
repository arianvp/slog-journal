package slogjournal

import (
	"io"
	"net"
	"os"
	"syscall"
)

// journalWriter encapsulates the behaviour of writing unixgrams to the journal socket.
// It will try to write the message with a single write call, but if the message is too large
// it will write the message to a temporary file and send the file descriptor as OOB data.
type journalWriter struct {
	addr *net.UnixAddr
	conn *net.UnixConn
}

// Write implements io.Writer.
// If the message is too large, it will write the message to a temporary file and send the file descriptor as OOB data.
func (j *journalWriter) Write(p []byte) (n int, err error) {
	// NOTE: No mutex needed. datagram socket writes are atomic
	n, err = j.conn.WriteToUnix(p, j.addr)
	if err == nil {
		return n, nil
	}
	opErr, ok := err.(*net.OpError)
	if !ok {
		return n, err
	}
	errno, ok := opErr.Err.(*os.SyscallError)
	if !ok {
		return n, err
	}
	// fail silently if the journal is not available
	if errno.Err == syscall.ENOENT {
		return n, nil
	}
	if errno.Err != syscall.ENOBUFS && errno.Err != syscall.EMSGSIZE {
		return n, err
	}

	file, err := tempFd()
	if err != nil {
		return n, err
	}
	if n, err := file.Write(p); err != nil {
		return n, err
	}
	if err := trySeal(file); err != nil {
		return n, err
	}
	fd := int(file.Fd())
	if n, _, err := j.conn.WriteMsgUnix([]byte{}, syscall.UnixRights(fd), j.addr); err != nil {
		return n, err
	}
	return n, err
}

var _ io.Writer = &journalWriter{}
