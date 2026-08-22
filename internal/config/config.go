package config

// Config holds the application configuration
type Config struct {
	Paths         *Paths
	PortStart     int
	PortRangeSize int
	Assistants    map[string]AssistantConfig
	UI            UISettings
}

// AssistantConfig defines how to launch an AI assistant
type AssistantConfig struct {
	Command          string // Shell command to launch the assistant
	InterruptCount   int    // Number of Ctrl-C signals to send (default 1, claude needs 2)
	InterruptDelayMs int    // Delay between interrupts in milliseconds
}

// DefaultConfig returns the default configuration
func DefaultConfig() (*Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Paths:         paths,
		PortStart:     6200,
		PortRangeSize: 10,
		UI:            loadUISettings(paths.ConfigPath),
		Assistants: map[string]AssistantConfig{
			"claude": {
				Command:          "claude",
				InterruptCount:   2,
				InterruptDelayMs: 200,
			},
			// Codex interrupts the running turn on the first Ctrl-C; a second
			// one is its quit gesture, so a repeat would kill the session
			// medusa was only trying to interrupt.
			"codex": {
				Command:        "codex",
				InterruptCount: 1,
			},
		},
	}
	return cfg, nil
}
