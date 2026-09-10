package gitreview

// Viewed marks: "I have read this file, stop showing it to me as new work."
//
// The whole design rests on one property. A mark is stored against the digest of
// the diff that was on screen when it was made, never as a plain boolean, so it
// clears itself the moment the agent touches that file again. A review page is
// live -- the agent is editing under the reader -- and a tick that survives a
// rewrite is worse than no tick at all: it tells the reader they have seen code
// they have never seen.
//
// Nothing prunes the map. Entries are only ever added by an explicit click and
// are keyed by path, so re-marking the same file overwrites rather than grows,
// and the total is bounded by how many distinct files the user has ever ticked
// in that workspace. That is a very different shape from threads, which
// accumulate prose daily and do need a cap.

// SetViewed marks a file viewed, or clears the mark, and reports whether the
// path is one the review is actually showing.
//
// An unknown path is refused rather than recorded. That keeps the map to real
// files, and it is also why nothing here has to check for path traversal: the
// only strings that get in are ones the snapshot already produced.
func (s *Session) SetViewed(path string, viewed bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.fileLocked(path)
	if !ok {
		return false
	}
	if s.viewed == nil {
		s.viewed = make(map[string]string)
	}
	if viewed {
		s.viewed[path] = f.Digest
	} else {
		delete(s.viewed, path)
	}
	s.persistLocked()
	return true
}

// ViewedPaths returns the files whose mark still matches what is on screen.
//
// The comparison is done here rather than on the page so the invalidation rule
// has exactly one home, and so the page never has to hold a digest it could get
// wrong.
func (s *Session) ViewedPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.viewed))
	for _, f := range s.snapshot.Files {
		if mark, ok := s.viewed[f.Path]; ok && mark == f.Digest {
			out = append(out, f.Path)
		}
	}
	return out
}

// HasFile reports whether a path is one the current snapshot is showing. It is
// the gate every path arriving from the browser goes through.
func (s *Session) HasFile(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.fileLocked(path)
	return ok
}

// fileLocked finds a file in the current snapshot. The caller must hold the lock.
func (s *Session) fileLocked(path string) (File, bool) {
	for _, f := range s.snapshot.Files {
		if f.Path == path {
			return f, true
		}
	}
	return File{}, false
}

// viewedSnapshotLocked copies the marks for persistence. The caller must hold
// the lock.
//
// Every mark is saved, not just the ones still matching the current diff: a mark
// whose file has momentarily left the diff -- committed away, or absent from the
// other scope -- comes back valid when the file does, and dropping it here would
// silently lose the reader's progress across a scope switch.
func (s *Session) viewedSnapshotLocked() map[string]string {
	out := make(map[string]string, len(s.viewed))
	for path, mark := range s.viewed {
		out[path] = mark
	}
	return out
}
