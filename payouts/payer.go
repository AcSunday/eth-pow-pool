package payouts

import (
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/etclabscore/core-pool/common"
	"github.com/etclabscore/core-pool/library/logger"
	"github.com/etclabscore/core-pool/rpc"
	"github.com/etclabscore/core-pool/storage"
	"github.com/etclabscore/core-pool/util"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// 支付模块

const txCheckInterval = 5 * time.Second

type PayoutsConfig struct {
	Enabled      bool   `json:"enabled"`
	RequirePeers int64  `json:"requirePeers"`
	Interval     string `json:"interval"`
	Daemon       string `json:"daemon"`
	Timeout      string `json:"timeout"`
	Address      string `json:"address"`
	Gas          string `json:"gas"`
	GasPrice     string `json:"gasPrice"`
	AutoGas      bool   `json:"autoGas"`
	// In Shannon
	Threshold int64 `json:"threshold"`
	BgSave    bool  `json:"bgsave"`
}

func (self PayoutsConfig) GasHex() string {
	x := util.String2Big(self.Gas)
	return hexutil.EncodeBig(x)
}

func (self PayoutsConfig) GasPriceHex() string {
	x := util.String2Big(self.GasPrice)
	return hexutil.EncodeBig(x)
}

type PayoutsProcessor struct {
	config   *PayoutsConfig
	backend  *storage.RedisClient
	rpc      *rpc.RPCClient
	halt     bool
	lastFail error
}

func NewPayoutsProcessor(cfg *PayoutsConfig, backend *storage.RedisClient) *PayoutsProcessor {
	u := &PayoutsProcessor{config: cfg, backend: backend}
	u.rpc = rpc.NewRPCClient("PayoutsProcessor", cfg.Daemon, cfg.Timeout)
	return u
}

func (u *PayoutsProcessor) Start() {
	logger.Info("Starting payouts")

	// 检查是否需要进入维护模式
	if u.mustResolvePayout() {
		logger.Warn("Running with env RESOLVE_PAYOUT=1, now trying to resolve locked payouts")
		u.resolvePayouts()
		logger.Error("Now you have to restart payouts module with RESOLVE_PAYOUT=0 for normal run")
		return
	}

	intv := util.MustParseDuration(u.config.Interval)
	timer := time.NewTimer(intv)
	logger.Info("Set payouts interval to %v", intv)

	// 检查之前是否有支付失败的记录
	payments := u.backend.GetPendingPayments()
	if len(payments) > 0 {
		logger.Error("Previous payout failed, you have to resolve it. List of failed payments: %v",
			formatPendingPayments(payments))
		return
	}

	// 检查支付模块是否被锁，被锁则不能运行支付模块
	locked, err := u.backend.IsPayoutsLocked()
	if err != nil {
		logger.Error("Unable to start payouts: %v", err)
		return
	}
	if locked {
		logger.Info("Unable to start payouts because they are locked")
		return
	}

	// Immediately process payouts after start
	u.process()
	timer.Reset(intv)

	common.RoutineGroup.GoRecover(func() error {
		for {
			select {
			case <-common.RoutineCtx.Done():
				logger.Info("Stopping payouts working module")
				return nil
			case <-timer.C:
				u.process()
				timer.Reset(intv)
			}
		}
	})
}

func (u *PayoutsProcessor) process() {
	if u.halt { // 检查上一次错误
		logger.Error("Payments suspended due to last critical error: %v", u.lastFail)
		return
	}
	mustPay := 0
	minersPaid := 0
	totalAmount := big.NewInt(0)
	payees, err := u.backend.GetPayees()
	if err != nil {
		logger.Error("Error while retrieving payees from backend: %v", err)
		return
	}

	// 循环从backend拿到的所有需要支付的记录，并处理(tips: login是钱包地址)
	for _, login := range payees {
		amount, err := u.backend.GetBalance(login)
		if err != nil {
			err = fmt.Errorf("Get %s balance fail, from backend err: %v", login, err)
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}
		amountInShannon := big.NewInt(amount)

		// Shannon^2 = Wei
		amountInWei := new(big.Int).Mul(amountInShannon, util.Shannon)

		// 校验该地址是否满足支付条件
		if !u.reachedThreshold(amountInShannon) {
			logger.Info("miner %s does not meet the minimum payment, miner has %s GWei, min need %v GWei",
				login, amountInShannon.String(), u.config.Threshold)
			continue
		}
		mustPay++

		// Require active peers before processing
		// 检查钱包连上的节点数是否满足
		if !u.checkPeers() {
			break
		}
		// Require unlocked account
		// 检查付款账户是否处于解锁状态(内部实现是签名操作)
		if !u.isUnlockedAccount() {
			break
		}

		// Check if we have enough funds
		poolBalance, err := u.rpc.GetBalance(u.config.Address)
		if err != nil {
			err = fmt.Errorf("Get pool balance failed, err: %v", err)
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}
		if poolBalance.Cmp(amountInWei) < 0 { // 检查pool余额是否足够支付
			err := fmt.Errorf("Not enough balance for payment, need %s Wei, pool has %s Wei",
				amountInWei.String(), poolBalance.String())
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}

		// Lock payments for current payout
		err = u.backend.LockPayouts(login, amount)
		if err != nil {
			err = fmt.Errorf("Failed to lock payment for %s: %v", login, err)
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}
		logger.Info("Locked payment for %s, %v Shannon", login, amount)

		// Debit miner's balance and update stats
		err = u.backend.UpdateBalance(login, amount)
		if err != nil {
			err = fmt.Errorf("Failed to update balance for %s, %v Shannon: %v", login, amount, err)
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}

		value := hexutil.EncodeBig(amountInWei)
		txHash, err := u.rpc.SendTransaction(u.config.Address, login, u.config.GasHex(), u.config.GasPriceHex(), value, u.config.AutoGas)
		if err != nil {
			err = fmt.Errorf("Failed to send payment to %s, %v Shannon: %v. Check outgoing tx for %s in block explorer and docs/PAYOUTS.md",
				login, amount, err, login)
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}

		// Log transaction hash
		// 将该笔支付写入backend持久化
		err = u.backend.WritePayment(login, txHash, amount)
		if err != nil {
			err = fmt.Errorf("Failed to log payment data for %s, %v Shannon, tx: %s: %v", login, amount, txHash, err)
			logger.Error(err.Error())
			u.halt = true
			u.lastFail = err
			break
		}

		minersPaid++
		totalAmount.Add(totalAmount, big.NewInt(amount))
		logger.Info("Paid %v Shannon to %v, TxHash: %v", amount, login, txHash)

		// Wait for TX confirmation before further payouts
		// 在支付新的交易之前，先等待当前交易确认
		for {
			logger.Info("Waiting for tx confirmation: %v", txHash)
			time.Sleep(txCheckInterval)
			receipt, err := u.rpc.GetTxReceipt(txHash)
			if err != nil {
				logger.Error("Failed to get tx receipt for %v: %v", txHash, err)
				continue
			}
			// Tx has been mined
			if receipt != nil && receipt.Confirmed() {
				if receipt.Successful() {
					logger.Info("Payout tx successful for %s: %s", login, txHash)
				} else {
					logger.Error("Payout tx failed for %s: %s. Address contract throws on incoming tx.", login, txHash)
				}
				break
			}
		}
	}

	if mustPay > 0 {
		logger.Info("Paid total %v Shannon to %v of %v payees", totalAmount, minersPaid, mustPay)
	} else {
		logger.Info("No payees that have reached payout threshold")
	}

	// Save redis state to disk
	if minersPaid > 0 && u.config.BgSave {
		u.bgSave()
	}
}

// 钱包账户签名（用于判断钱包地址是否解锁）
func (self PayoutsProcessor) isUnlockedAccount() bool {
	_, err := self.rpc.Sign(self.config.Address, "0x0")
	if err != nil {
		logger.Error("Unable to process payouts: %v", err)
		return false
	}
	return true
}

// 钱包连接的节点数量检查（用于保证交易确认成功率与速度）
func (self PayoutsProcessor) checkPeers() bool {
	n, err := self.rpc.GetPeerCount()
	if err != nil {
		logger.Error("Unable to start payouts, failed to retrieve number of peers from node: %v", err)
		return false
	}
	if n < self.config.RequirePeers {
		logger.Warn("Unable to start payouts, number of peers on a node is less than required %d", self.config.RequirePeers)
		return false
	}
	return true
}

// 判断是否满足支付的最小值
func (self PayoutsProcessor) reachedThreshold(amount *big.Int) bool {
	return big.NewInt(self.config.Threshold).Cmp(amount) < 0
}

func formatPendingPayments(list []*storage.PendingPayment) string {
	var s string
	for _, v := range list {
		s += fmt.Sprintf("\tAddress: %s, Amount: %v Shannon, %v\n", v.Address, v.Amount, time.Unix(v.Timestamp, 0))
	}
	return s
}

// 持久化支付数据
func (self PayoutsProcessor) bgSave() {
	result, err := self.backend.BgSave()
	if err != nil {
		logger.Error("Failed to perform BGSAVE on backend: %v", err)
		return
	}
	logger.Info("Saving backend state to disk: %s", result)
}

// 解决支付问题处理
func (self PayoutsProcessor) resolvePayouts() {
	payments := self.backend.GetPendingPayments()

	if len(payments) > 0 {
		logger.Info("Will credit back following balances: %s", formatPendingPayments(payments))

		for _, v := range payments {
			err := self.backend.RollbackBalance(v.Address, v.Amount)
			if err != nil {
				logger.Error("Failed to credit %v Shannon back to %s, error is: %v", v.Amount, v.Address, err)
				return
			}
			logger.Info("Credited %v Shannon back to %s", v.Amount, v.Address)
		}
		err := self.backend.UnlockPayouts()
		if err != nil {
			logger.Error("Failed to unlock payouts: %v", err)
			return
		}
	} else {
		logger.Info("No pending payments to resolve")
	}

	if self.config.BgSave {
		self.bgSave()
	}
	logger.Info("Payouts unlocked")
}

// 维护模式: 根据系统环境变量RESOLVE_PAYOUT，判断是否必须要解决支付问题
//  RESOLVE_PAYOUT=1 或 RESOLVE_PAYOUT=0
//  tips: 为1时必须解决，0则不用
func (self PayoutsProcessor) mustResolvePayout() bool {
	v, _ := strconv.ParseBool(os.Getenv("RESOLVE_PAYOUT"))
	return v
}
