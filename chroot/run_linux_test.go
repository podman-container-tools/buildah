package chroot

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatFlagNames(t *testing.T) {
	var names []string
	var flags int
	for flag := range statFlagMap {
		flags |= flag
		names = append(names, statFlagMap[flag])
		assert.Equal(t, []string{statFlagMap[flag]}, statFlagNames(uintptr(flag)))
	}
	slices.Sort(names)
	assert.Equal(t, names, statFlagNames(uintptr(flags)))
}

func TestMountFlagNames(t *testing.T) {
	var names []string
	var flags uintptr
	for flag := range mountFlagMap {
		flags |= flag
		names = append(names, mountFlagMap[flag])
		assert.Equal(t, []string{mountFlagMap[flag]}, mountFlagNames(flag))
	}
	slices.Sort(names)
	assert.Equal(t, names, mountFlagNames(flags))
}
