package evaluation

import "testing"

func TestMetricsPenalizeWrongAndMissingEvidence(t *testing.T) {
	r, err := ScoreRetrieval([]string{"wrong", "a", "a"}, []string{"a", "b"}, 3)
	if err != nil || r.RecallAtK != 0.5 || r.ReciprocalRank != 0.5 {
		t.Fatalf("%+v %v", r, err)
	}
	c, err := ScoreCitations("claim [1] [1] [2] [9]", []string{"a", "wrong"}, []string{"a", "b"})
	if err != nil || c.Validity != 2.0/3 || c.SourcePrecision != 1.0/3 || c.SourceRecall != 0.5 {
		t.Fatalf("%+v %v", c, err)
	}
	c, err = ScoreCitations("no citations", []string{"a"}, []string{"a"})
	if err != nil || c.SourcePrecision != 0 || c.SourceRecall != 0 {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := ScoreRetrieval(nil, nil, 0); err == nil {
		t.Fatal("accepted missing labels")
	}
}
