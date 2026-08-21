# ResourceHub

多服务器资源监控（CPU / GPU / 内存 / 磁盘等）：轻量 Agent 采集 + 中央服务 + Web 看板。

## 当前状态

**文档先行阶段。** 代码骨架尚未实现。

**文档入口 → [docs/README.md](docs/README.md)**

| 主文档 | 说明 |
|--------|------|
| [docs/PRD.md](docs/PRD.md) | 产品需求 |
| [docs/DESIGN.md](docs/DESIGN.md) | 总体设计 |

专题指南（采集性能、目录软配额等）见 [`docs/guides/`](docs/guides/)。

## 设计要点（摘要）

- **Agent 推送**指标到 Server；看板查询只读已存储快照，不在线扫远程机器  
- **磁盘容量**使用 `statfs` 等 O(1) 接口，**禁止**在热路径使用 `du` / 递归 walk  
- 目录级分析与软配额走**异步慢路径**（默关、限流）；限额仅告警/提醒，不拦写入  

## 计划中的仓库结构

```text
agent/    # 采集端
server/   # 后端 API
web/      # 前端
deploy/   # 部署与安装
docs/     # 设计与规范（入口 docs/README.md）
```

## 后续

确认 [docs/DESIGN.md](docs/DESIGN.md) 中的开放问题后，按 Phase 1 初始化 Agent / Server / Web 工程。
