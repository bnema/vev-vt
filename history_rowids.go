package vt

import "slices"

// rowIDSet detects duplicate row IDs across one or more decoded blobs without
// a hash map in the common case. Row IDs are NOT required to be monotonic
// (History.AppendWithID accepts any unused ID, so persisted blobs may list IDs
// out of order), so this is an exact set, not an ordering check: it accepts
// and rejects precisely the inputs a map[RowID]struct{} would.
//
// While every view contributes IDs strictly above everything seen so far, IDs
// are kept in an ascending slice. The first view that interleaves with earlier
// IDs converts the set to a map; from then on it behaves like a plain set.
type rowIDSet struct {
	sorted  []RowID
	members map[RowID]struct{}
	scratch []RowID
}

// reserve makes room for n more collected IDs.
func (s *rowIDSet) reserve(n int) { s.scratch = slices.Grow(s.scratch, n) }

// collect records one ID of the view being collected.
func (s *rowIDSet) collect(id RowID) { s.scratch = append(s.scratch, id) }

// commit reports whether the collected IDs are distinct from each other and
// from every previously committed ID, and if so adds them to the set.
func (s *rowIDSet) commit() bool {
	ids := s.scratch
	s.scratch = s.scratch[:0]
	if len(ids) == 0 {
		return true
	}
	slices.Sort(ids) // the scratch copy is private, so sorting in place is safe

	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return false
		}
	}
	if s.members == nil {
		if len(s.sorted) == 0 || ids[0] > s.sorted[len(s.sorted)-1] {
			s.sorted = append(s.sorted, ids...)
			return true
		}
		s.members = make(map[RowID]struct{}, len(s.sorted)+len(ids))
		for _, id := range s.sorted {
			s.members[id] = struct{}{}
		}
		s.sorted = nil
	}
	for _, id := range ids {
		if _, duplicate := s.members[id]; duplicate {
			return false
		}
	}
	for _, id := range ids {
		s.members[id] = struct{}{}
	}
	return true
}
