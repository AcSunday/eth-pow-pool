package coinhash

import (
	"math/big"
)

// coin hash 配置集合
//  针对分叉变更的特殊标识，例如分叉后区块奖励减少

// 所有coin集成封装
type CoinConfig struct {
	EtcConfig
	EthConfig
}

var (
	// 币种配置集合
	CoinConf = new(CoinConfig)

	// 杂项常量
	Big32 = big.NewInt(32)
	Big8  = big.NewInt(8)
	Big2  = big.NewInt(2)
)
