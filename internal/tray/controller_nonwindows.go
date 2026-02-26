//go:build !windows

package tray

type Controller struct{}

func New(_ any, _ func(), _ func(), _ func() (string, error), _ func(string), _ string) (*Controller, error) {
	return &Controller{}, nil
}

func (c *Controller) SetLanguage(_ string) {}
func (c *Controller) Dispose()          {}
