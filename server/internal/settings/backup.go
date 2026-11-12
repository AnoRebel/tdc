package settings

/*
Backups an operator schedules and bounds while the server runs.

The passphrase scheduled backups are encrypted with is not among them: it is
configuration (backup.passphrase), because a setting lives in the database
the archive protects.
*/
const (
	// BackupIntervalHours is how often a scheduled backup is taken. Zero, the
	// default, is none; a schedule also needs backup.passphrase configured.
	BackupIntervalHours Key = "backup.interval_hours"
	// BackupKeep is how many scheduled backups are kept. Zero keeps them all.
	// Backups made by hand or uploaded are never deleted by this.
	BackupKeep Key = "backup.keep"
	/*
		BackupMaxBytes bounds an archive being written or uploaded. Zero is no
		limit, as with the other limits: a number nobody chose would stop
		backups working for a reason the operator cannot see.
	*/
	BackupMaxBytes Key = "backup.max_bytes"
)

func init() {
	define(map[Key]definition{
		BackupIntervalHours: {
			fallback:    "0",
			description: "Hours between scheduled backups, or 0 for none. Needs backup.passphrase in the configuration.",
			validate:    nonNegative,
		},
		BackupKeep: {
			fallback:    "7",
			description: "How many scheduled backups to keep, or 0 to keep them all. Backups made by hand are never deleted.",
			validate:    nonNegative,
		},
		BackupMaxBytes: {
			fallback:    "0",
			description: "The largest archive this server will write or accept, in bytes, or 0 for no limit.",
			validate:    nonNegative,
		},
	})
}
