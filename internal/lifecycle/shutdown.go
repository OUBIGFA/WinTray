package lifecycle

import "context"

type ExitFunc func()

func ExitIfCompleted(ctx context.Context, enabled bool, hadTasks bool, exit ExitFunc) {
	if ctx.Err() != nil {
		return
	}
	if enabled && hadTasks && exit != nil {
		exit()
	}
}
