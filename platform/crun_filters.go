package platform

import "regexp"

// CompileFilters compiles a platform's crun-filters: regular expressions in
// Go's syntax, each matched against one
// output line of a collection block. It is the compiler of the list for the
// configuration's check (config_platform_crun_filter_invalid) and the
// writer; the plan's check (execution_plan_invalid) calls the same
// regexp.Compile itself, since that package imports only inventory. On a
// pattern that does not compile it returns that entry's index and the
// compiler's own error; the caller names the code.
func CompileFilters(list []string) ([]*regexp.Regexp, int, error) {
	if len(list) == 0 {
		return nil, -1, nil
	}
	res := make([]*regexp.Regexp, 0, len(list))
	for i, p := range list {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, i, err
		}
		res = append(res, re)
	}
	return res, -1, nil
}
