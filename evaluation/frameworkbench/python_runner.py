"""Real frameworks with deterministic local model protocols; no paid APIs."""
import argparse
import asyncio
import importlib.metadata
import json
import platform
import resource
import os
import socket
import time

os.environ["LANGSMITH_TRACING"]="false"
os.environ["LANGCHAIN_TRACING_V2"]="false"
os.environ["OTEL_SDK_DISABLED"]="true"

def _no_network(*args, **kwargs):
    raise RuntimeError("Network disabled for framework microbenchmark")
socket.socket.connect = _no_network
socket.socket.connect_ex = _no_network

PROMPT = "Look up refund_days for order DEMO-1001. Return only the number."

class Adapter:
    async def prepare(self): pass
    async def cleanup(self): pass
    async def close(self): pass

def make_runner(framework, scenario):
    factories = {"langgraph": graph_runner, "langchain": langchain_runner, "openai_agents": openai_runner, "google_adk": adk_runner}
    return factories[framework](scenario)

def graph_runner(scenario):
    from langgraph.graph import StateGraph, START, END
    from typing import TypedDict
    class State(TypedDict):
        value: int
    graph = StateGraph(State)
    count = 8 if scenario == "graph_8" else 1
    previous = START
    async def increment(state):
        return {"value":state["value"]+1}
    async def route(state):
        return previous if state["value"]<8 else END
    for i in range(count):
        name = f"n{i}"
        graph.add_node(name, increment)
        graph.add_edge(previous, name)
        previous = name
    if count == 1:
        graph.add_conditional_edges(previous, route)
    else:
        graph.add_edge(previous, END)
    compiled = graph.compile()
    class GraphAdapter(Adapter):
        async def run(self):
            result = await compiled.ainvoke({"value": 0}, config={"recursion_limit": 32})
            return dict(answer=str(result["value"]), model_calls=0, tool_calls=0, chunks=0)
    return GraphAdapter()

def langchain_runner(scenario):
    from langchain.agents import create_agent
    from langchain_core.language_models.chat_models import BaseChatModel
    from langchain_core.messages import AIMessage, AIMessageChunk, ToolMessage
    from langchain_core.outputs import ChatGeneration, ChatGenerationChunk, ChatResult
    from langchain_core.tools import tool
    counters = {"tool": 0}
    @tool
    async def lookup(order_id: str) -> str:
        """Look up refund_days for an order."""
        if order_id != "DEMO-1001": raise ValueError("wrong arguments")
        counters["tool"] += 1
        return "30"
    class LocalModel(BaseChatModel):
        calls: int = 0
        @property
        def _llm_type(self): return "benchmark-local"
        def bind_tools(self, tools, **kwargs): return self
        def _generate(self, messages, stop=None, run_manager=None, **kwargs):
            self.calls += 1
            if isinstance(messages[-1], ToolMessage):
                if messages[-1].content != "30" or messages[-1].tool_call_id != "call-1": raise ValueError("tool result not preserved")
                message = AIMessage(content=messages[-1].content, usage_metadata={"input_tokens":1,"output_tokens":1,"total_tokens":2})
            else:
                message = AIMessage(content="", usage_metadata={"input_tokens":1,"output_tokens":1,"total_tokens":2}, tool_calls=[dict(name="lookup", args={"order_id":"DEMO-1001"}, id="call-1", type="tool_call")])
            return ChatResult(generations=[ChatGeneration(message=message)])
        async def _agenerate(self, messages, stop=None, run_manager=None, **kwargs):
            return self._generate(messages, stop, run_manager, **kwargs)
        def _stream(self, messages, stop=None, run_manager=None, **kwargs):
            self.calls += 1
            for _ in range(64): yield ChatGenerationChunk(message=AIMessageChunk(content="x"))
        async def _astream(self, messages, stop=None, run_manager=None, **kwargs):
            for chunk in self._stream(messages,stop,run_manager,**kwargs): yield chunk
    model = LocalModel()
    agent = create_agent(model=model, tools=[lookup] if scenario=="agent_tool" else [])
    class LangchainAdapter(Adapter):
        async def run(self):
            model.calls=0; counters["tool"]=0
            first=0; chunks=0; text=""; start=time.perf_counter_ns()
            if scenario=="stream_64":
                async for chunk,_ in agent.astream({"messages":[("user",PROMPT)]}, stream_mode="messages",config={"recursion_limit":8}):
                    if chunk.content:
                        first=first or time.perf_counter_ns()-start
                        text+=chunk.content; chunks+=1
            else:
                result=await agent.ainvoke({"messages":[("user",PROMPT)]},config={"recursion_limit":8})
                text=result["messages"][-1].content
            return dict(answer=text,model_calls=model.calls,tool_calls=counters["tool"],chunks=chunks,first_ns=first)
    return LangchainAdapter()

def openai_runner(scenario):
    from agents import Agent,Model,ModelResponse,Runner,Usage,function_tool,set_tracing_disabled,RunConfig
    from openai.types.responses import Response,ResponseCompletedEvent,ResponseTextDeltaEvent,ResponseOutputMessage,ResponseOutputText,ResponseFunctionToolCall
    set_tracing_disabled(True)
    counters={"model":0,"tool":0}
    @function_tool
    async def lookup(order_id:str)->str:
        """Look up refund_days for an order."""
        if order_id!="DEMO-1001": raise ValueError("wrong arguments")
        counters["tool"]+=1
        return "30"
    def message(text):
        return ResponseOutputMessage(id="msg-1",content=[ResponseOutputText(text=text,type="output_text",annotations=[])],role="assistant",status="completed",type="message")
    class LocalModel(Model):
        async def get_response(self,*args,**kwargs):
            counters["model"]+=1
            items=kwargs.get("input",args[1] if len(args)>1 else [])
            if isinstance(items,list) and items and items[-1].get("type")=="function_call_output":
                if items[-1]["output"]!="30" or items[-1]["call_id"]!="call-1":raise ValueError("tool result not preserved")
                output=[message(items[-1]["output"])]
            else:
                output=[ResponseFunctionToolCall(id="fc-1",call_id="call-1",name="lookup",arguments='{"order_id":"DEMO-1001"}',type="function_call",status="completed")]
            return ModelResponse(output=output,usage=Usage(requests=1,input_tokens=1,output_tokens=1,total_tokens=2),response_id="resp-1")
        async def stream_response(self,*args,**kwargs):
            counters["model"]+=1
            for i in range(64):
                yield ResponseTextDeltaEvent(type="response.output_text.delta",content_index=0,output_index=0,item_id="msg-1",delta="x",logprobs=[],sequence_number=i)
            response=Response(id="resp-1",created_at=0,model="benchmark-local",object="response",output=[message("x"*64)],parallel_tool_calls=False,tool_choice="auto",tools=[],top_p=1,temperature=0,status="completed")
            yield ResponseCompletedEvent(type="response.completed",response=response,sequence_number=64)
    agent=Agent(name="bench",model=LocalModel(),tools=[lookup] if scenario=="agent_tool" else [])
    config=RunConfig(tracing_disabled=True)
    class OpenAIAdapter(Adapter):
        async def run(self):
            counters.update(model=0,tool=0)
            chunks=0; first=0; start=time.perf_counter_ns()
            if scenario=="stream_64":
                result=Runner.run_streamed(agent,input=PROMPT,max_turns=2,run_config=config)
                async for event in result.stream_events():
                    if event.type=="raw_response_event" and event.data.type=="response.output_text.delta":
                        first=first or time.perf_counter_ns()-start;chunks+=1
            else:
                result=await Runner.run(agent,input=PROMPT,max_turns=2,run_config=config)
            return dict(answer=result.final_output,model_calls=counters["model"],tool_calls=counters["tool"],chunks=chunks,first_ns=first)
    return OpenAIAdapter()

def adk_runner(scenario):
    from google.adk.agents import LlmAgent
    from google.adk.models.base_llm import BaseLlm
    from google.adk.models.llm_response import LlmResponse
    from google.adk.runners import InMemoryRunner
    from google.adk.agents.run_config import RunConfig,StreamingMode
    from google.genai import types
    counters={"model":0,"tool":0}
    async def lookup(order_id:str)->str:
        """Look up refund_days for an order."""
        if order_id!="DEMO-1001":raise ValueError("wrong arguments")
        counters["tool"]+=1
        return "30"
    class LocalModel(BaseLlm):
        async def generate_content_async(self,llm_request,stream=False):
            counters["model"]+=1
            if scenario=="stream_64":
                for _ in range(64):
                    yield LlmResponse(content=types.Content(role="model",parts=[types.Part(text="x")]),partial=True)
                yield LlmResponse(content=types.Content(role="model",parts=[types.Part(text="x"*64)]),partial=False,turn_complete=True,usage_metadata=types.GenerateContentResponseUsageMetadata(prompt_token_count=1,candidates_token_count=1,total_token_count=2))
                return
            replies=[p.function_response for p in llm_request.contents[-1].parts if p.function_response]
            if replies:
                if replies[0].response!={"result":"30"}:raise ValueError(f"tool result not preserved: {replies[0].response}")
                content=types.Content(role="model",parts=[types.Part(text=replies[0].response["result"])])
            else:
                content=types.Content(role="model",parts=[types.Part(function_call=types.FunctionCall(id="call-1",name="lookup",args={"order_id":"DEMO-1001"}))])
            yield LlmResponse(content=content,partial=False,turn_complete=True,usage_metadata=types.GenerateContentResponseUsageMetadata(prompt_token_count=1,candidates_token_count=1,total_token_count=2))
    agent=LlmAgent(name="bench",model=LocalModel(model="benchmark-local"),tools=[lookup] if scenario=="agent_tool" else [])
    runner=InMemoryRunner(agent=agent,app_name="benchmark")
    config=RunConfig(streaming_mode=StreamingMode.SSE if scenario=="stream_64" else StreamingMode.NONE,max_llm_calls=2)
    class ADKAdapter(Adapter):
        sequence=0
        async def prepare(self):
            self.sequence+=1; self.session_id=str(self.sequence)
            await runner.session_service.create_session(app_name="benchmark",user_id="bench",session_id=self.session_id)
        async def cleanup(self):
            await runner.session_service.delete_session(app_name="benchmark",user_id="bench",session_id=self.session_id)
        async def close(self):await runner.close()
        async def run(self):
            counters.update(model=0,tool=0)
            chunks=0;text="";first=0;start=time.perf_counter_ns()
            async for event in runner.run_async(user_id="bench",session_id=self.session_id,new_message=types.Content(role="user",parts=[types.Part(text=PROMPT)]),run_config=config):
                if event.error_code:raise ValueError(event.error_message)
                if not event.content:continue
                if scenario=="stream_64" and event.partial:
                    for part in event.content.parts:
                        if part.text:first=first or time.perf_counter_ns()-start;chunks+=1
                if event.is_final_response():text="".join(p.text or "" for p in event.content.parts)
            return dict(answer=text,model_calls=counters["model"],tool_calls=counters["tool"],chunks=chunks,first_ns=first)
    return ADKAdapter()

def validate(scenario,result):
    expected=dict(answer="8",model_calls=0,tool_calls=0,chunks=0)
    if scenario=="agent_tool":expected=dict(answer="30",model_calls=2,tool_calls=1,chunks=0)
    elif scenario=="stream_64":expected=dict(answer="x"*64,model_calls=1,tool_calls=0,chunks=64)
    for key,value in expected.items():
        if result[key]!=value:raise ValueError(f"contract failure {scenario} {key}: {result[key]!r} != {value!r}")

async def measure(framework,scenario,samples,warmup):
    runner=make_runner(framework,scenario);durations=[];firsts=[];result=None
    try:
        for i in range(warmup+samples):
            await runner.prepare()
            try:
                start=time.perf_counter_ns();result=await runner.run();duration=time.perf_counter_ns()-start
                validate(scenario,result)
                if i>=warmup:
                    durations.append(duration)
                    if result.get("first_ns"):firsts.append(result["first_ns"])
            finally:await runner.cleanup()
    finally:await runner.close()
    versions={p:importlib.metadata.version(p) for p in ("langgraph","langchain","openai-agents","google-adk")}
    return dict(framework=framework,scenario=scenario,asyncio_debug=asyncio.get_running_loop().get_debug(),runtime=platform.python_version(),versions=versions,durations_ns=durations,first_ns=firsts,contract=result,peak_rss_bytes=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss)

if __name__=="__main__":
    parser=argparse.ArgumentParser();parser.add_argument("--framework",required=True);parser.add_argument("--scenario",required=True);parser.add_argument("--samples",type=int,default=400);parser.add_argument("--warmup",type=int,default=50)
    args=parser.parse_args()
    if args.samples<1 or args.warmup<0:parser.error("samples must be positive; warmup nonnegative")
    print(json.dumps(asyncio.run(measure(args.framework,args.scenario,args.samples,args.warmup))))
