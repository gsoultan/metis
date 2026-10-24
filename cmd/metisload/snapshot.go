package main

// snapshot is the database's size and each table's, at one moment.
type snapshot struct {
	database int64
	tables   map[string]int64
}
