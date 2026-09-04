package define

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPullPolicy(t *testing.T) {
	t.Parallel()
	for key, val := range PolicyMap {
		t.Run(key, func(t *testing.T) {
			assert.Equal(t, val, PolicyMap[val.String()])
		})
	}
}
