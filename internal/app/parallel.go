package app

import (
	"errors"
)

func getHostStatesInParallel(hosts []string, getter func(string) (*HostState, error)) (map[string]*HostState, error) {
	type result struct {
		name  string
		state *HostState
		err   error
	}
	results := make(chan result, len(hosts))
	for _, host := range hosts {
		go func(host string) {
			state, err := getter(host)
			results <- result{host, state, err}
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
		key string
		err error
	}
	errs := make(chan pair, len(arguments))
	for _, argValue := range arguments {
		go func(host string) {
			errs <- pair{host, f(host)}
		}(argValue)
	}
	result := make(map[string]error)
	for i := 0; i < len(arguments); i++ {
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
