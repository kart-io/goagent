数据集：locomo（locomo10.json），每个方案 20 题，模型 deepseek，判定 deepseek，嵌入 gemini:gemini-embedding-001

| 方案 | 题型 | 题数 | 准确率 | F1 | 证据召回 | 平均上下文字符 |
|---|---|---:|---:|---:|---:|---:|
| community | multi-hop | 8 | 62.5% | 0.376 | 12.5% | 3561 |
| community | open-domain | 2 | 50.0% | 0.167 | 0.0% | 3036 |
| community | temporal | 10 | 70.0% | 0.544 | 50.0% | 3479 |
| community | all | 20 | 65.0% | 0.439 | 30.0% | 3468 |
| graph | multi-hop | 8 | 75.0% | 0.555 | 37.5% | 3700 |
| graph | open-domain | 2 | 50.0% | 0.167 | 0.0% | 3280 |
| graph | temporal | 10 | 70.0% | 0.420 | 80.0% | 3732 |
| graph | all | 20 | 70.0% | 0.449 | 55.0% | 3674 |
| memory | multi-hop | 8 | 62.5% | 0.449 | 25.0% | 3868 |
| memory | open-domain | 2 | 50.0% | 0.167 | 0.0% | 3948 |
| memory | temporal | 10 | 40.0% | 0.206 | 60.0% | 3783 |
| memory | all | 20 | 50.0% | 0.299 | 40.0% | 3834 |
| recent | multi-hop | 8 | 12.5% | 0.135 | 0.0% | 4000 |
| recent | open-domain | 2 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | temporal | 10 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | all | 20 | 5.0% | 0.054 | 0.0% | 4000 |
