// +build go1.9

package main

import (
	"encoding/json"
	"github.com/etclabscore/core-pool/util/logger"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/yvasiyarov/gorelic"

	"github.com/etclabscore/core-pool/api"
	"github.com/etclabscore/core-pool/payouts"
	"github.com/etclabscore/core-pool/proxy"
	"github.com/etclabscore/core-pool/storage"
)

var (
	cfg     proxy.Config
	backend *storage.RedisClient
)

func startProxy() {
	s := proxy.NewProxy(&cfg, backend)
	s.Start()
}

func startApi() {
	s := api.NewApiServer(&cfg.Api, backend)
	s.Start()
}

func startBlockUnlocker() {
	u := payouts.NewBlockUnlocker(&cfg.BlockUnlocker, backend, cfg.Network)
	u.Start()
}

func startPayoutsProcessor() {
	u := payouts.NewPayoutsProcessor(&cfg.Payouts, backend)
	u.Start()
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
	logger.Info("Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		logger.Error("File error: %s", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err := jsonParser.Decode(&cfg); err != nil {
		logger.Error("Config error: %s", err.Error())
	}
}

func main() {
	readConfig(&cfg)
	rand.Seed(time.Now().UnixNano())
	err := logger.InitTimeLogger(cfg.Logger.LogPath, cfg.Logger.ErrLogPath, cfg.Logger.SaveDays, cfg.Logger.CutInterval)
	if err != nil {
		logger.Fatal("Loading logger config fail, err: %v", err)
	}

	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		logger.Info("Running with %v threads", cfg.Threads)
	}

	startNewrelic()

	backend = storage.NewRedisClient(&cfg.Redis, cfg.Coin)
	pong, err := backend.Check()
	if err != nil {
		logger.Error("Can't establish connection to backend: %v", err)
	} else {
		logger.Info("Backend check reply: %v", pong)
	}

	if cfg.Proxy.Enabled {
		go startProxy()
	}
	if cfg.Api.Enabled {
		go startApi()
	}
	if cfg.BlockUnlocker.Enabled {
		go startBlockUnlocker()
	}
	if cfg.Payouts.Enabled {
		go startPayoutsProcessor()
	}
	quit := make(chan bool)
	<-quit
}
