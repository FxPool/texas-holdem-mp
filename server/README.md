# Texas Hold'em Server (Go)

后端骨架，本阶段仅占位。运行：

```bash
cd server
go run ./cmd/server
# -> texas-holdem server listening on :8080 (skeleton only — TODO: implement)
```

## 后续阶段路线图

### 阶段 2：基础后端

- [ ] `internal/api/` HTTP 路由：`POST /login`（微信 code2session）、`GET /rooms`、`POST /rooms`
- [ ] `internal/ws/` WebSocket Hub：连接管理、心跳、消息广播
- [ ] `internal/room/` 房间管理：创建、加入、断线重连
- [ ] `internal/model/` 共享数据结构（与前端 `types/game.d.ts` 字段对齐）
- [ ] 协议定义：客户端→服务端 `action`/`join`/`leave`；服务端→客户端 `state-snapshot`/`state-delta`/`event`

### 阶段 3：游戏核心

- [ ] `internal/game/deck.go` 牌堆 + 安全洗牌（crypto/rand）
- [ ] `internal/game/engine.go` 手牌阶段状态机（preflop/flop/turn/river/showdown）
- [ ] `internal/game/hand.go` 牌型识别 + 比较（含同花顺、四条…高牌）
- [ ] `internal/game/pot.go` 主池/边池分配
- [ ] 单元测试覆盖牌型比较与分池场景

### 阶段 4：周边

- [ ] 持久化：Redis（房间状态/在线列表）+ Postgres（用户/战绩）
- [ ] 防作弊：服务端权威，客户端只发动作意图，绝不信任客户端任何金额/牌型计算
- [ ] 限频：玩家每秒动作上限、断线重连灰度

## 合规

- 虚拟筹码定位为娱乐道具，**不可与现金互兑**
- 不开放任何充值/兑换/交易功能
- 在用户协议与小程序简介明确"娱乐性质，禁止赌博"
