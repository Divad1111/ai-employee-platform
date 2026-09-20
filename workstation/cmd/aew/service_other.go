//go:build !windows

package main

func checkWindowsService() (bool, error) {
	return false, nil
}
