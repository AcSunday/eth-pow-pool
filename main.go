// +build go1.9

package main

import (
	"github.com/etclabscore/core-pool/common"
	"github.com/etclabscore/core-pool/library/clean"
	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/proxy"
	"github.com/etclabscore/core-pool/storage"
)

var (
	cfg         proxy.Config
	backend     *storage.RedisClient
	runLevelMap = map[string]bool{
		"production": false,
		"testing":    false,
		"dev":        true,
	}
)

func main() {
	Init()
	startNewrelic()

	// 启动redis
	backend = storage.NewRedisClient(&cfg.Redis, cfg.Coin)
	pong, err := backend.Check()
	if err != nil {
		logger.Fatal("Can't establish connection to backend: %v", err)
	}
	logger.Info("Backend check reply: %v", pong)

	// 启动模块 proxy, api, unlocker, payer
	// start会校验配置文件，检测该服务是否需要开启
	go startProxy()
	go startApi()
	go startBlockUnlocker()
	go startPayoutsProcessor()

	// 等待goroutine group退出
	if err := common.RoutineGroup.Wait(); err != nil {
		logger.Error("Wait goroutine group failed, err: %s", err.Error())
	}
	clean.Exit()
}
