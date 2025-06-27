//go:build unix

package ally

import (
	"os"
	"syscall"
)

func keySignals() (os.Signal, []os.Signal) {
	sigs := []os.Signal{
		syscall.SIGUSR2, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM,
	}
	return sigs[0], sigs
}
