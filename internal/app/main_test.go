package app

import (
	"os"
	"sync"
	"testing"
)

// browserOpens records the URLs the package tried to open. Tests drive the
// real Review Changes and skill-usage handlers, and without this stub every
// `go test` run opened a browser tab on the developer's machine.
var browserOpens struct {
	sync.Mutex
	urls []string
}

func TestMain(m *testing.M) {
	openInBrowser = func(url string) error {
		browserOpens.Lock()
		browserOpens.urls = append(browserOpens.urls, url)
		browserOpens.Unlock()
		return nil
	}
	os.Exit(m.Run())
}

func openedURLs() []string {
	browserOpens.Lock()
	defer browserOpens.Unlock()
	return append([]string(nil), browserOpens.urls...)
}
