package collector

import (
	"github.com/stretchr/testify/assert"
	"github.com/weaponry/pgscv/internal/filter"
	"github.com/weaponry/pgscv/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemCollector_Update(t *testing.T) {
	var input = pipelineInput{
		required: []string{
			"node_filesystem_bytes",
			"node_filesystem_bytes_total",
			"node_filesystem_files",
			"node_filesystem_files_total",
		},
		collector:         NewFilesystemCollector,
		collectorSettings: model.CollectorSettings{Filters: filter.New()},
	}

	pipeline(t, input)
}

func Test_getFilesystemStats(t *testing.T) {
	got, err := getFilesystemStats()
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Greater(t, len(got), 0)
}

func Test_parseFilesystemStats(t *testing.T) {
	file, err := os.Open(filepath.Clean("testdata/proc/mounts.golden"))
	assert.NoError(t, err)

	stats, err := parseFilesystemStats(file)
	assert.NoError(t, err)
	assert.Greater(t, len(stats), 1)
	assert.Greater(t, stats[0].size, float64(0))
	assert.Greater(t, stats[0].free, float64(0))
	assert.Greater(t, stats[0].avail, float64(0))
	assert.Greater(t, stats[0].files, float64(0))
	assert.Greater(t, stats[0].filesfree, float64(0))

	_ = file.Close()

	// test with wrong format file
	file, err = os.Open(filepath.Clean("testdata/proc/netdev.golden"))
	assert.NoError(t, err)

	stats, err = parseFilesystemStats(file)
	assert.Error(t, err)
	assert.Nil(t, stats)
	_ = file.Close()
}
