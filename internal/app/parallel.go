package app

import (
	"errors"
)

func getHostStatesInParallel(hosts []string, getter func(string) (*HostState, error)) (map[string]*HostState, error) {
	type result struct {
		err   error
		state *HostState
		name  string
	}
	results := make(chan result, len(hosts))
	for _, host := range hosts {
		go func(host string) {
			state, err := getter(host)
			results <- result{err, state, host}
		}(host)
	}
	shardState := make(map[string]*HostState)
	var err error
	for range hosts {
		result := <-results
		if result.err != nil {
			err = result.err
		} else {
			shardState[result.name] = result.state
		}
	}
	if err != nil {
		return nil, err
	}
	return shardState, nil
}

func runParallel(f func(string) error, arguments []string) map[string]error {
	type pair struct {
		err error
		key string
	}
	errs := make(chan pair, len(arguments))
	for _, argValue := range arguments {
		go func(host string) {
			errs <- pair{f(host), host}
		}(argValue)
	}
	result := make(map[string]error)
	for range arguments {
		pairValue := <-errs
		result[pairValue.key] = pairValue.err
	}
	return result
}

func combineErrors(allErrors map[string]error) error {
	var errStr string
	for _, err := range allErrors {
		if err != nil {
			errStr += err.Error() + ";"
		}
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}
