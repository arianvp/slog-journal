package slogjournal

import (
	"os"
	"syscall"
)

func tempFdCommon() (*os.File, error) {
	file, err := os.CreateTemp("/dev/shm/", "journal")
	if err != nil {
		file, err = os.CreateTemp(os.TempDir(), "journal")
		if err != nil {
			return nil, err
		}
	}
	if err := syscall.Unlink(file.Name()); err != nil {
		return nil, err
	}
	return file, nil
}
