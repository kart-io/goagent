数据集：locomo（locomo10.json），每个方案 15 题，模型 deepseek，判定 openai:gpt-6.1-sol，嵌入 gemini:gemini-embedding-001

| 方案 | 题型 | 题数 | 准确率 | F1 | 证据召回 | 平均上下文字符 |
|---|---|---:|---:|---:|---:|---:|
| graph | multi-hop | 8 | 25.0% | 0.398 | 37.5% | 3783 |
| graph | open-domain | 2 | 0.0% | 0.000 | 0.0% | 3556 |
| graph | temporal | 10 | 40.0% | 0.244 | 40.0% | 3581 |
| graph | all | 20 | 30.0% | 0.281 | 35.0% | 3659 |
| memory | multi-hop | 8 | 50.0% | 0.382 | 12.5% | 3308 |
| memory | open-domain | 2 | 0.0% | 0.143 | 0.0% | 3301 |
| memory | temporal | 10 | 20.0% | 0.157 | 40.0% | 3264 |
| memory | all | 20 | 30.0% | 0.246 | 25.0% | 3285 |
| recent | multi-hop | 8 | 12.5% | 0.135 | 0.0% | 4000 |
| recent | open-domain | 2 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | temporal | 10 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | all | 20 | 5.0% | 0.054 | 0.0% | 4000 |
