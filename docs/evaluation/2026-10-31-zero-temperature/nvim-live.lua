local root = assert(os.getenv("GOAGENT_LIVE_ROOT"))
vim.opt.runtimepath:prepend(root .. "/codecompanion")
vim.opt.runtimepath:prepend((os.getenv("GOAGENT_PLENARY_ROOT") or (root .. "/plenary")))
vim.cmd("cd " .. vim.fn.fnameescape(root .. "/workspace"))
local report = { client = "Neovim/CodeCompanion", tests = {}, events = {} }
local connections = {}
for _,name in ipairs({"nvim-agent-stderr.log","nvim-agent-stdout.jsonl"}) do
  local f=assert(io.open(root.."/"..name,"w"));f:close()
end
local function save()
  vim.fn.writefile({vim.json.encode(report)}, root .. "/nvim-report.json")
end
local function record(name, value)
  report.tests[#report.tests + 1] = {name=name, result=value}
  save()
end
local function must(value, message)
  if not value then error(message, 2) end
  return value
end
local function connect()
  local adapter = require("codecompanion.adapters.acp").extend("cagent", {
    name="goagent_validation", formatted_name="GoAgent 真实验证",
    commands={default={root .. "/goagent-acp-live"}},
    defaults={timeout=120000,mcpServers={}},
    env={LIVE_AGENT_STORE=root .. "/state", LIVE_AGENT_MODEL="llama3.1:8b"},
  })
  local connection = require("codecompanion.acp").new({adapter=adapter,opts={
    job=function(cmd,opts,exit)
      local previous_stdout=opts.stdout
      opts.stdout=function(err,data)
        if data then local f=assert(io.open(root.."/nvim-agent-stdout.jsonl","a"));f:write(data);f:close() end
        if previous_stdout then previous_stdout(err,data) end
      end
      local previous=opts.stderr
      opts.stderr=function(err,data)
        if data then local f=assert(io.open(root.."/nvim-agent-stderr.log","a"));f:write(data);f:close() end
        if previous then previous(err,data) end
      end
      return vim.system(cmd,opts,exit)
    end,
  }})
  must(connection:connect_and_initialize(),"实际 ACP 进程握手/建会话失败")
  connections[#connections+1]=connection
  return connection
end
local function prompt(c,text,deny,cancel_on_running)
  local output,updates,permissions={}, {}, {}
  local done,reason,failed=false,nil,nil
  local cancel_pid=nil
  local p = c:session_prompt({{role="user",content=text,_meta={sent=false}}})
  p:on_message_chunk(function(chunk) output[#output+1]=chunk end)
    :on_tool_call(function(call) updates[#updates+1]=call end)
    :on_tool_update(function(update)
      updates[#updates+1]=update
      if cancel_on_running and update.status=="in_progress" then
        -- 在操作已开始后稍等，验证实际命令进程已创建，再通过原生客户端取消。
        vim.defer_fn(function()
          local processes=vim.system({"ps","-axo","pid=,ppid=,command="},{text=true}):wait().stdout
          for line in processes:gmatch("[^\n]+") do
            local pid,ppid,command=line:match("^%s*(%d+)%s+(%d+)%s+(.+)$")
            if tonumber(ppid)==c._state.handle.pid and command:match("sleep%s+30") then cancel_pid=tonumber(pid) end
          end
          p:cancel()
        end,500)
      end
    end)
    :on_permission_request(function(req)
      permissions[#permissions+1]=req.tool_call
      local kind=deny and "reject_once" or "allow_once"
      for _,option in ipairs(req.options) do
        if option.kind==kind then req.respond(option.optionId,false);return end
      end
      req.respond(nil,true)
    end)
    :on_complete(function(stop) reason=stop;done=true end)
    :on_error(function(err) failed=err;done=true end)
    :send()
  if cancel_on_running then
    must(vim.wait(120000,function()
      local response=c.pending_responses[p._request_id]
      if response then
        if response[2] then failed=response[2] else reason=response[1].stopReason end
        done=true
      end
      return done
    end,20),"取消后未收到服务端终态响应")
  else
    must(vim.wait(180000,function()return done end,20),"真实模型请求超时")
  end
  if cancel_on_running then
    must(cancel_pid,"未观察到 Agent 启动的真实 sleep 进程")
    must(vim.wait(5000,function() return not vim.uv.kill(cancel_pid,0) end,20),"取消响应后真实进程仍存在")
  end
  local result={cancelled_pid=cancel_pid,text=table.concat(output),stopReason=reason,error=failed,updates=updates,permissions=permissions}
  report.events[#report.events+1]=result;save()
  must(not failed,"真实请求失败："..vim.inspect(failed))
  return result
end
local function close(c)
  must(c:send_rpc_request("session/close",{sessionId=c.session_id}),"关闭会话失败")
  c:disconnect()
end
local ok,err=xpcall(function()
  local c=connect()
  report.agent=c._agent_info;report.client_parameters=c.adapter_modified.parameters
  report.agent_pid=c._state.handle.pid
  record("握手与实际进程",{pid=report.agent_pid,session_id=c.session_id})
  local answer=prompt(c,"Do not call any tools. Reply with exactly LIVE_MODEL_OK.",false,false)
  must(answer.text:find("LIVE_MODEL_OK",1,true),"真实模型没有返回指定标记")
  record("真实 Ollama 模型回答",answer.text)
  vim.fn.writefile({"DISK_OLD"},root.."/workspace/probe.txt")
  vim.cmd("edit "..vim.fn.fnameescape(root.."/workspace/probe.txt"))
  vim.api.nvim_buf_set_lines(0,0,-1,false,{"EDITOR_UNSAVED_739184"})
  local read=prompt(c,'Use read_file now with path="probe.txt". Return exactly the file content, do not guess.',false,false)
  must(read.text:find("DISK_OLD",1,true),"客户端磁盘读取结果异常")
  must(vim.fn.readfile(root.."/workspace/probe.txt")[1]=="DISK_OLD","验证缓冲区意外写盘")
  record("编辑器文件读取",{text=read.text,unsaved_buffer_supported=false,client_limitation="CodeCompanion v20.0.1 read_text_file 使用磁盘读取"})
  local written=prompt(c,'Use write_file now with path="nested/result.txt" and content="WRITE_REAL_482901". Confirm after the tool succeeds.',false,false)
  must(vim.fn.filereadable(root.."/workspace/nested/result.txt")==1,"真实写入产物不存在")
  must(vim.fn.readfile(root.."/workspace/nested/result.txt")[1]=="WRITE_REAL_482901","真实写入内容不一致")
  must(#written.permissions>0,"写入未触发真实审批协议")
  record("审批允许与文件写入",{path="nested/result.txt",content="WRITE_REAL_482901",permissions=#written.permissions})
  local command=prompt(c,'Use run_command now with argv=["printf","EXEC_REAL_529103"]. This command only prints a test marker. Return the actual output.',false,false)
  must(command.text:find("EXEC_REAL_529103",1,true),"真实命令输出未返回")
  must(#command.permissions>0,"命令执行未触发审批")
  record("命令执行与允许审批",command.text)
  local denied=prompt(c,'Call write_file with path="denied.txt" and content="MUST_NOT_WRITE". If permission is rejected, do not retry.',true,false)
  must(#denied.permissions>0,"拒绝场景未请求审批")
  must(vim.fn.filereadable(root.."/workspace/denied.txt")==0,"审批拒绝仍产生写入")
  record("审批拒绝无副作用",{permissions=#denied.permissions,artifact_exists=false})
  local cancelled=prompt(c,'Call run_command now with argv=["sleep","30"]. This is a cancellation test. Wait until the tool completes. Do not call other tools.',false,true)
  must(cancelled.stopReason=="cancelled","未收到服务端 cancelled")
  record("运行中取消",{stopReason=cancelled.stopReason,pid=cancelled.cancelled_pid,process_stopped=true})
  local sid=c.session_id
  local listed=c:session_list()
  must(#listed>0,"会话列表为空")
  local same_replay={}
  must(c:load_session(sid,{on_session_update=function(update)same_replay[#same_replay+1]=update end}),"同连接重载失败")
  must(#same_replay>0,"同连接重载未回放历史")
  record("同连接重载",{session_id=sid,replayed=#same_replay})
  close(c)
  local c2=connect()
  must(c2:send_rpc_request("session/close",{sessionId=c2.session_id}),"关闭临时会话失败")
  local replay={}
  must(c2:load_session(sid,{on_session_update=function(update)replay[#replay+1]=update end}),"实际新进程恢复失败")
  must(#replay>0,"恢复未回放历史")
  local continued=prompt(c2,"Based on our earlier conversation, what exact text did read_file return for probe.txt? Do not call tools; reply only with that text.",false,false)
  must(continued.text:find("DISK_OLD",1,true),"恢复后模型没有先前对话历史")
  record("关闭/跨进程恢复/继续对话",{session_id=sid,replayed=#replay,text=continued.text})
  close(c2)
  report.pass=true
end,debug.traceback)
if not ok then report.pass=false;report.error=err end
for _,c in ipairs(connections) do
  if c._state.handle then pcall(function()c:disconnect()end) end
end
save()
if ok then print("真实 Neovim ACP 验证通过");vim.cmd("qa!")else print(err);vim.cmd("cquit 1")end
