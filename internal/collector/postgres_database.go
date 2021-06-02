package collector

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaponry/pgscv/internal/log"
	"github.com/weaponry/pgscv/internal/model"
	"github.com/weaponry/pgscv/internal/store"
	"strconv"
)

const (
	databaseQuery = "SELECT " +
		"coalesce(datname, 'global') AS database, " +
		"xact_commit, xact_rollback, blks_read, blks_hit, tup_returned, tup_fetched, tup_inserted, tup_updated, tup_deleted, " +
		"conflicts, temp_files, temp_bytes, deadlocks, blk_read_time, blk_write_time, pg_database_size(datname) as size_bytes, " +
		"coalesce(extract('epoch' from age(now(), stats_reset)), 0) as stats_age_seconds " +
		"FROM pg_stat_database WHERE datname IN (SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate) " +
		"OR datname IS NULL"

	xidLimitQuery = "SELECT 'database' AS src, 2147483647 - greatest(max(age(datfrozenxid)), max(age(coalesce(nullif(datminmxid, 1), datfrozenxid)))) AS to_limit FROM pg_database " +
		"UNION SELECT 'prepared_xacts' AS src, 2147483647 - coalesce(max(age(transaction)), 0) AS to_limit FROM pg_prepared_xacts " +
		"UNION SELECT 'replication_slots' AS src, 2147483647 - greatest(coalesce(min(age(xmin)), 0), coalesce(min(age(catalog_xmin)), 0)) AS to_limit FROM pg_replication_slots"
)

type postgresDatabasesCollector struct {
	commits    typedDesc
	rollbacks  typedDesc
	conflicts  typedDesc
	deadlocks  typedDesc
	blocks     typedDesc
	tuples     typedDesc
	tempbytes  typedDesc
	tempfiles  typedDesc
	blockstime typedDesc
	sizes      typedDesc
	statsage   typedDesc
	xidlimit   typedDesc
	labelNames []string
}

// NewPostgresDatabasesCollector returns a new Collector exposing postgres databases stats.
// For details see https://www.postgresql.org/docs/current/monitoring-stats.html#PG-STAT-DATABASE-VIEW
func NewPostgresDatabasesCollector(constLabels labels, settings model.CollectorSettings) (Collector, error) {
	var labels = []string{"database"}

	return &postgresDatabasesCollector{
		labelNames: labels,
		commits: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "xact_commits_total", "Total number of transactions had been committed.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		rollbacks: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "xact_rollbacks_total", "Total number of transactions had been rolled back.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		conflicts: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "conflicts_total", "Total number of recovery conflicts occurred.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		deadlocks: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "deadlocks_total", "Total number of deadlocks occurred.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		blocks: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "blocks_total", "Total number of disk blocks had been accessed by each type of access.", 0},
			prometheus.CounterValue,
			[]string{"database", "access"}, constLabels,
			settings.Filters,
		),
		tuples: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "tuples_total", "Total number of rows processed by each type of operation.", 0},
			prometheus.CounterValue,
			[]string{"database", "tuples"}, constLabels,
			settings.Filters,
		),
		tempbytes: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "temp_bytes_total", "Total amount of data written to temporary files by queries.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		tempfiles: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "temp_files_total", "Total number of temporary files created by queries.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		blockstime: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "blk_time_seconds_total", "Total time spent accessing data blocks by backends in this database in each access type, in seconds.", .001},
			prometheus.CounterValue,
			[]string{"database", "type"}, constLabels,
			settings.Filters,
		),
		sizes: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "size_bytes", "Total size of the database, in bytes.", 0},
			prometheus.GaugeValue,
			labels, constLabels,
			settings.Filters,
		),
		statsage: newBuiltinTypedDesc(
			descOpts{"postgres", "database", "stats_age_seconds_total", "The age of the databases activity statistics, in seconds.", 0},
			prometheus.CounterValue,
			labels, constLabels,
			settings.Filters,
		),
		xidlimit: newBuiltinTypedDesc(
			descOpts{"postgres", "xacts", "left_before_wraparound", "The number of transactions left before force shutdown due to XID wraparound.", 0},
			prometheus.CounterValue,
			[]string{"xid_from"}, constLabels,
			settings.Filters,
		),
	}, nil
}

// Update method collects statistics, parse it and produces metrics that are sent to Prometheus.
func (c *postgresDatabasesCollector) Update(config Config, ch chan<- prometheus.Metric) error {
	conn, err := store.New(config.ConnString)
	if err != nil {
		return err
	}
	defer conn.Close()

	res, err := conn.Query(databaseQuery)
	if err != nil {
		return err
	}

	stats := parsePostgresDatabasesStats(res, c.labelNames)

	res, err = conn.Query(xidLimitQuery)
	if err != nil {
		return err
	}

	xidStats := parsePostgresXidLimitStats(res)

	for _, stat := range stats {
		ch <- c.commits.newConstMetric(stat.xactcommit, stat.database)
		ch <- c.rollbacks.newConstMetric(stat.xactrollback, stat.database)
		ch <- c.conflicts.newConstMetric(stat.conflicts, stat.database)
		ch <- c.deadlocks.newConstMetric(stat.deadlocks, stat.database)
		ch <- c.blocks.newConstMetric(stat.blksread, stat.database, "read")
		ch <- c.blocks.newConstMetric(stat.blkshit, stat.database, "hit")
		ch <- c.tuples.newConstMetric(stat.tupreturned, stat.database, "returned")
		ch <- c.tuples.newConstMetric(stat.tupfetched, stat.database, "fetched")
		ch <- c.tuples.newConstMetric(stat.tupinserted, stat.database, "inserted")
		ch <- c.tuples.newConstMetric(stat.tupupdated, stat.database, "updated")
		ch <- c.tuples.newConstMetric(stat.tupdeleted, stat.database, "deleted")

		ch <- c.tempbytes.newConstMetric(stat.tempbytes, stat.database)
		ch <- c.tempfiles.newConstMetric(stat.tempfiles, stat.database)
		ch <- c.blockstime.newConstMetric(stat.blkreadtime, stat.database, "read")
		ch <- c.blockstime.newConstMetric(stat.blkwritetime, stat.database, "write")
		ch <- c.sizes.newConstMetric(stat.sizebytes, stat.database)
		ch <- c.statsage.newConstMetric(stat.statsage, stat.database)
	}

	ch <- c.xidlimit.newConstMetric(xidStats.database, "pg_database")
	ch <- c.xidlimit.newConstMetric(xidStats.prepared, "pg_prepared_xacts")
	ch <- c.xidlimit.newConstMetric(xidStats.replSlot, "pg_replication_slots")

	return nil
}

// postgresDatabaseStat represents per-database stats based on pg_stat_database.
type postgresDatabaseStat struct {
	database     string
	xactcommit   float64
	xactrollback float64
	blksread     float64
	blkshit      float64
	tupreturned  float64
	tupfetched   float64
	tupinserted  float64
	tupupdated   float64
	tupdeleted   float64
	conflicts    float64
	tempfiles    float64
	tempbytes    float64
	deadlocks    float64
	blkreadtime  float64
	blkwritetime float64
	sizebytes    float64
	statsage     float64
}

// parsePostgresDatabasesStats parses PGResult, extract data and return struct with stats values.
func parsePostgresDatabasesStats(r *model.PGResult, labelNames []string) map[string]postgresDatabaseStat {
	log.Debug("parse postgres database stats")

	var stats = make(map[string]postgresDatabaseStat)

	// process row by row
	for _, row := range r.Rows {
		stat := postgresDatabaseStat{}

		// collect label values
		for i, colname := range r.Colnames {
			switch string(colname.Name) {
			case "database":
				stat.database = row[i].String
			}
		}

		// Define a map key as a database name.
		databaseFQName := stat.database

		// Put stats with labels (but with no data values yet) into stats store.
		stats[databaseFQName] = stat

		// fetch data values from columns
		for i, colname := range r.Colnames {
			// skip columns if its value used as a label
			if stringsContains(labelNames, string(colname.Name)) {
				continue
			}

			// Skip empty (NULL) values.
			if !row[i].Valid {
				continue
			}

			// Get data value and convert it to float64 used by Prometheus.
			v, err := strconv.ParseFloat(row[i].String, 64)
			if err != nil {
				log.Errorf("invalid input, parse '%s' failed: %s; skip", row[i].String, err)
				continue
			}

			// Run column-specific logic
			switch string(colname.Name) {
			case "xact_commit":
				s := stats[databaseFQName]
				s.xactcommit = v
				stats[databaseFQName] = s
			case "xact_rollback":
				s := stats[databaseFQName]
				s.xactrollback = v
				stats[databaseFQName] = s
			case "blks_read":
				s := stats[databaseFQName]
				s.blksread = v
				stats[databaseFQName] = s
			case "blks_hit":
				s := stats[databaseFQName]
				s.blkshit = v
				stats[databaseFQName] = s
			case "tup_returned":
				s := stats[databaseFQName]
				s.tupreturned = v
				stats[databaseFQName] = s
			case "tup_fetched":
				s := stats[databaseFQName]
				s.tupfetched = v
				stats[databaseFQName] = s
			case "tup_inserted":
				s := stats[databaseFQName]
				s.tupinserted = v
				stats[databaseFQName] = s
			case "tup_updated":
				s := stats[databaseFQName]
				s.tupupdated = v
				stats[databaseFQName] = s
			case "tup_deleted":
				s := stats[databaseFQName]
				s.tupdeleted = v
				stats[databaseFQName] = s
			case "conflicts":
				s := stats[databaseFQName]
				s.conflicts = v
				stats[databaseFQName] = s
			case "temp_files":
				s := stats[databaseFQName]
				s.tempfiles = v
				stats[databaseFQName] = s
			case "temp_bytes":
				s := stats[databaseFQName]
				s.tempbytes = v
				stats[databaseFQName] = s
			case "deadlocks":
				s := stats[databaseFQName]
				s.deadlocks = v
				stats[databaseFQName] = s
			case "blk_read_time":
				s := stats[databaseFQName]
				s.blkreadtime = v
				stats[databaseFQName] = s
			case "blk_write_time":
				s := stats[databaseFQName]
				s.blkwritetime = v
				stats[databaseFQName] = s
			case "size_bytes":
				s := stats[databaseFQName]
				s.sizebytes = v
				stats[databaseFQName] = s
			case "stats_age_seconds":
				s := stats[databaseFQName]
				s.statsage = v
				stats[databaseFQName] = s
			default:
				continue
			}
		}
	}

	return stats
}

// xidLimitStats describes how many XIDs left before force database shutdown due to XID wraparound.
type xidLimitStats struct {
	database float64 // based on pg_database.datfrozenxid and datminmxid
	prepared float64 // based on pg_prepared_xacts.transaction
	replSlot float64 // based on pg_replication_slots.xmin and catalog_xmin
}

// parsePostgresXidLimitStats parses database response and returns xidLimitStats.
func parsePostgresXidLimitStats(r *model.PGResult) xidLimitStats {
	log.Debug("parse postgres xid limit stats")

	var stats xidLimitStats

	// process row by row
	for _, row := range r.Rows {
		// Get data value and convert it to float64 used by Prometheus.
		value, err := strconv.ParseFloat(row[1].String, 64)
		if err != nil {
			log.Errorf("invalid input, parse '%s' failed: %s; skip", row[1].String, err)
			continue
		}

		switch row[0].String {
		case "database":
			stats.database = value
		case "prepared_xacts":
			stats.prepared = value
		case "replication_slots":
			stats.replSlot = value
		}
	}

	return stats
}
