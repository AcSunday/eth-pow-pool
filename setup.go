package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/yvasiyarov/gorelic"

	"github.com/etclabscore/core-pool/common"
	"github.com/etclabscore/core-pool/library/clean"
	"github.com/etclabscore/core-pool/library/routine"

	"github.com/etclabscore/core-pool/api"
	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/payouts"
	"github.com/etclabscore/core-pool/proxy"
)

func Init() {
	// 加载配置文件与设置log
	readConfig(&cfg)
	rand.Seed(time.Now().UnixNano())
	err := logger.InitTimeLogger(cfg.Logger.LogPath, cfg.Logger.ErrLogPath, cfg.Logger.SaveDays, cfg.Logger.CutInterval)
	if err != nil {
		logger.Fatal("Loading logger config fail, err: %v", err)
	}
	logger.DEBUG = runLevelMap[cfg.RunLevel]
	logger.Info("Loading config complete")

	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		logger.Info("Running with %v threads", cfg.Threads)
	}

	// 初始化goroutine Group，注册退出函数进clean
	ctx, cancelFunc := context.WithCancel(context.Background())
	common.RoutineGroup, common.RoutineCtx = routine.NewGroupWithContext(cfg.MaxRoutine, ctx)
	common.RoutineGroup.Go(recoverGoroutine) // 启动任务恢复
	clean.PushFunc(func() error {
		close(routine.RecoverFuncChan)
		cancelFunc()
		return nil
	})
}

func startProxy() {
	if cfg.Proxy.Enabled {
		s := proxy.NewProxy(&cfg, backend)
		s.Start()
	}
}

func startApi() {
	if cfg.Api.Enabled {
		s := api.NewApiServer(&cfg.Api, backend)
		s.Start()
	}
}

func startBlockUnlocker() {
	if cfg.BlockUnlocker.Enabled {
		u := payouts.NewBlockUnlocker(&cfg.BlockUnlocker, backend, cfg.Network)
		u.Start()
	}
}

func startPayoutsProcessor() {
	if cfg.Payouts.Enabled {
		u := payouts.NewPayoutsProcessor(&cfg.Payouts, backend)
		u.Start()
	}
}

func startNewrelic() {
	if cfg.NewrelicEnabled {
		nr := gorelic.NewAgent()
		nr.Verbose = cfg.NewrelicVerbose
		nr.NewrelicLicense = cfg.NewrelicKey
		nr.NewrelicName = cfg.NewrelicName
		nr.Run()
	}
}

func readConfig(cfg *proxy.Config) {
	configFileName := "config.json"
	if len(os.Args) > 1 {
		configFileName = os.Args[1]
	}
	configFileName, _ = filepath.Abs(configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		panic(fmt.Sprintf("Open config file error: %s", err.Error()))
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err := jsonParser.Decode(&cfg); err != nil {
		panic(fmt.Sprintf("JSON decode config error: %s", err.Error()))
	}
}

// goroutine恢复器，恢复需要常驻的工作函数
func recoverGoroutine() error {
	for {
		select {
		case <-common.RoutineCtx.Done():
			logger.Info("Stopping recover goroutine working")
			return nil
		case f := <-routine.RecoverFuncChan:
			common.RoutineGroup.GoRecover(f)
		}
	}
}
