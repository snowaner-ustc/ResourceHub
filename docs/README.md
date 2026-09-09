# ResourceHub 文档

从这里开始。一级目录只放主文档；专题与实现指南在 [`guides/`](./guides/)。

## 主文档

| 文档 | 说明 | 谁先看 |
|------|------|--------|
| [PRD.md](./PRD.md) | 产品需求：做什么、为谁、优先级 | 产品 / 全员 |
| [DESIGN.md](./DESIGN.md) | 总体设计：架构、模型、API、分期 | 研发 |

## 专题指南（`guides/`）

旁路细节，按需阅读；设计正文只引用结论。

| 文档 | 说明 |
|------|------|
| [guides/COLLECTION_PERF.md](./guides/COLLECTION_PERF.md) | 采集性能红线（热路径 / 磁盘 `statfs`） |
| [guides/STORAGE_ATTRIBUTION_AND_QUOTA.md](./guides/STORAGE_ATTRIBUTION_AND_QUOTA.md) | 大目录定位 + 目录软配额告警 |
| [guides/PROCESS_MONITORING.md](./guides/PROCESS_MONITORING.md) | 进程监控：活跃 Top-N 与僵尸进程 |

## 建议阅读顺序

1. [PRD.md](./PRD.md) — 范围与优先级  
2. [DESIGN.md](./DESIGN.md) — 架构与约束（含已确认决策）  
3. 需要落地采集或存储专题时，再进 `guides/`

## 目录结构

```text
docs/
  README.md          ← 本入口
  PRD.md
  DESIGN.md
  guides/            ← 专题 / 旁路
    COLLECTION_PERF.md
    STORAGE_ATTRIBUTION_AND_QUOTA.md
    PROCESS_MONITORING.md
```
