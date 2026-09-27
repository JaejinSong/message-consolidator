package store

import "testing"

func TestLatestScanCursor(t *testing.T) {
	metadataMu.Lock()
	scanCache["u@x.io:slack:C1"] = "1789648490.549189"
	scanCache["u@x.io:slack:C2"] = "1789524620.852429"
	scanCache["u@x.io:slack:"+ScanTargetLastSuccess] = "1799999999"
	scanCache["u@x.io:gmail:inbox"] = "1799999998"
	scanCache["other@x.io:slack:C1"] = "1799999997"
	metadataMu.Unlock()
	defer func() {
		metadataMu.Lock()
		for _, k := range []string{"u@x.io:slack:C1", "u@x.io:slack:C2", "u@x.io:slack:" + ScanTargetLastSuccess, "u@x.io:gmail:inbox", "other@x.io:slack:C1"} {
			delete(scanCache, k)
		}
		metadataMu.Unlock()
	}()

	if got := LatestScanCursor("u@x.io", SourceSlack); got != 1789648490 {
		t.Errorf("LatestScanCursor = %d, want 1789648490 (newest channel cursor, last_success and other users/sources ignored)", got)
	}
	if got := LatestScanCursor("nobody@x.io", SourceSlack); got != 0 {
		t.Errorf("LatestScanCursor(no rows) = %d, want 0", got)
	}
}
