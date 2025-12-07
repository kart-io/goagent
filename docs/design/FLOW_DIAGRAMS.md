# GoAgent 流程图

本文档详细展示 GoAgent 框架中各核心流程的流程图。

## 1. Agent 生命周期流程

```mermaid
flowchart TB
    START([开始]) --> CREATE[创建 AgentBuilder]
    CREATE --> CONFIG[配置 Agent]

    subgraph CONFIG_PHASE[配置阶段]
        CONFIG --> SET_LLM[设置 LLM Client]
        SET_LLM --> SET_PROMPT[设置 System Prompt]
        SET_PROMPT --> SET_TOOLS[设置 Tools]
        SET_TOOLS --> SET_MW[设置 Middleware]
        SET_MW --> SET_MEMORY[设置 MemoryManager]
        SET_MEMORY --> SET_STORE[设置 Store]
        SET_STORE --> SET_CP[设置 Checkpointer]
        SET_CP --> BUILD[调用 Build]
    end

    BUILD --> VALIDATE{验证配置}
    VALIDATE -->|失败| ERROR1[返回配置错误]
    VALIDATE -->|成功| INIT[初始化 Agent]
    INIT --> READY[Agent 就绪]

    subgraph RUN_PHASE[运行阶段]
        READY --> INVOKE[接收 Invoke 调用]
        INVOKE --> PROCESS[处理请求]
        PROCESS --> RESPONSE[返回响应]
        RESPONSE --> INVOKE
    end

    READY --> STOP[停止 Agent]
    STOP --> CLEANUP[清理资源]
    CLEANUP --> END([结束])

    ERROR1 --> END
```

## 2. 请求处理流程

```mermaid
flowchart TB
    INPUT([接收输入]) --> VALIDATE[验证输入]
    VALIDATE -->|无效| ERROR[返回验证错误]
    VALIDATE -->|有效| CONTEXT[创建上下文]

    CONTEXT --> LOAD_HISTORY[加载对话历史]
    LOAD_HISTORY --> BUILD_MSG[构建消息列表]

    BUILD_MSG --> ADD_SYSTEM{有系统提示?}
    ADD_SYSTEM -->|是| SYSTEM[添加系统消息]
    ADD_SYSTEM -->|否| HISTORY
    SYSTEM --> HISTORY[添加历史消息]

    HISTORY --> ADD_USER[添加用户消息]
    ADD_USER --> MW_BEFORE[执行前置中间件]

    MW_BEFORE --> LLM[调用 LLM]
    LLM --> LLM_ERROR{LLM 错误?}
    LLM_ERROR -->|是| HANDLE_ERROR[处理错误]
    LLM_ERROR -->|否| PARSE[解析响应]

    PARSE --> TOOL_CALL{需要工具调用?}
    TOOL_CALL -->|是| EXEC_TOOL[执行工具]
    EXEC_TOOL --> TOOL_RESULT[获取结果]
    TOOL_RESULT --> BUILD_MSG

    TOOL_CALL -->|否| MW_AFTER[执行后置中间件]
    MW_AFTER --> SAVE_CONV[保存对话]

    SAVE_CONV --> AUTO_SAVE{启用自动保存?}
    AUTO_SAVE -->|是| CHECKPOINT[保存检查点]
    AUTO_SAVE -->|否| OUTPUT
    CHECKPOINT --> OUTPUT[返回输出]

    HANDLE_ERROR --> ERROR
    ERROR --> END([结束])
    OUTPUT --> END
```

## 3. ReAct 推理流程

```mermaid
flowchart TB
    START([开始]) --> INPUT[接收输入]
    INPUT --> INIT[初始化状态]
    INIT --> ITER_CHECK{迭代次数 < 最大?}

    ITER_CHECK -->|否| MAX_ITER[达到最大迭代]
    MAX_ITER --> FINAL_ANS[生成最终答案]

    ITER_CHECK -->|是| THINK[思考阶段]

    subgraph THINK_PHASE[思考]
        THINK --> LLM_THINK[调用 LLM 生成 Thought]
        LLM_THINK --> PARSE_THOUGHT[解析思考结果]
    end

    PARSE_THOUGHT --> DONE{思考表示完成?}
    DONE -->|是| EXTRACT[提取最终答案]
    EXTRACT --> OUTPUT[返回输出]

    DONE -->|否| ACT[行动阶段]

    subgraph ACT_PHASE[行动]
        ACT --> SELECT_ACTION[选择行动]
        SELECT_ACTION --> PARSE_ACTION[解析行动]
        PARSE_ACTION --> GET_TOOL[获取工具]
    end

    GET_TOOL --> TOOL_EXIST{工具存在?}
    TOOL_EXIST -->|否| TOOL_ERROR[工具错误]
    TOOL_ERROR --> UPDATE_OBS[更新观察]

    TOOL_EXIST -->|是| OBSERVE[观察阶段]

    subgraph OBSERVE_PHASE[观察]
        OBSERVE --> EXEC_TOOL[执行工具]
        EXEC_TOOL --> GET_RESULT[获取结果]
        GET_RESULT --> FORMAT_OBS[格式化观察]
    end

    FORMAT_OBS --> UPDATE_OBS
    UPDATE_OBS --> INC_ITER[迭代计数+1]
    INC_ITER --> ITER_CHECK

    FINAL_ANS --> OUTPUT
    OUTPUT --> END([结束])
```

## 4. Tool 执行流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收工具调用]
    RECEIVE --> LOOKUP[在注册表中查找]

    LOOKUP --> FOUND{找到工具?}
    FOUND -->|否| NOT_FOUND[ToolNotFoundError]
    NOT_FOUND --> ERROR_OUT[返回错误]

    FOUND -->|是| GET_SCHEMA[获取参数 Schema]
    GET_SCHEMA --> VALIDATE[验证参数]

    VALIDATE --> VALID{参数有效?}
    VALID -->|否| INVALID[ValidationError]
    INVALID --> ERROR_OUT

    VALID -->|是| CUSTOM_VALID{实现 ValidatableTool?}
    CUSTOM_VALID -->|是| CUSTOM_CHECK[执行自定义验证]
    CUSTOM_CHECK --> CUSTOM_OK{验证通过?}
    CUSTOM_OK -->|否| CUSTOM_ERROR[返回验证错误]
    CUSTOM_ERROR --> ERROR_OUT

    CUSTOM_VALID -->|否| MW_BEFORE
    CUSTOM_OK -->|是| MW_BEFORE[执行前置中间件]

    MW_BEFORE --> EXEC[执行工具]
    EXEC --> EXEC_ERROR{执行错误?}

    EXEC_ERROR -->|是| RETRY_CHECK{可重试?}
    RETRY_CHECK -->|是| RETRY[等待重试]
    RETRY --> RETRY_COUNT{重试次数 < 最大?}
    RETRY_COUNT -->|是| EXEC
    RETRY_COUNT -->|否| EXEC_FAIL[ExecutionError]
    RETRY_CHECK -->|否| EXEC_FAIL
    EXEC_FAIL --> ERROR_OUT

    EXEC_ERROR -->|否| MW_AFTER[执行后置中间件]
    MW_AFTER --> RESULT[构建 ToolResult]
    RESULT --> SUCCESS[返回成功结果]

    ERROR_OUT --> END([结束])
    SUCCESS --> END
```

## 5. 中间件链执行流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收请求]
    RECEIVE --> INIT_CHAIN[初始化中间件链]

    INIT_CHAIN --> HAS_MW{还有中间件?}

    subgraph BEFORE_PHASE[前置处理]
        HAS_MW -->|是| GET_MW[获取下一个中间件]
        GET_MW --> BEFORE[执行 OnBefore]
        BEFORE --> BEFORE_ERROR{发生错误?}
        BEFORE_ERROR -->|是| ON_ERROR[执行 OnError]
        ON_ERROR --> ERROR_OUT[返回错误]
        BEFORE_ERROR -->|否| MODIFY_REQ[更新请求]
        MODIFY_REQ --> HAS_MW
    end

    HAS_MW -->|否| HANDLER[执行 Handler]
    HANDLER --> HANDLER_ERROR{Handler 错误?}
    HANDLER_ERROR -->|是| ERROR_CHAIN[错误处理链]

    subgraph ERROR_CHAIN_PHASE[错误处理]
        ERROR_CHAIN --> EACH_MW[遍历每个中间件]
        EACH_MW --> CALL_ERROR[调用 OnError]
        CALL_ERROR --> NEXT_MW{还有更多?}
        NEXT_MW -->|是| EACH_MW
        NEXT_MW -->|否| ERROR_OUT
    end

    HANDLER_ERROR -->|否| INIT_AFTER[初始化后置处理]

    subgraph AFTER_PHASE[后置处理]
        INIT_AFTER --> HAS_MW_AFTER{还有中间件?}
        HAS_MW_AFTER -->|是| GET_MW_AFTER[获取上一个中间件]
        GET_MW_AFTER --> AFTER[执行 OnAfter]
        AFTER --> AFTER_ERROR{发生错误?}
        AFTER_ERROR -->|是| ERROR_CHAIN
        AFTER_ERROR -->|否| MODIFY_RESP[更新响应]
        MODIFY_RESP --> HAS_MW_AFTER
        HAS_MW_AFTER -->|否| RESPONSE[返回响应]
    end

    ERROR_OUT --> END([结束])
    RESPONSE --> END
```

## 6. Memory 操作流程

### 6.1 对话管理流程

```mermaid
flowchart TB
    subgraph ADD_CONV[添加对话]
        A_START([开始]) --> A_VALIDATE[验证对话数据]
        A_VALIDATE --> A_VALID{有效?}
        A_VALID -->|否| A_ERROR[返回错误]
        A_VALID -->|是| A_GENERATE_ID[生成 ID]
        A_GENERATE_ID --> A_SET_TIME[设置时间戳]
        A_SET_TIME --> A_STORE[存储到 ConversationStore]
        A_STORE --> A_SUCCESS[返回成功]
        A_ERROR --> A_END([结束])
        A_SUCCESS --> A_END
    end

    subgraph GET_HISTORY[获取历史]
        G_START([开始]) --> G_QUERY[查询 SessionID]
        G_QUERY --> G_FOUND{找到记录?}
        G_FOUND -->|否| G_EMPTY[返回空列表]
        G_FOUND -->|是| G_LIMIT{超过限制?}
        G_LIMIT -->|是| G_TRUNCATE[截断到限制]
        G_LIMIT -->|否| G_RETURN
        G_TRUNCATE --> G_RETURN[返回对话列表]
        G_EMPTY --> G_END([结束])
        G_RETURN --> G_END
    end

    subgraph CLEAR_CONV[清除对话]
        C_START([开始]) --> C_DELETE[删除 SessionID 记录]
        C_DELETE --> C_SUCCESS[返回成功]
        C_SUCCESS --> C_END([结束])
    end
```

### 6.2 案例搜索流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收搜索查询]
    RECEIVE --> EMBED[生成查询向量]

    EMBED --> EMBED_ERROR{嵌入错误?}
    EMBED_ERROR -->|是| ERROR[返回错误]
    EMBED_ERROR -->|否| SEARCH[向量相似度搜索]

    SEARCH --> HAS_RESULTS{有结果?}
    HAS_RESULTS -->|否| EMPTY[返回空列表]
    HAS_RESULTS -->|是| RANK[按相似度排序]

    RANK --> FILTER{应用过滤器?}
    FILTER -->|是| APPLY_FILTER[应用过滤条件]
    APPLY_FILTER --> LIMIT
    FILTER -->|否| LIMIT[限制结果数量]

    LIMIT --> CONVERT[转换为 Case 对象]
    CONVERT --> ADD_SCORE[添加相似度分数]
    ADD_SCORE --> RETURN[返回结果]

    ERROR --> END([结束])
    EMPTY --> END
    RETURN --> END
```

## 7. Checkpoint 流程

### 7.1 保存检查点

```mermaid
flowchart TB
    START([开始]) --> TRIGGER[触发保存]

    TRIGGER --> AUTO{自动保存?}
    AUTO -->|是| CHECK_INTERVAL[检查保存间隔]
    CHECK_INTERVAL --> SHOULD_SAVE{需要保存?}
    SHOULD_SAVE -->|否| SKIP[跳过保存]
    SHOULD_SAVE -->|是| PREPARE
    AUTO -->|否| PREPARE[准备检查点]

    PREPARE --> GET_STATE[获取当前状态]
    GET_STATE --> GENERATE_ID[生成检查点 ID]
    GENERATE_ID --> CREATE_CP[创建 Checkpoint 对象]

    CREATE_CP --> SERIALIZE[序列化状态]
    SERIALIZE --> SER_ERROR{序列化错误?}
    SER_ERROR -->|是| ERROR[返回错误]

    SER_ERROR -->|否| PERSIST[持久化到存储]
    PERSIST --> PERSIST_ERROR{持久化错误?}
    PERSIST_ERROR -->|是| ERROR

    PERSIST_ERROR -->|否| UPDATE_META[更新元数据]
    UPDATE_META --> RETURN_ID[返回检查点 ID]

    SKIP --> END([结束])
    ERROR --> END
    RETURN_ID --> END
```

### 7.2 恢复检查点

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收检查点 ID]
    RECEIVE --> LOOKUP[查找检查点]

    LOOKUP --> FOUND{找到?}
    FOUND -->|否| NOT_FOUND[NotFoundError]
    NOT_FOUND --> ERROR[返回错误]

    FOUND -->|是| LOAD[加载检查点数据]
    LOAD --> LOAD_ERROR{加载错误?}
    LOAD_ERROR -->|是| ERROR

    LOAD_ERROR -->|否| DESERIALIZE[反序列化状态]
    DESERIALIZE --> DES_ERROR{反序列化错误?}
    DES_ERROR -->|是| ERROR

    DES_ERROR -->|否| VALIDATE[验证状态完整性]
    VALIDATE --> VALID{状态有效?}
    VALID -->|否| INVALID[StateCorruptedError]
    INVALID --> ERROR

    VALID -->|是| RESTORE[恢复状态]
    RESTORE --> UPDATE[更新 Agent 状态]
    UPDATE --> SUCCESS[返回成功]

    ERROR --> END([结束])
    SUCCESS --> END
```

## 8. LLM 调用流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收请求]
    RECEIVE --> BUILD[构建请求体]

    BUILD --> HAS_TOOLS{包含工具?}
    HAS_TOOLS -->|是| ADD_TOOLS[添加工具定义]
    ADD_TOOLS --> SEND
    HAS_TOOLS -->|否| SEND[发送请求]

    SEND --> TIMEOUT{超时?}
    TIMEOUT -->|是| TIMEOUT_ERROR[TimeoutError]
    TIMEOUT_ERROR --> SHOULD_RETRY

    TIMEOUT -->|否| RESPONSE[接收响应]
    RESPONSE --> STATUS{响应状态}

    STATUS -->|200| PARSE[解析响应]
    STATUS -->|429| RATE_LIMIT[速率限制]
    STATUS -->|500-503| SERVER_ERROR[服务器错误]
    STATUS -->|400-403| CLIENT_ERROR[客户端错误]

    RATE_LIMIT --> SHOULD_RETRY{可重试?}
    SERVER_ERROR --> SHOULD_RETRY

    SHOULD_RETRY -->|是| WAIT[等待退避]
    WAIT --> RETRY_COUNT{重试次数 < 最大?}
    RETRY_COUNT -->|是| SEND
    RETRY_COUNT -->|否| FINAL_ERROR[返回最终错误]

    SHOULD_RETRY -->|否| FINAL_ERROR
    CLIENT_ERROR --> FINAL_ERROR

    PARSE --> PARSE_ERROR{解析错误?}
    PARSE_ERROR -->|是| FINAL_ERROR
    PARSE_ERROR -->|否| EXTRACT[提取内容]

    EXTRACT --> HAS_TOOL_CALLS{有工具调用?}
    HAS_TOOL_CALLS -->|是| EXTRACT_TOOLS[提取工具调用]
    EXTRACT_TOOLS --> BUILD_RESP
    HAS_TOOL_CALLS -->|否| BUILD_RESP[构建响应]

    BUILD_RESP --> RETURN[返回响应]

    FINAL_ERROR --> END([结束])
    RETURN --> END
```

## 9. 并行执行流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收任务列表]
    RECEIVE --> ANALYZE[分析依赖关系]

    ANALYZE --> HAS_DEPS{有依赖?}

    HAS_DEPS -->|是| BUILD_GRAPH[构建依赖图]
    BUILD_GRAPH --> TOPO_SORT[拓扑排序]
    TOPO_SORT --> GROUP[按层分组]

    HAS_DEPS -->|否| SINGLE_GROUP[单层分组]
    SINGLE_GROUP --> PARALLEL

    GROUP --> PROCESS_LAYER[处理当前层]

    subgraph PARALLEL[并行执行]
        PROCESS_LAYER --> SPAWN[生成 Goroutines]
        SPAWN --> G1[Goroutine 1]
        SPAWN --> G2[Goroutine 2]
        SPAWN --> GN[Goroutine N]

        G1 --> EXEC1[执行任务]
        G2 --> EXEC2[执行任务]
        GN --> EXECN[执行任务]

        EXEC1 --> COLLECT[收集结果]
        EXEC2 --> COLLECT
        EXECN --> COLLECT
    end

    COLLECT --> WAIT[等待所有完成]
    WAIT --> CHECK_ERROR{有错误?}
    CHECK_ERROR -->|是| HANDLE_ERROR[处理错误]

    CHECK_ERROR -->|否| NEXT_LAYER{还有下一层?}
    NEXT_LAYER -->|是| PROCESS_LAYER
    NEXT_LAYER -->|否| MERGE[合并结果]

    HANDLE_ERROR --> STOP_OTHERS{停止其他?}
    STOP_OTHERS -->|是| CANCEL[取消上下文]
    CANCEL --> MERGE
    STOP_OTHERS -->|否| NEXT_LAYER

    MERGE --> RETURN[返回结果]
    RETURN --> END([结束])
```

## 10. 错误处理流程

```mermaid
flowchart TB
    START([发生错误]) --> CLASSIFY[错误分类]

    CLASSIFY --> TYPE{错误类型}

    TYPE -->|ValidationError| VALIDATE_HANDLE[验证错误处理]
    VALIDATE_HANDLE --> LOG_WARN[记录警告]
    LOG_WARN --> RETURN_400[返回 400 错误]

    TYPE -->|NotFoundError| NOT_FOUND_HANDLE[未找到处理]
    NOT_FOUND_HANDLE --> LOG_INFO[记录信息]
    LOG_INFO --> RETURN_404[返回 404 错误]

    TYPE -->|TimeoutError| TIMEOUT_HANDLE[超时处理]
    TIMEOUT_HANDLE --> RETRY_CHECK{可重试?}
    RETRY_CHECK -->|是| RETRY[执行重试]
    RETRY --> RETRY_SUCCESS{重试成功?}
    RETRY_SUCCESS -->|是| RETURN_OK[返回成功]
    RETRY_SUCCESS -->|否| RETRY_COUNT{还能重试?}
    RETRY_COUNT -->|是| RETRY
    RETRY_COUNT -->|否| RETURN_TIMEOUT
    RETRY_CHECK -->|否| RETURN_TIMEOUT[返回超时错误]

    TYPE -->|LLMError| LLM_HANDLE[LLM 错误处理]
    LLM_HANDLE --> LLM_TYPE{LLM 错误类型}
    LLM_TYPE -->|RateLimit| WAIT_BACKOFF[等待退避]
    WAIT_BACKOFF --> RETRY
    LLM_TYPE -->|AuthError| RETURN_AUTH[返回认证错误]
    LLM_TYPE -->|Other| LOG_ERROR[记录错误]
    LOG_ERROR --> RETURN_500[返回 500 错误]

    TYPE -->|ToolError| TOOL_HANDLE[工具错误处理]
    TOOL_HANDLE --> TOOL_RETRY{工具可重试?}
    TOOL_RETRY -->|是| RETRY
    TOOL_RETRY -->|否| RETURN_TOOL_ERR[返回工具错误]

    TYPE -->|Unknown| UNKNOWN_HANDLE[未知错误处理]
    UNKNOWN_HANDLE --> LOG_ERROR
    LOG_ERROR --> WRAP[包装错误]
    WRAP --> RETURN_500

    RETURN_400 --> CALLBACK[执行错误回调]
    RETURN_404 --> CALLBACK
    RETURN_TIMEOUT --> CALLBACK
    RETURN_AUTH --> CALLBACK
    RETURN_500 --> CALLBACK
    RETURN_TOOL_ERR --> CALLBACK
    RETURN_OK --> END([结束])

    CALLBACK --> EMIT[发送错误事件]
    EMIT --> END
```

## 11. 缓存流程

```mermaid
flowchart TB
    START([开始]) --> RECEIVE[接收请求]
    RECEIVE --> HASH[计算请求哈希]

    HASH --> SHARD[确定分片]
    SHARD --> LOCK[获取读锁]
    LOCK --> LOOKUP[查找缓存]

    LOOKUP --> HIT{缓存命中?}

    HIT -->|是| CHECK_TTL[检查 TTL]
    CHECK_TTL --> EXPIRED{已过期?}
    EXPIRED -->|是| UNLOCK_R[释放读锁]
    UNLOCK_R --> MISS_FLOW
    EXPIRED -->|否| GET_VALUE[获取缓存值]
    GET_VALUE --> UNLOCK_R2[释放读锁]
    UNLOCK_R2 --> RETURN_CACHED[返回缓存结果]

    HIT -->|否| UNLOCK_R3[释放读锁]
    UNLOCK_R3 --> MISS_FLOW[缓存未命中流程]

    subgraph MISS_FLOW[缓存未命中]
        MISS_FLOW --> EXECUTE[执行实际请求]
        EXECUTE --> EXEC_ERROR{执行错误?}
        EXEC_ERROR -->|是| RETURN_ERROR[返回错误]
        EXEC_ERROR -->|否| SHOULD_CACHE{应该缓存?}
        SHOULD_CACHE -->|否| RETURN_RESULT[返回结果]
        SHOULD_CACHE -->|是| WRITE_LOCK[获取写锁]
        WRITE_LOCK --> STORE_CACHE[存储到缓存]
        STORE_CACHE --> CHECK_SIZE{超过容量?}
        CHECK_SIZE -->|是| EVICT[LRU 淘汰]
        EVICT --> UNLOCK_W
        CHECK_SIZE -->|否| UNLOCK_W[释放写锁]
        UNLOCK_W --> RETURN_RESULT
    end

    RETURN_CACHED --> END([结束])
    RETURN_RESULT --> END
    RETURN_ERROR --> END
```

## 相关文档

- [时序图详解](SEQUENCE_DIAGRAMS.md)
- [设计概述](DESIGN_OVERVIEW.md)
- [架构概述](../architecture/ARCHITECTURE.md)
