// Package pq is a pure Go openGauss driver for the database/sql package.

// +build js android hurd illumos zos

package pq

func userCurrent() (string, error) {
	return "", ErrCouldNotDetectUsername
}
