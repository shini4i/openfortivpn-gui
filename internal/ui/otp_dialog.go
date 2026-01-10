package ui

import (
	"regexp"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// otpPattern matches valid OTP codes (4-8 digits).
// Most authenticators produce 6-digit codes, but some systems use 4 or 8.
var otpPattern = regexp.MustCompile(`^\d{4,8}$`)

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
// Valid OTPs are 4-8 digit numeric strings.
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
	od.otpEntry.SetInputPurpose(gtk.InputPurposeNumber)

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

	// Handle responses
	od.dialog.ConnectResponse(func(response string) {
		// Guard against double invocation
		if od.resultSent {
			return
		}

		result := OTPDialogResult{
			Cancelled: response != "submit",
		}

		if !result.Cancelled {
			otp := od.otpEntry.Text()
			// Validate OTP format (4-8 digits)
			if !isValidOTP(otp) {
				// Show error styling and keep dialog open
				od.otpEntry.AddCSSClass("error")
				od.dialog.SetBody("Invalid OTP. Please enter 4-8 digits.")
				return
			}
			result.OTP = otp
		}

		od.resultSent = true
		if od.onResult != nil {
			od.onResult(result)
		}
	})

	// Enable submit on Enter key
	od.otpEntry.ConnectApply(func() {
		// Guard against double invocation
		if od.resultSent {
			return
		}

		otp := od.otpEntry.Text()
		// Validate OTP format (4-8 digits)
		if !isValidOTP(otp) {
			// Show error styling and keep dialog open
			od.otpEntry.AddCSSClass("error")
			od.dialog.SetBody("Invalid OTP. Please enter 4-8 digits.")
			return
		}

		od.resultSent = true
		// Trigger the result callback directly with success
		result := OTPDialogResult{
			Cancelled: false,
			OTP:       otp,
		}
		if od.onResult != nil {
			od.onResult(result)
		}
		od.dialog.Close()
	})

	// Clear error styling when user starts typing
	od.otpEntry.ConnectChanged(func() {
		od.otpEntry.RemoveCSSClass("error")
		od.dialog.SetBody("Enter the one-time password from your authenticator app.")
	})
}

// Present shows the OTP dialog.
func (od *OTPDialog) Present(parent gtk.Widgetter) {
	// Reset state for reuse
	od.otpEntry.SetText("")
	od.otpEntry.RemoveCSSClass("error")
	od.dialog.SetBody("Enter the one-time password from your authenticator app.")
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
