package main

import (
	"flag"
	"strconv"
	"strings"
)

// parseInterspersed parses fs from args, allowing flags before and after the
// positional arguments ("new test --profile Work" as well as
// "new --profile Work test"). The flag package alone stops at the first
// positional argument and would silently ignore every flag after the name.
// "--" ends flag parsing; everything after it is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// isFlagError reports whether err came from the flag package rejecting the
// command line, which is a usage error rather than a failure.
func isFlagError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "flag provided but not defined") ||
		strings.HasPrefix(msg, "flag needs an argument") ||
		strings.HasPrefix(msg, "invalid value") ||
		strings.HasPrefix(msg, "invalid boolean")
}

// flagErrorMessage rewrites the flag package's messages to the "--name"
// spelling the help uses.
func flagErrorMessage(err error) string {
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "flag provided but not defined: -"); ok {
		return "unknown flag --" + strings.TrimPrefix(name, "-")
	}
	if name, ok := strings.CutPrefix(msg, "flag needs an argument: -"); ok {
		return "flag --" + strings.TrimPrefix(name, "-") + " needs a value"
	}
	return msg
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// optionalBool is a boolean flag that also records whether it was given, so
// "not passed" can fall back to a saved setting while --x=false overrides it.
type optionalBool struct {
	set   bool
	value bool
}

func (o *optionalBool) String() string {
	if o == nil || !o.set {
		return ""
	}
	return strconv.FormatBool(o.value)
}

func (o *optionalBool) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	o.set, o.value = true, v
	return nil
}

func (o *optionalBool) IsBoolFlag() bool { return true }

// ptr returns the value as a pointer, nil when the flag was not given.
func (o *optionalBool) ptr() *bool {
	if !o.set {
		return nil
	}
	v := o.value
	return &v
}
