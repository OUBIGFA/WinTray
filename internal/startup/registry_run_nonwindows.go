//go:build !windows

package startup

type Registrar struct{}

func NewRegistrar() *Registrar { return &Registrar{} }

func (r *Registrar) SetEnabled(_, _ string, _ bool) error { return nil }
