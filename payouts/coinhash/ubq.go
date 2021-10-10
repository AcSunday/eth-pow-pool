package coinhash

import "math/big"

// ubq挖矿算法配置

// params for ubqhash
var (
	UbiqStartReward = big.NewInt(8e+18)
)

// ubqhash 计算正常情况下的区块奖励
func GetConstRewardUbiq(height int64) *big.Int {
	// Rewards
	reward := new(big.Int).Set(UbiqStartReward)
	headerNumber := big.NewInt(height)

	if headerNumber.Cmp(big.NewInt(358363)) > 0 {
		reward = big.NewInt(7e+18)
		// Year 1
	}
	if headerNumber.Cmp(big.NewInt(716727)) > 0 {
		reward = big.NewInt(6e+18)
		// Year 2
	}
	if headerNumber.Cmp(big.NewInt(1075090)) > 0 {
		reward = big.NewInt(5e+18)
		// Year 3
	}
	if headerNumber.Cmp(big.NewInt(1433454)) > 0 {
		reward = big.NewInt(4e+18)
		// Year 4
	}
	if headerNumber.Cmp(big.NewInt(1791818)) > 0 {
		reward = big.NewInt(3e+18)
		// Year 5
	}
	if headerNumber.Cmp(big.NewInt(2150181)) > 0 {
		reward = big.NewInt(2e+18)
		// Year 6
	}
	if headerNumber.Cmp(big.NewInt(2508545)) > 0 {
		reward = big.NewInt(1e+18)
		// Year 7
	}

	return reward
}

// ubqhash 计算叔块奖励
func GetUncleRewardUbiq(uHeight *big.Int, height *big.Int, reward *big.Int) *big.Int {
	r := new(big.Int)

	r.Add(uHeight, Big2)
	r.Sub(r, height)
	r.Mul(r, reward)
	r.Div(r, Big2)
	if r.Cmp(big.NewInt(0)) < 0 {
		// blocks older than the previous block are not rewarded
		r = big.NewInt(0)
	}

	return r
}
