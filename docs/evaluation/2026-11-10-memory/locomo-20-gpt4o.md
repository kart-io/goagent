数据集：locomo（locomo10.json），每个方案 20 题，模型 deepseek，判定 openai:gpt-4o-2024-08-06，嵌入 gemini:gemini-embedding-001

| 方案 | 题型 | 题数 | 准确率 | F1 | 证据召回 | 平均上下文字符 |
|---|---|---:|---:|---:|---:|---:|
| community | multi-hop | 8 | 25.0% | 0.306 | 12.5% | 3665 |
| community | open-domain | 2 | 0.0% | 0.167 | 50.0% | 2753 |
| community | temporal | 10 | 70.0% | 0.430 | 70.0% | 3511 |
| community | all | 20 | 45.0% | 0.354 | 45.0% | 3497 |
| graph | multi-hop | 8 | 37.5% | 0.533 | 12.5% | 3800 |
| graph | open-domain | 2 | 50.0% | 0.125 | 0.0% | 3914 |
| graph | temporal | 10 | 40.0% | 0.169 | 40.0% | 3559 |
| graph | all | 20 | 40.0% | 0.310 | 25.0% | 3691 |
| memory | multi-hop | 8 | 37.5% | 0.381 | 12.5% | 3761 |
| memory | open-domain | 2 | 50.0% | 0.305 | 50.0% | 3712 |
| memory | temporal | 10 | 40.0% | 0.157 | 40.0% | 3690 |
| memory | all | 20 | 40.0% | 0.261 | 30.0% | 3720 |
| recent | multi-hop | 8 | 12.5% | 0.135 | 0.0% | 4000 |
| recent | open-domain | 2 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | temporal | 10 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | all | 20 | 5.0% | 0.054 | 0.0% | 4000 |
