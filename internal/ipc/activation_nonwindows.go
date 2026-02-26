//go:build !windows

package ipc

type ActivationListener struct{}

func NewActivationListener(_ string) (*ActivationListener, error) {
	return &ActivationListener{}, nil
}

func (l *ActivationListener) Start(_ func()) {}

func (l *ActivationListener) Close() {}

func TrySignalActivation(_ string) bool { return false }
