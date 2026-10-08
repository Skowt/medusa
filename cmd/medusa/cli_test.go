package main

import (
	"bytes"
	"flag"
	"reflect"
	"strings"
	"testing"
)

// runForTest dispatches args through the real command tree and captures what
// it prints.
func runForTest(t *testing.T, args ...string) (handled bool, code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	handled, code = runCLI(args, &cliContext{stdout: &out, stderr: &errOut, stdin: strings.NewReader("")})
	return handled, code, out.String(), errOut.String()
}

func TestNoArgumentsStartsTheTUI(t *testing.T) {
	if handled, _, _, _ := runForTest(t); handled {
		t.Fatal("medusa with no arguments must fall through to the TUI")
	}
}

func TestTopLevelHelp(t *testing.T) {
	for _, arg := range []string{"help", "?", "-h", "--help"} {
		handled, code, out, _ := runForTest(t, arg)
		if !handled || code != 0 {
			t.Errorf("medusa %s: handled=%v code=%d, want handled with 0", arg, handled, code)
		}
		for _, want := range []string{"Usage:", "workspace", "skills", "--version"} {
			if !strings.Contains(out, want) {
				t.Errorf("medusa %s help lacks %q:\n%s", arg, want, out)
			}
		}
	}
}

func TestWorkspaceHelpAtEveryEntryPoint(t *testing.T) {
	for _, args := range [][]string{
		{"workspace"}, {"workspace", "help"}, {"help", "workspace"}, {"ws", "--help"}, {"workspace", "?"},
	} {
		_, code, out, _ := runForTest(t, args...)
		if code != 0 {
			t.Errorf("%v: code %d, want 0", args, code)
		}
		for _, want := range []string{"new", "list", "delete", "medusa workspace <command>"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v help lacks %q:\n%s", args, want, out)
			}
		}
	}
}

func TestLeafHelpAtEveryEntryPoint(t *testing.T) {
	for _, args := range [][]string{
		{"workspace", "new", "--help"}, {"workspace", "new", "help"}, {"help", "workspace", "new"},
		{"ws", "new", "-h"}, {"workspace", "new", "test", "--profile", "Work", "--help"},
	} {
		_, code, out, _ := runForTest(t, args...)
		if code != 0 {
			t.Errorf("%v: code %d, want 0", args, code)
		}
		for _, want := range []string{"Usage:", "--prompt", "--profile <name>", "Examples:"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v help lacks %q:\n%s", args, want, out)
			}
		}
	}
}

func TestSkillsHelpListsItsFlags(t *testing.T) {
	_, code, out, _ := runForTest(t, "help", "skills")
	if code != 0 || !strings.Contains(out, "--report") {
		t.Fatalf("code %d, help:\n%s", code, out)
	}
}

func TestUnknownCommandsExitTwoWithHelp(t *testing.T) {
	_, code, out, errOut := runForTest(t, "bogus")
	if code != 2 || out != "" || !strings.Contains(errOut, `unknown command "bogus"`) || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("medusa bogus: code %d stdout %q stderr %q", code, out, errOut)
	}
	_, code, _, errOut = runForTest(t, "workspace", "bogus")
	if code != 2 || !strings.Contains(errOut, "medusa workspace <command>") {
		t.Fatalf("medusa workspace bogus: code %d stderr %q", code, errOut)
	}
	_, code, _, errOut = runForTest(t, "help", "nope")
	if code != 2 || !strings.Contains(errOut, `unknown command "nope"`) {
		t.Fatalf("medusa help nope: code %d stderr %q", code, errOut)
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	_, code, _, errOut := runForTest(t, "workspace", "list", "--nope")
	if code != 2 || !strings.Contains(errOut, "--nope") || !strings.Contains(errOut, "medusa workspace list --help") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestFlagsMayFollowThePositionalName(t *testing.T) {
	for _, args := range [][]string{
		{"test", "--profile", "Work", "--json"},
		{"--profile", "Work", "test", "--json"},
		{"--profile=Work", "--json", "test"},
	} {
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		profile := fs.String("profile", "", "")
		asJSON := fs.Bool("json", false, "")
		pos, err := parseInterspersed(fs, args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !reflect.DeepEqual(pos, []string{"test"}) || *profile != "Work" || !*asJSON {
			t.Errorf("%v: positional %v profile %q json %v", args, pos, *profile, *asJSON)
		}
	}
}

func TestDoubleDashEndsFlags(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	_ = fs.Bool("json", false, "")
	pos, err := parseInterspersed(fs, []string{"--json", "--", "-weird", "--json"})
	if err != nil || !reflect.DeepEqual(pos, []string{"-weird", "--json"}) {
		t.Fatalf("positional %v, err %v", pos, err)
	}
}

func TestOptionalBool(t *testing.T) {
	parse := func(args ...string) optionalBool {
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		var b optionalBool
		fs.Var(&b, "fullscreen", "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if b := parse(); b.set {
		t.Error("absent flag reads as set")
	}
	if b := parse("--fullscreen"); !b.set || !b.value {
		t.Errorf("--fullscreen = %+v", b)
	}
	if b := parse("--fullscreen=false"); !b.set || b.value {
		t.Errorf("--fullscreen=false = %+v", b)
	}
}
