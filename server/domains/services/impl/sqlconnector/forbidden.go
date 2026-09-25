package sqlconnector

// Why a word is refused, said once each.
const (
	changesData    = "changes data"
	changesSchema  = "changes the database's structure"
	changesAccess  = "changes who may do what"
	controlsWork   = "controls transactions or sessions, which the lookup does itself"
	runsOtherCode  = "runs code other than this query"
	reachesOutside = "reads files, or reaches another server"
	holdsLocks     = "locks rows that other work may be waiting for"
	holdsTime      = "spends time on purpose, holding a connection other lookups need"
	administers    = "administers the server"
)

// forbiddenWords are refused wherever they appear outside a string literal —
// as a keyword, a function name, or the whole of a quoted identifier.
//
// Anywhere, not only at the start. SQL Server needs no semicolon between
// statements, so SELECT * FROM t DROP TABLE t is two of them; the first word
// being SELECT proves nothing about the second. And inside a statement that
// really is a SELECT, PostgreSQL allows a data-changing CTE, MySQL a locking
// read, and every server SELECT ... INTO.
//
// A word that can only start a statement of its own on PostgreSQL or MySQL is
// not listed. Those two are held to one statement by the protocol — the
// connector forces it whatever the connection string asks for — so listing
// such a word would stop nothing and would refuse a column that happens to
// share its name: a security in a trading system, a cluster in an inventory.
//
// This is a second line, not the first. On PostgreSQL and MySQL the query runs
// in a read-only transaction the server enforces, which stops a write this list
// never thought of. SQL Server has no such transaction, so there the list — and
// the login's own permissions — are what stand in the way, and the transaction
// every lookup runs in is rolled back rather than committed.
//
// A word that is really somebody's column name is refused too. Quoting it does
// not help, because a function can be called by quoted name; the column has to
// be reached through a view that names it something else.
var forbiddenWords = map[string]string{
	"insert": changesData, "update": changesData, "delete": changesData, "merge": changesData,
	"into": changesData, "writetext": changesData, "updatetext": changesData,

	"create": changesSchema, "alter": changesSchema, "drop": changesSchema, "truncate": changesSchema,

	"grant": changesAccess, "revoke": changesAccess, "deny": changesAccess, "setuser": changesAccess,

	"begin": controlsWork, "commit": controlsWork, "rollback": controlsWork, "save": controlsWork,
	"savepoint": controlsWork, "declare": controlsWork, "deallocate": controlsWork, "use": controlsWork,
	"set_config": controlsWork,

	"exec": runsOtherCode, "execute": runsOtherCode, "call": runsOtherCode,
	"sp_executesql": runsOtherCode, "query_to_xml": runsOtherCode,
	"query_to_xml_and_xmlschema": runsOtherCode, "cursor_to_xml": runsOtherCode,

	"outfile": reachesOutside, "dumpfile": reachesOutside, "load_file": reachesOutside,
	"openrowset": reachesOutside, "opendatasource": reachesOutside, "openquery": reachesOutside,
	"openxml": reachesOutside, "dblink": reachesOutside, "dblink_exec": reachesOutside,
	"dblink_connect": reachesOutside, "dblink_open": reachesOutside, "lo_import": reachesOutside,
	"lo_export": reachesOutside, "lo_unlink": reachesOutside, "pg_read_file": reachesOutside,
	"pg_read_binary_file": reachesOutside, "pg_ls_dir": reachesOutside, "pg_stat_file": reachesOutside,
	"xp_cmdshell": reachesOutside, "xp_dirtree": reachesOutside, "xp_fileexist": reachesOutside,
	"xp_regread": reachesOutside, "sp_oacreate": reachesOutside, "sp_oamethod": reachesOutside,
	"sys_exec": reachesOutside, "sys_eval": reachesOutside,

	"lock": holdsLocks, "updlock": holdsLocks, "xlock": holdsLocks, "holdlock": holdsLocks,
	"tablockx": holdsLocks,

	"waitfor": holdsTime, "pg_sleep": holdsTime, "pg_sleep_for": holdsTime, "pg_sleep_until": holdsTime,
	"sleep": holdsTime, "benchmark": holdsTime,

	"backup": administers, "restore": administers, "shutdown": administers, "kill": administers,
	"dbcc": administers, "reconfigure": administers, "sp_configure": administers,
	"checkpoint": administers, "pg_terminate_backend": administers, "pg_cancel_backend": administers,
	"pg_reload_conf": administers, "pg_rotate_logfile": administers,
}
