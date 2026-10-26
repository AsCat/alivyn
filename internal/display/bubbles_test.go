package display

import (
	"reflect"
	"testing"
)

func TestBubblesPreservesOneTurnAsNaturalBeats(t *testing.T) {
	if got := Bubbles("你先说。\n\n我在听。"); !reflect.DeepEqual(got, []string{"你先说。", "我在听。"}) {
		t.Fatal(got)
	}
	for _, reply := range []string{"一句话", "第一句\n\n\n第二句", "一\n\n二\n\n三\n\n四"} {
		if got := Bubbles(reply); !reflect.DeepEqual(got, []string{reply}) {
			t.Fatalf("ambiguous layout changed: %q -> %+v", reply, got)
		}
	}
}
