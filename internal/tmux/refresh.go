package tmux

import "strings"

// RefreshSessionClients makes tmux redraw every client attached to a session
// from scratch. tmux otherwise sends a client only the cells that changed, so
// a client that lost part of that stream stays wrong until those cells change
// again; a full redraw puts it back in step with the pane. A session that does
// not exist has nothing to redraw.
func RefreshSessionClients(sessionName string, opts Options) error {
	if err := EnsureAvailable(); err != nil {
		return err
	}
	exists, err := hasSession(sessionName, opts)
	if err != nil || !exists {
		return err
	}
	list, cancel := tmuxCommand(opts, "list-clients", "-t", sessionName, "-F", "#{client_name}")
	out, err := list.Output()
	cancel()
	if err != nil {
		return err
	}
	for _, client := range strings.Split(string(out), "\n") {
		client = strings.TrimSpace(client)
		if client == "" {
			continue
		}
		refresh, cancel := tmuxCommand(opts, "refresh-client", "-t", client)
		err := refresh.Run()
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
