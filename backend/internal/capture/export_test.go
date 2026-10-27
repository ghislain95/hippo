package capture

import "io"

// DayIn exposes dayIn to the tests of capture_test.
var DayIn = dayIn

// NoteTitle exposes noteTitle to the tests of capture_test.
var NoteTitle = noteTitle

// DetectImage exposes detectImage to the tests of capture_test.
func DetectImage(r io.ReaderAt, size int64) (mimeType string, width, height *int64, err error) {
	format, err := detectImage(r, size)
	return format.mimeType, format.width, format.height, err
}

// ImageTitle exposes imageTitle to the tests of capture_test.
var ImageTitle = imageTitle
