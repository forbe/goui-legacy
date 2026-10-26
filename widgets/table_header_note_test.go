package widgets

import "testing"

// 表头标题后面的小字提示：默认不出现，设置后与标题同一行，清空后再次隐藏。
func TestTableViewHeaderNote(t *testing.T) {
	v, _, c, _ := tableFixture(1)
	defer v.SetModel(nil)
	c.SetWidth(220)
	h := v.headers[c]
	if h == nil || h.note == nil {
		t.Fatal("表头未建立 note")
	}
	if h.note.Visible() {
		t.Fatal("未设置提示时 note 应隐藏")
	}
	c.SetHeaderNote("(拖拽列表可排序)", "table-header-note")
	if !h.note.Visible() || h.note.Text() != "(拖拽列表可排序)" {
		t.Fatalf("note 未生效: visible=%v text=%q", h.note.Visible(), h.note.Text())
	}
	if got := h.note.StyleName(); got != "table-header-note" {
		t.Fatalf("note 样式名 = %q", got)
	}
	c.SetHeaderNote("", "")
	if h.note.Visible() {
		t.Fatal("清空后 note 应隐藏")
	}
}
