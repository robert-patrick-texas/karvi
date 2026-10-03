package configload

// cliFlags is a test's option values as the cli layer passes them, each
// sourced to an option named for the test.
func cliFlags(values map[string]any) map[string]FlagValue {
	flags := map[string]FlagValue{}
	for k, v := range values {
		flags[k] = FlagValue{Value: v, Option: "--test-option"}
	}
	return flags
}
