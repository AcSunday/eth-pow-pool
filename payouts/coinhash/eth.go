package coinhash

import (
	"math/big"

	"github.com/etclabscore/core-pool/rpc"
	"github.com/etclabscore/core-pool/util"
)

// eth挖矿算法配置

type EthConfig struct {
	ByzantiumHardForkHeight      *big.Int `json:"byzantiumFBlock"`      // 拜占庭硬分叉高度
	ConstantinopleHardForkHeight *big.Int `json:"constantinopleFBlock"` // 君士坦丁堡硬分叉高度
	LondonHardForkHeight         *big.Int `json:"londonFBlock"`         // 伦敦硬分叉高度
}

// params for ethash
var (
	FrontierBlockReward       = big.NewInt(5e+18) // 前沿区块奖励
	ByzantiumBlockReward      = big.NewInt(3e+18) // 拜占庭区块奖励
	ConstantinopleBlockReward = big.NewInt(2e+18) // 君士坦丁堡区块奖励
)

// 计算某个区块的燃烧费用，伦敦硬分叉后新增燃烧费用
func CalcLondonBurntFees(block *rpc.GetBlockReply) (BurntFees *big.Int) {
	// 计算公式 burntFees = baseFeePerGas * gasUsed
	baseFeePerGas := util.String2Big(block.BaseFeePerGas)
	gasUsed := util.String2Big(block.GasUsed)
	BurntFees = new(big.Int).Mul(baseFeePerGas, gasUsed)
	return
}

// 获取ETH静态区块奖励
//  headerNumber指区块高度
func GetStaticBlockRewardForETH(headerNumber *big.Int) *big.Int {
	// Select the correct block reward based on chain progression
	blockReward := FrontierBlockReward
	if CoinConf.ByzantiumHardForkHeight.Cmp(headerNumber) <= 0 { // 拜占庭硬分叉区块奖励变更
		blockReward = ByzantiumBlockReward
	}
	if CoinConf.ConstantinopleHardForkHeight.Cmp(headerNumber) <= 0 { // 君士坦丁堡硬分叉区块奖励变更
		blockReward = ConstantinopleBlockReward
	}
	return blockReward
}

// ethash 正常区块奖励计算
func GetConstRewardEthereum(height int64, block *rpc.GetBlockReply) *big.Int {
	headerNumber := big.NewInt(height)
	blockReward := GetStaticBlockRewardForETH(headerNumber)
	if CoinConf.LondonHardForkHeight.Cmp(headerNumber) <= 0 { // 伦敦硬分叉，计算燃烧费用(块奖励 减去 燃烧数量)
		burntFees := CalcLondonBurntFees(block)
		blockReward = new(big.Int).Sub(blockReward, burntFees)
	}
	// Accumulate the rewards for the miner and any included uncles
	reward := new(big.Int).Set(blockReward)
	return reward
}

// ethash 计算叔块奖励
func GetUncleRewardEthereum(uHeight *big.Int, height *big.Int, reward *big.Int) *big.Int {
	r := new(big.Int)
	r.Add(uHeight, Big8)
	r.Sub(r, height)
	r.Mul(r, reward)
	r.Div(r, Big8)
	if r.Cmp(big.NewInt(0)) < 0 {
		r = big.NewInt(0)
	}

	return r
}
