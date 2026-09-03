//go:build linux

package quota

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func newSuspendAwareTimer(delay time.Duration) (<-chan time.Time, func(), error) {
	if delay <= 0 {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch, func() {}, nil
	}

	fd, err := unix.TimerfdCreate(unix.CLOCK_BOOTTIME, unix.TFD_CLOEXEC|unix.TFD_NONBLOCK)
	if err != nil {
		return nil, nil, err
	}

	spec := unix.ItimerSpec{
		Value: unix.NsecToTimespec(delay.Nanoseconds()),
	}
	if err := unix.TimerfdSettime(fd, 0, &spec, nil); err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}

	file := os.NewFile(uintptr(fd), "suspend-aware-timer")
	ch := make(chan time.Time, 1)
	go func() {
		var buf [8]byte
		n, err := file.Read(buf[:])
		if err == nil && n == 8 {
			select {
			case ch <- time.Now():
			default:
			}
		}
	}()

	stop := func() {
		_ = file.Close()
	}

	return ch, stop, nil
}
