package sqlconnector

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// The connection settings, as the catalogue names them.
const (
	driverSetting         = "driver"
	dsnSetting            = "dsn"
	timeoutSetting        = "statement_timeout_ms"
	maxRowsSetting        = "max_rows"
	maxResultBytesSetting = "max_result_bytes"
)

// The kinds of database a lookup can read, as the catalogue offers them.
const (
	driverPostgres  = "postgres"
	driverMySQL     = "mysql"
	driverSQLServer = "sqlserver"
)

// Defaults and ceilings. A setting above its ceiling is held to it; the
// catalogue's description of each field says where the ceiling is.
const (
	defaultTimeout        = 5 * time.Second
	maxTimeout            = 30 * time.Second
	defaultMaxRows        = 500
	maxMaxRows            = 10_000
	defaultMaxResultBytes = 256 << 10
	maxMaxResultBytes     = 1 << 20
)

var dialects = map[string]dialect{
	driverPostgres:  postgresDialect{},
	driverMySQL:     mysqlDialect{},
	driverSQLServer: sqlServerDialect{},
}

var (
	errUnknownDriver = errors.New("the connection does not say which kind of database it is; choose PostgreSQL, MySQL or SQL Server on the connector's settings")
	errNoDSN         = errors.New("the connection has no connection string; add one on the connector's settings")
)

// connection is a lookup's connector settings, read and bounded.
type connection struct {
	dialect dialect
	driver  string
	dsn     string
	timeout time.Duration
	limits  resultLimits
}

// resultLimits bound what one lookup may bring back.
type resultLimits struct {
	rows  int
	bytes int
}

func connectionFrom(config map[string]any) (connection, error) {
	driver, _ := connectors.TextSetting(config, driverSetting)
	d, known := dialects[strings.ToLower(strings.TrimSpace(driver))]
	if !known {
		return connection{}, errUnknownDriver
	}
	dsn, _ := connectors.TextSetting(config, dsnSetting)
	if strings.TrimSpace(dsn) == "" {
		return connection{}, errNoDSN
	}
	return connection{
		dialect: d,
		driver:  strings.ToLower(strings.TrimSpace(driver)),
		dsn:     dsn,
		timeout: time.Duration(bounded(config, timeoutSetting, int(defaultTimeout.Milliseconds()), int(maxTimeout.Milliseconds()))) * time.Millisecond,
		limits: resultLimits{
			rows:  bounded(config, maxRowsSetting, defaultMaxRows, maxMaxRows),
			bytes: bounded(config, maxResultBytesSetting, defaultMaxResultBytes, maxMaxResultBytes),
		},
	}, nil
}

// poolKey names the pool a connection uses without holding the password in a
// map key: a hash of the driver and the connection string.
func (c connection) poolKey() string {
	sum := sha256.Sum256([]byte(c.driver + "\x00" + c.dsn))
	return hex.EncodeToString(sum[:])
}

// bounded reads a positive whole-number setting, which may arrive as a number
// from the API or as the text a form sends, and holds it to its ceiling. A
// setting that is missing, unreadable or not positive takes the default.
func bounded(config map[string]any, key string, fallback, ceiling int) int {
	var value float64
	switch v := config[key].(type) {
	case float64:
		value = v
	case int:
		value = float64(v)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return fallback
		}
		value = parsed
	default:
		return fallback
	}
	if value <= 0 || math.IsNaN(value) {
		return fallback
	}
	return int(math.Min(value, float64(ceiling)))
}
