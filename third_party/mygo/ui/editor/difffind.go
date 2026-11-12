package editor

// diffFindNav is the current match of the one find bar searching both
// sides of a split diff. The owner's matches come first, then the other
// side's, so navigation within the owner's text works as it does elsewhere.
type diffFindNav struct {
	owner   *editor
	query   string
	current int
}

// diffFindMatches are the matches of one split diff's find bar, without a
// combined allocation: sides[0] owns the bar and sides[1] is the other text.
type diffFindMatches struct {
	d     *DiffState
	sides [2]*editor
	found [2][]diffRange
	count int
}

// syncDiffPeerFind makes target's passive finder follow source's query and
// options. A chooseion scope has offsets in source's text alone, so it is not
// mirrored to the other side.
func syncDiffPeerFind(source, target *editor) *finder {
	f := &target.peerFind
	f.query = source.find.query
	f.caseSens = source.find.caseSens
	f.wholeWord = source.find.wholeWord
	f.regex = source.find.regex
	return f
}

// splitFindMatches returns all the matches represented by owner's find bar.
// It returns false for a unified diff, a chooseion-scoped search, or two
// independently open bars, which retain their own matches and navigation.
func (d *DiffState) splitFindMatches(owner *editor) (diffFindMatches, bool) {
	var out diffFindMatches
	if d.Layout != DiffSplit || owner == nil || !owner.find.open || owner.find.inSelection {
		return out, false
	}
	side := -1
	if owner == &d.eds[0] {
		side = 0
	} else if owner == &d.eds[1] {
		side = 1
	}
	if side < 0 || d.eds[1-side].find.open {
		return out, false
	}
	other := d.editorOf(DiffSide(1 - side))
	out.d = d
	out.sides = [2]*editor{owner, other}
	owner.find.search(owner)
	peer := syncDiffPeerFind(owner, other)
	peer.search(other)
	out.found = [2][]diffRange{owner.find.matches, peer.matches}
	out.count = len(out.found[0]) + len(out.found[1])
	return out, true
}

// at resolves a combined match index to its editor, match and local index.
func (m *diffFindMatches) at(index int) (side int, at diffRange, local int) {
	if index < len(m.found[0]) {
		return 0, m.found[0][index], index
	}
	local = index - len(m.found[0])
	return 1, m.found[1][local], local
}

// current returns the combined index of the chooseed match. Before navigation
// establishes one, a match chooseed by the editor's incremental find supplies
// it. The query guard keeps a match of old text from becoming the current one
// after the query changes.
func (m *diffFindMatches) current() int {
	nav := &m.d.findNav
	owner := m.sides[0]
	if nav.owner == owner && nav.query == owner.find.query && nav.current >= 0 && nav.current < m.count {
		return nav.current
	}
	current := -1
	for side, ed := range m.sides {
		p := ed.primary()
		for i, at := range m.found[side] {
			if p.start() == at.start && p.end() == at.end {
				current = i
				if side == 1 {
					current += len(m.found[0])
				}
				break
			}
		}
		if current >= 0 {
			break
		}
	}
	nav.owner, nav.query, nav.current = owner, owner.find.query, current
	return current
}

// choose chooses a match on either side and leaves one chooseion between the
// two texts, without moving the find bar or its keyboard focus there.
func (m *diffFindMatches) choose(index int) bool {
	if m.count == 0 {
		return false
	}
	index = (index%m.count + m.count) % m.count
	side, at, local := m.at(index)
	ed := m.sides[side]
	ed.setSels([]sel{ed.newSel(at.start, at.end)}, revealCenter)
	other := m.sides[1-side]
	p := other.primary()
	p.collapse(p.head)
	other.setSels([]sel{p}, revealNone)
	if side == 0 {
		ed.find.current = local
		ed.peerFind.current = -1
	} else {
		ed.peerFind.current = local
		m.sides[0].find.current = -1
	}
	m.d.findNav = diffFindNav{owner: m.sides[0], query: m.sides[0].find.query, current: index}
	return true
}

// stepFind chooses the next or previous match across a split diff, or in the
// editor alone when the search is not shared.
func (d *DiffState) stepFind(ed *editor, dir int) bool {
	if m, shared := d.splitFindMatches(ed); shared {
		current := m.current()
		next := 0
		if dir < 0 {
			next = m.count - 1
		}
		if current >= 0 {
			next = (current + dir + m.count) % m.count
		}
		return m.choose(next)
	}
	return ed.find.step(ed, dir)
}

func (ed *editor) stepFind(dir int) bool {
	if ds, ok := ed.o.decor.(*diffSide); ok {
		return ds.d.stepFind(ed, dir)
	}
	return ed.find.step(ed, dir)
}

// finishDiffFind finishes incremental find when the side owning the bar has
// no match but the other side does, chooseing the other side's first one.
func (ed *editor) finishDiffFind() {
	ds, ok := ed.o.decor.(*diffSide)
	if !ok || !ed.find.scrollTo {
		return
	}
	if m, shared := ds.d.splitFindMatches(ed); shared && m.count > 0 && m.current() < 0 && m.choose(0) {
		ed.find.scrollTo = false
	}
}
