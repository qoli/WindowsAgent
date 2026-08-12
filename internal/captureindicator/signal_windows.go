//go:build windows

package captureindicator

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// Publisher owns one manual-reset event and keeps it signaled for at least
// PulseDuration after the most recent accepted capture request.
type Publisher struct {
	handle    windows.Handle
	pulses    chan chan error
	close     chan chan error
	done      chan struct{}
	closeOnce sync.Once
}

func NewPublisher() (*Publisher, error) {
	name, err := windows.UTF16PtrFromString(SignalName)
	if err != nil {
		return nil, fmt.Errorf("encode capture indicator name: %w", err)
	}
	handle, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		return nil, fmt.Errorf("create capture indicator: %w", err)
	}
	publisher := &Publisher{
		handle: handle,
		pulses: make(chan chan error),
		close:  make(chan chan error),
		done:   make(chan struct{}),
	}
	go publisher.run()
	return publisher, nil
}

func (p *Publisher) Pulse() error {
	if p == nil {
		return errors.New("capture indicator publisher is required")
	}
	result := make(chan error, 1)
	select {
	case p.pulses <- result:
		return <-result
	case <-p.done:
		return errors.New("capture indicator publisher is closed")
	}
}

func (p *Publisher) Close() error {
	if p == nil {
		return nil
	}
	var result error
	p.closeOnce.Do(func() {
		response := make(chan error, 1)
		p.close <- response
		result = <-response
		<-p.done
	})
	return result
}

func (p *Publisher) run() {
	defer close(p.done)
	var timer *time.Timer
	var timerChannel <-chan time.Time
	for {
		select {
		case result := <-p.pulses:
			err := windows.SetEvent(p.handle)
			if err == nil {
				if timer == nil {
					timer = time.NewTimer(PulseDuration)
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(PulseDuration)
				}
				timerChannel = timer.C
			}
			result <- err
		case <-timerChannel:
			_ = windows.ResetEvent(p.handle)
			timerChannel = nil
		case result := <-p.close:
			if timer != nil {
				timer.Stop()
			}
			resetErr := windows.ResetEvent(p.handle)
			closeErr := windows.CloseHandle(p.handle)
			if resetErr != nil {
				result <- fmt.Errorf("reset capture indicator: %w", resetErr)
			} else if closeErr != nil {
				result <- fmt.Errorf("close capture indicator: %w", closeErr)
			} else {
				result <- nil
			}
			return
		}
	}
}

func Active() (bool, error) {
	name, err := windows.UTF16PtrFromString(SignalName)
	if err != nil {
		return false, fmt.Errorf("encode capture indicator name: %w", err)
	}
	handle, err := windows.OpenEvent(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open capture indicator: %w", err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, fmt.Errorf("read capture indicator: %w", err)
	}
	switch status {
	case windows.WAIT_OBJECT_0:
		return true, nil
	case uint32(windows.WAIT_TIMEOUT):
		return false, nil
	default:
		return false, fmt.Errorf("capture indicator returned unexpected wait status %#x", status)
	}
}
