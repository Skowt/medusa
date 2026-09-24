package config

import "strings"

// AssistantFor returns the assistant a new tab under profile should open on:
// the one last launched under that profile, or the global last-used assistant
// when the profile has never launched one (or the workspace has no profile).
func (s *UISettings) AssistantFor(profile string) string {
	if assistant := s.LastAssistantByProfile[strings.TrimSpace(profile)]; assistant != "" {
		return assistant
	}
	return s.LastAssistant
}

// RememberAssistant records assistant as the last one launched under profile.
// The global value moves too, so a profile that has never launched anything
// opens on whatever was used most recently anywhere.
func (s *UISettings) RememberAssistant(profile, assistant string) {
	s.LastAssistant = assistant
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return
	}
	if s.LastAssistantByProfile == nil {
		s.LastAssistantByProfile = map[string]string{}
	}
	s.LastAssistantByProfile[profile] = assistant
}

// RenameProfileAssistant carries a profile's remembered assistant over to its
// new name.
func (s *UISettings) RenameProfileAssistant(oldName, newName string) {
	assistant, ok := s.LastAssistantByProfile[oldName]
	if !ok {
		return
	}
	delete(s.LastAssistantByProfile, oldName)
	s.LastAssistantByProfile[newName] = assistant
}

// ForgetProfileAssistant drops a deleted profile's remembered assistant, so a
// new profile later created under the same name starts from the global value.
func (s *UISettings) ForgetProfileAssistant(profile string) {
	delete(s.LastAssistantByProfile, profile)
}
