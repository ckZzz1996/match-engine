# Match Engine - 高性能撮合引擎

一个用 Go 实现的高性能金融交易撮合引擎，支持多种订单类型、连续竞价和集合竞价模式。

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
- ✅ **gRPC 接口** - 订单服务、行情服务、管理服务
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
│   ├── orderbook/           # 订单簿
│   └── types/               # 类型定义
├── config.json              # 配置文件
├── Dockerfile
├── Makefile
└── README.md
```

## API 使用

### gRPC 接口

#### 创建订单
```protobuf
rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
```

#### 取消订单
```protobuf
rpc CancelOrder(CancelOrderRequest) returns (CancelOrderResponse);
```

#### 查询订单簿
```protobuf
rpc GetOrderBook(GetOrderBookRequest) returns (GetOrderBookResponse);
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

## 配置说明

```json
{
  "server": {
    "grpc_addr": ":50051",      // gRPC 地址
    "websocket_addr": ":8081"   // WebSocket 地址
  },
  "engine": {
    "event_buffer_size": 100000,   // 事件缓冲区大小
    "command_buffer_size": 100000  // 命令缓冲区大小
  },
  "metrics": {
    "enabled": true,
    "addr": ":9090"             // Prometheus 指标地址
  },
  "symbols": ["BTC-USDT", "ETH-USDT"]  // 交易对列表
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
| 订单处理 | < 50 μs | 100K+ ops/s |
| 订单簿快照 | < 100 μs | 10K+ ops/s |
| 订单添加 | < 10 μs | 500K+ ops/s |

## License

MIT License
