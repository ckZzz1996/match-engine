# Match Engine - 高性能撮合引擎

一个用 Go 实现的高性能金融交易撮合引擎，支持**现货交易**和**永续合约交易**，包含多种订单类型、连续竞价和集合竞价模式。

## 特性

### 核心撮合能力
- ✅ **价格-时间优先原则** - 同方向订单按价格优先（买高卖低），同价格按时间优先（FIFO）
- ✅ **连续竞价撮合** - 支持实时逐笔撮合
- ✅ **集合竞价撮合** - 支持开盘/收盘集合竞价
- ✅ **部分成交** - 订单可分多次成交，剩余量继续挂单
- ✅ **完全成交** - 订单一次性全部匹配
- ✅ **自动撤单** - 成交后自动移除已完成订单
- ✅ **成交价格确定规则** - 以被动方（Maker）价格成交
- ✅ **多交易对支持** - 同时处理多个交易对

### 订单类型
- ✅ **限价单（Limit Order）** - 指定价格和数量
- ✅ **市价单（Market Order）** - 以当前最优价格成交
- ✅ **IOC（Immediate or Cancel）** - 立即成交可成交部分，剩余取消
- ✅ **FOK（Fill or Kill）** - 必须全部立即成交，否则整单取消
- ✅ **止损单（Stop Order）** - 触发价触发后转为市价单
- ✅ **止盈单（Take-Profit）** - 锁定利润
- ✅ **冰山单（Iceberg Order）** - 隐藏大额订单
- ✅ **Post-Only** - 仅作为 Maker，否则拒绝

### 合约交易功能
- ✅ **永续合约** - 支持永续合约交易
- ✅ **双向持仓** - 支持多头/空头独立持仓
- ✅ **杠杆交易** - 支持 1-125 倍杠杆
- ✅ **全仓/逐仓** - 支持两种保证金模式
- ✅ **仓位管理** - 开仓、加仓、减仓、平仓
- ✅ **止盈止损** - 仓位级别 TP/SL 设置
- ✅ **强制平仓** - 保证金不足自动强平
- ✅ **ADL（自动减仓）** - 强平失败时触发
- ✅ **资金费率** - 8小时结算周期
- ✅ **标记价格** - 用于强平和未实现盈亏计算
- ✅ **保险基金** - 穿仓损失覆盖

### 订单管理
- ✅ **内存订单簿（Order Book）** - 实时维护买卖盘深度
- ✅ **订单生命周期管理** - 创建 → 挂单 → 成交 → 归档
- ✅ **用户挂单查询** - 按用户 ID 查询未成交订单
- ✅ **订单状态同步** - 实时广播订单状态变更
- ✅ **快照（Snapshot）** - 定期生成订单簿全量快照
- ✅ **增量更新（Delta）** - 推送订单簿变化

### 性能优化
- ✅ **微秒级撮合延迟** - 单笔订单处理延迟目标 < 100 微秒
- ✅ **高吞吐量设计** - 支持高并发订单处理
- ✅ **事件驱动架构** - 避免线程竞争
- ✅ **批处理优化** - 消息批量发送

### 接口支持
- ✅ **gRPC 接口** - 订单服务、行情服务、管理服务、合约服务
- ✅ **WebSocket** - 实时行情推送
- ✅ **NATS 消息队列** - 成交广播
- ✅ **管理 API** - 动态配置、强制撤单、暂停撮合
- ✅ **审计日志** - 记录所有关键操作
- ✅ **Prometheus 监控** - 延迟、TPS、队列长度等指标

## 快速开始

### 环境要求
- Go 1.23+
- 可选：protoc（用于生成 gRPC 代码）
- 可选：Docker

### 安装

```bash
# 克隆项目
git clone <repository-url>
cd match-engine

# 下载依赖
go mod tidy

# 编译
make build
```

### 运行

```bash
# 生成默认配置
make init-config

# 启动服务
make run

# 或开发模式
make run-dev
```

### Docker 部署

```bash
# 构建镜像
make docker-build

# 运行容器
make docker-run
```

## 项目结构

```
match-engine/
├── api/
│   └── proto/                 # Protobuf 定义
├── cmd/
│   └── server/               # 主程序入口
├── internal/
│   ├── audit/                # 审计日志
│   ├── config/               # 配置管理
│   ├── metrics/              # 监控指标
│   ├── mq/
│   │   └── nats/            # NATS 消息队列
│   ├── server/
│   │   ├── grpc/            # gRPC 服务
│   │   └── websocket/       # WebSocket 服务
│   └── snapshot/            # 快照管理
├── pkg/
│   ├── engine/              # 撮合引擎核心
│   │   ├── match_engine.go      # 现货撮合引擎
│   │   ├── contract_engine.go   # 合约撮合引擎
│   │   ├── auction_engine.go    # 集合竞价引擎
│   │   └── manager.go           # 引擎管理器
│   ├── orderbook/           # 订单簿
│   ├── position/            # 仓位管理
│   ├── liquidation/         # 强平引擎
│   ├── funding/             # 资金费率服务
│   └── types/               # 类型定义
├── snapshots/               # 快照存储目录
├── config.json              # 配置文件
├── Dockerfile
├── Makefile
└── README.md
```

## API 使用

### gRPC 接口

#### 现货订单服务

```protobuf
// 创建订单
rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
// 取消订单
rpc CancelOrder(CancelOrderRequest) returns (CancelOrderResponse);
// 查询订单
rpc GetOrder(GetOrderRequest) returns (GetOrderResponse);
// 查询用户订单
rpc GetUserOrders(GetUserOrdersRequest) returns (GetUserOrdersResponse);
```

#### 合约订单服务

```protobuf
// 创建合约订单
rpc CreateContractOrder(CreateContractOrderRequest) returns (CreateContractOrderResponse);
// 平仓
rpc ClosePosition(ClosePositionRequest) returns (ClosePositionResponse);
// 获取仓位
rpc GetPosition(GetPositionRequest) returns (GetPositionResponse);
// 获取用户所有仓位
rpc GetUserPositions(GetUserPositionsRequest) returns (GetUserPositionsResponse);
// 修改杠杆
rpc ChangeLeverage(ChangeLeverageRequest) returns (ChangeLeverageResponse);
// 修改保证金模式
rpc ChangeMarginType(ChangeMarginTypeRequest) returns (ChangeMarginTypeResponse);
// 设置止盈止损
rpc SetPositionTPSL(SetPositionTPSLRequest) returns (SetPositionTPSLResponse);
// 获取资金费率
rpc GetFundingRate(GetFundingRateRequest) returns (GetFundingRateResponse);
// 获取标记价格
rpc GetMarkPrice(GetMarkPriceRequest) returns (GetMarkPriceResponse);
```

#### 行情服务

```protobuf
// 查询订单簿
rpc GetOrderBook(GetOrderBookRequest) returns (GetOrderBookResponse);
// 订阅订单簿更新
rpc SubscribeOrderBook(SubscribeOrderBookRequest) returns (stream OrderBookUpdate);
// 订阅成交
rpc SubscribeTrades(SubscribeTradesRequest) returns (stream Trade);
```

### WebSocket

连接地址：`ws://localhost:8081/ws`

#### 订阅交易对
```json
{"action": "subscribe", "symbol": "BTC-USDT"}
```

#### 获取快照
```json
{"action": "snapshot", "symbol": "BTC-USDT"}
```

## 合约交易说明

### 仓位计算

```
# 保证金 = 仓位价值 / 杠杆
Margin = Position Value / Leverage

# 多头强平价 = 开仓价 × (1 - 1/杠杆 + 维持保证金率)
Long Liquidation Price = Entry Price × (1 - 1/Leverage + MMR)

# 空头强平价 = 开仓价 × (1 + 1/杠杆 - 维持保证金率)
Short Liquidation Price = Entry Price × (1 + 1/Leverage - MMR)

# 未实现盈亏（多头）= (标记价 - 开仓价) × 数量
Unrealized PnL (Long) = (Mark Price - Entry Price) × Quantity

# 未实现盈亏（空头）= (开仓价 - 标记价) × 数量
Unrealized PnL (Short) = (Entry Price - Mark Price) × Quantity
```

### 资金费率

- 结算时间：每 8 小时（00:00, 08:00, 16:00 UTC）
- 费率计算：基于溢价指数和利率
- 费率范围：-0.75% ~ +0.75%
- 多头支付/空头收取（费率为正时）

### 止盈止损

- 支持仓位级别设置
- 多头止盈价必须高于标记价，止损价必须低于标记价
- 空头止盈价必须低于标记价，止损价必须高于标记价
- 触发后自动执行市价平仓

## 配置说明

```json
{
  "server": {
    "grpc_addr": ":50051",
    "websocket_addr": ":8081"
  },
  "engine": {
    "event_buffer_size": 100000,
    "command_buffer_size": 100000
  },
  "metrics": {
    "enabled": true,
    "addr": ":9090"
  },
  "symbols": ["BTC-USDT", "ETH-USDT"],
  "contracts": [
    {
      "symbol": "BTC-USDT-PERP",
      "base_asset": "BTC",
      "quote_asset": "USDT",
      "contract_type": "PERPETUAL",
      "max_leverage": 125,
      "maker_fee_rate": "0.0002",
      "taker_fee_rate": "0.0004",
      "maintenance_rate": "0.004"
    }
  ]
}
```

## 监控

### Prometheus 指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| matchengine_orders_received_total | Counter | 收到的订单总数 |
| matchengine_trades_executed_total | Counter | 成交总数 |
| matchengine_match_latency_microseconds | Histogram | 撮合延迟 |
| matchengine_orderbook_depth | Gauge | 订单簿深度 |
| matchengine_pending_orders | Gauge | 待处理订单数 |
| matchengine_positions_total | Gauge | 持仓总数 |
| matchengine_liquidations_total | Counter | 强平总数 |
| matchengine_funding_settlements_total | Counter | 资金费率结算次数 |

### 健康检查

- 健康检查：`GET http://localhost:8081/health`
- 就绪检查：`GET http://localhost:8081/ready`

## 测试

```bash
# 运行测试
make test

# 运行基准测试
make bench

# 生成覆盖率报告
make coverage
```

## 性能基准

在典型硬件（Intel i7, 16GB RAM）上的性能参考：

| 操作 | 延迟（p99） | 吞吐量 |
|------|-------------|--------|
| 现货订单处理 | < 50 μs | 100K+ ops/s |
| 合约订单处理 | < 80 μs | 80K+ ops/s |
| 订单簿快照 | < 100 μs | 10K+ ops/s |
| 订单添加 | < 10 μs | 500K+ ops/s |
| 仓位更新 | < 20 μs | 200K+ ops/s |

## 架构图

```
                    ┌─────────────────────────────────────────────────┐
                    │                   API Layer                      │
                    │  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │
                    │  │   gRPC   │  │WebSocket │  │  NATS MQ     │  │
                    │  └────┬─────┘  └────┬─────┘  └──────┬───────┘  │
                    └───────┼─────────────┼───────────────┼──────────┘
                            │             │               │
                    ┌───────▼─────────────▼───────────────▼──────────┐
                    │              Engine Manager                     │
                    │  ┌──────────────────────────────────────────┐  │
                    │  │           Event Channel                   │  │
                    │  └──────────────────────────────────────────┘  │
                    │                                                 │
                    │  ┌─────────────┐        ┌───────────────────┐  │
                    │  │ Spot Engine │        │  Contract Engine  │  │
                    │  │             │        │                   │  │
                    │  │ ┌─────────┐ │        │ ┌───────────────┐ │  │
                    │  │ │OrderBook│ │        │ │  OrderBook    │ │  │
                    │  │ └─────────┘ │        │ └───────────────┘ │  │
                    │  │             │        │ ┌───────────────┐ │  │
                    │  │ ┌─────────┐ │        │ │Position Mgr   │ │  │
                    │  │ │ Auction │ │        │ └───────────────┘ │  │
                    │  │ └─────────┘ │        │ ┌───────────────┐ │  │
                    │  │             │        │ │Liquidation Eng│ │  │
                    │  │             │        │ └───────────────┘ │  │
                    │  │             │        │ ┌───────────────┐ │  │
                    │  │             │        │ │Funding Service│ │  │
                    │  │             │        │ └───────────────┘ │  │
                    │  └─────────────┘        └───────────────────┘  │
                    └─────────────────────────────────────────────────┘
```

## License

MIT License
