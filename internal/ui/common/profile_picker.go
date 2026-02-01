package common

// NewProfileOption is the sentinel option shown at the bottom of the profile
// picker that triggers the creation of a new profile.
const NewProfileOption = "New profile..."

// NewProfilePicker creates a select dialog listing existing profiles
// with a "New profile..." option at the end. If currentProfile matches
// one of the profiles, the cursor starts on that entry.
func NewProfilePicker(id string, profiles []string, currentProfile string) *Dialog {
	options := make([]string, 0, len(profiles)+1)
	options = append(options, profiles...)
	options = append(options, NewProfileOption)

	cursor := 0
	if currentProfile != "" {
		for i, p := range options {
			if p == currentProfile {
				cursor = i
				break
			}
		}
	}

	return &Dialog{
		id:             id,
		dtype:          DialogSelect,
		title:          "Set Profile",
		message:        "Profile isolates Claude settings (permissions, memory) for this project.",
		options:        options,
		cursor:         cursor,
		verticalLayout: true,
	}
}
