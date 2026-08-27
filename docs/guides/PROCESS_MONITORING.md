# 进程监控方案（活跃进程与僵尸进程）

> 回答：如何监控各机器**活跃进程**，以及如何发现与告警 **僵尸进程（Zombie）**？  
> 归属 **Phase 2** 能力；与快路径（CPU/内存/磁盘）**分频采集**，不阻塞 10s 心跳。  
> 主设计见 [../DESIGN.md](../DESIGN.md)，性能红线见 [COLLECTION_PERF.md](./COLLECTION_PERF.md)。

---

## 1. 目标与范围

### 1.1 要回答的问题

| 问题 | 示例 |
|------|------|
| 这台机有多少进程？各状态分布？ | running 12 / sleeping 340 / zombie 2 |
| 哪些进程最「活跃」？ | CPU Top-N、内存 RSS Top-N |
| 有没有僵尸进程？是谁的子进程？ | PID 12345 `Z` state，ppid=9876 |
| 是否持续恶化？ | 僵尸数 > 0 持续 5 分钟 → 告警 |

### 1.2 本期做 / 不做

| 做 | 不做 |
|----|------|
| 进程状态汇总（含 zombie 计数） | 远程 `kill` / 自动清理僵尸 |
| Top-N 活跃进程（CPU / RSS） | 完整 `ps aux` 级全量表（默认定时） |
| 僵尸进程列表（有僵尸时展开） | strace / perf / eBPF 深度追踪 |
| 僵尸存在 / 持续告警 | 替代 systemd / supervisor 管理进程 |

**原则：** 只读监控 + 告警提醒；清理仍由运维或上层进程管理器处理。

---

## 2. 与现有架构的关系

```text
快路径（已有，~10s）
  CPU / Mem / Disk(statfs) / 心跳
  → 不扫 /proc/PID 目录树

中路径（本章，~30s，默认开）
  ProcessSummary + ZombieDetect + Top-N
  → 读 /proc，有预算与超时

慢路径（可选，按需）
  全量进程快照 / 过滤查询（异步任务）
```

与磁盘方案一致：**看板/API 只读 Agent 已上报快照**，刷新页面不会 SSH 到目标机跑 `ps`。

---

## 3. 数据来源（Linux）

| 数据 | 来源 | 说明 |
|------|------|------|
| 进程状态 `R/S/D/Z/T/...` | `/proc/<pid>/stat` 第 3 字段（括号 comm 之后） | 轻量，单行 |
| 父进程 | `/proc/<pid>/stat` ppid | 定位僵尸归属 |
| CPU 时间 | `/proc/<pid>/stat` utime/stime | 两次采样差分得 CPU% |
| 内存 RSS | `/proc/<pid>/stat` rss 或 `/status` VmRSS | Top 内存 |
| 用户名 | `/proc/<pid>/status` Uid → `/etc/passwd` 或缓存 | 可选 |
| 命令名 | `/proc/<pid>/comm` 或 stat 内 comm | 展示用 |

**僵尸判定：** `stat` 中 state = **`Z`**（zombie / defunct）。

**不推荐：** 每次采集 `ps aux`、`top -b` 子进程——开销大、输出解析慢、难控时延。

---

## 4. 采集策略（三层）

### 4.1 层 A：进程汇总 + 僵尸计数（默认开启）

**频率：** 30s（与 10s 快路径分离）  
**做法：** 遍历 `/proc` 下数字目录 → 读每个 `stat` **仅解析 state** → 计数

```text
summary:
  total, running, sleeping, uninterruptible(D),
  zombie, stopped, dead, unknown
collect_duration_ms, partial, scanned_pids
```

**性能预算：**

| 项 | 建议 |
|----|------|
| 墙钟超时 | 200～500ms（可配置） |
| 超时行为 | `partial=true`，已扫部分仍上报；zombie 计数尽量完整（优先扫完或二次补扫） |
| 与快路径 | **不同 goroutine / ticker**，绝不阻塞 CPU/磁盘采集 |
| 禁止 | 在 10s 心跳里全量扫 `/proc` |

**SLO 初稿：** 500 进程以内 P99 < 200ms；5000 进程 P99 < 500ms（或 partial）。

### 4.2 层 B：活跃进程 Top-N（默认开启，同 30s）

在层 A 同一轮或相邻轮中，对**有限数量** PID 读完整 stat/status：

| 榜单 | 规则 | 默认 N |
|------|------|--------|
| **Top CPU** | 两次采样 utime+stime 差 / 墙钟 / ncpu | 10 |
| **Top RSS** | 单次 rss 排序 | 10 |

优化：层 A 遍历时缓存 stat 行；仅对 CPU/RSS 候选做排序，避免二次 IO。

**「活跃」定义（产品默认）：**

- CPU Top：占 CPU 时间增量最多的进程  
- 内存 Top：RSS 最大的进程  
- 列表页总览**不展示**全量进程，仅详情页 Top-N + 汇总数字  

### 4.3 层 C：僵尸明细（条件触发）

**触发条件（满足任一）：**

- `summary.zombie > 0`  
- 用户点击「查看僵尸进程」  
- 告警详情页下钻  

**内容：** 最多 50 条（可配置）

```json
{
  "pid": 12345,
  "ppid": 9876,
  "ppid_comm": "python",
  "comm": "worker",
  "state": "Z",
  "user": "alice",
  "zombie_since": "unknown"
}
```

说明：Linux 不直接提供「成为僵尸的时间」，可用 `starttime` + 状态 Z 作参考，UI 标注「近似」。

### 4.4 层 D：全量进程表（慢路径，默认关）

类似存储 Top-N 下钻：异步任务 `ProcessScanJob`，支持 filter（user、comm 前缀、state=Z）。  
仅运维手动或 cron 低频触发；**禁止**总览页同步拉全量。

---

## 5. 数据模型（扩展 MetricSnapshot）

```json
{
  "processes": {
    "collected_at": "2026-08-27T05:00:00Z",
    "summary": {
      "total": 412,
      "running": 3,
      "sleeping": 406,
      "uninterruptible": 1,
      "zombie": 2,
      "stopped": 0,
      "collect_duration_ms": 87,
      "partial": false
    },
    "top_cpu": [
      {"pid": 1001, "comm": "python", "user": "alice", "cpu_percent": 85.2, "rss_bytes": 1073741824}
    ],
    "top_rss": [
      {"pid": 2002, "comm": "java", "user": "bob", "cpu_percent": 12.0, "rss_bytes": 8589934592}
    ],
    "zombies": [
      {"pid": 3003, "ppid": 1001, "ppid_comm": "python", "comm": "worker", "state": "Z", "user": "alice"}
    ]
  }
}
```

**存储：** 并入现有 `metric_snapshots` JSON；Server 侧可冗余 `zombie_count` 到 `HostSummary` 便于列表筛选。

---

## 6. API 与 UI

### 6.1 API（读路径，无同步采集）

| 接口 | 说明 |
|------|------|
| `GET /api/v1/hosts` | 摘要增加 `zombie_count`、`process_total`（可选） |
| `GET /api/v1/hosts/:id` | 快照内 `processes` 全文 |
| `GET /api/v1/alerts` | 含 `process:zombie` 规则告警 |
| `POST /api/v1/hosts/:id/process-scans` | （慢路径）提交全量/过滤扫描任务 |

### 6.2 UI

**总览列表：**

- 新列或徽章：`僵尸 2`（仅 > 0 时高亮）  
- 可选：进程总数（muted）

**单机详情 — 进程 Tab：**

1. 状态汇总卡片（running / sleeping / zombie …）  
2. Top CPU / Top RSS 表格  
3. **僵尸进程表**（zombie > 0 时置顶、标红）  
4. 文案：`as_of` 采集时间；`partial` 时提示「进程过多，计数可能不完整」

**告警页：**

- `process.zombie`：消息含 host、数量、部分 ppid/comm  

---

## 7. 告警规则（软监控，只提醒）

| 规则 | 条件 | 严重度 | 恢复 |
|------|------|--------|------|
| `process.zombie` | `zombie >= 1` | warning | zombie = 0 |
| `process.zombie.persistent` | `zombie >= 1` 持续 ≥ 5min | critical | zombie = 0 持续 2 个周期 |
| `process.zombie.high` | `zombie >= 10` | critical | zombie < 10 |
| `process.count.high`（可选） | total > 阈值 | warning | 低于阈值 |

评估在 **Server** 侧，基于已入库快照；payload 附带 `zombies` 摘要供排查。

**运维提示（文档/告警文案）：** 僵尸需**父进程** `wait()` 回收；杀父进程或修业务代码，监控本身不代为清理。

---

## 8. Agent 调度（更新）

```text
ticker(fast=10s)  → CPU / Mem / Disk / 上报
ticker(proc=30s)  → ProcessSummary + Top-N + ZombieDetail(若需要)
SlowJobWorker     → ProcessScanJob（默认关）
```

配置项：

```yaml
process:
  enabled: true
  interval: 30s
  top_n: 10
  scan_timeout_ms: 500
  zombie_detail_max: 50
  full_scan: false
```

---

## 9. 性能与反模式

### 9.1 允许

- `readdir("/proc")` + 读 `stat` / `comm` / `status`  
- 有超时的单轮扫描  
- zombie > 0 时多读 zombie 明细  

### 9.2 禁止

- 在 10s 快路径全量扫 `/proc`  
- 每次看板刷新 `exec ps aux`  
- 无超时扫描 10 万 PID  
- 为进程监控默认开启层 D 全量表  

### 9.3 风险

| 风险 | 对策 |
|------|------|
| PID 很多（>1 万）扫描变慢 | 超时 + partial；可调大 interval |
| 读 `/proc/PID` 遇权限/竞态（PID 已退出） | 跳过并计数 `scan_errors` |
| 容器内 PID namespace | 文档说明：Agent 看的是**当前 namespace** |
| 僵尸告警风暴 | 去重 + 持续 N 分钟再升级 critical |

---

## 10. 分期

| 阶段 | 交付 |
|------|------|
| **Phase 2a** | 层 A+B：汇总 + Top CPU/RSS + 详情页进程 Tab |
| **Phase 2b** | 层 C：僵尸明细 + `process.zombie` 告警 + 列表徽章 |
| **Phase 2c**（可选） | 层 D 异步全量扫描；按 user/comm 过滤 |

建议 **2a 与 GPU 并行开发**；僵尸告警（2b）优先于全量扫描（2c）。

---

## 11. 验收标准

1. 30s 进程采集不阻塞 10s 快路径  
2. 快照含 `summary.zombie`，zombie > 0 时可看到 PID/ppid/comm  
3. Top CPU / Top RSS 各 ≤ 10 条，详情页可读  
4. 列表/告警能发现僵尸；恢复后告警自动 resolve  
5. 看板刷新不触发远程 `ps` / 同步扫 `/proc`  

---

## 12. 开放问题

1. Top-N 默认 10 是否够用？是否需要按 user 过滤（如只看某训练账号）？  
2. 总览是否展示「进程总数」，还是仅 zombie > 0 时突出？  
3. 容器 / K8s 节点上是否需对接 cgroup / container 维度（Phase 2c+）？  
4. 僵尸 `critical` 阈值：≥1 即 critical，还是「持续 5 分钟」才 critical？  

---

## 附录：算法伪代码

```go
// 层 A：汇总 + 僵尸 PID 列表（单轮）
func collectProcessSummary(timeout time.Duration) (Summary, []Zombie, error) {
    deadline := time.Now().Add(timeout)
    pids := listNumericProcEntries()
    var sum Summary
    var zombies []Zombie
    for _, pid := range pids {
        if time.Now().After(deadline) {
            sum.Partial = true
            break
        }
        stat, err := readProcStat(pid) // 一次 read，解析 state/ppid/utime/rss
        if err != nil { continue }
        sum.Total++
        switch stat.State {
        case 'R': sum.Running++
        case 'S', 'I': sum.Sleeping++
        case 'D': sum.Uninterruptible++
        case 'Z':
            sum.Zombie++
            zombies = append(zombies, toZombie(stat))
        case 'T': sum.Stopped++
        default: sum.Unknown++
        }
        // 收集 Top-N 候选 heap...
    }
    return sum, zombies, nil
}
```

```go
// Top CPU：需与上一轮 stat 缓存 utime+stime，按 delta 排序
cpuPercent = (delta_jiffies / delta_wall) / numCPU * 100
```
