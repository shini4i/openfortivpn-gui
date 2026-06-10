package ui

import (
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	logDialogMaxLines = 500
	// logDialogTrimChunk is the overshoot allowed before trimming back to
	// logDialogMaxLines. Trimming requires a full text-buffer rebuild, so it
	// runs once per chunk of appended lines instead of once per line.
	logDialogTrimChunk = 100
)

// appendLogLine appends line to lines and trims the oldest entries back to
// max once the slice overshoots max+chunk. Returns the updated slice and
// whether a trim occurred (the caller must then rebuild its display buffer).
func appendLogLine(lines []string, line string, max, chunk int) ([]string, bool) {
	lines = append(lines, line)
	if len(lines) < max+chunk {
		return lines, false
	}
	return lines[len(lines)-max:], true
}

// LogDialog displays VPN connection logs in a separate window.
type LogDialog struct {
	dialog    *adw.Dialog
	logBuffer *gtk.TextBuffer
	logView   *gtk.TextView
	logLines  []string
}

// NewLogDialog creates a new log dialog.
func NewLogDialog() *LogDialog {
	ld := &LogDialog{
		logLines: make([]string, 0, logDialogMaxLines),
	}
	ld.setupDialog()
	return ld
}

// setupDialog creates the dialog UI.
func (ld *LogDialog) setupDialog() {
	ld.dialog = adw.NewDialog()
	ld.dialog.SetTitle("Connection Log")
	ld.dialog.SetContentWidth(700)
	ld.dialog.SetContentHeight(400)

	// Create toolbar view
	toolbarView := adw.NewToolbarView()

	// Header bar with close button
	headerBar := adw.NewHeaderBar()

	// Clear button
	clearButton := gtk.NewButtonFromIconName("edit-clear-symbolic")
	clearButton.SetTooltipText("Clear Log")
	clearButton.ConnectClicked(func() {
		ld.Clear()
	})
	headerBar.PackStart(clearButton)

	toolbarView.AddTopBar(headerBar)

	// Log view
	ld.logBuffer = gtk.NewTextBuffer(nil)
	ld.logView = gtk.NewTextViewWithBuffer(ld.logBuffer)
	ld.logView.SetEditable(false)
	ld.logView.SetCursorVisible(false)
	ld.logView.SetMonospace(true)
	ld.logView.SetWrapMode(gtk.WrapWordChar)
	ld.logView.SetTopMargin(8)
	ld.logView.SetBottomMargin(8)
	ld.logView.SetLeftMargin(12)
	ld.logView.SetRightMargin(12)

	scrolledWindow := gtk.NewScrolledWindow()
	scrolledWindow.SetPolicy(gtk.PolicyAutomatic, gtk.PolicyAutomatic)
	scrolledWindow.SetChild(ld.logView)

	toolbarView.SetContent(scrolledWindow)

	ld.dialog.SetChild(toolbarView)
}

// AppendLog adds a line to the log. The common case inserts only the new
// line at the end of the buffer; a full rebuild happens only when the
// retention limit trims old lines (once per logDialogTrimChunk appends).
func (ld *LogDialog) AppendLog(line string) {
	glib.IdleAdd(func() {
		var trimmed bool
		ld.logLines, trimmed = appendLogLine(ld.logLines, line, logDialogMaxLines, logDialogTrimChunk)

		if trimmed {
			ld.logBuffer.SetText(strings.Join(ld.logLines, "\n"))
		} else {
			end := ld.logBuffer.EndIter()
			if ld.logBuffer.CharCount() > 0 {
				ld.logBuffer.Insert(end, "\n")
			}
			ld.logBuffer.Insert(end, line)
		}

		// Scroll to end
		end := ld.logBuffer.EndIter()
		ld.logView.ScrollToIter(end, 0, false, 0, 0)
	})
}

// Clear clears the log.
func (ld *LogDialog) Clear() {
	glib.IdleAdd(func() {
		ld.logLines = ld.logLines[:0]
		ld.logBuffer.SetText("")
	})
}

// Present shows the log dialog.
func (ld *LogDialog) Present(parent gtk.Widgetter) {
	ld.dialog.Present(parent)
}
