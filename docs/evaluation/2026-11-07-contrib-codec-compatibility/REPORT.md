# Contrib 八项兼容失败修复

此前跨仓15模块中7通过、8构建失败。本轮修复后 **15/15通过，0失败**：65个包、1663项测试通过，125项依赖外部服务等条件的测试跳过并独立保留。跳过不算已执行。

本轮核心实现提交 `4e0d38d`，Contrib实现提交 `abc3260`，分别在独立工作树验证。两个原始master工作区保持干净；未推送、合并或发布版本，未调用真实模型或生产服务。

## 根因与修复

Contrib的12个源文件仍导入已删除的`github.com/kart-io/goagent/utils/json`。直接依赖和传递依赖共同阻断document、document/pdf、retrieval、retrieval/cohere、retrieval/qdrant、retrieval/vertexai、tools/database和tools/practical八个模块。

- 12处迁移为`github.com/kart-io/goagent/codec/json`。调用仅涉及Marshal/Unmarshal；旧Sonic.ConfigStd与新入口均以标准库兼容语义处理JSON，现有业务回归保留。
- 八模块最低核心版本更新到已经实际发布的`v0.49.2`，Go最低版本1.27.0；工作区与CI工具链更新为1.27.3，修正脚本把workspace强行降到1.26.0的问题。
- retrieval、database、practical三个叶子模块独立tidy，移除不再使用的Sonic间接依赖，更新真实版本校验和。
- 核心compatcheck支持仓库根自身为符号链接时解析到真实工作树，报告记录实际源码路径。普通目录的既有路径表示保留；测试覆盖两个嵌套模块，不会误测未修改的原master。

没有恢复旧包兼容别名，没有修改JSON业务规则，也没有把绝对replace写入发布go.mod。

## 验证

四个基础模块先复现旧导入构建失败，再迁移并运行完整race；其余子模块由联合矩阵覆盖。工作区版本问题与链接发现问题也先RED，再修复并运行回归。

| 检查 | 结果 |
|---|---|
| 当前核心＋修复Contrib＋Harness＋Examples | 15模块全部通过 |
| Contrib当前源码＋真实发布核心v0.49.2 | 11模块vet、race全部通过 |
| Contrib全部模块lint | 11模块均0问题 |
| GOWORK=off、-mod=readonly、发布核心v0.49.2 | retrieval、database、practical均通过race |
| 核心全仓race | 60测试包通过 |
| 核心build、vet、lint、独立frameworkbench race | 全部通过，lint0问题 |
| 两仓独立只读代码审查 | Critical0、Important0、Minor0 |

Go模块下载记录核对v0.49.2来源为正式tag，提交497a491；用于发布核心测试的go list记录无Replace字段。Contrib模块间仍按仓库已有临时workspace使用当前源码，不能把它误称为已发布Contrib依赖的验证。

重复验证：

```sh
# Contrib仓库：不覆盖核心，使用go.mod选择的正式发布版本。
GO_TEST_MODE=true bash scripts/test-all.sh -timeout=180s
bash scripts/lint-all.sh

# Contrib叶子模块：关闭workspace验证锁定依赖。
GOWORK=off GO_TEST_MODE=true go test -mod=readonly -race -count=1 ./...

# 核心仓库：roots目录下goagent/contrib/examples/harness可链接到实际工作树。
go run ./scripts/compatcheck -root /path/to/roots -core-dir /path/to/current/goagent -out /new/report

python3 docs/evaluation/2026-11-07-contrib-codec-compatibility/verify.py
```

使用Go1.27.3并清除陈旧GOROOT。检查程序会核对核心与Contrib的真实目录，防止用原master生成通过报告；不改写被测仓库的go.mod、go.sum或用户工作区。

## 发布边界

本轮完成当前源码兼容修复，尚未发布Contrib新版本。document等模块锁定的旧版retrieval仍使用utils/json，单模块`go mod tidy`不能借助workspace看到未发布源码，该失败记录也保留。

发布时须先发布迁移后的retrieval，再更新并发布依赖它的document、cohere、qdrant、vertexai，最后更新PDF依赖。版本锁定应在联合发布时更新，不能伪造未发布版本。本轮不宣称尚未发布的Contrib版本已经可从远端独立安装。

## 证据

[摘要](summary.json)、[完整证据](evidence.json.gz)、[逐文件清单](manifest.json)、[离线核对程序](verify.py)。100份脱敏证据包含修复前RED、业务回归、正式核心版本来源、全模块检查、15模块逐项JSON日志、独立审查和源码快照，SHA-256与长度逐份校验。

此前[五项设计改进报告](../2026-11-07-execution-design-hardening/REPORT.md)中保留的8项失败属于历史结果，本报告提供其后续修复结果，不改写旧记录。
