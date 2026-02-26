//go:build !windows

package ipc

type SingleInstance struct{}

func Acquire(_ string) (*SingleInstance, bool, error) {
	return &SingleInstance{}, false, nil
}

func (s *SingleInstance) Close() {}
