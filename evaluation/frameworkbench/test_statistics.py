import unittest
from run_comparison import summarize, percentile

class StatisticsTest(unittest.TestCase):
    def test_round_medians_and_tail_are_separate(self):
        runs=[dict(framework='f',scenario='s',durations_ns=[1,2,100],first_ns=[1,2,3]),dict(framework='f',scenario='s',durations_ns=[3,4,200],first_ns=[4,5,6])]
        row=summarize(runs)[0]
        self.assertEqual(row['p50_ns'],3)
        self.assertEqual(row['p95_ns'],150)
        self.assertEqual(row['first_p50_ns'],3.5)
        self.assertEqual(row['samples'],6)
    def test_nearest_rank_tail(self):
        self.assertEqual(percentile(list(range(1,101)),.95),95)

if __name__=='__main__':unittest.main()

class EnvironmentTest(unittest.TestCase):
    def test_inherited_debug_is_removed_not_set_to_zero(self):
        import os
        from unittest.mock import patch
        from run_comparison import benchmark_env
        with patch.dict(os.environ, {"PYTHONASYNCIODEBUG":"1", "GOROOT":"stale"}):
            result=benchmark_env()
            self.assertNotIn("PYTHONASYNCIODEBUG",result)
            self.assertNotIn("GOROOT",result)
            self.assertEqual(result["GOMAXPROCS"],"4")
