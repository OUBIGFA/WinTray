package ui

// rendererEnvVar names the mygo variable that selects the drawing path.
// "0" asks for mygo's dirty-rect CPU renderer; any other value lets mygo
// choose its GPU renderer.
const rendererEnvVar = "MYGO_GPU"

// firstWindows11Build is the first Windows 11 build number (22000, 21H2).
const firstWindows11Build = 22000

// preferCPURenderer reports whether mygo's dirty-rect CPU renderer should be
// forced on this Windows build. The CPU path draws a blank client area on
// Windows 10 (issues #5 and #6), so Windows 10 leaves the choice to mygo,
// which uses the GPU renderer there. Windows 11 keeps the smaller path.
func preferCPURenderer(build uint32) bool {
	return build >= firstWindows11Build
}

// configureRenderer asks mygo for the CPU renderer where it is known to
// paint. An explicit MYGO_GPU from the environment is never overridden, and
// the value in effect is returned so the startup log records the path taken.
// A caller keeps the returned string only for logging: mygo reads the
// variable itself.
func configureRenderer(build uint32, lookupEnv func(string) (string, bool), setenv func(string, string) error) (string, error) {
	if value, set := lookupEnv(rendererEnvVar); set {
		return value, nil
	}
	if !preferCPURenderer(build) {
		return "", nil
	}
	if err := setenv(rendererEnvVar, "0"); err != nil {
		return "", err
	}
	return "0", nil
}
