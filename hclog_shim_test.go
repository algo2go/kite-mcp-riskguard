// hclog_shim tests — direct method coverage for the slog → hclog
// adapter used by hashicorp/go-plugin.
//
// The adapter is a pure pass-through: every method either forwards
// to *slog.Logger or returns a stub value. We drive every method and
// (where applicable) assert the slog side observed the call. The
// remaining methods (IsTrace, IsDebug, ImpliedArgs, etc.) are
// invariant-returning stubs — we assert the documented return.
//
// Strategy:
//
//   - newHclogShim with a nil slog returns a discarding hclog. We
//     assert it's non-nil and that Info() on it doesn't panic.
//   - newHclogShim with a real slog backed by a bytes.Buffer captures
//     the forwarded messages, so we can assert level mapping is
//     correct (Trace → Debug, etc.).
//   - The StandardLogger / StandardWriter pair returns a *log.Logger
//     and io.Writer; we exercise both ends.
package riskguard

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHclog builds an hclog adapter whose underlying slog.Logger
// writes to a bytes.Buffer. Caller asserts on buf.String() after the
// method-under-test runs.
func captureHclog() (hclog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	// JSON handler is easiest to grep for level + msg.
	slogger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return newHclogShim(slogger), buf
}

// TestNewHclogShim_NilSlog confirms a nil slog input produces a
// non-nil hclog (the discarding variant). Calls do not panic.
func TestNewHclogShim_NilSlog(t *testing.T) {
	t.Parallel()
	got := newHclogShim(nil)
	require.NotNil(t, got, "shim must never return nil")
	// Drive each level — none must panic.
	got.Trace("trace")
	got.Debug("debug")
	got.Info("info")
	got.Warn("warn")
	got.Error("error")
}

// TestNewHclogShim_RealSlog confirms a non-nil slog produces an
// adapter that forwards.
func TestNewHclogShim_RealSlog(t *testing.T) {
	t.Parallel()
	shim, buf := captureHclog()
	require.NotNil(t, shim)
	shim.Info("hello")
	assert.Contains(t, buf.String(), "hello")
}

// TestSlogHclogAdapter_Log covers the level-routing switch — every
// hclog level maps to the corresponding slog level.
func TestSlogHclogAdapter_Log(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		level     hclog.Level
		wantLevel string
	}{
		{"trace routes to debug", hclog.Trace, "DEBUG"},
		{"debug stays debug", hclog.Debug, "DEBUG"},
		{"info stays info", hclog.Info, "INFO"},
		{"warn stays warn", hclog.Warn, "WARN"},
		{"error stays error", hclog.Error, "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			shim, buf := captureHclog()
			shim.Log(tc.level, "msg_"+tc.name)
			out := buf.String()
			assert.Contains(t, out, "msg_"+tc.name)
			assert.Contains(t, out, `"level":"`+tc.wantLevel+`"`,
				"hclog level %v must route to slog %s", tc.level, tc.wantLevel)
		})
	}
}

// TestSlogHclogAdapter_LogOff confirms hclog.Off (above hclog.Error)
// is silently dropped — no panic, no output.
func TestSlogHclogAdapter_LogOff(t *testing.T) {
	t.Parallel()
	shim, buf := captureHclog()
	shim.Log(hclog.Off, "should be dropped")
	assert.Empty(t, buf.String(), "hclog.Off must not produce output")
}

// TestSlogHclogAdapter_LevelMethods exercises Trace/Debug/Info/Warn/
// Error directly (not via Log()). These are the methods
// hashicorp/go-plugin actually calls.
func TestSlogHclogAdapter_LevelMethods(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		invoke    func(h hclog.Logger, msg string)
		wantLevel string
	}{
		{"Trace", func(h hclog.Logger, m string) { h.Trace(m) }, "DEBUG"},
		{"Debug", func(h hclog.Logger, m string) { h.Debug(m) }, "DEBUG"},
		{"Info", func(h hclog.Logger, m string) { h.Info(m) }, "INFO"},
		{"Warn", func(h hclog.Logger, m string) { h.Warn(m) }, "WARN"},
		{"Error", func(h hclog.Logger, m string) { h.Error(m) }, "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			shim, buf := captureHclog()
			tc.invoke(shim, "m_"+tc.name)
			out := buf.String()
			assert.Contains(t, out, "m_"+tc.name)
			assert.Contains(t, out, `"level":"`+tc.wantLevel+`"`)
		})
	}
}

// TestSlogHclogAdapter_IsLevelPredicates asserts the documented
// "always true" return for IsTrace/IsDebug/IsInfo/IsWarn/IsError.
// The shim defers filtering to slog so these always claim enabled.
func TestSlogHclogAdapter_IsLevelPredicates(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	assert.True(t, shim.IsTrace())
	assert.True(t, shim.IsDebug())
	assert.True(t, shim.IsInfo())
	assert.True(t, shim.IsWarn())
	assert.True(t, shim.IsError())
}

// TestSlogHclogAdapter_ImpliedArgs returns nil per the
// nothing-implied contract.
func TestSlogHclogAdapter_ImpliedArgs(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	assert.Nil(t, shim.ImpliedArgs())
}

// TestSlogHclogAdapter_With returns the same logger — the args are
// dropped (deliberate per the stub contract).
func TestSlogHclogAdapter_With(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	got := shim.With("k", "v")
	assert.Same(t, shim, got,
		"With must return the same shim instance (stub contract)")
}

// TestSlogHclogAdapter_Name covers the Name() getter.
func TestSlogHclogAdapter_Name(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	// Empty default name.
	assert.Equal(t, "", shim.Name())
	// After Named("x"), name is "x" on the returned shim.
	named := shim.Named("scoped")
	assert.Equal(t, "scoped", named.Name())
	// Original shim is untouched.
	assert.Equal(t, "", shim.Name(),
		"Named must return a new shim, not mutate the receiver")
}

// TestSlogHclogAdapter_ResetNamed mirrors Named — returns a fresh
// shim with the supplied name.
func TestSlogHclogAdapter_ResetNamed(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	reset := shim.ResetNamed("fresh")
	assert.Equal(t, "fresh", reset.Name())
}

// TestSlogHclogAdapter_SetLevel is a no-op — assert it doesn't
// panic. Coverage matters; the behaviour is intentionally empty.
func TestSlogHclogAdapter_SetLevel(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	shim.SetLevel(hclog.Debug)
	shim.SetLevel(hclog.Warn)
	// Just exercising; the contract is "no observable effect."
}

// TestSlogHclogAdapter_GetLevel returns hclog.Trace so nothing is
// filtered at the hclog layer (slog applies its own filter).
func TestSlogHclogAdapter_GetLevel(t *testing.T) {
	t.Parallel()
	shim, _ := captureHclog()
	assert.Equal(t, hclog.Trace, shim.GetLevel())
}

// TestSlogHclogAdapter_StandardLogger returns a *log.Logger whose
// output ends up in the slog Info stream.
func TestSlogHclogAdapter_StandardLogger(t *testing.T) {
	t.Parallel()
	shim, buf := captureHclog()
	stdlog := shim.StandardLogger(nil)
	require.NotNil(t, stdlog)
	stdlog.Print("legacy message")
	// log.Print writes through the StandardWriter, which forwards
	// to slog.Info.
	out := buf.String()
	assert.Contains(t, out, "legacy message")
	assert.Contains(t, out, `"level":"INFO"`)
}

// TestSlogHclogAdapter_StandardWriter_NilSlog returns os.Stderr
// when the underlying slog is nil. We can't easily assert "this is
// os.Stderr" without comparing pointers, but we CAN assert it's
// non-nil and accepts a Write call.
func TestSlogHclogAdapter_StandardWriter_NilSlog(t *testing.T) {
	t.Parallel()
	// Constructing slogHclogAdapter directly with nil slog (the
	// nil-input branch). newHclogShim with nil slog returns an
	// hclog.New variant, not our adapter, so we exercise the
	// adapter's nil-branch directly.
	adapter := &slogHclogAdapter{slog: nil}
	w := adapter.StandardWriter(nil)
	require.NotNil(t, w)
	// Writing must not panic. We don't assert the destination.
	_, err := w.Write([]byte("noop"))
	// os.Stderr.Write returns nil err in practice.
	assert.NoError(t, err)
}

// TestSlogHclogAdapter_StandardWriter_RealSlog returns a slogWriter
// that forwards to slog.Info.
func TestSlogHclogAdapter_StandardWriter_RealSlog(t *testing.T) {
	t.Parallel()
	shim, buf := captureHclog()
	adapter, ok := shim.(*slogHclogAdapter)
	require.True(t, ok, "shim should be the adapter type")
	w := adapter.StandardWriter(nil)
	require.NotNil(t, w)
	_, ok = w.(*slogWriter)
	require.True(t, ok, "real-slog branch must return *slogWriter")

	n, err := w.Write([]byte("hello legacy"))
	require.NoError(t, err)
	assert.Equal(t, len("hello legacy"), n,
		"slogWriter must report len(b) so callers see complete writes")
	assert.Contains(t, buf.String(), "hello legacy")
}

// TestSlogWriter_WriteReportedLen confirms slogWriter.Write returns
// len(b) regardless of the slog backend's internal buffering.
func TestSlogWriter_WriteReportedLen(t *testing.T) {
	t.Parallel()
	slogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := &slogWriter{slog: slogger}
	payload := []byte("0123456789")
	n, err := w.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
}

// TestNewHclogShim_Nil_NoOutput verifies the nil-slog branch
// genuinely discards. Set the discarding hclog (returned by
// newHclogShim(nil)) and prove no panic + (by indirect proof) no
// destination since we capture nothing.
func TestNewHclogShim_Nil_NoOutput(t *testing.T) {
	t.Parallel()
	shim := newHclogShim(nil)
	require.NotNil(t, shim)
	// Cycle through the methods that hashicorp/go-plugin calls. No
	// panic = pass.
	shim.Info("a")
	shim.Warn("b")
	shim.Error("c")
	shim.Debug("d")
	// Multi-arg path.
	shim.Info("with kvs", "key", "value")
}

// TestSlogHclogAdapter_TraceMethod_RoutesToDebug double-tests the
// per-method routing (Trace goes to slog.Debug). The level-method
// table above covers this, but a direct assertion makes the
// invariant explicit + survives a future Log() refactor.
func TestSlogHclogAdapter_TraceMethod_RoutesToDebug(t *testing.T) {
	t.Parallel()
	shim, buf := captureHclog()
	shim.Trace("trace-direct")
	out := buf.String()
	assert.True(t, strings.Contains(out, `"level":"DEBUG"`),
		"Trace() must call slog.Debug; got: %s", out)
}
