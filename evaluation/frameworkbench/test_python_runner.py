import asyncio
import unittest

from python_runner import make_runner

class FrameworkContractTest(unittest.IsolatedAsyncioTestCase):
    async def test_agent_and_stream_contracts(self):
        for framework in ("langchain", "openai_agents", "google_adk"):
            for scenario in ("agent_tool", "stream_64"):
                with self.subTest(framework=framework, scenario=scenario):
                    runner = make_runner(framework, scenario)
                    try:
                        for _ in range(3):
                            await runner.prepare()
                            try:
                                result = await runner.run()
                                expected = {"answer": "30", "model_calls": 2, "tool_calls": 1, "chunks": 0}
                                if scenario == "stream_64":
                                    expected = {"answer": "x" * 64, "model_calls": 1, "tool_calls": 0, "chunks": 64}
                                for key, value in expected.items():
                                    self.assertEqual(result[key], value, key)
                            finally:
                                await runner.cleanup()
                    finally:
                        await runner.close()

    async def test_langgraph_graph_contracts(self):
        for scenario in ("graph_8", "branch_loop_8"):
            runner = make_runner("langgraph", scenario)
            self.assertEqual((await runner.run())["answer"], "8")

if __name__ == "__main__":
    unittest.main()
