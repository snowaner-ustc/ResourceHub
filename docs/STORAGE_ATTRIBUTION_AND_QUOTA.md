# 大目录定位与目录配额告警

> 回答两个产品问题：  
> 1）磁盘快满时，如何定位「空间被谁占」？  
> 2）如何限制用户目录下各文件夹大小，并对超限提出警告？  
>  
> 均属于 **慢路径 / 策略层**，不得进入 Agent 热路径心跳。详见 [COLLECTION_PERF.md](./COLLECTION_PERF.md)、[DESIGN.md](./DESIGN.md)。

---

## 1. 问题拆分

| 问题 | 本质 | `statfs` 能回答吗？ |
|------|------|---------------------|
| 盘还剩多少？ | 文件系统块统计 | ✅ 能（快路径） |
| 空间被谁占？ | 目录/文件归因 | ❌ 不能，需要扫树或配额账本 |
| 某文件夹超没超限额？ | 策略对比（用量 vs 限额） | ❌ 不能，先要有「该目录用量」 |

因此：挂载点告警只能告诉你「哪块盘满了」；要回答「谁占的 / 谁超限」，必须另建 **归因与配额** 能力。

---

## 2. 如何定位大空间占用者

### 2.1 推荐产品体验：分层下钻（Top-N Drill-down）

不要一次 `du -sh /*` 扫完整棵树。按「一层一层、只留大户」下钻：

```text
挂载点 /data 使用率 92%
    └─ 分析根：/data
         ├─ alice/     1.2T   ← Top
         ├─ bob/       800G
         ├─ shared/    300G
         └─ … (其余合并为 Others)
              │ 用户点击 alice/
              ▼
         /data/alice
              ├─ datasets/   900G
              ├─ checkpoints/ 250G
              └─ …
```

**算法要点（BFS / 限深）：**

1. 从配置的 `roots` 开始（如 `/home`、`/data`），**禁止默认从 `/` 无差别扫**  
2. 每一层只对**直接子目录**做体积汇总（等价于该层 `du -xs` / 可控 walk）  
3. 每层只保留 **Top-N**（如 20），其余记入 `others_bytes`  
4. 用户下钻某节点时，再对该节点发**新的异步任务**（可复用缓存）  
5. 可选第二视图：**大文件 Top-N**（按文件 size，跳过目录累加细节）

这样平均 IO 远小于全树一次扫完，且 UI 与运维心智一致。

### 2.2 归因维度（「是谁占用的」）

| 维度 | 做法 | 适用 |
|------|------|------|
| **路径** | 目录树 Top-N | 最通用，首选 |
| **Unix owner** | walk 时累加 `st_uid` → 用户名 | 多用户共享盘 |
| **项目/组** | 路径约定（`/data/<project>`）或目录标签 | 有规范目录结构时 |
| **年龄** | 按 mtime 分桶（30/90/365 天） | 找可删冷数据 |

MVP 建议：**路径 Top-N + owner 汇总**；年龄分桶作增强。

### 2.3 任务模型（异步，禁止同步 HTTP 扫盘）

```text
用户/定时器
   │  POST /api/v1/hosts/:id/disk-scans   { root, max_depth, top_n, mode }
   ▼
Server 创建 DiskScanJob (queued)
   │  下发 / Agent 拉取任务
   ▼
Agent SlowWorker
   │  ionice + timeout + max_files
   │  产出 TreeTopN / OwnerBreakdown / LargeFiles
   ▼
POST 结果 → Server 存报告（带 as_of、duration、partial）
   │
   ▼
前端轮询 GET .../disk-scans/:job_id 或 WS 通知完成
```

报告字段示例：

```json
{
  "root": "/data",
  "as_of": "2026-08-21T03:00:00Z",
  "duration_ms": 12400,
  "partial": false,
  "entries": [
    {"path": "/data/alice", "bytes": 1319413953331, "owner": "alice", "entry_type": "dir"},
    {"path": "/data/bob", "bytes": 858993459200, "owner": "bob", "entry_type": "dir"}
  ],
  "by_owner": [
    {"owner": "alice", "bytes": 1319413953331},
    {"owner": "bob", "bytes": 858993459200}
  ]
}
```

### 2.4 降低成本的工程手段

| 手段 | 说明 |
|------|------|
| 低频定时 + 手动触发 | 如每天低峰 1 次；盘 > 阈值时提示「一键分析」 |
| 单机 concurrency=1 | 避免多个 walk 并发打盘 |
| `max_depth` / `timeout` / `max_files` | 超时标记 `partial=true`，仍返回已有 Top-N |
| 跳过名单 | `.git`、容器层、快照目录、`lost+found` |
| 不跨挂载点 | 等价 `du -x`，避免扫进 NFS 把任务拖死 |
| 结果缓存 | 同 root 在 TTL（如 6～24h）内复用 |
| 增量（后期） | `fanotify`/`inotify` 维护账本，或依赖文件系统配额账本 |

### 2.5 明确不推荐

- 看板刷新时全盘 `du`  
- 同步接口卡到扫完才返回  
- 无白名单从 `/` 扫到叶子  
- 用 `find / -type f -size +1G` 作为默认巡检（同样重）  

---

## 3. 限制用户目录下每个文件夹大小，并告警

先分清两件事：

| 能力 | 含义 | ResourceHub 角色 |
|------|------|------------------|
| **软限制（Soft）** | 超限只告警，不阻止写入 | ✅ 产品主路径：策略 + 扫描用量 + Alert |
| **硬限制（Hard）** | 超限后写失败 / 无法再占空间 | ⚠️ 主要靠 **OS/文件系统配额**；监控系统负责展示与告警，不在应用层拦 `write(2)` |

应用层 Agent **无法可靠硬拦截**所有写入（除非改内核/FUSE/专用存储网关）。因此推荐：

> **ResourceHub 做软配额告警；需要强制时，叠加 OS 硬配额。**

### 3.1 软配额：策略模型

场景：`/home/<user>/` 下每个**一级子目录**（或任意匹配路径）限额，例如每文件夹 ≤ 100GiB，超 80% 预警、100% 告警。

```text
QuotaPolicy
  id, name, enabled
  scope:
    host_selector: { group, labels }
    path_pattern: "/home/*/*"     # 或 /data/{user}/{project}
    match_mode: "glob" | "regex"
  limit_bytes: 107374182400       # 100GiB
  warn_percent: 80
  critical_percent: 100
  scan_interval: "24h"            # 慢路径
  exclude: [".cache", "node_modules"]  # 可选
```

**用量如何得到（仍属慢路径）：**

1. **定向扫描**：只枚举匹配 `path_pattern` 的目录集合（例如每个 `/home/*/` 的直接子目录），对每个目标做**有界**体积统计  
2. **对比策略**：`used / limit` → 生成 `QuotaBreach` 事件  
3. **告警**：Server 侧去重（同一 path 未恢复前不重复轰炸），通知 Viewer/当事人  

伪流程：

```text
每天 03:00 / 手动
  → 列出 targets = expand("/home/*/*")   # 只列一层，不深 recurse 列目录名
  → 对每个 target 测体积（限时）
  → used > warn/critical → Alert(quota)
```

**为何比「全盘 du」可控：** 目标集合由 glob 展开，数量≈用户数×每户文件夹数；每个 target 可并行度=1、带超时，失败则跳过并记错误。

### 3.2 硬配额：操作系统方案（强制写入限制时）

| 方案 | 粒度 | 说明 |
|------|------|------|
| **XFS Project Quota** | 目录树（推荐） | 给目录绑定 project id + 限额；超限写入失败 |
| **ext4 quota** | 用户/组为主 | 用户级限额容易；任意「每个子文件夹」不如 XFS project 直观 |
| **独立挂载 / LVM / ZFS dataset** | 每目录一块「盘」 | `statfs` 即可看用量，硬隔离最好，运维成本高 |
| **NFS/存储网关配额** | 视存储而定 | 家目录在 NAS 上时在存储侧做 |

**与 ResourceHub 的分工：**

```text
OS 硬配额（可选）          ResourceHub 软配额（推荐先做）
  阻止超限写入               扫描用量、预警、工单式提醒
  本机 `repquota` 等         统一多机策略、看板、通知
        └──────── 可同时启用：硬拦 + 软预警（在接近硬顶前告警）
```

若已启用 XFS project quota，Agent **优先读配额账本**（`xfs_quota`/`quotactl`）拿 used/limit——比自己 walk **轻得多**，应作为「有硬配额时」的快路径增强。

### 3.3 产品交互建议

1. **策略页**：配置 path 模板、默认 limit、预警阈值、作用主机范围  
2. **配额看板**：按 host / user / path 列出 used、limit、利用率；超限标红  
3. **告警**：`quota.warn` / `quota.critical`，payload 含 path、owner、used、limit  
4. **与大目录分析联动**：告警详情一键「对该 path 做 Top-N 下钻」，继续找子目录元凶  
5. **不默认自动删除**：本期仍保持只读监控；清理需人工或后续独立工作流  

### 3.4 配置示例（用户目录每子文件夹 100GiB）

```yaml
# 示意：server 下发或 agent 本地策略
quota_policies:
  - name: home-subdir-100g
    path_pattern: "/home/*/*"
    # 匹配 /home/alice/datasets 、/home/bob/work 等「用户下一级文件夹」
    limit_bytes: 107374182400
    warn_percent: 80
    critical_percent: 100
    scan_interval: 24h
    roots_must_exist: ["/home"]
```

展开规则：

- `/home/*` → 用户目录  
- `/home/*/*` → 每个用户下的一级文件夹（正是「用户目录下每个文件夹」）  
- 若要限制「整个用户家目录」：pattern 用 `/home/*`，limit 另设  

---

## 4. 和现有分层如何衔接

```text
快路径（已有）
  statfs → 挂载点容量 → 「哪块盘满了」

慢路径 A — 归因（本章 §2）
  DiskScanJob → Top-N / by_owner → 「谁占的」

慢路径 B — 软配额（本章 §3.1）
  QuotaPolicy + 定向测体积 → 「谁超策略」

可选 OS 硬配额（本章 §3.2）
  XFS project / 用户配额 → 「超了写不进去」
  Agent 读账本 → 低成本展示 used/limit
```

告警链示例：

1. `/data` 使用率 > 90%（快路径）  
2. 一键分析 → `/data/alice` 最大（归因）  
3. 策略显示 `/home/alice/datasets` 已 120%（软配额）  
4. （若启用）XFS project 已拒写（硬配额）  

---

## 5. 分期建议

| 阶段 | 交付 |
|------|------|
| Phase 3a | 异步目录 Top-N 下钻 + 报告缓存 + 大文件 Top-N |
| Phase 3b | QuotaPolicy + 定向扫描 + 超限告警 + 配额看板 |
| Phase 3c | 对接 XFS project / 用户配额账本只读展示；文档化硬配额落地手册 |
| 更后 | 增量账本、年龄分桶、通知到具体用户 |

---

## 6. 验收要点

1. 定位大户必须经异步任务，界面展示 `as_of`，可 `partial`  
2. 热路径与列表刷新不得触发全树扫描  
3. 软配额超限能稳定告警，并支持按 path 去重  
4. 文档区分 soft vs hard；硬限制不宣称「仅靠 Agent 拦截写入」  
5. 有硬配额的主机优先走账本读取，walk 仅作 fallback  

---

## 7. 开放问题

1. 用户目录根路径约定是 `/home`、`/data/home` 还是 NFS 挂载？  
2. 「每个文件夹」是指用户下**一级子目录**，还是任意深度？  
3. 超限是否需要通知到目录 owner（邮件/IM），还是只告警给运维？  
4. 是否计划上 XFS project 硬配额，还是第一期只做软告警？  
