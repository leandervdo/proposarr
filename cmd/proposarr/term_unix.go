//go:build darwin || linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// stdinIsTerminal asks the tty driver, so /dev/null and pipes are not terminals.
func stdinIsTerminal() bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), ioctlReadTermios, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
