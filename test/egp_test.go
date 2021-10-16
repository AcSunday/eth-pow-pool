package test

import (
	"fmt"
	"testing"
	"time"

	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/library/routine"
)

// 测试errGroup捕捉panic

func recoverGoroutine() {
	for f := range routine.RecoverFuncChan {
		group := routine.NewGroup(10)
		group.Go(func() error {
			logger.Info("recovering worker...")
			return f()
		})
		close(routine.RecoverFuncChan)
	}
}

func TestErrGp(t *testing.T) {
	logger.InitTimeLogger("./run.log", "./run_err.log", 7, 10)
	go recoverGoroutine()

	group := routine.NewGroup(10)
	for i := 0; i < 10; i++ {
		num := i
		group.GoRecover(func() error {
			if num == 5 {
				panic(fmt.Sprintf("工人编号 %d 出错", num))
			}
			t.Error(num)
			return nil
		})
	}
	group.Wait()
	time.Sleep(1)
}
