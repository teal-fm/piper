package pages

// LoginErrorMessage accepts only known error codes, never arbitrary URL text.
func LoginErrorMessage(code string) string {
	switch code {
	case "missing_handle":
		return "Enter your ATProto handle to sign in."
	case "invalid_handle":
		return "Enter a valid ATProto handle, such as alice.bsky.social. Do not include a URL or slash."
	case "lookup_failed":
		return "We could not find that account. Check your handle and try again."
	case "not_allowed":
		return "This account is not allowed to sign in to this Piper instance."
	case "login_failed":
		return "Could not start sign-in with your provider. Please try again."
	case "callback_failed":
		return "Sign-in could not be completed. Please try again."
	default:
		return ""
	}
}
