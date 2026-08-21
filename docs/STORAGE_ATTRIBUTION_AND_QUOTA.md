# 大目录定位与目录配额告警

> 回答两个产品问题：  
> 1）磁盘快满时，如何定位「空间被谁占」？  
> 2）如何对用户目录下各文件夹做**软限额**，超限时告警/提醒（不阻止写入）？  
>  
> 均属于 **慢路径 / 策略层**，不得进入 Agent 热路径心跳。详见 [COLLECTION_PERF.md](./COLLECTION_PERF.md)、[DESIGN.md](./DESIGN.md)。  
>  
> **已确认：** 目录限额只做软限制（告警与提醒），不做 OS 硬配额。

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

## 3. 目录软限额：告警与提醒（已定范围）

### 3.0 范围决策（已确认）

> **只做软限制：告警 + 提醒，不阻止写入。**  
> 不做 OS 硬配额、不拦截 `write`、不自动删文件。超限后用户仍可继续写入；系统负责发现、展示、通知，推动人工清理。

| 能力 | 是否做 | 说明 |
|------|--------|------|
| 软限额策略 + 用量扫描 + 告警/提醒 | ✅ 做 | 产品主路径 |
| OS 硬配额 / 写失败 / 自动清理 | ❌ 不做 | 非目标 |

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
3. **告警 / 提醒**：Server 侧去重（同一 path 未恢复前不重复轰炸），通知运维和/或目录当事人  

伪流程：

```text
每天 03:00 / 手动
  → 列出 targets = expand("/home/*/*")   # 只列一层，不深 recurse 列目录名
  → 对每个 target 测体积（限时）
  → used > warn/critical → Alert(quota)   # 仅通知，不阻断写入
```

**为何比「全盘 du」可控：** 目标集合由 glob 展开，数量≈用户数×每户文件夹数；每个 target 可并行度=1、带超时，失败则跳过并记错误。

### 3.2 产品交互建议

1. **策略页**：配置 path 模板、默认 limit、预警阈值、作用主机范围  
2. **配额看板**：按 host / user / path 列出 used、limit、利用率；超限标红；文案标明「软限制 · 仅告警」  
3. **告警**：`quota.warn` / `quota.critical`，payload 含 path、owner、used、limit  
4. **与大目录分析联动**：告警详情一键「对该 path 做 Top-N 下钻」，继续找子目录元凶  
5. **不自动删除、不拦写入**：清理由人工处理；系统只提醒  

### 3.3 配置示例（用户目录每子文件夹 100GiB）

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

慢路径 B — 软配额告警（本章 §3）
  QuotaPolicy + 定向测体积 → 「谁超策略」→ 告警/提醒（不拦写）
```

告警链示例：

1. `/data` 使用率 > 90%（快路径）  
2. 一键分析 → `/data/alice` 最大（归因）  
3. 策略显示 `/home/alice/datasets` 已超限（软配额告警，提醒清理）  

---

## 5. 分期建议

| 阶段 | 交付 |
|------|------|
| Phase 3a | 异步目录 Top-N 下钻 + 报告缓存 + 大文件 Top-N |
| Phase 3b | QuotaPolicy + 定向扫描 + 超限告警/提醒 + 配额看板 |
| 更后 | 通知到具体目录 owner、年龄分桶等增强（仍保持软限制） |

---

## 6. 验收要点

1. 定位大户必须经异步任务，界面展示 `as_of`，可 `partial`  
2. 热路径与列表刷新不得触发全树扫描  
3. 软配额超限能稳定告警/提醒，并支持按 path 去重  
4. 产品与文档明确：**仅软限制，超限不阻断写入、不自动删除**  

---

## 7. 开放问题

1. 用户目录根路径约定是 `/home`、`/data/home` 还是 NFS 挂载？  
2. 「每个文件夹」是指用户下**一级子目录**，还是任意深度？  
3. 超限通知对象：仅运维，还是同时提醒目录 owner（邮件/IM）？  
