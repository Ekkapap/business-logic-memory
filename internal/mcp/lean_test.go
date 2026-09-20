package mcp

import "testing"

func TestLeanJSON(t *testing.T) {
	in := map[string]any{"terminal": "big", "ok": true, "empty": "", "zero": 0, "off": false, "nil": nil, "list": []any{}, "sub": map[string]any{"a": "", "b": "x"}, "items": []any{map[string]any{"c": "", "d": 1}}}
	if got := leanJSON(in); got != `{"items":[{"d":1}],"ok":true,"sub":{"b":"x"}}` {
		t.Fatal(got)
	}
}
