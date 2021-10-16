package common

import (
	"context"
	"github.com/etclabscore/core-pool/library/routine"
)

var (
	// 全局
	RoutineCtx   context.Context
	RoutineGroup *routine.Group
)
