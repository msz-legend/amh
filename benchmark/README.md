# amh 公开基准（host-eval 协议）

任何人可以在自己的机器上复现 amh 的检索质量。两条协议：

## 1. 确定性评测（`amh eval`，随仓库内置）

```sh
amh eval
# eval: corpus=100 memories (20 topics × 5 variants), queries=30
# eval: recall@5 = 1.000 (threshold 0.85) PASS
```

- 生成 100 条记忆（20 个主题 × 5 个改写变体，seed 固定，完全确定性）
- 30 个查询，指标 recall@5（top-5 中同主题记忆的占比）
- 通过阈值 **recall@5 ≥ 0.85**，不达标 exit 1（可接 CI）
- 复现：`./run.sh`（unix）/ `./run.ps1`（Windows）

## 2. LoCoMo 子集（beta，`locomo_recall.py`）

对齐 memhub 论文使用的 LoCoMo 多轮对话基准的**检索子集**口径：

- 把 LoCoMo 对话切分为句子级记忆入库（amh Insert）
- 用 category-1（single-hop）问题作查询，测 recall@5
- 注意：只测检索召回，不测时间推理/多跳；结果与 memhub 报告的 LoCoMo 总分**不可直接对比**
- 运行：`python3 locomo_recall.py --top-k 5`（需要 `datasets`，首次下载约 100MB）

## host-eval 协议（供第三方宿主接入评测）

任何 Agent 宿主想报告"接入 amh 后的记忆命中率"，按以下步骤：

1. 安装 amh 并 `amh connect --agent <host>`
2. 跑固定脚本化会话（≥20 个决定性事实，分散在 ≥3 个项目目录）
3. 新开 N 个会话，每会话问 5 个跨项目问题，记录 session-start 注入 + MCP recall 的命中数
4. 报告：`host-eval: hits=X/(N*5), protocol=amh/v1`，并附会话脚本

发布时请在 PR 中附上你的 host-eval 结果，我们会收录到本目录。
