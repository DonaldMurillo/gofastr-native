package desktop

import "testing"

func TestGPUViewportKeepsPausedRowsDuringReplay(t *testing.T) {
	table := Table{Key: "all", Count: 1_000_000, Follow: true}
	var v viewport
	v.update(table, 20)
	if v.top != table.Count-20 {
		t.Fatalf("live viewport started at %d", v.top)
	}
	table.Follow = false
	v.update(table, 20)
	v.scroll(-50)
	anchor := v.top
	for range 500 {
		table.Count += 100
		v.update(table, 20)
		if v.top != anchor {
			t.Fatalf("paused viewport moved from %d to %d", anchor, v.top)
		}
	}
	v.update(table, 24)
	if v.top != anchor {
		t.Fatalf("resize moved paused viewport to %d", v.top)
	}
	table.Follow = true
	v.update(table, 24)
	if v.top != table.Count-24 {
		t.Fatalf("resumed viewport stopped at %d", v.top)
	}
	table.Key, table.Follow = "filtered", false
	v.update(table, 24)
	if v.top != 0 {
		t.Fatalf("filter started at %d", v.top)
	}
}
