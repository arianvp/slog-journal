//go:build linux

package slogjournal

import (
	"os"

	"golang.org/x/sys/unix"
)

func tempFd() (*os.File, error) {
	fd, err := unix.MemfdCreate("journal", unix.MFD_ALLOW_SEALING)
	if err == nil {
		return os.NewFile(uintptr(fd), ""), nil
	}
	return tempFdCommon()
}

func trySeal(f *os.File) error {
	_, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE)
	return err
}
