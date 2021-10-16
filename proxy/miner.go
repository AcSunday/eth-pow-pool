package proxy

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/payouts"
	"github.com/etclabscore/core-pool/util"

	"github.com/etclabscore/go-etchash"
	"github.com/ethereum/go-ethereum/common"
)

var (
	maxUint256                             = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
	ecip1099FBlockClassic uint64           = 11700000 // classic mainnet
	ecip1099FBlockMordor  uint64           = 2520000  // mordor
	uip1FEpoch            uint64           = 22       // ubiq mainnet
	hasher                *etchash.Etchash = nil
)

func (s *ProxyServer) processShare(login, id, ip string, t *BlockTemplate, params []string, stratum bool) (bool, bool) {
	if hasher == nil {
		switch s.config.Network {
		case payouts.EtcNetwork:
			hasher = etchash.New(&ecip1099FBlockClassic, nil)
		case payouts.MordorNetwork:
			hasher = etchash.New(&ecip1099FBlockMordor, nil)
		case payouts.UbiqNetwork:
			hasher = etchash.New(nil, &uip1FEpoch)
		case payouts.EthNetwork, payouts.RopstenNetwork:
			hasher = etchash.New(&ecip1099FBlockClassic, nil)
		default:
			// unknown network
			logger.Error("Unknown network configuration %s", s.config.Network)
			return false, false
		}
	}

	if len(params) != 3 {
		logger.Warn("Shared length params must be 3, params: %v", params)
		return false, false
	}
	nonceHex, hashNoNonce, mixDigest := params[0], params[1], params[2]
	nonce, _ := strconv.ParseUint(strings.Replace(nonceHex, "0x", "", -1), 16, 64)
	shareDiff := s.config.Proxy.Difficulty

	var result common.Hash
	if stratum {
		hashNoNonceTmp := common.HexToHash(params[2])

		mixDigestTmp, hashTmp := hasher.Compute(t.Height, hashNoNonceTmp, nonce)
		params[1] = hashNoNonceTmp.Hex()
		params[2] = mixDigestTmp.Hex()
		hashNoNonce = params[1]
		result = hashTmp
	} else {
		hashNoNonceTmp := common.HexToHash(hashNoNonce)
		mixDigestTmp, hashTmp := hasher.Compute(t.Height, hashNoNonceTmp, nonce)

		// check mixDigest
		if mixDigestTmp.Hex() != mixDigest {
			return false, false
		}
		result = hashTmp
	}

	// Block "difficulty" is BigInt
	// NiceHash "difficulty" is float64 ...
	// diffFloat => target; then: diffInt = 2^256 / target
	shareDiffCalc := util.TargetHexToDiff(result.Hex()).Int64()
	shareDiffFloat := util.DiffIntToFloat(shareDiffCalc)
	if shareDiffFloat < 0.0001 {
		logger.Warn("share difficulty too low, %f < %d, from %v@%v", shareDiffFloat, t.Difficulty, login, ip)
		return false, false
	}

	h, ok := t.headers[hashNoNonce]
	if !ok {
		logger.Warn("Stale share from %v@%v", login, ip)
		return false, false
	}

	logger.Debug("Difficulty pool/block/share = %d / %d / %d(%f) from %v@%v", shareDiff, t.Difficulty, shareDiffCalc, shareDiffFloat, login, ip)

	// check share difficulty
	shareTarget := new(big.Int).Div(maxUint256, big.NewInt(shareDiff))
	if result.Big().Cmp(shareTarget) > 0 {
		return false, false
	}

	// check target difficulty
	target := new(big.Int).Div(maxUint256, big.NewInt(h.diff.Int64()))
	if result.Big().Cmp(target) <= 0 {
		ok, err := s.rpc().SubmitBlock(params)
		if err != nil {
			logger.Error("Block submission failure at height %v for %v: %v", h.height, t.Header, err)
		} else if !ok {
			logger.Error("Block rejected at height %v for %v", h.height, t.Header)
			return false, false
		} else {
			s.fetchBlockTemplate()
			exist, err := s.backend.WriteBlock(login, id, params, shareDiff, h.diff.Int64(), h.height, s.hashrateExpiration)
			if exist {
				return true, false
			}
			if err != nil {
				logger.Error("Failed to insert block candidate into backend: %v", err)
			} else {
				logger.Info("Inserted block %v to backend", h.height)
			}
			logger.Info("Block found by miner %v@%v at height %d", login, ip, h.height)
		}
	} else {
		exist, err := s.backend.WriteShare(login, id, params, shareDiff, h.height, s.hashrateExpiration)
		if exist {
			return true, false
		}
		if err != nil {
			logger.Error("Failed to insert share data into backend: %v", err)
		}
	}
	return false, true
}
