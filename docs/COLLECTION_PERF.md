# 采集性能指南（磁盘与热路径）

本文落实设计方案中的硬约束，供 Agent 实现与 Code Review 使用。

## 1. 热路径允许的操作

| 允许 | 说明 |
|------|------|
| `statfs` / `statvfs` | 按挂载点取块/inode 统计 |
| 读 `/proc/mounts` 或等价 | 枚举挂载并过滤 |
| 读 `/proc/stat`、`meminfo`、`loadavg`、`diskstats`、`net/dev` | 标准 proc 指标 |
| NVML API | GPU；失败则标记不可用 |
| 短超时 HTTP 上报 | 与采集临界区分离 |

## 2. 热路径禁止的操作

| 禁止 | 原因 |
|------|------|
| `du`、`find` + 累加大小 | 递归 IO，耗时不可控 |
| 无深度限制的 `filepath.Walk` / `os.walk` 算目录体积 | 同上 |
| 每次采集 `nvidia-smi` 启进程且无缓存 | 进程开销大（可作为罕见 fallback） |
| 同步等待慢任务完成再上报快照 | 拖长采集周期 |

## 3. 推荐伪代码（磁盘容量）

```go
// 示意：仅表达逻辑，非最终代码
func collectDisks(cfg Config) ([]Disk, time.Duration) {
    start := time.Now()
    mounts := listMounts() // 读挂载表
    mounts = filter(mounts, cfg.DenyFSTypes, cfg.AllowPrefixes)
    mounts = dedupeByDevice(mounts)

    out := make([]Disk, 0, len(mounts))
    for _, m := range mounts {
        var st unix.Statfs_t
        if err := unix.Statfs(m.Path, &st); err != nil {
            continue
        }
        total := st.Blocks * uint64(st.Bsize)
        avail := st.Bavail * uint64(st.Bsize)
        free  := st.Bfree  * uint64(st.Bsize)
        used  := total - free
        out = append(out, Disk{
            Mountpoint: m.Path,
            FSType:     m.FSType,
            Total:      total,
            Avail:      avail,
            Used:       used,
            Method:     "statfs",
        })
    }
    return out, time.Since(start)
}
```

## 4. 默认过滤（可配置）

建议默认排除的 `fstype`：

`tmpfs`, `devtmpfs`, `devfs`, `sysfs`, `proc`, `cgroup`, `cgroup2`, `overlay`, `squashfs`, `ramfs`, `rpc_pipefs`, `nfsd`, `autofs`, `fuse.portal`, `iso9660`

可选：仅监控 `allow_mount_prefixes`（如 `/`, `/data`, `/var`）。

## 5. 慢路径（目录分析 / 软配额扫描）清单

仅当产品明确开启时（产品设计见 [STORAGE_ATTRIBUTION_AND_QUOTA.md](./STORAGE_ATTRIBUTION_AND_QUOTA.md)）：

1. 独立队列与 worker，默认 concurrency = 1  
2. 必须有：`root` 白名单、`max_depth`、`timeout`、`max_files`；配额扫描用 glob 展开目标，勿全盘 walk  
3. Linux 建议：`ionice -c3`、降低 nice；不跨挂载点  
4. 结果带 `started_at` / `finished_at` / `truncated` / `partial`  
5. API 只提供「提交任务 + 查任务状态」，禁止同步扫盘接口  
6. 若主机已有 XFS project / 用户配额，优先读账本，避免重复 walk

## 6. 测试门禁建议

- 单元测试：mock 挂载表 + statfs，断言不调用 walk  
- 集成测试：在含大量小文件的 fixture 上跑快路径，断言耗时上限  
- CI grep 门禁（可选）：Agent 热路径包禁止出现 `\bdu\b`、无条件 `Walk(`  

## 7. 自监控字段

每轮快照建议带：

- `agent.collect_duration_ms`  
- `agent.disk_collect_duration_ms`  
- `agent.report_queue_len`  

Server 可对 `disk_collect_duration_ms` 过高的主机打「采集异常」提示，便于发现错误实现或病态挂载（如网络盘 statfs 卡住——需单挂载点超时）。

### 单挂载点超时

对每个 `statfs` 建议有超时（如 100～200ms）：网络文件系统卡住时跳过该挂载并标记 `error=timeout`，避免整轮采集被拖死。
