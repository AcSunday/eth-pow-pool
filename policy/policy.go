package policy

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/etclabscore/core-pool/common"
	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/storage"
	"github.com/etclabscore/core-pool/util"
)

type Config struct {
	Workers         int     `json:"workers"`
	Banning         Banning `json:"banning"`
	Limits          Limits  `json:"limits"`
	ResetInterval   string  `json:"resetInterval"`
	RefreshInterval string  `json:"refreshInterval"`
}

type Limits struct {
	Enabled   bool   `json:"enabled"`
	Limit     int32  `json:"limit"`
	Grace     string `json:"grace"`
	LimitJump int32  `json:"limitJump"`
}

type Banning struct {
	Enabled        bool    `json:"enabled"`
	IPSet          string  `json:"ipset"`
	Timeout        int64   `json:"timeout"`
	InvalidPercent float32 `json:"invalidPercent"`
	CheckThreshold int32   `json:"checkThreshold"`
	MalformedLimit int32   `json:"malformedLimit"`
}

type Stats struct {
	sync.Mutex
	// We are using atomic with LastBeat,
	// so moving it before the rest in order to avoid alignment issue
	LastBeat      int64
	BannedAt      int64
	ValidShares   int32
	InvalidShares int32
	Malformed     int32
	ConnLimit     int32
	Banned        int32
}

type PolicyServer struct {
	sync.RWMutex
	statsMu    sync.Mutex
	config     *Config
	stats      map[string]*Stats
	banChannel chan string
	startedAt  int64
	grace      int64
	timeout    int64
	blacklist  []string
	whitelist  []string
	storage    *storage.RedisClient
}

func Start(cfg *Config, storage *storage.RedisClient) *PolicyServer {
	s := &PolicyServer{config: cfg, startedAt: util.MakeTimestamp()}
	grace := util.MustParseDuration(cfg.Limits.Grace)
	s.grace = int64(grace / time.Millisecond)
	s.banChannel = make(chan string, 64)
	s.stats = make(map[string]*Stats)
	s.storage = storage
	s.refreshState()

	timeout := util.MustParseDuration(s.config.ResetInterval)
	s.timeout = int64(timeout / time.Millisecond)

	resetIntv := util.MustParseDuration(s.config.ResetInterval)
	resetTimer := time.NewTimer(resetIntv)
	logger.Info("Set policy stats reset every %v", resetIntv)

	refreshIntv := util.MustParseDuration(s.config.RefreshInterval)
	refreshTimer := time.NewTimer(refreshIntv)
	logger.Info("Set policy state refresh every %v", refreshIntv)

	common.RoutineGroup.GoRecover(func() error {
		for {
			select {
			case <-common.RoutineCtx.Done():
				logger.Info("Stopping policy state refresh")
				return nil
			case <-resetTimer.C:
				s.resetStats()
				resetTimer.Reset(resetIntv)
			case <-refreshTimer.C:
				s.refreshState()
				refreshTimer.Reset(refreshIntv)
			}
		}
	})

	for i := 0; i < s.config.Workers; i++ {
		s.startPolicyWorker(i)
	}
	logger.Info("Running with %v policy workers", s.config.Workers)
	return s
}

func (s *PolicyServer) startPolicyWorker(workId int) {
	common.RoutineGroup.GoRecover(func() error {
		id := workId
		for {
			select {
			case <-common.RoutineCtx.Done():
				logger.Info("Stopping ban worker, id: %d", id)
				return nil
			case ip := <-s.banChannel:
				s.doBan(ip)
			}
		}
	})
}

func (s *PolicyServer) resetStats() {
	now := util.MakeTimestamp()
	banningTimeout := s.config.Banning.Timeout * 1000
	total := 0
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	for key, m := range s.stats {
		lastBeat := atomic.LoadInt64(&m.LastBeat)
		bannedAt := atomic.LoadInt64(&m.BannedAt)

		if now-bannedAt >= banningTimeout {
			atomic.StoreInt64(&m.BannedAt, 0)
			if atomic.CompareAndSwapInt32(&m.Banned, 1, 0) {
				logger.Info("Ban dropped for %v", key)
				delete(s.stats, key)
				total++
			}
		}
		if now-lastBeat >= s.timeout {
			delete(s.stats, key)
			total++
		}
	}
	logger.Info("Flushed stats for %v IP addresses", total)
}

func (s *PolicyServer) refreshState() {
	s.Lock()
	defer s.Unlock()
	var err error

	s.blacklist, err = s.storage.GetBlacklist()
	if err != nil {
		logger.Error("Failed to get blacklist from backend: %v", err)
	}
	s.whitelist, err = s.storage.GetWhitelist()
	if err != nil {
		logger.Error("Failed to get whitelist from backend: %v", err)
	}
	logger.Info("Policy state refresh complete")
}

func (s *PolicyServer) NewStats() *Stats {
	x := &Stats{
		ConnLimit: s.config.Limits.Limit,
	}
	x.heartbeat()
	return x
}

func (s *PolicyServer) Get(ip string) *Stats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	if x, ok := s.stats[ip]; !ok {
		x = s.NewStats()
		s.stats[ip] = x
		return x
	} else {
		x.heartbeat()
		return x
	}
}

func (s *PolicyServer) BanClient(ip string) {
	x := s.Get(ip)
	s.forceBan(x, ip)
}

func (s *PolicyServer) IsBanned(ip string) bool {
	x := s.Get(ip)
	return atomic.LoadInt32(&x.Banned) > 0
}

func (s *PolicyServer) ApplyLimitPolicy(ip string) bool {
	if !s.config.Limits.Enabled {
		return true
	}
	now := util.MakeTimestamp()
	if now-s.startedAt > s.grace {
		return s.Get(ip).decrLimit() > 0
	}
	return true
}

func (s *PolicyServer) ApplyLoginPolicy(addy, ip string) bool {
	if s.InBlackList(addy) {
		x := s.Get(ip)
		s.forceBan(x, ip)
		return false
	}
	return true
}

func (s *PolicyServer) ApplyMalformedPolicy(ip string) bool {
	x := s.Get(ip)
	n := x.incrMalformed()
	if n >= s.config.Banning.MalformedLimit {
		s.forceBan(x, ip)
		return false
	}
	return true
}

func (s *PolicyServer) ApplySharePolicy(ip string, validShare bool) bool {
	x := s.Get(ip)
	x.Lock()

	if validShare {
		x.ValidShares++
		if s.config.Limits.Enabled {
			x.incrLimit(s.config.Limits.LimitJump)
		}
	} else {
		x.InvalidShares++
	}

	totalShares := x.ValidShares + x.InvalidShares
	if totalShares < s.config.Banning.CheckThreshold {
		x.Unlock()
		return true
	}
	validShares := float32(x.ValidShares)
	invalidShares := float32(x.InvalidShares)
	x.resetShares()
	x.Unlock()

	ratio := invalidShares / validShares

	if ratio >= s.config.Banning.InvalidPercent/100.0 {
		s.forceBan(x, ip)
		return false
	}
	return true
}

func (x *Stats) resetShares() {
	x.ValidShares = 0
	x.InvalidShares = 0
}

func (s *PolicyServer) forceBan(x *Stats, ip string) {
	if !s.config.Banning.Enabled || s.InWhiteList(ip) {
		return
	}
	atomic.StoreInt64(&x.BannedAt, util.MakeTimestamp())

	if atomic.CompareAndSwapInt32(&x.Banned, 0, 1) {
		if len(s.config.Banning.IPSet) > 0 {
			s.banChannel <- ip
		} else {
			logger.Info("Banned peer %s", ip)
		}
	}
}

func (x *Stats) incrLimit(n int32) {
	atomic.AddInt32(&x.ConnLimit, n)
}

func (x *Stats) incrMalformed() int32 {
	return atomic.AddInt32(&x.Malformed, 1)
}

func (x *Stats) decrLimit() int32 {
	return atomic.AddInt32(&x.ConnLimit, -1)
}

func (s *PolicyServer) InBlackList(addy string) bool {
	s.RLock()
	defer s.RUnlock()
	return util.StringInSlice(addy, s.blacklist)
}

func (s *PolicyServer) InWhiteList(ip string) bool {
	s.RLock()
	defer s.RUnlock()
	return util.StringInSlice(ip, s.whitelist)
}

func (s *PolicyServer) doBan(ip string) {
	set, timeout := s.config.Banning.IPSet, s.config.Banning.Timeout
	cmd := fmt.Sprintf("sudo ipset add %s %s timeout %v -!", set, ip, timeout)
	args := strings.Fields(cmd)
	head := args[0]
	args = args[1:]

	logger.Info("Banned %v with timeout %v on ipset %s", ip, timeout, set)

	_, err := exec.Command(head, args...).Output()
	if err != nil {
		logger.Error("CMD Error: %v", err)
	}
}

func (x *Stats) heartbeat() {
	now := util.MakeTimestamp()
	atomic.StoreInt64(&x.LastBeat, now)
}
