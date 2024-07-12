//go:build unix && !linux

package slogjournal

import "os"

func tempFd() (*os.File, error) {
	return tempFdCommon()
}

func trySeal(*os.File) error {
	return nil
}
