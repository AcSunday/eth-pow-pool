package routine

import (
	"context"
	"fmt"
	"runtime"

	"github.com/etclabscore/core-pool/library/logger"
	"golang.org/x/sync/errgroup"
)

var RecoverFuncChan = make(chan func() error, 16) // 需要恢复的work通道

type Group struct {
	mac *MaxAmountCtrl
	egp *errgroup.Group
}

func NewGroup(max int) *Group {
	return &Group{
		mac: NewMaxAmountCtrl(max),
		egp: &errgroup.Group{},
	}
}

func NewGroupWithContext(max int, ctx context.Context) (*Group, context.Context) {
	egp, ctx := errgroup.WithContext(ctx)
	return &Group{
		mac: NewMaxAmountCtrl(max),
		egp: egp,
	}, ctx
}

// go协程panic后，能够恢复，但不会重新执行该协程函数
func (g *Group) Go(f func() error) {
	g.mac.Incr()
	g.egp.Go(func() error {
		defer func() {
			if p := recover(); p != nil {
				var buf [4096]byte
				n := runtime.Stack(buf[:], false)
				logger.Error(fmt.Sprintf("[panic] worker exit from a panic: %v", p))
				logger.Error(fmt.Sprintf("[panic] worker exit from panic: %s", string(buf[:n])))
			}
			g.mac.Decr()
		}()
		return f()
	})
}

// go协程出现panic后，能够恢复并重新执行该协程函数
func (g *Group) GoRecover(f func() error) {
	g.mac.Incr()
	g.egp.Go(func() error {
		defer func() {
			if p := recover(); p != nil {
				var buf [4096]byte
				n := runtime.Stack(buf[:], false)
				logger.Error(fmt.Sprintf("[panic] worker exit from a panic: %v", p))
				logger.Error(fmt.Sprintf("[panic] worker exit from panic: %s", string(buf[:n])))
				RecoverFuncChan <- f
			}
			g.mac.Decr()
		}()
		return f()
	})
}

func (g *Group) Wait() error {
	return g.egp.Wait()
}
