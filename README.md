# core-pool
基于github上的core-pool修改与优化
https://github.com/etclabscore/core-pool

## Features

* 支持 HTTP 和 Stratum 挖掘
* 详细的区块统计数据，包括运气百分比和全额奖励
* 故障转移geth实例：内置geth高可用性
* 工人的单独统计数据：可以突出显示超时的工人，以便矿工可以对钻机进行维护
* 用于统计的 JSON-API
* 新的基于 vue 的 UI（前后端分离）
* 支持以太坊经典、魔多、以太坊、Ropsten 和 Ubiq。

## Building on Linux

环境与依赖关系:

  * go >= 1.13
  * core-geth
  * redis-server >= 2.8.0
  * nodejs >= 4 LTS
  * nginx

**我强烈建议使用 Ubuntu 20.04 LTS**

首先安装  [core-geth](https://geth.ethereum.org/downloads/).

Clone并编译（需要gcc环境）:

    git config --global http.https://gopkg.in.followRedirects true
    git clone https://github.com/etclabscore/core-pool.git
    cd core-pool
    make

安装 redis-server:

    sudo apt update
    sudo apt install redis-server

*默认情况下，受监督的指令设置为 no。 由于您正在运行使用 systemd init 系统的 Ubuntu，请将其更改为 systemd:*

    sudo nano /etc/redis/redis.conf

```bash
. . .

# If you run Redis from upstart or systemd, Redis can interact with your
# supervision tree. Options:
#   supervised no      - no supervision interaction
#   supervised upstart - signal upstart by putting Redis into SIGSTOP mode
#   supervised systemd - signal systemd by writing READY=1 to $NOTIFY_SOCKET
#   supervised auto    - detect upstart or systemd method based on
#                        UPSTART_JOB or NOTIFY_SOCKET environment variables
# Note: these supervision methods only signal "process is ready."
#       They do not enable continuous liveness pings back to your supervisor.
supervised systemd

. . .
```

    sudo systemctl restart redis.service


**确保 redis 绑定到本地主机**

  sudo nano /etc/redis/redis.conf

找到这一行并确保它没有被注释（删除 # 如果存在）:

```bash
bind 127.0.0.1 ::1
```

重启 redis

    sudo systemctl restart redis

## 运行 Pool

    ./build/bin/core-pool config.json

您可以使用 Ubuntu upstart - 检查 <code>upstart.conf</code> 中的示例配置.

### 使用 nginx 提供 API

创建一个 upstream 的转发 API:

    upstream api {
        server 127.0.0.1:8080;
    }

并在之后添加此设置 <code>location /</code>:

    location /api {
        proxy_pass http://api;
    }

### 配置文件说明

配置其实很简单，在更改默认值之前，只需阅读两遍并三思而后行。

**不要直接从本手册中复制配置。 请使用项目中的示例配置，
  否则你会因为 JSON 注释而在运行时出错。**

```javascript
{
  // 为您服务器设置的 CPU 核心数
  "threads": 2,
  // redis 存储中键的前缀
  "coin": "etc",
  // 为每个实例赋予唯一名称
  "name": "main",
  // 币种网络 mordor, classic, ethereum, ropsten 或 ubiq
  "network": "classic",
  // 运行级别：production，testing，dev 三种，只有dev会记录DEBUG日志
  "runlevel": "dev",
  // 最大运行的goroutine数量
  "maxRoutine": 10000,

  "proxy": {
    "enabled": true,

    // 将 HTTP 挖掘服务绑定到此 IP:PORT
    "listen": "0.0.0.0:8888",

    // 仅允许来自矿工的 HTTP 请求的此标头和正文大小
    "limitHeadersSize": 1024,
    "limitBodySize": 256,

    /*   如果您支持 CloudFlare（不推荐）或支持 http-reverse 代理，
       则设置为 true 以启用来自 X-Forwarded-For 标头的 IP 检测。
       仅限高级用户。 使它正确和安全是很棘手的。
    */
    "behindReverseProxy": false,

    // Stratum 协议挖矿配置
    "stratum": {
      "enabled": true,
      // 绑定 stratum 协议的 IP:PORT （走TCP）
      "listen": "0.0.0.0:8008",
      "timeout": "120s",
      "maxConn": 8192,
      // 安全证书验证
      "tls": false,
      "certFile": "/path/to/cert.pem",
      "keyFile": "/path/to/key.pem"
    },

    // 尝试在此时间间隔内，从钱包节点获取新的挖矿job
    "blockRefreshInterval": "120ms",
    "stateUpdateInterval": "3s",
    // 让矿工们共享这个难度
    "difficulty": 2000000000,

    /*   如果 redis 不可用，则向矿工而不是作业回复错误。
       如果矿池出问题并且它们没有设置故障转移，应该为矿工节省电力。
    */
    "healthCheck": true,
    // 检查 redis 多少次失败后，将池标记为生病（有问题）。
    "maxFails": 100,
    // 工人统计数据的 TTL，通常应等于 API 部分的大哈希率窗口（一个长时间的算力平滑窗口期）
    "hashrateExpiration": "3h",

    // 策略配置（如ban掉有问题矿工的策略）
    "policy": {
      // 工作协程数
      "workers": 8,
      "resetInterval": "60m",
      "refreshInterval": "1m",

      "banning": {
        "enabled": false,
        /*   禁止的ipset名称。
           请查看 http://ipset.netfilter.org/ 文档。
        */
        "ipset": "blacklist",
        // 在这段时间后解除禁令（解除黑名单时长）
        "timeout": 1800,
        // 禁止矿工的所有shares中无效share的百分比
        "invalidPercent": 30,
        // 在矿工提交此数量的share后检查
        "checkThreshold": 30,
        // 在此数量的格式错误的请求之后出现错误的矿工
        "malformedLimit": 5
      },
      // 连接速率限制
      "limits": {
        "enabled": false,
        // 初始连接数
        "limit": 30,
        "grace": "5m",
        // 增加每个有效共享上允许的连接数
        "limitJump": 10
      }
    }
  },

  // 为静态网站前端提供 JSON 格式的API数据
  "api": {
    "enabled": true,
    "listen": "0.0.0.0:8080",
    // 在此时间间隔内收集矿工统计数据（哈希率，等...）
    "statsCollectInterval": "5s",
    // 清除陈旧的统计数据间隔
    "purgeInterval": "10m",
    // 每个矿工的快速统计算力估计窗口
    "hashrateWindow": "30m",
    // 时间相对长而比较精确的算力，推荐值 3h 很酷，保持它
    "hashrateLargeWindow": "3h",
    // 收集此数量块的 份额/差异比率 的统计数据
    "luckWindow": [64, 128, 256],
    // 在前端显示的最大付款数
    "payments": 50,
    // 在前端显示的最大块数
    "blocks": 50,

    /*    如果您在不同的服务器上运行 API 节点，该模块正在从 redis 可写从属服务器读取数据，
        则必须在启用此选项的情况下运行 api 实例，以便从主 redis 节点清除哈希率统计信息。
        如果您使用 redis slaves 进行分发，则只有 redis 可写 slave 才能正常工作。
        很先进。 通常所有模块应该共享同一个 redis 实例。
    */
    "purgeOnly": false
  },

  // 检查此时间间隔内每个节点的健康状况
  "upstreamCheckInterval": "5s",

  /*    要轮询新作业的奇偶校验节点列表。 
      池将尝试从第一个活着的钱包节点开始工作，并在后台检查备份失败。
      池的当前块模板确实总是缓存在 RAM 中。
  */
  "upstream": [
    {
      "name": "main",
      "url": "http://127.0.0.1:8545",
      "timeout": "10s"
    },
    {
      "name": "backup",
      "url": "http://127.0.0.2:8545",
      "timeout": "10s"
    }
  ],

  // 这是标准的 redis 连接选项
  "redis": {
    // 您的 redis 实例在哪个IP:PORT侦听命令
    "endpoint": "127.0.0.1:6379",
    "poolSize": 10,
    "database": 0,
    "password": ""
  },

  // 该模块定期统计挖到的块是否成熟，并计算每个矿工应得的奖励
  "unlocker": {
    "enabled": false,
    // 池手续费的百分比，1.0 为 1%
    "poolFee": 1.0,
    // 池费受益人地址（留空以禁用费用提取）
    "poolFeeAddress": "",
    // 将池手续费的 10% 捐赠给开发商
    "donate": true,
    // 仅当挖回此数量的区块时才解锁
    "depth": 120,
    // 只需不要碰这个选项
    "immatureDepth": 20,
    // 将开采的交易费用保留为矿池费用
    "keepTxFees": false,
    // 在此时间间隔内运行解锁器unlocker
    "interval": "10m",
    // 用于解锁块的奇偶校验节点 rpc 端点
    "daemon": "http://127.0.0.1:8545",
    // 超时时间：如果无法达到奇偶校验，则上升错误
    "timeout": "10s"
  },

  // 此模块将给矿工支付它应得的区块奖励
  "payouts": {
    "enabled": false,
    // 转账时需要检查钱包节点上 最少连接上多少个节点
    "requirePeers": 25,
    // 在此时间间隔内进行付款给矿工
    "interval": "12h",
    // 用于支付处理的奇偶节点 rpc 端点
    "daemon": "http://127.0.0.1:8545",
    // 超时时间：如果无法达到奇偶校验，则上升错误
    "timeout": "10s",
    // 池聚合挖矿的基本钱包地址，也用于支付给矿工
    "address": "0x0",
    // 自动gas费：让网络来确定 gas 和 gasPrice
    "autoGas": true,
    // 支付交易的 Gas 数量和价格（仅限高级用户）
    "gas": "21000",
    "gasPrice": "50000000000",
    // 仅当矿工余额 >= 0.5 Ether 时才发送付款
    "threshold": 500000000,
    // 支付会话成功后在 Redis 上执行 BGSAVE
    "bgsave": false
  },

  // 日志配置
  "logger": {
        "logPath": "./demo.log",
        "errLogPath": "./demo_err.log",
        // 保存多少（天）内的日志
        "saveDays": 7,
        // 日志切割的时间间隔
        "cutInterval": 86400
  }
}
```

如果您要将池部署分发到多个服务器或进程，
在每台服务器上创建几个配置并禁用不需要的模块。 （高级用户）

我推荐这个部署策略:

* 挖矿实例 - x1（视情况而定，您可以为欧盟运行一个节点，为美国运行一个节点，为亚洲运行一个节点）
* 解锁器和支付实例 - 各 x1（最好这样！）
* API 实例 - x1

### 备注

* 解锁和支付是顺序的，第一个交易开始，第二个等待第一个确认等等。 您可以在代码中禁用它。 仔细阅读`docs/PAYOUTS.md`。
* 另外，请记住**在后端或节点 RPC 错误的情况下将停止解锁和支付**。 在这种情况下，检查一切并重新启动。
* 如果您看到带有 **suspended** 字样的错误，您必须重新启动模块。
* 不要将支付和解锁模块作为挖矿节点的一部分运行。 为两者创建单独的配置，独立启动并确保每个模块都有一个运行的实例。
* 如果未指定`poolFeeAddress`，则所有池利润将保留在coinbase 地址上。 如果有指定，请确保定期发送一些付款所需的灰尘。

### 前端方面

请看 https://github.com/etclabscore/core-pool-interface
