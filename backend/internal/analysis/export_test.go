package analysis

// SegmentFileSize is the size of the segment file of r: the requests kept, nothing more.
func SegmentFileSize(r *Requests) (int64, error) {
	info, err := r.segW.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
