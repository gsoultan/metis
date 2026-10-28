package app

import "testing"

// A replica follows the Settings page for what applies live, and keeps what
// it started with for what applies at restart: the page says "after a
// restart", and a worker pool resized under a running process would make that
// untrue.
func TestMergeLiveSettings(t *testing.T) {
	boot := map[string]string{
		"METIS_JOB_WORKERS":                       "10",
		"METIS_RETENTION_FINISHED_INSTANCES_DAYS": "30",
		"NOTIFICATION_SMTP_HOST":                  "old.example.com",
	}
	latest := map[string]string{
		"METIS_JOB_WORKERS":                       "50",
		"METIS_RETENTION_FINISHED_INSTANCES_DAYS": "90",
	}

	merged := mergeLiveSettings(boot, latest)

	if merged["METIS_JOB_WORKERS"] != "10" {
		t.Errorf("a restart setting changed under a running process: %q", merged["METIS_JOB_WORKERS"])
	}
	if merged["METIS_RETENTION_FINISHED_INSTANCES_DAYS"] != "90" {
		t.Errorf("a live setting kept its old value: %q", merged["METIS_RETENTION_FINISHED_INSTANCES_DAYS"])
	}
	if _, kept := merged["NOTIFICATION_SMTP_HOST"]; kept {
		t.Error("a live setting cleared on the page is still read")
	}
}
