package main

import "errors"

// rootErr returns the innermost error in an unwrap chain.
func rootErr(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}
