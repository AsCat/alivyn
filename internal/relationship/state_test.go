package relationship

import (
	"testing"
	"time"
)

func TestFamiliarityNeedsConversationAndRespectsDistance(t *testing.T) {
	day := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s := Initial()
	for i := 0; i < 10; i++ {
		s, _ = Advance(s, "你好！", false, day, time.UTC)
	}
	if s.Stage != Stranger || s.MeaningfulTurns != 0 {
		t.Fatalf("greetings created false familiarity: %+v", s)
	}
	for i := 0; i < 4; i++ {
		s, _ = Advance(s, "今天读完了一本书。", false, day, time.UTC)
	}
	for i := 0; i < 4; i++ {
		s, _ = Advance(s, "那本书结尾我挺喜欢。", false, day.AddDate(0, 0, 1), time.UTC)
	}
	for i := 0; i < 4; i++ {
		s, _ = Advance(s, "今天又想起里面一句话。", false, day.AddDate(0, 0, 2), time.UTC)
	}
	if s.Stage != Familiar || s.SharedDays != 3 {
		t.Fatalf("shared conversation did not change stage: %+v", s)
	}
	s, _ = Advance(s, "别跟我这么熟，保持距离", false, day, time.UTC)
	if s.Stage != Acquaintance || !s.Reserved {
		t.Fatalf("explicit distance did not win: %+v", s)
	}
	s, _ = Advance(s, "不用那么客气", false, day, time.UTC)
	if s.Stage != Familiar || s.Reserved {
		t.Fatalf("distance could not be relaxed: %+v", s)
	}
}
