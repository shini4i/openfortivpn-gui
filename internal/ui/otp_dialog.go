package ui

import (
	"regexp"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// otpPattern matches valid OTP codes: 4-16 alphanumeric characters.
// Most authenticators produce 6-digit codes, but FortiToken hardware and SMS
// tokens can be longer and alphanumeric.
var otpPattern = regexp.MustCompile(`^[A-Za-z0-9]{4,16}$`)

// OTPDialogResult represents the result of the OTP dialog.
type OTPDialogResult struct {
	// OTP is the one-time password entered by the user.
	OTP string
	// Cancelled indicates whether the dialog was cancelled.
	Cancelled bool
}

// OTPDialog prompts the user for a one-time password.
type OTPDialog struct {
	dialog   *adw.AlertDialog
	otpEntry *adw.EntryRow

	// Result callback
	onResult func(result OTPDialogResult)

	// Guard flag to prevent double callback invocation
	resultSent bool
}

// isValidOTP checks if the given string is a valid OTP code.
// Valid OTPs are 4-16 alphanumeric characters.
func isValidOTP(otp string) bool {
	return otpPattern.MatchString(otp)
}

// NewOTPDialog creates a new OTP entry dialog.
func NewOTPDialog() *OTPDialog {
	od := &OTPDialog{}
	od.setupDialog()
	return od
}

// setupDialog creates the OTP dialog UI.
func (od *OTPDialog) setupDialog() {
	od.dialog = adw.NewAlertDialog("Two-Factor Authentication", "")
	od.dialog.SetBody("Enter the one-time password from your authenticator app.")

	// Create OTP entry
	od.otpEntry = adw.NewEntryRow()
	od.otpEntry.SetTitle("One-Time Password")
	// Free-form input: tokens may be alphanumeric, not just digits.
	od.otpEntry.SetInputPurpose(gtk.InputPurposeFreeForm)

	// Wrap in preferences group for proper styling
	group := adw.NewPreferencesGroup()
	group.Add(od.otpEntry)

	od.dialog.SetExtraChild(group)

	// Add buttons
	od.dialog.AddResponse("cancel", "Cancel")
	od.dialog.AddResponse("submit", "Submit")
	od.dialog.SetResponseAppearance("submit", adw.ResponseSuggested)
	od.dialog.SetDefaultResponse("submit")
	od.dialog.SetCloseResponse("cancel")

	// Submit is gated on valid input (toggled in ConnectChanged). AdwAlertDialog
	// closes automatically whenever a response is activated, so an invalid value
	// cannot be rejected from inside the response handler without silently
	// dismissing the dialog — instead we prevent it from activating Submit at
	// all. A disabled response is also skipped as the default response, so
	// pressing Enter with invalid input does nothing rather than aborting.
	od.dialog.SetResponseEnabled("submit", false)

	// Handle responses. Submit is only reachable with valid input (see gating
	// above), so the handler never vetoes: it always produces a result, which
	// guarantees the caller's callback runs and the dialog never dead-ends.
	od.dialog.ConnectResponse(func(response string) {
		// Guard against double invocation (e.g. the close-response emitted after
		// the Enter path in ConnectApply already delivered a result).
		if od.resultSent {
			return
		}

		od.resultSent = true
		if od.onResult != nil {
			od.onResult(otpResultFor(response, od.otpEntry.Text()))
		}
	})

	// Enable submit on Enter key
	od.otpEntry.ConnectApply(func() {
		// Guard against double invocation
		if od.resultSent {
			return
		}

		otp := od.otpEntry.Text()
		// Validate OTP format (4-16 alphanumeric characters)
		if !isValidOTP(otp) {
			// Show error styling and keep dialog open
			od.otpEntry.AddCSSClass("error")
			od.dialog.SetBody("Invalid OTP. Please enter 4-16 letters or digits.")
			return
		}

		od.resultSent = true
		// Deliver the result directly, then close. Close() emits the
		// close-response, but the resultSent guard makes the resulting
		// ConnectResponse call a no-op.
		if od.onResult != nil {
			od.onResult(otpResultFor("submit", otp))
		}
		od.dialog.Close()
	})

	// Clear error styling when user starts typing and gate Submit on validity.
	od.otpEntry.ConnectChanged(func() {
		od.otpEntry.RemoveCSSClass("error")
		od.dialog.SetBody("Enter the one-time password from your authenticator app.")
		od.dialog.SetResponseEnabled("submit", isValidOTP(od.otpEntry.Text()))
	})
}

// otpResultFor maps an AdwAlertDialog response id and the entered token to a
// dialog result. Only the "submit" response carries the OTP; every other
// response — cancel, the close-response emitted when the dialog is dismissed,
// or any unexpected id — is treated as a cancellation.
func otpResultFor(response, otp string) OTPDialogResult {
	if response != "submit" {
		return OTPDialogResult{Cancelled: true}
	}
	return OTPDialogResult{OTP: otp}
}

// Present shows the OTP dialog.
func (od *OTPDialog) Present(parent gtk.Widgetter) {
	// Reset state for reuse. The entry is empty, which is not a valid OTP, so
	// Submit starts disabled and is enabled once valid input is typed.
	od.otpEntry.SetText("")
	od.otpEntry.RemoveCSSClass("error")
	od.dialog.SetBody("Enter the one-time password from your authenticator app.")
	od.dialog.SetResponseEnabled("submit", false)
	od.resultSent = false
	od.dialog.Present(parent)
}

// OnResult registers a callback for when the dialog is closed.
func (od *OTPDialog) OnResult(callback func(result OTPDialogResult)) {
	od.onResult = callback
}

// ShowOTPDialog is a convenience function to show an OTP dialog and get the result.
// It returns the OTP string and a boolean indicating whether the dialog was cancelled.
func ShowOTPDialog(parent gtk.Widgetter, callback func(otp string, cancelled bool)) {
	dialog := NewOTPDialog()
	dialog.OnResult(func(result OTPDialogResult) {
		callback(result.OTP, result.Cancelled)
	})
	dialog.Present(parent)
}
