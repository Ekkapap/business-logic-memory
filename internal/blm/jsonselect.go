package blm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SelectJSON — ดึงเฉพาะจุดที่ต้องการจาก JSON ด้วย path สั้น ๆ (เจ้าของ 2026-09-21: อยากได้ data ตรงจุด ไม่อ่านทั้งไฟล์)
//
//	topics                → array ทั้งก้อน
//	topics[0].subs        → index
//	topics[id=nws6d].name → กรอง key=value (ตรงหลายตัว = array · ตรงตัวเดียว = object นั้น)
//	items[kind=candidate].label → กรองแล้วดึง field จากทุกตัว = array ของค่า
//	topics.status         → array → field ของทุกตัว
//
// v เป็นผลจาก json.Unmarshal (map/[]any/scalar) — คืน nil เมื่อไม่พบ
func SelectJSON(v any, sel string) (any, error) {
	sel = strings.TrimSpace(sel)
	if sel == "" || sel == "." {
		return v, nil
	}
	for _, tok := range strings.Split(sel, ".") {
		if tok == "" {
			continue
		}
		key, filters := tok, []string{}
		if i := strings.Index(tok, "["); i >= 0 {
			key = tok[:i]
			for _, f := range strings.Split(tok[i:], "[") {
				if f = strings.TrimSuffix(strings.TrimSpace(f), "]"); f != "" {
					filters = append(filters, f)
				}
			}
		}
		if key != "" {
			v = field(v, key)
		}
		for _, f := range filters {
			arr, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%q: not an array before [%s]", tok, f)
			}
			if n, err := strconv.Atoi(f); err == nil {
				if n < 0 {
					n += len(arr)
				}
				if n < 0 || n >= len(arr) {
					return nil, fmt.Errorf("%q: index %d out of %d", tok, n, len(arr))
				}
				v = arr[n]
				continue
			}
			k, want, ok := strings.Cut(f, "=")
			if !ok {
				return nil, fmt.Errorf("%q: filter must be [n] or [key=value]", tok)
			}
			hits := []any{}
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok && fmt.Sprint(m[k]) == want {
					hits = append(hits, e)
				}
			}
			if len(hits) == 1 {
				v = hits[0]
			} else {
				v = hits
			}
		}
	}
	return v, nil
}

// field — key ของ object · หรือ key ของทุก element เมื่อเป็น array (topics.status)
func field(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		return x[key]
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m[key])
			}
		}
		return out
	}
	return nil
}

// SelectRaw — SelectJSON บน bytes
func SelectRaw(raw []byte, sel string) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return SelectJSON(v, sel)
}

// SetJSON — แก้เฉพาะจุดที่ sel ชี้ (ในที่ — map/slice ของ json.Unmarshal ใช้ร่วมกับ root) · val nil = ลบ key
// จุดสุดท้ายของ sel เป็น key ของ object หรือ key[n] / key[k=v] ของ array (ต้องตรงตัวเดียว) · key[+] = ต่อท้าย array
func SetJSON(root any, sel string, val any) error {
	toks := strings.Split(strings.TrimSpace(sel), ".")
	if len(toks) == 0 || toks[len(toks)-1] == "" {
		return fmt.Errorf("set: path required")
	}
	parent, err := SelectJSON(root, strings.Join(toks[:len(toks)-1], "."))
	if err != nil {
		return err
	}
	last := toks[len(toks)-1]
	key, filter := last, ""
	if i := strings.Index(last, "["); i >= 0 {
		key, filter = last[:i], strings.TrimSuffix(last[i+1:], "]")
	}
	m, ok := parent.(map[string]any)
	if !ok {
		return fmt.Errorf("set: %q is not an object", strings.Join(toks[:len(toks)-1], "."))
	}
	if filter == "" {
		if val == nil {
			delete(m, key)
		} else {
			m[key] = val
		}
		return nil
	}
	arr, ok := m[key].([]any)
	if !ok {
		if filter == "+" && m[key] == nil { // ต่อท้าย array ที่ยังไม่มี
			m[key] = []any{val}
			return nil
		}
		return fmt.Errorf("set: %q is not an array", key)
	}
	if filter == "+" { // key[+] = ต่อท้าย
		m[key] = append(arr, val)
		return nil
	}
	idx := -1
	if n, err := strconv.Atoi(filter); err == nil {
		if n < 0 {
			n += len(arr)
		}
		idx = n
	} else if k, want, ok := strings.Cut(filter, "="); ok {
		for i, e := range arr {
			if em, ok := e.(map[string]any); ok && fmt.Sprint(em[k]) == want {
				if idx >= 0 {
					return fmt.Errorf("set: [%s] matches more than one element", filter)
				}
				idx = i
			}
		}
	}
	if idx < 0 || idx >= len(arr) {
		return fmt.Errorf("set: [%s] not found in %s (%d elements)", filter, key, len(arr))
	}
	if val == nil {
		m[key] = append(arr[:idx:idx], arr[idx+1:]...)
	} else {
		arr[idx] = val
	}
	return nil
}
