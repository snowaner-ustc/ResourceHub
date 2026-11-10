# ResourceHub

多服务器资源监控（CPU / GPU / 内存 / 磁盘等）：轻量 Agent 采集 + 中央服务 + Web 看板。

## 当前状态

**Phase 3 已合入**：异步目录 Top-N 下钻 + 目录软配额告警（仅提醒，不拦写入）。看板只读可随便看；扫盘可选用运维口令；飞书通知可选。详见下方快速启动。

**文档入口 → [docs/README.md](docs/README.md)**

| 主文档 | 说明 |
|--------|------|
| [docs/PRD.md](docs/PRD.md) | 产品需求 |
| [docs/DESIGN.md](docs/DESIGN.md) | 总体设计 |

专题指南（采集性能、目录软配额等）见 [`docs/guides/`](docs/guides/)。

## 设计要点（摘要）

- **Agent 推送**指标到 Server；看板查询只读已存储快照，不在线扫远程机器  
- **磁盘容量**使用 `statfs` 等 O(1) 接口，**禁止**在热路径使用 `du` / 递归 walk  
- **进程监控**（30s 中路径）：进程总数、Top CPU/RSS、僵尸检测与告警  
- **GPU**：nvidia-smi 采集利用率/显存；无卡则标记 unavailable  
- **历史曲线**：SQLite 短时序，默认保留 7 天；详情页 1h/6h/24h  
- **分组**：注册 `--group` 或详情页修改，总览可筛选  
- **目录分析**：详情页手动触发异步 Top-N 下钻（禁止从 `/` 全盘扫）；报告带 `as_of`，可 `partial`  
- **软配额**：策略页配置 `/home/*/*` 等模板；超限只告警/提醒，不拦写入、不自动删除  

## 快速启动（本地）

```bash
# 1. 启动后端
make server

# 2. 另开终端：启动 agent（自动注册并上报）
make agent

# 3. 另开终端：启动前端
make web
# 浏览器打开 http://127.0.0.1:5173
```

Docker Compose：

```bash
cd deploy && docker compose up --build
# Web: http://127.0.0.1:5173  API: http://127.0.0.1:8080
```

## 仓库结构

```text
agent/    # Go 采集端（/proc + statfs）
server/   # Go API + SQLite + 告警
web/      # React 看板
deploy/   # docker-compose
docs/     # 设计与规范（入口 docs/README.md）
```

扫盘口令（可选）：

```bash
export RESOURCEHUB_OPS_PASSWORD='your-lab-password'
make server
# 打开看板不用登录；点「开始分析」或「立即扫描」时才会要口令
```

飞书告警（可选，不配也能用）：

```bash
# 默认关闭。打开：在飞书群添加「自定义机器人」，把 webhook 填上即可
export RESOURCEHUB_FEISHU_WEBHOOK='https://open.feishu.cn/open-apis/bot/v2/hook/xxxxxxxx'
# 机器人开了签名校验时再配：
export RESOURCEHUB_FEISHU_SECRET='your-sign-secret'
# 临时关掉（webhook 可以留着）：
# export RESOURCEHUB_FEISHU_NOTIFY=0
make server
```

群机器人若要求「自定义关键词」，消息里已带 `ResourceHub`。只在告警新触发、升级或恢复时发一条，不会跟心跳刷屏。

## 后续

Phase 4：高可用、SLO。详见 [docs/DESIGN.md](docs/DESIGN.md)。
