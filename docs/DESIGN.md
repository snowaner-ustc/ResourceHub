# ResourceHub 设计方案

> 多服务器资源监控（CPU / GPU / 存储等）— 文档先行版本  
> 状态：Draft · 版本：0.3 · 日期：2026-08-21

---

## 1. 背景与目标

### 1.1 要解决什么问题

运维/研发需要同时查看多台机器的资源占用（CPU、内存、GPU、磁盘等），快速发现瓶颈与容量风险。现有常见痛点：

- 逐台 SSH / 临时脚本查询，体验差、难复用
- 磁盘相关查询（尤其 `du` 扫目录）耗时长、IO 高，容易干扰业务
- 缺少统一告警与历史趋势

### 1.2 产品目标

| 目标 | 说明 |
|------|------|
| 统一看板 | 多机资源一屏总览，可下钻到单机 |
| 低开销采集 | 查询与采集不能明显拖慢业务机，磁盘查询必须快 |
| 可扩展 | 新增机器 = 装 Agent + 登记，无需改核心代码 |
| 可演进 | 先监控与告警，后续可接容量规划、自动清理建议 |

### 1.3 非目标（本期不做）

- 不替代完整 APM / 日志平台（如 Datadog、ELK）
- 不做容器编排层的细粒度调度
- 不做远程执行/改配置（只读监控）
- 不做跨云账单分析

---

## 2. 范围与角色

### 2.1 监控对象（MVP）

| 类别 | 指标 | 备注 |
|------|------|------|
| CPU | 使用率、负载、核数 | 整体 + 可选 per-core |
| 内存 | 总量 / 已用 / available / swap | 优先 `MemAvailable` |
| GPU | 利用率、显存、温度、功耗 | NVIDIA 优先（NVML）；无 GPU 则跳过 |
| 磁盘容量 | 挂载点总容量 / 已用 / 可用 / 使用率 | **仅文件系统级，禁止默认 `du`** |
| 磁盘 IO | 读写吞吐、IOPS、利用率（可选） | 来自 `/proc/diskstats` |
| 网络 | 收发字节/包（可选） | 来自 `/proc/net/dev` |
| 主机元信息 | hostname、OS、uptime、Agent 版本 | 注册与健康检查 |

### 2.2 用户角色

- **Viewer**：只看看板与告警
- **Operator**：管理机器分组、阈值、阈值策略
- **Admin**：鉴权、接入密钥、系统配置

---

## 3. 关键约束：磁盘与采集必须“轻”

这是本项目的**硬约束**，设计与实现都必须可验证。

### 3.1 原则

1. **快路径 vs 慢路径分离**  
   - 快路径：CPU / 内存 / GPU / `statfs`/`df` 级磁盘容量 — 毫秒～数十毫秒级  
   - 慢路径：目录级体积排行、inode 深扫 — **默认关闭**，仅人工触发或低频后台任务，且限流

2. **禁止在热路径上做全盘 `du` / 递归 walk**  
   - 热路径 = Agent 定时采集、Dashboard 刷新、列表页批量查询  
   - 全盘递归会吃满磁盘 IO，且耗时随文件数线性增长

3. **采集侧推送，查询侧读缓存**  
   - 前端/API **不直连** 目标机扫盘  
   - API 只读中央存储里的**最近快照**与短期时序

4. **预算化**  
   - 单次采集 CPU 时间、IO、墙钟时间设上限；超时则跳过慢项并打点告警

### 3.2 磁盘容量：正确做法

| 做法 | 用途 | 预期耗时 | 是否默认开启 |
|------|------|----------|--------------|
| `statfs` / `statvfs`（或等价库）按挂载点查询 | 总/可用/已用空间 | **O(1)，通常 < 5ms/挂载点** | ✅ 是 |
| 读取挂载表过滤伪文件系统 | 排除 `tmpfs`/`proc`/`sysfs`/`overlay` 等噪音 | 极低 | ✅ 是 |
| `df` 命令解析 | 仅作 fallback，不推荐主路径 | 略慢于 syscall | ⚠️ 兜底 |
| `du -x` / 递归统计目录 | 目录排行、找大文件 | **秒～分钟，高 IO** | ❌ 否（慢路径） |

**推荐实现要点：**

```text
for each mount in filtered_mounts:
    usage = statfs(mount.path)   # blocks * bsize → total/free/avail
    emit DiskCapacity{mount, total, used, avail, inodes...}
```

- 过滤规则可配置：包含前缀（如 `/data`）、排除类型（`tmpfs`、`devtmpfs`、`squashfs`…）
- 同一块设备多个挂载点去重（按 `fsid` / device id）
- Windows 后续若支持：用 `GetDiskFreeSpaceEx`，同样是 O(1)

### 3.3 目录级分析与配额（慢路径，Phase 3）

`statfs` 只能回答「盘还剩多少」，不能回答「谁占用」或「某文件夹是否超限」。后两者见专题文档：

→ **[STORAGE_ATTRIBUTION_AND_QUOTA.md](./STORAGE_ATTRIBUTION_AND_QUOTA.md)**

摘要：

| 能力 | 做法 | 路径 |
|------|------|------|
| 定位大户 | 异步 **分层 Top-N 下钻**（每层只统计直接子目录），可选 by-owner / 大文件 Top-N | 慢路径 A |
| 目录限额告警 | **软配额（已定）**：`path_pattern`（如 `/home/*/*`）+ 定向测体积 + 告警/提醒；**不拦写入** | 慢路径 B |

硬约束不变：

| 机制 | 说明 |
|------|------|
| 显式触发 / 低频定时 | 用户点击「分析」或 cron；禁止跟心跳绑定 |
| 范围限制 | 只扫配置 roots / glob 展开目标；禁止从 `/` 无差别扫 |
| 限深 / Top-N | 每层 Top-N；下钻再开新任务 |
| 熔断 | 墙钟超时、`max_files`、`ionice`、不跨挂载点 |
| 结果缓存 | 带 `as_of`；前端展示非实时 |
| 与快路径隔离 | 独立 SlowWorker，绝不阻塞 heartbeat |
| 仅软限制 | 超限只告警/提醒；不做 OS 硬配额、不拦截写入、不自动删除 |

### 3.4 其它指标的低开销策略

| 指标 | 推荐源 | 注意 |
|------|--------|------|
| CPU | `/proc/stat` 两次采样差分，或 `gopsutil` 等 | 采样间隔 ≥ 1s；避免频繁短间隔 |
| 内存 | `/proc/meminfo` | 一次读即可 |
| 负载 | `/proc/loadavg` | 一次读即可 |
| GPU | NVML（`nvidia-smi` 作 fallback） | 优先库调用；缓存 1～5s |
| 磁盘 IO | `/proc/diskstats` 差分 | 与容量查询分离 |
| 进程 Top | 可选、低频 | 全量扫 `/proc` 有成本，默认关或 30s+ |

### 3.5 SLO（采集侧，初稿）

| 项 | 目标 |
|----|------|
| 单机一轮「快路径」采集墙钟时间 | P99 < 200ms（无 GPU）；有 GPU P99 < 500ms |
| 快路径磁盘容量（≤ 16 挂载点） | P99 < 50ms |
| Agent 常驻额外 CPU | 空闲时平均 < 1% 单核 |
| Agent 常驻 RSS | < 80MB（无嵌入式模型等重依赖） |
| 中央 API「机器列表」读路径 | P99 < 100ms（纯读库/缓存，不触发远程采集） |

---

## 4. 总体架构

### 4.1 逻辑架构

```text
┌─────────────┐     ┌──────────────┐     ┌────────────────────────┐
│  Web UI     │────▶│  API Gateway │────▶│  Control Plane         │
│  (Frontend) │     │  / REST+WS   │     │  - 机器注册 / 鉴权      │
└─────────────┘     └──────────────┘     │  - 告警规则             │
                                         │  - 查询最近快照/时序    │
                                         └───────────┬────────────┘
                                                     │
                                         ┌───────────▼────────────┐
                                         │  Metrics Store         │
                                         │  - 最新快照 (KV/关系库) │
                                         │  - 短时序 (TSDB 或表)  │
                                         └───────────▲────────────┘
                                                     │ push (心跳+指标)
                      ┌──────────────────────────────┼──────────────┐
                      │                              │              │
               ┌──────┴──────┐              ┌───────┴──────┐ ┌─────┴─────┐
               │ Agent Node A│              │ Agent Node B │ │ Agent ... │
               │ 快路径采集   │              │              │ │           │
               │ 可选慢路径   │              │              │ │           │
               └─────────────┘              └──────────────┘ └───────────┘
```

### 4.2 为什么用 Agent 推送，而不是中心 Pull（SSH）

| 方案 | 优点 | 缺点 |
|------|------|------|
| **Agent Push（推荐）** | 不依赖 SSH；采集贴近本机 API；易控频率与熔断；扩机器简单 | 需部署 Agent |
| 中心 SSH Pull | 无 Agent | 密钥管理重；查询延迟高；易打爆目标机；磁盘慢查询难控 |

**结论：MVP 采用 Agent Push。** 中心只消费已采集数据。

### 4.3 组件划分

| 组件 | 职责 |
|------|------|
| `agent` | 本机采集、鉴权、上报、本地熔断与自监控 |
| `server`（后端） | 注册、鉴权、接收上报、存储、查询 API、告警评估 |
| `web`（前端） | 总览、机器详情、告警、配置（频率/阈值） |
| `docs` | 设计、API、运维手册 |

---

## 5. 数据模型（概念）

### 5.1 核心实体

```text
Host
  id, name, group, labels{}, agent_version, last_seen_at, status

MetricSnapshot          # 每个 Host 最新一份（覆盖写）
  host_id, collected_at, cpu{}, memory{}, gpus[], disks[], net{}, meta{}

MetricSample (时序)     # 降采样后的点
  host_id, metric_name, labels{}, ts, value

AlertRule
  id, name, expr/阈值, severity, targets(group/host), enabled

AlertEvent
  id, rule_id, host_id, fired_at, resolved_at, status, payload
```

### 5.2 磁盘快照字段（快路径）

```json
{
  "mountpoint": "/data",
  "device": "/dev/nvme0n1p1",
  "fstype": "ext4",
  "total_bytes": 1000000000000,
  "used_bytes": 700000000000,
  "avail_bytes": 300000000000,
  "used_percent": 70.0,
  "inodes_total": 61000000,
  "inodes_used": 1200000,
  "collect_method": "statfs",
  "collect_duration_ms": 1
}
```

> `collect_duration_ms` 用于观测采集自身是否变慢，便于守住 §3.5 SLO。

---

## 6. 通信与 API 草图

### 6.1 Agent → Server

| 接口 | 说明 |
|------|------|
| `POST /api/v1/agent/register` | 首次注册，下发 `host_id` / token |
| `POST /api/v1/agent/heartbeat` | 轻量存活（可与 metrics 合并） |
| `POST /api/v1/agent/metrics` | 上报快路径快照（幂等：按 `collected_at`） |
| `POST /api/v1/agent/slow-jobs/:id/result` | 慢路径结果（二期） |

建议：TLS + 每 Agent 独立 token；支持 gzip。

### 6.2 Frontend → Server

| 接口 | 说明 |
|------|------|
| `GET /api/v1/hosts` | 列表 + 关键状态 + 关键容量摘要 |
| `GET /api/v1/hosts/:id` | 详情最新快照 |
| `GET /api/v1/hosts/:id/metrics?from&to&names=` | 时序（只读存储） |
| `GET /api/v1/alerts` | 告警列表 |
| `WS /api/v1/ws/overview` | 可选：推送状态变更 |

**明确禁止：** 任何「同步触发远端全盘扫描并等待结果」的同步 HTTP API。

---

## 7. 采集调度设计

### 7.1 Agent 内部调度

```text
┌──────────────────────────────────────────┐
│                Agent Runtime             │
│  ticker(fast=5~15s) → FastCollector      │
│       CPU / Mem / GPU / Disk(statfs)     │
│       → enqueue report (async HTTP)      │
│                                          │
│  ticker(io=15~30s) → IoCollector(可选)   │
│                                          │
│  SlowJobWorker (默认 off)                │
│       接受任务 → 限流执行 → 回传结果      │
└──────────────────────────────────────────┘
```

- FastCollector **同步采集、异步上报**（上报失败本地短队列，丢旧保新）
- 可配置：`collect_interval`、`disk_mount_allowlist`、`enable_gpu`、`enable_proc_top`
- 采集与上报解耦，避免网络抖动拖长采集临界区

### 7.2 默认频率建议

| 项 | 默认 | 可调范围 |
|----|------|----------|
| 快路径采集 | 10s | 5s～60s |
| 上报（可批量） | 10s | 与采集对齐或略合并 |
| GPU | 与快路径同频或 15s | — |
| 目录分析 | 关闭 | 手动 / ≥ 24h |

---

## 8. 告警（MVP）

最小集：

- 磁盘使用率 > 阈值（按挂载点）
- CPU 持续 N 分钟 > 阈值
- 内存 available < 阈值
- GPU 显存 > 阈值
- Agent 掉线（`last_seen` 超时）

评估在 **Server 侧** 基于已入库快照进行，避免在 Agent 上堆复杂规则引擎（Agent 仅可做本地紧急自保护日志）。

---

## 9. 前端信息架构

### 9.1 页面

1. **总览**：机器卡片/表格（状态、CPU、内存、最满磁盘%、GPU）  
2. **机器详情**：资源曲线 + 磁盘挂载表 + GPU 列表  
3. **告警中心**  
4. **接入指引**：如何安装 Agent、复制 token  

### 9.2 交互约束

- 列表刷新读缓存，不触发远程重采集  
- 磁盘详情默认展示挂载点容量；「目录分析」入口需二次确认并提示耗时/IO 风险（二期）  
- 展示 `collected_at`，避免用户误以为是瞬时 SSH 结果

---

## 10. 技术选型建议（可调整）

> 选型以「实现快、运维简单、采集轻」为优先，落地前可再确认。

| 层 | 建议 | 备选 |
|----|------|------|
| Agent | Go（静态编译、低 RSS、部署简单） | Rust / Python（Python 需更严控依赖与 RSS） |
| Server | Go 或 Node（Nest/Fastify） | Python FastAPI |
| 存储 | PostgreSQL（元数据+最新快照） + 可选 Timescale/VictoriaMetrics（时序） | MVP 可先只用 PostgreSQL 存降采样点 |
| Frontend | React + Vite | — |
| 部署 | Docker Compose 单机起步 | K8s 后续 |

**磁盘采集库参考（Go）：** `golang.org/x/sys/unix.Statfs`；或 `gopsutil/disk`（需审计其是否内部走 `du`——容量 API 应仅用 statfs）。

---

## 11. 安全

- Agent 一机一密，可轮换、可吊销  
- Server API：用户登录（MVP 可用基础账号或 OIDC）  
- 传输 TLS；内网可 mTLS（二期）  
- 最小权限：Agent 以非 root 运行（GPU/部分磁盘场景再按需提权说明）  
- 审计：注册、吊销、告警确认留痕  

---

## 12. 分阶段交付

### Phase 0 — 文档与骨架（本阶段）

- [x] 设计方案  
- [ ] 仓库目录约定、README  
- [ ] 接口与数据字段草案（可继续细化）

### Phase 1 — MVP 可演示

- Agent：CPU / 内存 / 磁盘（statfs）/ 心跳上报  
- Server：注册、收指标、查列表与详情  
- Web：总览 + 详情  
- 基础磁盘/掉线告警  

### Phase 2 — GPU 与时序

- NVML GPU 指标  
- 历史曲线与简单降采样  
- 分组 / 标签  

### Phase 3 — 深度存储分析与软配额告警（谨慎开启）

- **3a** 异步目录 Top-N 下钻 + 报告缓存 + 大文件 Top-N（定位「谁占用」）  
- **3b** QuotaPolicy（如 `/home/*/*` 每文件夹限额）+ 定向扫描 + **超限告警/提醒**（软限制，不拦写）  

细节见 [STORAGE_ATTRIBUTION_AND_QUOTA.md](./STORAGE_ATTRIBUTION_AND_QUOTA.md)。

### Phase 4 — 硬化

- 高可用、权限细化、通知渠道（飞书/Slack/邮件）、SLO 大盘  

---

## 13. 风险与对策

| 风险 | 对策 |
|------|------|
| 误用 `du` 导致业务 IO 抖动 | Code review 清单 + 集成测试断言采集耗时；文档红线 |
| 伪文件系统刷屏 | 默认挂载过滤；可配置 allow/deny |
| Agent 与 Server 时钟偏差 | 以 Server 入库时间为准，同时保留 Agent `collected_at` |
| GPU 驱动/权限缺失 | 优雅降级，标记 `gpu: unavailable` |
| 上报风暴 | 抖动上报、Server 限流、按 host 串行写最新快照 |

---

## 14. 验收标准（设计层）

1. 文档明确区分快/慢路径，且慢路径默认关闭  
2. 磁盘容量方案基于 `statfs`（或等价 O(1) API），不依赖递归扫盘  
3. 查询 API 不触发同步远程采集  
4. 给出可量化的采集耗时/资源占用 SLO 初稿  
5. 分阶段路线图可指导后续初始化代码与实现  
6. 大目录定位与**目录软配额告警**方案有独立文档，且不污染热路径  

---

## 15. 开放问题（待确认）

1. 首批目标 OS：仅 Linux，还是需要 Windows？  
2. GPU：是否只需 NVIDIA？是否有多卡训练机优先级？  
3. 部署形态：内网 Docker Compose，还是已有 K8s？  
4. 认证：简单账号密码是否可接受 MVP？  
5. 是否需要「目录级占用来源」在第一版就出现（建议放到 Phase 3）？  
6. 历史数据保留多久（7 / 30 / 90 天）？  
7. 用户目录根路径与「每文件夹」粒度（一级子目录 vs 任意深度）？  
8. 软配额超限通知对象：仅运维，还是同时提醒目录 owner？  

### 已确认决策

- **目录限额仅软限制**：告警与提醒，不阻止写入；不做 OS 硬配额对接。  

---

## 附录 A. 仓库目录约定（初始化预留）

```text
ResourceHub/
  docs/                 # 设计与 API 文档
  agent/                # 采集端
  server/               # 后端
  web/                  # 前端
  deploy/               # compose / 安装脚本
  README.md
```

## 附录 B. 反模式清单（磁盘）

- ❌ 在列表页对每台机器 SSH 执行 `du -sh /*`  
- ❌ 每次心跳递归 walk 工作目录算「项目占用」  
- ❌ 同步 API：`GET /hosts/:id/disk/analyze` 阻塞直到扫完  
- ❌ 无超时、无 `ionice`、无路径白名单的定时全盘分析  
- ✅ `statfs` 容量 + 异步、限流、可取消的分析任务（如需要）
