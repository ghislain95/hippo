package imagecopy

import (
	"bufio"
	"encoding/json"
	"io"
	"os"

	"github.com/ghislain95/hippo/backend/internal/childproc"
)

// RunChild is the child process: it reads one request from stdin, copies the image of
// descriptor 3 to the file of descriptor 4, writes its answer as JSON to stdout and returns the
// exit code. It loads no configuration, opens no database and logs nothing. Above the memory
// limit of the request it exits at once with childproc.ExitMemory.
func RunChild(stdin io.Reader, stdout io.Writer) int {
	line, err := bufio.NewReader(io.LimitReader(stdin, maxRequest)).ReadBytes('\n')
	if err != nil {
		return childproc.ExitInput
	}
	var req request
	if json.Unmarshal(line, &req) != nil || !req.valid() {
		return childproc.ExitInput
	}
	watch := childproc.Guard(req.MemoryLimit, req.MemoryLimit/softLimitDen*softLimitNum)
	defer watch.Stop()

	asset := os.NewFile(assetDescriptor, "asset")
	out := os.NewFile(outputDescriptor, "copy")
	if asset == nil || out == nil {
		return childproc.ExitInput
	}
	defer func() { _ = asset.Close() }() // read only
	info, err := asset.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return childproc.ExitInput
	}

	ans, err := copyImage(asset, info.Size(), out, req)
	if err != nil {
		return exitOutput
	}
	if err := out.Close(); err != nil {
		return exitOutput
	}
	ans.PeakBytes = watch.Peak()
	if err := json.NewEncoder(stdout).Encode(ans); err != nil {
		return exitOutput
	}
	return 0
}
