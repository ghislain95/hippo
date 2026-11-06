package childproc_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/childproc"
)

// Modes of the test binary when a test runs it as a child.
const (
	envChild     = "env-child"         // writes its environment, one variable per line
	echoChild    = "echo-child"        // copies its stdin to its stdout
	fileChild    = "file-child"        // copies the file of descriptor 3 to its stdout
	hangChild    = "hang-child"        // says it started on the FIFO given, then never answers
	crashChild   = "crash-child"       // exits with an unexpected code
	floodChild   = "flood-child"       // writes more than the parent reads
	memoryChild  = "memory-child"      // allocates beyond the hard limit of its guard
	slowMemChild = "slow-memory-child" // the same, under a guard that samples every 10 ms
	peakChild    = "peak-child"        // holds 16 MiB under its guard, then writes the peak it saw
	stderrChild  = "stderr-child"      // writes to stderr only
	guardedLimit = 32 << 20            // the hard limit of memoryChild and peakChild
	floodSize    = 1 << 20             // what floodChild writes
)

// TestMain makes the test binary the child of the tests.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		runChildMode(os.Args[1], os.Args[2:])
	}
	os.Exit(m.Run())
}

// runChildMode runs the test binary as the child of mode; it returns only for an unknown mode.
// syscall.Exit, not os.Exit: under -race, a test binary that exits with 0 through os.Exit waits
// a second for the race detector.
func runChildMode(mode string, args []string) {
	switch mode {
	case envChild:
		for _, v := range os.Environ() {
			fmt.Println(v)
		}
		syscall.Exit(0)
	case echoChild:
		_, _ = io.Copy(os.Stdout, os.Stdin)
		syscall.Exit(0)
	case fileChild:
		file := os.NewFile(3, "input")
		if file == nil {
			syscall.Exit(childproc.ExitInput)
		}
		if _, err := io.Copy(os.Stdout, file); err != nil {
			syscall.Exit(childproc.ExitInput)
		}
		syscall.Exit(0)
	case hangChild:
		if fifo, err := os.OpenFile(args[0], os.O_WRONLY, 0); err == nil {
			_, _ = fifo.WriteString("started\n")
			_ = fifo.Close()
		}
		// Waits for a signal that never comes: a bare select{} is a deadlock the runtime
		// reports, not a child that hangs.
		never := make(chan os.Signal, 1)
		signal.Notify(never, syscall.SIGUSR2)
		<-never
		syscall.Exit(0)
	case crashChild:
		syscall.Exit(7)
	case floodChild:
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), floodSize))
		syscall.Exit(0)
	case memoryChild, slowMemChild:
		if mode == slowMemChild {
			childproc.GuardEvery(guardedLimit, guardedLimit*3/4, 10*time.Millisecond)
		} else {
			childproc.Guard(guardedLimit, guardedLimit*3/4)
		}
		var held [][]byte
		for range 64 { // 256 MiB at most, far beyond the limit
			chunk := make([]byte, 4<<20)
			for i := range chunk {
				chunk[i] = byte(i)
			}
			held = append(held, chunk)
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Println(len(held))
		syscall.Exit(0)
	case peakChild:
		watch := childproc.Guard(guardedLimit*4, guardedLimit*3)
		chunk := make([]byte, 16<<20)
		for i := range chunk {
			chunk[i] = byte(i)
		}
		time.Sleep(20 * time.Millisecond)
		fmt.Println(watch.Peak(), chunk[len(chunk)-1])
		syscall.Exit(0)
	case stderrChild:
		_, _ = os.Stderr.WriteString("a panic message that may quote an item\n")
		syscall.Exit(0)
	}
}

// argv runs the test binary as the child of mode.
func argv(mode string, args ...string) []string {
	return append([]string{os.Args[0], mode}, args...)
}

// startedFIFO makes a FIFO that a hang child writes to once started; the channel is closed
// then. Opening a FIFO blocks until both ends are open: no sleep is needed.
func startedFIFO(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "started")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	started := make(chan struct{})
	go func() {
		fifo, err := os.Open(path)
		if err != nil {
			return
		}
		defer func() { _ = fifo.Close() }()
		if _, err := bufio.NewReader(fifo).ReadString('\n'); err == nil {
			close(started)
		}
	}()
	t.Cleanup(func() {
		// Unblocks the reader when no child ever opened the FIFO.
		if fifo, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = fifo.Close()
		}
	})
	return path, started
}

func spec(mode string, args ...string) childproc.Spec {
	return childproc.Spec{Argv: argv(mode, args...), Timeout: 10 * time.Second, MaxOutput: 64 << 10, WaitDelay: time.Second}
}

func TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("the environment holds only GOMAXPROCS=2", func(t *testing.T) {
		t.Setenv("API_TOKEN", "test-token-that-must-not-reach-the-child")
		t.Setenv("ANTHROPIC_API_KEY", "test-key-that-must-not-reach-the-child")
		res, err := childproc.Run(ctx, spec(envChild))
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := strings.Fields(string(res.Output)); !slices.Equal(got, []string{"GOMAXPROCS=2"}) {
			t.Errorf("child environment = %q, want only GOMAXPROCS=2", got)
		}
		if !slices.Equal(childproc.Env(), []string{"GOMAXPROCS=2"}) {
			t.Errorf("Env() = %q, want only GOMAXPROCS=2", childproc.Env())
		}
	})

	t.Run("the command carries the environment, the files and no stderr", func(t *testing.T) {
		file, err := os.Open(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		s := spec(envChild)
		s.ExtraFiles = []*os.File{file}
		cmd := childproc.Command(ctx, s, io.Discard)
		// A nil Env would inherit the environment of the service.
		if cmd.Env == nil || !slices.Equal(cmd.Env, []string{"GOMAXPROCS=2"}) {
			t.Errorf("command environment = %q, want only GOMAXPROCS=2", cmd.Env)
		}
		if len(cmd.ExtraFiles) != 1 || cmd.ExtraFiles[0] != file || cmd.Stderr != nil {
			t.Errorf("extra files %v, stderr %v; want the file alone, stderr discarded", cmd.ExtraFiles, cmd.Stderr)
		}
		if cmd.WaitDelay != time.Second {
			t.Errorf("wait delay %v, want the spec's", cmd.WaitDelay)
		}
	})

	t.Run("stdin reaches the child and its output comes back", func(t *testing.T) {
		s := spec(echoChild)
		s.Stdin = strings.NewReader("request line\n")
		res, err := childproc.Run(ctx, s)
		if err != nil || string(res.Output) != "request line\n" || res.Overflow || res.ExitCode != 0 {
			t.Errorf("output %q, overflow %v, exit %d, %v; want the echo", res.Output, res.Overflow, res.ExitCode, err)
		}
	})

	t.Run("an extra file is descriptor 3", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "input")
		if err := os.WriteFile(path, []byte("fictional content"), 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		s := spec(fileChild)
		s.ExtraFiles = []*os.File{file}
		res, err := childproc.Run(ctx, s)
		if err != nil || string(res.Output) != "fictional content" {
			t.Errorf("output %q, %v; want the file", res.Output, err)
		}
	})

	t.Run("stderr is discarded", func(t *testing.T) {
		res, err := childproc.Run(ctx, spec(stderrChild))
		if err != nil || len(res.Output) != 0 {
			t.Errorf("output %q, %v; want nothing", res.Output, err)
		}
	})

	t.Run("memory over the limit exits 86", func(t *testing.T) {
		res, err := childproc.Run(ctx, spec(memoryChild))
		if !errors.Is(err, childproc.ErrMemory) || res.ExitCode != childproc.ExitMemory {
			t.Errorf("exit %d, %v; want %d, ErrMemory", res.ExitCode, err, childproc.ExitMemory)
		}
	})

	t.Run("memory over the limit exits 86 under a slower watch", func(t *testing.T) {
		res, err := childproc.Run(ctx, spec(slowMemChild))
		if !errors.Is(err, childproc.ErrMemory) || res.ExitCode != childproc.ExitMemory {
			t.Errorf("exit %d, %v; want %d, ErrMemory", res.ExitCode, err, childproc.ExitMemory)
		}
	})

	t.Run("the watch reports the peak it saw", func(t *testing.T) {
		res, err := childproc.Run(ctx, spec(peakChild))
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		var peak uint64
		var last int
		if _, err := fmt.Sscan(string(res.Output), &peak, &last); err != nil {
			t.Fatalf("answer %q: %v", res.Output, err)
		}
		if peak < 16<<20 || peak > guardedLimit*4 {
			t.Errorf("peak %d MiB, want at least the 16 MiB held and under the limit", peak>>20)
		}
		if res.MaxRSS < 16<<20 {
			t.Errorf("resident set %d MiB, want at least the 16 MiB held", res.MaxRSS>>20)
		}
	})

	t.Run("timeout kills", func(t *testing.T) {
		fifo, _ := startedFIFO(t)
		s := spec(hangChild, fifo)
		s.Timeout = 300 * time.Millisecond
		begin := time.Now()
		_, err := childproc.Run(ctx, s)
		if !errors.Is(err, childproc.ErrTimeout) {
			t.Errorf("error = %v, want ErrTimeout", err)
		}
		if elapsed := time.Since(begin); elapsed > 5*time.Second {
			t.Errorf("answer after %v, want about the time limit", elapsed)
		}
	})

	t.Run("a missing binary is unavailable", func(t *testing.T) {
		s := spec(envChild)
		s.Argv = []string{filepath.Join(t.TempDir(), "missing")}
		_, err := childproc.Run(ctx, s)
		if !errors.Is(err, childproc.ErrUnavailable) {
			t.Errorf("error = %v, want ErrUnavailable", err)
		}
	})

	t.Run("a cancelled parent context is stopped", func(t *testing.T) {
		fifo, started := startedFIFO(t)
		stopCtx, stop := context.WithCancel(ctx)
		go func() {
			<-started
			stop()
		}()
		begin := time.Now()
		_, err := childproc.Run(stopCtx, spec(hangChild, fifo))
		if !errors.Is(err, childproc.ErrStopped) {
			t.Errorf("error = %v, want ErrStopped", err)
		}
		if elapsed := time.Since(begin); elapsed > 5*time.Second {
			t.Errorf("answer after %v, want the child killed at once", elapsed)
		}
	})

	t.Run("a parent context done before the start is stopped", func(t *testing.T) {
		stopCtx, stop := context.WithCancel(ctx)
		stop()
		_, err := childproc.Run(stopCtx, spec(envChild))
		if !errors.Is(err, childproc.ErrStopped) {
			t.Errorf("error = %v, want ErrStopped", err)
		}
	})

	t.Run("output beyond the bound is an overflow", func(t *testing.T) {
		s := spec(floodChild)
		s.MaxOutput = 1000
		res, err := childproc.Run(ctx, s)
		if err != nil || !res.Overflow || len(res.Output) != 1000 {
			t.Errorf("overflow %v, %d bytes kept, %v; want an overflow and the first 1000 bytes", res.Overflow, len(res.Output), err)
		}
	})

	t.Run("an unexpected exit is a failure with its code", func(t *testing.T) {
		res, err := childproc.Run(ctx, spec(crashChild))
		if !errors.Is(err, childproc.ErrFailed) || res.ExitCode != 7 {
			t.Errorf("exit %d, %v; want 7 and ErrFailed", res.ExitCode, err)
		}
		for _, sentinel := range []error{childproc.ErrStopped, childproc.ErrTimeout, childproc.ErrUnavailable, childproc.ErrMemory} {
			if errors.Is(err, sentinel) {
				t.Errorf("error = %v, want no other sentinel", err)
			}
		}
	})
}

func TestCappedBuffer(t *testing.T) {
	t.Run("keeps the first bytes and flags the rest", func(t *testing.T) {
		b := childproc.NewCappedBuffer(5)
		for _, p := range []string{"abc", "def", "gh"} {
			if n, err := b.Write([]byte(p)); err != nil || n != len(p) {
				t.Fatalf("Write(%q) = %d, %v; want every byte taken", p, n, err)
			}
		}
		if string(b.Bytes()) != "abcde" || !b.Overflow() {
			t.Errorf("kept %q, overflow %v; want abcde and an overflow", b.Bytes(), b.Overflow())
		}
	})

	t.Run("io.Copy goes through the limit", func(t *testing.T) {
		b := childproc.NewCappedBuffer(10)
		if _, err := io.Copy(b, strings.NewReader(strings.Repeat("y", 100))); err != nil {
			t.Fatal(err)
		}
		if len(b.Bytes()) != 10 || !b.Overflow() {
			t.Errorf("kept %d bytes, overflow %v; want 10 and an overflow", len(b.Bytes()), b.Overflow())
		}
	})

	t.Run("an output at the limit is no overflow", func(t *testing.T) {
		b := childproc.NewCappedBuffer(3)
		_, _ = b.Write([]byte("abc"))
		if b.Overflow() {
			t.Error("overflow at the limit, want none")
		}
	})
}
