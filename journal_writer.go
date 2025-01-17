package slogjournal

import (
	"errors"
	"fmt"
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

func newJournalWriter() (io.Writer, error) {
	// The "net" library in Go really wants me to either Dial or Listen a UnixConn,
	// which would respectively bind() an address or connect() to a remote address,
	// but we want neither. We want to create a datagram socket and write to it directly
	// and not worry about reconnecting or rebinding.
	// so jumping through some hoops here
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), "journal")
	defer f.Close()

	fconn, err := net.FileConn(f)
	if err != nil {
		return nil, err
	}
	conn, ok := fconn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("expected *net.UnixConn, got %T", fconn)
	}

	if err := conn.SetWriteBuffer(sndBufSize); err != nil {
		return nil, err
	}

	addr := &net.UnixAddr{
		Name: "/run/systemd/journal/socket",
		Net:  "unixgram",
	}

	return &journalWriter{
		addr: addr,
		conn: conn,
	}, nil
}

// If the message is too large, it will write the message to a temporary file and send the file descriptor as OOB data.
func (j *journalWriter) Write(p []byte) (n int, err error) {
	// NOTE: No mutex needed. datagram socket writes are atomic
	n, err = j.conn.WriteToUnix(p, j.addr)
	// fail silently if the journal is not available
	if err == nil || errors.Is(err, syscall.ENOENT) {
		return n, nil
	}

	if !errors.Is(err, syscall.ENOBUFS) && !errors.Is(err, syscall.EMSGSIZE) {
		return n, err
	}

	// Message does not fit in a single datagram, write to a temp file and send the file descriptor
	file, err := tempFd()
	if err != nil {
		return n, err
	}
	defer file.Close()
	if n, err := file.Write(p); err != nil {
		return n, err
	}
	if err := trySeal(file); err != nil {
		return n, err
	}
	fd := int(file.Fd())
	if _, _, err := j.conn.WriteMsgUnix([]byte{}, syscall.UnixRights(fd), j.addr); err != nil {
		return 0, err
	}
	return n, err
}

var _ io.Writer = &journalWriter{}
