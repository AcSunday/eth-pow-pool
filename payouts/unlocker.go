package payouts

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/etclabscore/core-pool/common"
	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/payouts/coinhash"
	"github.com/etclabscore/core-pool/rpc"
	"github.com/etclabscore/core-pool/storage"
	"github.com/etclabscore/core-pool/util"
)

// ETH 算法类区块解锁器

type UnlockerConfig struct {
	Enabled        bool    `json:"enabled"`
	PoolFee        float64 `json:"poolFee"`
	PoolFeeAddress string  `json:"poolFeeAddress"`
	Donate         bool    `json:"donate"`
	Depth          int64   `json:"depth"`
	ImmatureDepth  int64   `json:"immatureDepth"`
	KeepTxFees     bool    `json:"keepTxFees"`
	Interval       string  `json:"interval"`
	Daemon         string  `json:"daemon"`
	Timeout        string  `json:"timeout"`
	Network        string  `json:"network"`
}

type BlockUnlocker struct {
	config   *UnlockerConfig
	backend  *storage.RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

const (
	minDepth = 16

	// network常量
	EtcNetwork     = "classic"
	MordorNetwork  = "mordor"
	EthNetwork     = "ethereum"
	RopstenNetwork = "ropsten"
	UbiqNetwork    = "ubiq"
)

func NewBlockUnlocker(cfg *UnlockerConfig, backend *storage.RedisClient, network string) *BlockUnlocker {
	// determine which monetary policy to use based on network
	// configure any reward params if needed.
	// 根据EIP方案，对齐分叉高度
	switch network {
	case EtcNetwork:
		coinhash.CoinConf.Ecip1017FBlock = 5000000
		coinhash.CoinConf.Ecip1017EraRounds = big.NewInt(5000000)
	case MordorNetwork:
		coinhash.CoinConf.Ecip1017FBlock = 0
		coinhash.CoinConf.Ecip1017EraRounds = big.NewInt(2000000)
	case EthNetwork:
		coinhash.CoinConf.ByzantiumHardForkHeight = big.NewInt(4370000)
		coinhash.CoinConf.ConstantinopleHardForkHeight = big.NewInt(7280000)
		coinhash.CoinConf.LondonHardForkHeight = big.NewInt(12965000)
	case RopstenNetwork:
		coinhash.CoinConf.ByzantiumHardForkHeight = big.NewInt(1700000)
		coinhash.CoinConf.ConstantinopleHardForkHeight = big.NewInt(4230000)
		coinhash.CoinConf.LondonHardForkHeight = big.NewInt(10499401)
	case UbiqNetwork:
		// nothing needs configuring here, simply proceed.
		break
	default:
		logger.Fatal("Invalid network set %s", network)
	}

	cfg.Network = network

	if len(cfg.PoolFeeAddress) != 0 && !util.IsValidHexAddress(cfg.PoolFeeAddress) {
		logger.Fatal("Invalid poolFeeAddress: %s", cfg.PoolFeeAddress)
	}
	if cfg.Depth < minDepth*2 {
		logger.Fatal("Block maturity depth can't be < %v, your depth is %v", minDepth*2, cfg.Depth)
	}
	if cfg.ImmatureDepth < minDepth {
		logger.Fatal("Immature depth can't be < %v, your depth is %v", minDepth, cfg.ImmatureDepth)
	}
	u := &BlockUnlocker{config: cfg, backend: backend}
	u.rpc = rpc.NewRPCClient("BlockUnlocker", cfg.Daemon, cfg.Timeout)
	return u
}

func (u *BlockUnlocker) Start() {
	logger.Info("Starting block unlocker")
	intv := util.MustParseDuration(u.config.Interval)
	timer := time.NewTimer(intv)
	logger.Info("Set block unlock interval to %v", intv)

	// Immediately unlock after start
	u.unlockPendingBlocks()
	u.unlockAndCreditMiners()
	timer.Reset(intv)

	common.RoutineGroup.GoRecover(func() error {
		for {
			select {
			case <-common.RoutineCtx.Done():
				logger.Info("Stopping unlocker working module")
				return nil
			case <-timer.C:
				u.unlockPendingBlocks()
				u.unlockAndCreditMiners()
				timer.Reset(intv)
			}
		}
	})
}

type UnlockResult struct {
	maturedBlocks  []*storage.BlockData
	orphanedBlocks []*storage.BlockData
	orphans        int
	uncles         int
	blocks         int
}

/* Geth does not provide consistent state when you need both new height and new job,
 * so in redis I am logging just what I have in a pool state on the moment when block found.
 * Having very likely incorrect height in database results in a weird block unlocking scheme,
 * when I have to check what the hell we actually found and traversing all the blocks with height-N and height+N
 * to make sure we will find it. We can't rely on round height here, it's just a reference point.
 * ISSUE: https://github.com/ethereum/go-ethereum/issues/2333
 */
func (u *BlockUnlocker) unlockCandidates(candidates []*storage.BlockData) (*UnlockResult, error) {
	result := &UnlockResult{}

	// Data row is: "height:nonce:powHash:mixDigest:timestamp:diff:totalShares"
	for _, candidate := range candidates {
		orphan := true

		/* Search for a normal block with wrong height here by traversing 16 blocks back and forward.
		 * Also we are searching for a block that can include this one as uncle.
		 */
		if candidate.Height < minDepth {
			orphan = false
			// avoid scanning the first 16 blocks
			continue
		}
		for i := int64(minDepth * -1); i < minDepth; i++ {
			height := candidate.Height + i

			if height < 0 {
				continue
			}

			block, err := u.rpc.GetBlockByHeight(height)

			if err != nil {
				logger.Error("Error while retrieving block %v from node: %v", height, err)
				return nil, err
			}
			if block == nil {
				return nil, fmt.Errorf("Error while retrieving block %v from node, wrong node height. ", height)
			}

			if matchCandidate(block, candidate) {
				orphan = false
				result.blocks++

				err = u.handleBlock(block, candidate)
				if err != nil {
					u.halt = true
					u.lastFail = err
					return nil, err
				}
				result.maturedBlocks = append(result.maturedBlocks, candidate)
				logger.Info("Mature block %v with %v tx, hash: %v", candidate.Height, len(block.Transactions), candidate.Hash[0:10])
				break
			}

			if len(block.Uncles) == 0 {
				continue
			}

			// Trying to find uncle in current block during our forward check
			for uncleIndex, uncleHash := range block.Uncles {
				uncle, err := u.rpc.GetUncleByBlockNumberAndIndex(height, uncleIndex)
				if err != nil {
					return nil, fmt.Errorf("Error while retrieving uncle of block %v from node: %v ", uncleHash, err)
				}
				if uncle == nil {
					return nil, fmt.Errorf("Error while retrieving uncle of block %v from node. ", height)
				}

				// Found uncle
				if matchCandidate(uncle, candidate) {
					orphan = false
					result.uncles++

					err := handleUncle(height, uncle, candidate, u.config)
					if err != nil {
						u.halt = true
						u.lastFail = err
						return nil, err
					}
					result.maturedBlocks = append(result.maturedBlocks, candidate)
					logger.Info("Mature uncle %v/%v of reward %v with hash: %v", candidate.Height, candidate.UncleHeight,
						util.FormatReward(candidate.Reward), uncle.Hash[0:10])
					break
				}
			}
			// Found block or uncle
			if !orphan {
				break
			}
		}
		// Block is lost, we didn't find any valid block or uncle matching our data in a blockchain
		if orphan {
			result.orphans++
			candidate.Orphan = true
			result.orphanedBlocks = append(result.orphanedBlocks, candidate)
			logger.Warn("Orphaned block %v:%v", candidate.RoundHeight, candidate.Nonce)
		}
	}
	return result, nil
}

func matchCandidate(block *rpc.GetBlockReply, candidate *storage.BlockData) bool {
	// Just compare hash if block is unlocked as immature
	if len(candidate.Hash) > 0 && strings.EqualFold(candidate.Hash, block.Hash) {
		return true
	}
	// Geth-style candidate matching
	if len(block.Nonce) > 0 {
		return strings.EqualFold(block.Nonce, candidate.Nonce)
	}
	// Parity's EIP: https://github.com/ethereum/EIPs/issues/95
	if len(block.SealFields) == 2 {
		return strings.EqualFold(candidate.Nonce, block.SealFields[1])
	}
	return false
}

// 处理区块数据
func (u *BlockUnlocker) handleBlock(block *rpc.GetBlockReply, candidate *storage.BlockData) error {
	correctHeight, err := strconv.ParseInt(strings.Replace(block.Number, "0x", "", -1), 16, 64)
	if err != nil {
		return err
	}
	candidate.Height = correctHeight
	var reward = big.NewInt(0)
	if u.config.Network == EtcNetwork || u.config.Network == MordorNetwork {
		era := GetBlockEra(big.NewInt(candidate.Height), coinhash.CoinConf.Ecip1017EraRounds)
		reward = coinhash.GetConstReward(era)
		// Add reward for including uncles
		uncleReward := coinhash.GetRewardForUncle(reward)
		rewardForUncles := big.NewInt(0).Mul(uncleReward, big.NewInt(int64(len(block.Uncles))))
		reward.Add(reward, rewardForUncles)

	} else if u.config.Network == UbiqNetwork {
		reward = coinhash.GetConstRewardUbiq(candidate.Height)
		// Add reward for including uncles
		uncleReward := new(big.Int).Div(reward, coinhash.Big32)
		rewardForUncles := big.NewInt(0).Mul(uncleReward, big.NewInt(int64(len(block.Uncles))))
		reward.Add(reward, rewardForUncles)

	} else if u.config.Network == EthNetwork || u.config.Network == RopstenNetwork {
		reward = coinhash.GetConstRewardEthereum(candidate.Height, block)
		// Add reward for including uncles
		uncleReward := new(big.Int).Div(reward, coinhash.Big32)
		rewardForUncles := big.NewInt(0).Mul(uncleReward, big.NewInt(int64(len(block.Uncles))))
		reward.Add(reward, rewardForUncles)
	}

	// Add TX fees
	// 添加打包交易的手续费到reward
	extraTxReward, err := u.getExtraRewardForTx(block)
	if err != nil {
		return fmt.Errorf("Error while fetching TX receipt: %v ", err)
	}
	if u.config.KeepTxFees {
		candidate.ExtraReward = extraTxReward
	} else {
		reward.Add(reward, extraTxReward)
	}

	candidate.Orphan = false
	candidate.Hash = block.Hash
	candidate.Reward = reward

	return nil
}

func handleUncle(height int64, uncle *rpc.GetBlockReply, candidate *storage.BlockData, cfg *UnlockerConfig) error {
	uncleHeight, err := strconv.ParseInt(strings.Replace(uncle.Number, "0x", "", -1), 16, 64)
	if err != nil {
		return err
	}
	var reward = big.NewInt(0)
	if cfg.Network == EtcNetwork || cfg.Network == MordorNetwork {
		era := GetBlockEra(big.NewInt(height), coinhash.CoinConf.Ecip1017EraRounds)
		reward = coinhash.GetUncleReward(
			new(big.Int).SetInt64(uncleHeight), new(big.Int).SetInt64(height), era, coinhash.GetConstReward(era))
	} else if cfg.Network == UbiqNetwork {
		reward = coinhash.GetUncleRewardUbiq(
			new(big.Int).SetInt64(uncleHeight), new(big.Int).SetInt64(height), coinhash.GetConstRewardUbiq(height))
	} else if cfg.Network == EthNetwork || cfg.Network == RopstenNetwork {
		reward = coinhash.GetUncleRewardEthereum(
			new(big.Int).SetInt64(uncleHeight),
			new(big.Int).SetInt64(height),
			coinhash.GetStaticBlockRewardForETH(new(big.Int).SetInt64(height)),
		)
	}
	candidate.Height = height
	candidate.UncleHeight = uncleHeight
	candidate.Orphan = false
	candidate.Hash = uncle.Hash
	candidate.Reward = reward
	return nil
}

// 解锁待办（未成熟）区块
func (u *BlockUnlocker) unlockPendingBlocks() {
	if u.halt {
		logger.Error("Unlocking suspended due to last critical error: %v", u.lastFail)
		return
	}

	current, err := u.rpc.GetLatestBlock()
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Unable to get current blockchain height from node: %v", err)
		return
	}
	currentHeight, err := strconv.ParseInt(strings.Replace(current.Number, "0x", "", -1), 16, 64)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Can't parse pending block number: %v", err)
		return
	}

	candidates, err := u.backend.GetCandidates(currentHeight - u.config.ImmatureDepth)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Failed to get block candidates from backend: %v", err)
		return
	}

	if len(candidates) == 0 {
		logger.Info("No block candidates to unlock")
		return
	}

	result, err := u.unlockCandidates(candidates)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Failed to unlock blocks: %v", err)
		return
	}
	logger.Info("Immature %v blocks, %v uncles, %v orphans", result.blocks, result.uncles, result.orphans)

	err = u.backend.WritePendingOrphans(result.orphanedBlocks)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Failed to insert orphaned blocks into backend: %v", err)
		return
	} else if result.orphans > 0 { // 有孤块则警告
		logger.Warn("Inserted %d orphaned blocks to backend", result.orphans)
	}
	logger.Debug("There are no orphan blocks, insert %d orphan blocks into the backend", result.orphans)

	totalRevenue := new(big.Rat)
	totalMinersProfit := new(big.Rat)
	totalPoolProfit := new(big.Rat)

	for _, block := range result.maturedBlocks {
		revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewards(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			logger.Error("Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		err = u.backend.WriteImmatureBlock(block, roundRewards)
		if err != nil {
			u.halt = true
			u.lastFail = err
			logger.Error("Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		totalRevenue.Add(totalRevenue, revenue)
		totalMinersProfit.Add(totalMinersProfit, minersProfit)
		totalPoolProfit.Add(totalPoolProfit, poolProfit)

		logEntry := fmt.Sprintf(
			"IMMATURE %v: revenue %v, miners profit %v, pool profit: %v",
			block.RoundKey(),
			util.FormatRatReward(revenue),
			util.FormatRatReward(minersProfit),
			util.FormatRatReward(poolProfit),
		)
		entries := []string{logEntry}
		for login, reward := range roundRewards {
			entries = append(entries, fmt.Sprintf("\tREWARD %v: %v: %v Shannon", block.RoundKey(), login, reward))
		}
		logger.Info(strings.Join(entries, "\t"))
	}

	logger.Info(
		"IMMATURE SESSION: revenue %v, miners profit %v, pool profit: %v",
		util.FormatRatReward(totalRevenue),
		util.FormatRatReward(totalMinersProfit),
		util.FormatRatReward(totalPoolProfit),
	)
}

// 解锁成熟的块
func (u *BlockUnlocker) unlockAndCreditMiners() {
	if u.halt {
		logger.Error("Unlocking suspended due to last critical error: %v", u.lastFail)
		return
	}

	current, err := u.rpc.GetLatestBlock()
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Unable to get current blockchain height from node: %v", err)
		return
	}
	currentHeight, err := strconv.ParseInt(strings.Replace(current.Number, "0x", "", -1), 16, 64)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Can't parse pending block number: %v", err)
		return
	}

	immature, err := u.backend.GetImmatureBlocks(currentHeight - u.config.Depth)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Failed to get block candidates from backend: %v", err)
		return
	}

	if len(immature) == 0 {
		logger.Info("No immature blocks to credit miners")
		return
	}

	result, err := u.unlockCandidates(immature)
	if err != nil {
		u.halt = true
		u.lastFail = err
		logger.Error("Failed to unlock blocks: %v", err)
		return
	}
	logger.Info("Unlocked %v blocks, %v uncles, %v orphans", result.blocks, result.uncles, result.orphans)

	for _, block := range result.orphanedBlocks {
		err = u.backend.WriteOrphan(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			logger.Error("Failed to insert orphaned block into backend: %v", err)
			return
		}
	}
	logger.Info("Inserted %v orphaned blocks to backend", result.orphans)

	totalRevenue := new(big.Rat)
	totalMinersProfit := new(big.Rat)
	totalPoolProfit := new(big.Rat)

	for _, block := range result.maturedBlocks {
		revenue, minersProfit, poolProfit, roundRewards, err := u.calculateRewards(block)
		if err != nil {
			u.halt = true
			u.lastFail = err
			logger.Error("Failed to calculate rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		err = u.backend.WriteMaturedBlock(block, roundRewards)
		if err != nil {
			u.halt = true
			u.lastFail = err
			logger.Error("Failed to credit rewards for round %v: %v", block.RoundKey(), err)
			return
		}
		totalRevenue.Add(totalRevenue, revenue)
		totalMinersProfit.Add(totalMinersProfit, minersProfit)
		totalPoolProfit.Add(totalPoolProfit, poolProfit)

		logEntry := fmt.Sprintf(
			"MATURED %v: revenue %v, miners profit %v, pool profit: %v",
			block.RoundKey(),
			util.FormatRatReward(revenue),
			util.FormatRatReward(minersProfit),
			util.FormatRatReward(poolProfit),
		)
		entries := []string{logEntry}
		for login, reward := range roundRewards {
			entries = append(entries, fmt.Sprintf("\tREWARD %v: %v: %v Shannon", block.RoundKey(), login, reward))
		}
		logger.Info(strings.Join(entries, "\t"))
	}

	logger.Info(
		"MATURE SESSION: revenue %v, miners profit %v, pool profit: %v",
		util.FormatRatReward(totalRevenue),
		util.FormatRatReward(totalMinersProfit),
		util.FormatRatReward(totalPoolProfit),
	)
}

// 收益计算
func (u *BlockUnlocker) calculateRewards(block *storage.BlockData) (*big.Rat, *big.Rat, *big.Rat, map[string]int64, error) {
	revenue := new(big.Rat).SetInt(block.Reward)
	minersProfit, poolProfit := chargeFee(revenue, u.config.PoolFee)

	shares, err := u.backend.GetRoundShares(block.RoundHeight, block.Nonce)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// PPLNS 奖励计算
	rewards := calculateRewardsForShares(shares, block.TotalShares, minersProfit)

	if block.ExtraReward != nil {
		extraReward := new(big.Rat).SetInt(block.ExtraReward)
		poolProfit.Add(poolProfit, extraReward)
		revenue.Add(revenue, extraReward)
	}

	if len(u.config.PoolFeeAddress) != 0 {
		address := strings.ToLower(u.config.PoolFeeAddress)
		rewards[address] += weiToShannonInt64(poolProfit)
	}

	return revenue, minersProfit, poolProfit, rewards, nil
}

// PPLNS 根据每个钱包地址shares(key)的共享哈希shares(value)，按百分比分配收益
func calculateRewardsForShares(shares map[string]int64, total int64, reward *big.Rat) map[string]int64 {
	rewards := make(map[string]int64)

	for login, n := range shares {
		percent := big.NewRat(n, total)
		workerReward := new(big.Rat).Mul(reward, percent)
		rewards[login] += weiToShannonInt64(workerReward)
	}
	return rewards
}

// Returns new value after fee deduction and fee value.
//  扣除费用和费用值后返回新值
func chargeFee(value *big.Rat, fee float64) (*big.Rat, *big.Rat) {
	feePercent := new(big.Rat).SetFloat64(fee / 100)
	feeValue := new(big.Rat).Mul(value, feePercent)
	return new(big.Rat).Sub(value, feeValue), feeValue
}

// 单位转换，Wei转换为Shannon(GWei)
func weiToShannonInt64(wei *big.Rat) int64 {
	shannon := new(big.Rat).SetInt(util.Shannon)
	inShannon := new(big.Rat).Quo(wei, shannon)
	value, _ := strconv.ParseInt(inShannon.FloatString(0), 10, 64)
	return value
}

// GetBlockEra gets which "Era" a given block is within, given an era length (ecip-1017 has era=5,000,000 blocks)
// Returns a zero-index era number, so "Era 1": 0, "Era 2": 1, "Era 3": 2 ...
func GetBlockEra(blockNum, eraLength *big.Int) *big.Int {
	// If genesis block or impossible negative-numbered block, return zero-val.
	if blockNum.Sign() < 1 {
		return new(big.Int)
	}

	remainder := big.NewInt(0).Mod(big.NewInt(0).Sub(blockNum, big.NewInt(1)), eraLength)
	base := big.NewInt(0).Sub(blockNum, remainder)

	d := big.NewInt(0).Div(base, eraLength)
	dremainder := big.NewInt(0).Mod(d, big.NewInt(1))

	return new(big.Int).Sub(d, dremainder)
}

// ethash, etchash, ubqhash
func (u *BlockUnlocker) getExtraRewardForTx(block *rpc.GetBlockReply) (*big.Int, error) {
	amount := new(big.Int)

	for _, tx := range block.Transactions {
		receipt, err := u.rpc.GetTxReceipt(tx.Hash)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			gasUsed := util.String2Big(receipt.GasUsed)
			gasPrice := util.String2Big(tx.GasPrice)
			fee := new(big.Int).Mul(gasUsed, gasPrice)
			amount.Add(amount, fee)
		}
	}
	return amount, nil
}
