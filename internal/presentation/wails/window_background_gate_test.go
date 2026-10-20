package wails

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Regression for the recurring "wails runtime panic ... GetICoreWebView2Controller2"
// startup crash: ApplySettings used to call SetBackgroundColour while the main
// window's WebView2 controller was still embedding. On Windows the InvokeSync
// callback runs re-entrantly inside Embed and dereferences a nil controller,
// which aborts window creation and leaves the process running without any
// window. The WebView-touching background calls must be gated behind
// applyMainWindowBackground and flushed once WindowRuntimeReady fires.
func TestApplySettingsGatesMainWebViewBackgroundUntilRuntimeReady(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("window_manager.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)

	applyStart := strings.Index(text, "func (manager *WindowManager) ApplySettings(")
	if applyStart < 0 {
		t.Fatal("could not locate ApplySettings")
	}
	applyEnd := strings.Index(text[applyStart:], "\nfunc (manager *WindowManager) dispatchWindowEvent(")
	if applyEnd < 0 {
		t.Fatal("could not locate the end of ApplySettings")
	}
	applySettings := text[applyStart : applyStart+applyEnd]

	if !strings.Contains(applySettings, "manager.applyMainWindowBackground(func() {") {
		t.Fatal("ApplySettings must gate the main window WebView background calls behind applyMainWindowBackground")
	}
	direct := strings.Index(applySettings, "manager.mainWindow.SetBackgroundColour(")
	if direct >= 0 && !strings.Contains(applySettings[:direct], "manager.applyMainWindowBackground(func() {") {
		t.Fatal("ApplySettings calls SetBackgroundColour directly; this races with WebView2 controller embedding during startup")
	}
	if !strings.Contains(applySettings, "syncListenNativeVideoHostBackground(manager.mainWindow, color)") {
		t.Fatal("runtime theme changes do not preserve the active native-video underlay")
	}

	readyStart := strings.Index(text, "events.Common.WindowRuntimeReady")
	if readyStart < 0 {
		t.Fatal("could not locate the WindowRuntimeReady handler")
	}
	handler := text[readyStart:min(readyStart+400, len(text))]
	if !strings.Contains(handler, "flushPendingMainWindowBackground()") {
		t.Fatal("the WindowRuntimeReady handler must flush the pending main window background")
	}
}

func TestApplyMainWindowBackgroundDefersUntilRuntimeReady(t *testing.T) {
	t.Parallel()

	manager := &WindowManager{}
	var applied atomic.Int32
	manager.applyMainWindowBackground(func() { applied.Add(1) })
	if applied.Load() != 0 {
		t.Fatal("background must not be applied before the main window runtime is ready")
	}

	manager.flushPendingMainWindowBackground()
	if applied.Load() != 1 {
		t.Fatalf("pending background applied %d times after runtime ready, want 1", applied.Load())
	}
	if !manager.mainRuntimeReady.Load() {
		t.Fatal("flush must mark the main window runtime ready")
	}

	manager.applyMainWindowBackground(func() { applied.Add(1) })
	if applied.Load() != 2 {
		t.Fatal("background must apply immediately once the main window runtime is ready")
	}
}

func TestApplyMainWindowBackgroundKeepsLatestPendingOnly(t *testing.T) {
	t.Parallel()

	manager := &WindowManager{}
	var applied atomic.Int32
	manager.applyMainWindowBackground(func() { applied.Add(100) })
	manager.applyMainWindowBackground(func() { applied.Add(1) })
	manager.flushPendingMainWindowBackground()
	if applied.Load() != 1 {
		t.Fatalf("only the latest pending background should apply, got %d", applied.Load())
	}
}
