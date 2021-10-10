package coinhash

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
)

// etc挖矿算法配置

type EtcConfig struct {
	Ecip1017FBlock    int64    `json:"ecip1017FBlock"`
	Ecip1017EraRounds *big.Int `json:"ecip1017EraRounds"`
}

// params for etchash
var (
	HomesteadReward          = math.MustParseBig256("5000000000000000000")
	DisinflationRateQuotient = big.NewInt(4) // Disinflation rate quotient for ECIP1017
	DisinflationRateDivisor  = big.NewInt(5) // Disinflation rate divisor for ECIP1017
)

// etchash 正常情况奖励
func GetConstReward(era *big.Int) *big.Int {
	var blockReward = HomesteadReward
	wr := GetBlockWinnerRewardByEra(era, blockReward)
	return wr
}

// GetRewardByEra gets a block reward at disinflation rate.
// Constants MaxBlockReward, DisinflationRateQuotient, and DisinflationRateDivisor assumed.
func GetBlockWinnerRewardByEra(era *big.Int, blockReward *big.Int) *big.Int {
	if era.Cmp(big.NewInt(0)) == 0 {
		return new(big.Int).Set(blockReward)
	}

	// MaxBlockReward _r_ * (4/5)**era == MaxBlockReward * (4**era) / (5**era)
	// since (q/d)**n == q**n / d**n
	// qed
	var q, d, r = new(big.Int), new(big.Int), new(big.Int)

	q.Exp(DisinflationRateQuotient, era, nil)
	d.Exp(DisinflationRateDivisor, era, nil)

	r.Mul(blockReward, q)
	r.Div(r, d)

	return r
}

//etchash 获取包含的叔块奖励
func GetRewardForUncle(blockReward *big.Int) *big.Int {
	return new(big.Int).Div(blockReward, Big32) //return new(big.Int).Div(reward, new(big.Int).SetInt64(32))
}

// etchash 计算叔块奖励
func GetUncleReward(uHeight *big.Int, height *big.Int, era *big.Int, reward *big.Int) *big.Int {
	// Era 1 (index 0):
	//   An extra reward to the winning miner for including uncles as part of the block,
	//  in the form of an extra 1/32 (0.15625ETC) per uncle included, up to a maximum of two (2) uncles.
	if era.Cmp(big.NewInt(0)) == 0 {
		r := new(big.Int)
		r.Add(uHeight, Big8) // 2,534,998 + 8              = 2,535,006
		r.Sub(r, height)     // 2,535,006 - 2,534,999        = 7
		r.Mul(r, reward)     // 7 * 5e+18               = 35e+18
		r.Div(r, Big8)       // 35e+18 / 8                            = 7/8 * 5e+18

		return r
	}
	return GetRewardForUncle(reward)
}
