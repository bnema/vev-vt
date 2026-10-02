package ansi

// committedFrame returns the renderer's committed frame (sharing its storage).
func (r *Renderer) committedFrame() Frame { return r.committed }

// setCommittedFrame installs frame as the committed shadow.
func (r *Renderer) setCommittedFrame(frame Frame) {
	r.width = frame.Width
	r.height = frame.Height
	r.hasCommitted = true
	r.committed = frame
}

// replaceFrame overwrites dst's own storage with a copy of src, reusing
// capacity.
func replaceFrame(dst *Frame, src Frame) { dst.CopyFrom(src) }
