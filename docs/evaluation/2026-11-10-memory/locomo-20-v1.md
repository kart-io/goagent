数据集：locomo（locomo10.json），每个方案 20 题，模型 deepseek，判定 deepseek，嵌入 gemini:gemini-embedding-001

| 方案 | 题型 | 题数 | 准确率 | F1 | 证据召回 | 平均上下文字符 |
|---|---|---:|---:|---:|---:|---:|
| community | multi-hop | 8 | 37.5% | 0.365 | 50.0% | 3806 |
| community | open-domain | 2 | 50.0% | 0.000 | 0.0% | 4022 |
| community | temporal | 10 | 0.0% | 0.000 | 50.0% | 3922 |
| community | all | 20 | 20.0% | 0.146 | 45.0% | 3886 |
| graph | multi-hop | 8 | 37.5% | 0.280 | 50.0% | 3910 |
| graph | open-domain | 2 | 50.0% | 0.167 | 50.0% | 3798 |
| graph | temporal | 10 | 10.0% | 0.100 | 30.0% | 3688 |
| graph | all | 20 | 25.0% | 0.179 | 40.0% | 3788 |
| memory | multi-hop | 8 | 12.5% | 0.075 | 12.5% | 3584 |
| memory | open-domain | 2 | 0.0% | 0.000 | 50.0% | 3813 |
| memory | temporal | 10 | 30.0% | 0.100 | 30.0% | 3490 |
| memory | all | 20 | 20.0% | 0.080 | 25.0% | 3560 |
| recent | multi-hop | 8 | 12.5% | 0.044 | 0.0% | 4000 |
| recent | open-domain | 2 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | temporal | 10 | 0.0% | 0.000 | 0.0% | 4000 |
| recent | all | 20 | 5.0% | 0.018 | 0.0% | 4000 |
