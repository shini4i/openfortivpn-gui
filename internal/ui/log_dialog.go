package ui

import (
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	logDialogMaxLines = 500
)

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

// AppendLog adds a line to the log.
func (ld *LogDialog) AppendLog(line string) {
	glib.IdleAdd(func() {
		ld.logLines = append(ld.logLines, line)

		// Trim if too many lines
		if len(ld.logLines) > logDialogMaxLines {
			ld.logLines = ld.logLines[len(ld.logLines)-logDialogMaxLines:]
		}

		// Update buffer
		ld.logBuffer.SetText(strings.Join(ld.logLines, "\n"))

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
