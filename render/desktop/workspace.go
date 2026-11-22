package desktop

// viewport is renderer-owned state. A view update can replace the data source
// without changing where the user is looking.
type viewport struct {
	key        string
	top, count int
	page       int
	follow     bool
}

func (v *viewport) update(table Table, page int) {
	if table.Key != v.key {
		v.top = 0
	}
	if table.Follow && (table.Key != v.key || !v.follow || table.Count != v.count || page != v.page) {
		v.top = max(0, table.Count-page)
	}
	v.key, v.count, v.page, v.follow = table.Key, table.Count, page, table.Follow
	v.scroll(0)
}

func (v *viewport) scroll(rows int) {
	v.top = max(0, min(v.top+rows, v.count-v.page))
}

func (v *viewport) reveal(row int) {
	switch {
	case row < v.top:
		v.scroll(row - v.top)
	case row >= v.top+v.page:
		v.scroll(row - v.top - v.page + 1)
	}
}

const rowHeight = 26
