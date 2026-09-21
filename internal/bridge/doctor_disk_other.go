//go:build !linux

package bridge

import "errors"

func platformDoctorStatfs(string) (uint64, error) {
	return 0, errors.New("disk space check unavailable on this platform")
}
